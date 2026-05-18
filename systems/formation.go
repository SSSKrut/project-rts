package systems

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// FormationSystem drives every rostered unit to its formation offset around
// the squad center (PHASE-9.md P3). The contract with Phase 7
// UnitMovementSystem stays the same: we keep writing the unit's ActionQueue,
// the low-level controller hasn't been told squads exist.
//
// Phase 11.5 M11.5.3 / M11.5.5: tier-gating dropped; per-squad processing is
// dispatched through WorkerPool.ParallelFor. Each worker writes only to
// members of its own squad - disjoint sets across workers, so no shared
// writes. Stragglers (when ejection is enabled) drop into leaveBuffer for a
// serial post-pass. M11.5.6 / P8: leash 4 -> 8 and auto-ejection gated off.
type FormationSystem struct {
	filter         *ecs.Filter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData]
	posMap         *ecs.Map[components.WorldPos]
	actionQueueMap *ecs.Map[components.ActionQueue]
	pool           *core.WorkerPool
	squadService   *SquadService

	// Phase 14.6 M14.6.1 - slot clamping reads the surface NavGrid to avoid
	// pushing members onto NavInBuilding / blocked cells (units would walk
	// straight into walls otherwise).
	navGridMap    *ecs.Map[components.NavGrid]
	chunkIndexRes ecs.Resource[TerrainChunkIndex]

	// Phase 11.6 M11.6.2: reusable snapshot buffer.
	workBuf []formationWork

	// Phase 11.6 M11.6.4: per-worker buffers for stragglers. Length == pool
	// workers (or 1 for serial). Reused across ticks; reset to len=0 inside
	// Update.
	workerLeaveBufs [][]ecs.Entity
}

// NewFormationSystem wires the system with a worker pool. nil pool falls back
// to serial execution.
func NewFormationSystem(squadService *SquadService, pool *core.WorkerPool) *FormationSystem {
	workers := 1
	if pool != nil {
		if w := pool.Workers(); w > 0 {
			workers = w
		}
	}
	bufs := make([][]ecs.Entity, workers)
	for i := range bufs {
		bufs[i] = make([]ecs.Entity, 0, 4)
	}
	return &FormationSystem{
		squadService:    squadService,
		pool:            pool,
		workBuf:         make([]formationWork, 0, 16),
		workerLeaveBufs: bufs,
	}
}

func (sys *FormationSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.actionQueueMap = ecs.NewMap[components.ActionQueue](w)
	sys.navGridMap = ecs.NewMap[components.NavGrid](w)
	sys.chunkIndexRes = ecs.NewResource[TerrainChunkIndex](w)
}

func (FormationSystem) Name() string { return "formation" }

func (FormationSystem) LODPolicy() core.LODPolicy {
	// Phase 11.5 P1: universal sim - single 100 ms interval. The old
	// Relevant/Dormant fallbacks are gone since members no longer carry LOD
	// markers; FormationSystem touches every alive squad each cycle.
	return core.LODPolicy{
		ActiveEvery:   100 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// CohesionLeashCoeff × Spacing = max distance member-to-center before the
// member would fall out of the roster.
//
// Phase 11.5 P8: bumped 4 -> 8. For Line Spacing=2 m this widens the leash
// from 8 m to 16 m so a member that briefly snags on a tree / chunk seam
// doesn't get ejected by the next FormationSystem tick. The auto-ejection
// branch is also gated off (see cohesionEjectionEnabled) - stragglers are
// only flagged for future Tactical AI.
const CohesionLeashCoeff float32 = 8.0

// cohesionEjectionEnabled toggles whether stragglers are removed from the
// squad. Phase 11.5 keeps the detection code but turns the action off
// (squadService.Leave is the destructive call). Phase 15 (Tactical AI) will
// turn it back on per-doctrine (Patrol = strict, Assault = loose).
const cohesionEjectionEnabled = false

// formationPushTolerance - minimum target shift that re-pushes the unit's
// MoveTo. Below this, the unit keeps its existing queue. Without the gate the
// formation re-writes ActionQueue every 100 ms, the unit's
// arrival-pop / new-push cycle oscillates around the destination, and
// rotating Forward sweeps targets sideways. Slightly larger than
// UnitMovementSystem.arrivalRadius (0.6) so we don't re-issue on the cusp.
const formationPushTolerance float32 = 0.7

// formationForwardLockDist - once the center is within this many metres of
// the macro target, freeze Forward instead of recomputing it from
// (target - center). Avoids the unit-vector swing near arrival that twisted
// offsets into circular motion in the original Phase 9 build.
const formationForwardLockDist float32 = 5.0

// formationWork - snapshot row for the parallel per-squad pass. Pointers are
// stable between snapshot and ParallelFor since neither branch changes
// archetype for snapshotted entities.
type formationWork struct {
	roster *components.CommandRoster
	mp     *components.MacroPath
	fd     *components.FormationData
}

func (sys *FormationSystem) Update(ctx core.UpdateContext) {
	// Snapshot squads. Workers write per-squad -> per-member ActionQueue (each
	// member belongs to exactly one squad, so the write set is disjoint
	// across workers - no shared writes).
	sys.workBuf = sys.workBuf[:0]
	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd := q.Get()
		sys.workBuf = append(sys.workBuf, formationWork{roster: roster, mp: mp, fd: fd})
	}
	work := sys.workBuf

	// Reset per-worker straggler buffers. Phase 11.6 M11.6.4: each worker
	// gets its own buffer, then we drain all of them serially after the
	// parallel pass. With cohesionEjectionEnabled = false the buffers stay
	// empty, but the pattern is in place for when Phase 15's doctrine layer
	// flips the flag.
	for i := range sys.workerLeaveBufs {
		sys.workerLeaveBufs[i] = sys.workerLeaveBufs[i][:0]
	}

	world := ctx.World
	sys.pool.ParallelForIndexed(len(work), func(chunkIdx, start, end int) {
		// Bound the index - defensive in case ParallelForIndexed ever splits
		// into more chunks than the buffer slice (shouldn't happen with our
		// constructor, but cheap to be safe).
		if chunkIdx >= len(sys.workerLeaveBufs) {
			chunkIdx = len(sys.workerLeaveBufs) - 1
		}
		buf := &sys.workerLeaveBufs[chunkIdx]
		for i := start; i < end; i++ {
			sys.processSquad(world, work[i], buf)
		}
	})

	for i := range sys.workerLeaveBufs {
		for _, e := range sys.workerLeaveBufs[i] {
			sys.squadService.Leave(e)
		}
	}
}

// processSquad handles one squad's formation pass. Note: leaveBuffer writes
// are a no-op while cohesionEjectionEnabled = false, so the parallel pass
// has no contention on it; if Phase 15 turns ejection back on we'll need a
// per-worker buffer and a serial merge.
func (sys *FormationSystem) processSquad(world *ecs.World, w formationWork, leaveBuffer *[]ecs.Entity) {
	roster := w.roster
	mp := w.mp
	fd := w.fd
	if roster.Count == 0 {
		return
	}

	center, ok := SquadCenter(world, roster, sys.posMap)
	if !ok {
		return
	}

	// Pop reached waypoints - SquadMacroPathSystem runs only every 1 s,
	// FormationSystem at 100 ms is the responsive pace for head advance.
	for mp.Head < mp.Count {
		d := center.Sub(mp.Waypoints[mp.Head])
		if d.X*d.X+d.Z*d.Z < SquadWaypointReached*SquadWaypointReached {
			mp.Head++
		} else {
			break
		}
	}

	// Resolve the center's current macro target.
	var centerTarget components.WorldPos
	haveTarget := false
	if mp.HasGoal {
		if mp.Head < mp.Count {
			centerTarget = mp.Waypoints[mp.Head]
		} else {
			centerTarget = mp.Goal
		}
		haveTarget = true
	}

	// Update Forward toward the macro target - but only while the squad
	// is still far enough out that the unit vector (target - center) /
	// mag is geometrically stable. Once we're inside formationForwardLockDist
	// (or Forward was never set), the existing Forward stays.
	forwardZero := fd.Forward.X == 0 && fd.Forward.Z == 0
	if haveTarget {
		diff := centerTarget.Sub(center)
		mag := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
		if mag > 0.05 && (forwardZero || mag > formationForwardLockDist) {
			fd.Forward = rl.Vector3{X: diff.X / mag, Y: 0, Z: diff.Z / mag}
		}
	}

	leash := CohesionLeashCoeff * fd.Spacing
	leashSq := leash * leash

	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		mPos := sys.posMap.Get(mem)
		if mPos == nil {
			continue
		}

		if haveTarget {
			d := mPos.Sub(center)
			distSq := d.X*d.X + d.Z*d.Z
			if distSq > leashSq && cohesionEjectionEnabled {
				*leaveBuffer = append(*leaveBuffer, mem)
				continue
			}
		}

		if !haveTarget {
			continue
		}
		aq := sys.actionQueueMap.Get(mem)
		if aq == nil {
			continue
		}
		offX, offZ := FormationOffset(fd.Type, i, fd.Spacing, fd.Forward)
		target := centerTarget.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
		// Phase 14.6 M14.6.1 - keep slot targets out of building interiors and
		// blocked cells when approaching from outside. Once the squad center
		// has crossed into a footprint (commander entered through a door),
		// let inside slots stand - UnitMovement.reflectAgainstWalls keeps
		// trailing members away from walls while they funnel through the
		// open door behind the commander.
		if !sys.cellInsideBuilding(centerTarget) {
			target = sys.clampSlotXZ(target)
		}

		// Re-push only if the new target meaningfully differs from the
		// last queued MoveTo. Cuts the per-tick pop-push cycle at the
		// destination and stops the rotating-offset chase that some
		// formations could fall into.
		if aq.Count > 0 {
			lastIdx := (int(aq.Tail) + components.ActionQueueSize - 1) % components.ActionQueueSize
			last := aq.Actions[lastIdx]
			if last.Kind == components.ActionMoveTo {
				d := last.Target.Sub(target)
				if d.X*d.X+d.Z*d.Z < formationPushTolerance*formationPushTolerance {
					continue
				}
			}
		}
		ClearActions(aq)
		PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: target})
	}
}

// FormationOffset returns the XZ offset of `slot` from the squad center for
// each formation kind. Slot 0 always sits on center (commander). Output is in
// world-space metres. Phase 13.6 M13.6.1: exported so ghost-preview code in
// main.go can reuse the same slot layout that FormationSystem writes.
func FormationOffset(kind components.FormationKind, slot uint8, spacing float32, forward rl.Vector3) (float32, float32) {
	if slot == 0 {
		return 0, 0
	}
	// Right = rotate Forward 90° clockwise around +Y. Pre-normalised by
	// SquadMacroPathSystem / FormationSystem (mag check); falls back to a
	// sensible default if Forward is degenerate.
	fx, fz := forward.X, forward.Z
	if fx*fx+fz*fz < 1e-4 {
		fx, fz = 0, 1
	}
	rx, rz := fz, -fx
	k := int(slot)

	switch kind {
	case components.FormationLine:
		// Slot 1=+1, 2=-1, 3=+2, 4=-2 ... ranks fan outward from commander.
		side := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		d := side * sign * spacing
		return rx * d, rz * d

	case components.FormationColumn:
		// Slot k sits k*spacing behind the commander along -Forward.
		d := -float32(k) * spacing
		return fx * d, fz * d

	case components.FormationWedge:
		// Pairs fan back-left / back-right at 45°. Row index = (k+1)/2,
		// so 1-2 are the first row behind, 3-4 the second, etc.
		row := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		ox := (-fx*row + rx*row*sign) * spacing
		oz := (-fz*row + rz*row*sign) * spacing
		return ox, oz

	case components.FormationLoose:
		// Deterministic radial scatter - hash only on slot index so the
		// pattern doesn't shimmer when members swap squads (P-note in
		// PHASE-9.md "Что НЕ делать в formationOffset").
		h := slotHash32(uint32(slot))
		ang := float64(h&0xFFFF) / float64(0x10000) * 2 * math.Pi
		radF := float32((h>>16)&0xFFFF) / float32(0x10000)
		// Slot 1+ pushed outward at least 0.5×(2×spacing) so members don't
		// pile on the commander.
		rad := (0.5 + 0.5*radF) * 2 * spacing
		cos := float32(math.Cos(ang))
		sin := float32(math.Sin(ang))
		return rad * cos, rad * sin
	}
	return 0, 0
}

// clampSlotXZ moves `target` to the nearest walkable surface cell when it
// lands inside a building footprint or on a blocked cell. Phase 14.6 M14.6.1:
// commander macro path already routes through doors via NavService, but each
// member's individual slot is a raw offset from the squad center - without
// this clamp a side-slot whose XZ falls inside the building would make the
// member walk straight at the wall.
//
// Algorithm: a small 8-direction spiral, max radius 3 m at 1 m steps. Cheap
// (24 cell lookups worst-case) and runs at most once per member per
// FormationSystem tick (100 ms cadence). When no walkable cell exists in the
// radius (e.g. the slot fell into an unloaded chunk), return the original
// target - the member will walk toward it and either UnitMovement's
// wall-reflection (M14.6.1 step 4) catches the wall, or the next tick's
// recompute moves the slot.
func (sys *FormationSystem) clampSlotXZ(target components.WorldPos) components.WorldPos {
	if sys.cellWalkableAt(target) {
		return target
	}
	// 8 compass directions, evaluated at radius 1 / 2 / 3 metres.
	dirs := [8][2]float32{
		{1, 0}, {0, 1}, {-1, 0}, {0, -1},
		{1, 1}, {-1, 1}, {-1, -1}, {1, -1},
	}
	for r := float32(1); r <= 3; r++ {
		for i := range dirs {
			dx, dz := dirs[i][0]*r, dirs[i][1]*r
			candidate := target.Add(rl.Vector3{X: dx, Y: 0, Z: dz})
			if sys.cellWalkableAt(candidate) {
				return candidate
			}
		}
	}
	return target
}

// cellInsideBuilding returns true when the surface NavGrid cell covering
// `pos` has the NavInBuilding bit set. Used by processSquad to gate the
// outside-of-building slot clamp.
func (sys *FormationSystem) cellInsideBuilding(pos components.WorldPos) bool {
	if sys.navGridMap == nil {
		return false
	}
	idx := sys.chunkIndexRes.Get()
	if idx == nil {
		return false
	}
	worldX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	worldZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	cc := components.ChunkCoord{
		X: int32(math.Floor(float64(worldX / components.ChunkSize))),
		Z: int32(math.Floor(float64(worldZ / components.ChunkSize))),
	}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return false
	}
	grid := sys.navGridMap.Get(ent)
	if grid == nil {
		return false
	}
	lx := worldX - float32(cc.X)*components.ChunkSize
	lz := worldZ - float32(cc.Z)*components.ChunkSize
	ci := int(math.Floor(float64(lx)))
	cj := int(math.Floor(float64(lz)))
	if ci < 0 || ci >= components.NavGridSide || cj < 0 || cj >= components.NavGridSide {
		return false
	}
	return grid.Cells[cj*components.NavGridSide+ci].Flags&components.NavInBuilding != 0
}

// cellWalkableAt returns true when the surface NavGrid cell covering `pos`
// is loaded, has Cost > 0, and is not flagged NavInBuilding. False otherwise
// (unloaded chunk, blocked cell, or interior-of-building cell).
func (sys *FormationSystem) cellWalkableAt(pos components.WorldPos) bool {
	if sys.navGridMap == nil {
		return true // pre-init, can't validate - assume walkable.
	}
	idx := sys.chunkIndexRes.Get()
	if idx == nil {
		return true
	}
	worldX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	worldZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	cc := components.ChunkCoord{
		X: int32(math.Floor(float64(worldX / components.ChunkSize))),
		Z: int32(math.Floor(float64(worldZ / components.ChunkSize))),
	}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return true
	}
	grid := sys.navGridMap.Get(ent)
	if grid == nil {
		return true
	}
	lx := worldX - float32(cc.X)*components.ChunkSize
	lz := worldZ - float32(cc.Z)*components.ChunkSize
	ci := int(math.Floor(float64(lx)))
	cj := int(math.Floor(float64(lz)))
	if ci < 0 || ci >= components.NavGridSide || cj < 0 || cj >= components.NavGridSide {
		return true
	}
	cell := grid.Cells[cj*components.NavGridSide+ci]
	if cell.Cost == 0 {
		return false
	}
	if cell.Flags&components.NavInBuilding != 0 {
		return false
	}
	return true
}

// slotHash32 - SplitMix-style 32-bit finalizer used by FormationLoose.
func slotHash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}
