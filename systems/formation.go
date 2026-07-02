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
// the squad center. Writes the unit's ActionQueue; the low-level controller
// is unaware of squads.
//
// Parallelism: per-squad processing dispatches through ParallelFor. Each
// worker writes only to its own squad's members — disjoint sets, so no
// shared writes. Stragglers (when ejection is enabled) drop into leaveBuffer
// for a serial post-pass.
type FormationSystem struct {
	filter         *ecs.Filter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData]
	posMap         *ecs.Map[components.WorldPos]
	actionQueueMap *ecs.Map[components.ActionQueue]
	pool           *core.WorkerPool
	squadService   *SquadService

	// Slot clamping reads the surface NavGrid to avoid pushing members onto
	// NavInBuilding / blocked cells (units would walk into walls otherwise).
	navGridMap    *ecs.Map[components.NavGrid]
	chunkIndexRes ecs.Resource[TerrainChunkIndex]

	// Formation writes slot targets into ActionQueue.Head.Target and flips
	// MicroPath.Dirty when the goal shifts; MicroPathSystem then replans.
	microPathMap  *ecs.Map[components.MicroPath]
	blackboardMap *ecs.Map[components.LocalBlackboard]
	// Read the squad's head order kind to skip the outside-walkable clamp
	// when the player wants the squad INSIDE (Garrison / OccupyBuilding /
	// ClearBuilding); otherwise the clamp pushes inside slots back outside
	// and units pile up against the wall instead of entering through the door.
	orderQueueMap  *ecs.Map[components.OrderQueueHead]
	orderKindMap   *ecs.Map[components.OrderKind]
	orderTargetMap *ecs.Map[components.OrderTarget]
	levelMap       *ecs.Map[components.Level]

	// Members carrying a TacticalOverride are AI-driven (e.g. SurvivalInstinct
	// moving them to cover). FormationSystem reads but does not write their
	// ActionQueue while the marker is held.
	tacticalOverrideMap *ecs.Map[components.TacticalOverride]
	// Members with IndividualPosition override their slot goal with the
	// player-placed target. TacticalOverride still wins over this.
	individualPosMap *ecs.Map[components.IndividualPosition]

	orientMap      *ecs.Map[components.FormationOrientation]
	customSlotsMap *ecs.Map[components.FormationCustomSlots]

	workBuf []formationWork

	// Per-worker buffers for stragglers. Length == pool workers (or 1 for
	// serial). Reused across ticks; reset to len=0 inside Update.
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
	sys.tacticalOverrideMap = ecs.NewMap[components.TacticalOverride](w)
	sys.individualPosMap = ecs.NewMap[components.IndividualPosition](w)
	sys.microPathMap = ecs.NewMap[components.MicroPath](w)
	sys.orientMap = ecs.NewMap[components.FormationOrientation](w)
	sys.customSlotsMap = ecs.NewMap[components.FormationCustomSlots](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderTargetMap = ecs.NewMap[components.OrderTarget](w)
	sys.levelMap = ecs.NewMap[components.Level](w)
}

func (FormationSystem) Name() string { return "formation" }

func (FormationSystem) LODPolicy() core.LODPolicy {
	// Universal sim — single 100 ms interval; members no longer carry LOD
	// markers, FormationSystem touches every alive squad each cycle.
	return core.LODPolicy{
		ActiveEvery:   100 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// CohesionLeashCoeff × Spacing = max distance member-to-center before the
// member would fall out of the roster.
const CohesionLeashCoeff float32 = 8.0

// cohesionEjectionEnabled toggles whether stragglers are removed from the
// squad. Detection code stays on; the destructive Leave call is gated for
// future per-doctrine tuning.
const cohesionEjectionEnabled = false

// formationPushTolerance — minimum target shift that re-pushes the unit's
// MoveTo. Without this gate, formation re-writes ActionQueue every 100 ms and
// the arrival-pop / new-push cycle oscillates around the destination.
// Slightly larger than UnitMovementSystem.arrivalRadius (0.6).
const formationPushTolerance float32 = 0.7

// formationForwardLockDist — once the center is within this many metres of
// the macro target, freeze Forward instead of recomputing it from
// (target - center). Avoids unit-vector swing near arrival twisting offsets
// into circular motion.
const formationForwardLockDist float32 = 5.0

// formationWork — snapshot row for the parallel per-squad pass. Pointers are
// stable between snapshot and ParallelFor since neither branch changes
// archetype for snapshotted entities.
type formationWork struct {
	squad  ecs.Entity
	roster *components.CommandRoster
	mp     *components.MacroPath
	fd     *components.FormationData
}

func (sys *FormationSystem) Update(ctx core.UpdateContext) {
	// Each member belongs to exactly one squad, so workers' write sets are
	// disjoint — no shared writes on the parallel pass.
	sys.workBuf = sys.workBuf[:0]
	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd := q.Get()
		sys.workBuf = append(sys.workBuf, formationWork{
			squad:  q.Entity(),
			roster: roster,
			mp:     mp,
			fd:     fd,
		})
	}
	work := sys.workBuf

	// Reset per-worker straggler buffers; drained serially after the
	// parallel pass.
	for i := range sys.workerLeaveBufs {
		sys.workerLeaveBufs[i] = sys.workerLeaveBufs[i][:0]
	}

	world := ctx.World
	sys.pool.ParallelForIndexed(len(work), func(chunkIdx, start, end int) {
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

// processSquad handles one squad's formation pass. leaveBuffer writes are a
// no-op while cohesionEjectionEnabled = false.
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

	// Pause waypoint advance when the squad spreads beyond 2x Spacing so the
	// commander doesn't outrun stragglers. Self-clears once everyone is back
	// within Spacing.
	stragglerThreshold := 2.0 * fd.Spacing
	spread, caughtUp, totalLive := SquadSpread(world, roster, center, sys.posMap, fd.Spacing)
	waiting := spread > stragglerThreshold
	mp.WaitingForStragglers = waiting
	mp.StragglerCaughtUp = caughtUp
	mp.StragglerTotal = totalLive

	// SquadMacroPathSystem runs every 1 s, FormationSystem at 100 ms is the
	// responsive pace for head advance.
	if !waiting {
		for mp.Head < mp.Count {
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z < SquadWaypointReached*SquadWaypointReached {
				mp.Head++
			} else {
				break
			}
		}
	}

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

	// Interior intent: when the squad's head order is Garrison /
	// OccupyBuilding / ClearBuilding, the slot clamp must be disabled even
	// while the squad is still outside the building — otherwise units stop
	// at outside-the-wall slots instead of pathing through the door.
	interiorIntent := false
	if head := sys.orderQueueMap.Get(w.squad); head != nil && head.First != (ecs.Entity{}) {
		if kind := sys.orderKindMap.Get(head.First); kind != nil {
			switch kind.Code {
			case components.OrderKindGarrison,
				components.OrderKindOccupyBuilding,
				components.OrderKindClearBuilding:
				interiorIntent = true
			case components.OrderKindMoveTo:
				// "Occupy L<n>" from the building popup is a MoveTo whose
				// OrderTarget.Entity is a Level. Interior volumes are narrower
				// than line/wedge spreads — without the compact spread the
				// edge slots land beyond the walls and drag members out
				// through doors into neighbouring wings.
				if tgt := sys.orderTargetMap.Get(head.First); tgt != nil &&
					tgt.Entity != (ecs.Entity{}) && sys.levelMap.Has(tgt.Entity) {
					interiorIntent = true
				}
			}
		}
	}

	// Only recompute Forward when (target - center)/mag is geometrically
	// stable; inside formationForwardLockDist the existing Forward sticks.
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
		// SurvivalInstinct owns the unit's ActionQueue while the override
		// marker is held.
		if sys.tacticalOverrideMap.Has(mem) {
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

		// IndividualPosition runs even without a squad macro target — slot-
		// driven members need haveTarget, override-driven ones don't.
		ip := sys.individualPosMap.Get(mem)

		if !haveTarget && ip == nil {
			continue
		}
		aq := sys.actionQueueMap.Get(mem)
		if aq == nil {
			continue
		}

		var target components.WorldPos
		if ip != nil {
			switch ip.Mode {
			case components.IndividualPosAbsolute:
				target = ip.AbsolutePos
			case components.IndividualPosRelative:
				baseCenter := centerTarget
				if !haveTarget {
					baseCenter = center
				}
				target = baseCenter.Add(rl.Vector3{X: ip.RelativeOffset.X, Y: 0, Z: ip.RelativeOffset.Y})
			}
		} else if interiorIntent {
			// Spread each member around mp.Goal in a Loose formation —
			// pure collapse to a single XZ would bottleneck all units
			// through the door (ORCA queues + corner sliding slow to
			// ~0 m/s); 1.2 m offsets let the planner route them through
			// different cells and they stay spread out inside.
			if mp.HasGoal {
				const interiorSpreadSpacing float32 = 1.2
				offX, offZ := FormationOffset(components.FormationLoose, i, interiorSpreadSpacing, rl.Vector3{X: 0, Y: 0, Z: 1})
				target = mp.Goal.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
			} else {
				target = centerTarget
			}
		} else {
			// OrientNorth overrides the motion-derived forward with world +Z
			// so the formation keeps compass alignment regardless of march
			// direction.
			forward := fd.Forward
			if o := sys.orientMap.Get(w.squad); o != nil && o.Mode == components.OrientNorth {
				forward = rl.Vector3{X: 0, Y: 0, Z: 1}
			}
			var offX, offZ float32
			if cs := sys.customSlotsMap.Get(w.squad); cs != nil {
				offX, offZ = customSlotWorld(cs.Slots[i], forward)
			} else {
				offX, offZ = FormationOffset(fd.Type, i, fd.Spacing, forward)
			}
			// Leader-wake bias: when the leader has a MicroPath in flight, slot
			// i>0 anchors its target on waypoint k = min(i, remaining-1) from
			// the leader's queue. Pulls trailing members into a column-like
			// file through corridors; lateral offset still spreads them out
			// in the open.
			target = centerTarget.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
			if i > 0 {
				leader := roster.Members[0]
				if leader != (ecs.Entity{}) && world.Alive(leader) {
					if leaderMP := sys.microPathMap.Get(leader); leaderMP != nil && leaderMP.Count > leaderMP.Head {
						remaining := int(leaderMP.Count - leaderMP.Head)
						k := int(i)
						if k > remaining-1 {
							k = remaining - 1
						}
						wp := leaderMP.Waypoints[int(leaderMP.Head)+k]
						target = wp.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
					}
				}
			}
		}
		// Keep slot targets out of blocked cells when approaching from
		// outside a building, UNLESS the player explicitly wants the squad
		// inside. NavService routes through doors via TransitionEdge, so
		// the raw inside slot is correct; MicroPath handles per-unit routing.
		if !interiorIntent && ip == nil && !sys.cellInsideBuilding(centerTarget) {
			if clamped, ok := sys.clampSlotXZ(target); ok {
				target = clamped
			} else {
				target = center
			}
		}

		// Mirror the per-unit slot target into the blackboard so
		// UtilityEvaluator's DistToSlot signal sees the same goal.
		if bb := sys.blackboardMap.Get(mem); bb != nil {
			bb.GoalSlot = target
		}

		// Retarget the existing MoveTo head in place instead of
		// ClearActions+PushAction; flip MicroPath.Dirty on goal shift so
		// MicroPathSystem replans.
		if aq.Count > 0 {
			head := &aq.Actions[aq.Head]
			if head.Kind == components.ActionMoveTo {
				d := head.Target.Sub(target)
				if d.X*d.X+d.Z*d.Z < formationPushTolerance*formationPushTolerance {
					continue
				}
				head.Target = target
				if mp := sys.microPathMap.Get(mem); mp != nil {
					mp.Dirty = true
				}
				continue
			}
		}
		ClearActions(aq)
		PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: target})
		if mp := sys.microPathMap.Get(mem); mp != nil {
			mp.Dirty = true
		}
	}
}

// customSlotWorld projects a squad-local slot offset (X=right of forward,
// Y=along forward) into world XZ using the same right/forward basis as
// FormationOffset.
func customSlotWorld(slot rl.Vector2, forward rl.Vector3) (float32, float32) {
	fx, fz := forward.X, forward.Z
	if fx*fx+fz*fz < 1e-4 {
		fx, fz = 0, 1
	}
	rx, rz := fz, -fx
	return slot.X*rx + slot.Y*fx, slot.X*rz + slot.Y*fz
}

// FormationOffset returns the XZ offset of `slot` from the squad center for
// each formation kind. Slot 0 always sits on center (commander). Exported so
// ghost-preview code can reuse the same slot layout FormationSystem writes.
func FormationOffset(kind components.FormationKind, slot uint8, spacing float32, forward rl.Vector3) (float32, float32) {
	if slot == 0 {
		return 0, 0
	}
	// Right = rotate Forward 90° clockwise around +Y. Falls back to sensible
	// default if Forward is degenerate.
	fx, fz := forward.X, forward.Z
	if fx*fx+fz*fz < 1e-4 {
		fx, fz = 0, 1
	}
	rx, rz := fz, -fx
	k := int(slot)

	switch kind {
	case components.FormationLine:
		// Slots 1=+1, 2=-1, 3=+2, 4=-2 … ranks fan outward from commander.
		side := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		d := side * sign * spacing
		return rx * d, rz * d

	case components.FormationColumn:
		d := -float32(k) * spacing
		return fx * d, fz * d

	case components.FormationWedge:
		// Pairs fan back-left / back-right at 45°; row index = (k+1)/2.
		row := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		ox := (-fx*row + rx*row*sign) * spacing
		oz := (-fz*row + rz*row*sign) * spacing
		return ox, oz

	case components.FormationLoose:
		// Deterministic radial scatter — hash only on slot index so the
		// pattern doesn't shimmer when members swap squads.
		h := slotHash32(uint32(slot))
		ang := float64(h&0xFFFF) / float64(0x10000) * 2 * math.Pi
		radF := float32((h>>16)&0xFFFF) / float32(0x10000)
		// Push out by at least 0.5×(2×spacing) so members don't pile on the
		// commander.
		rad := (0.5 + 0.5*radF) * 2 * spacing
		cos := float32(math.Cos(ang))
		sin := float32(math.Sin(ang))
		return rad * cos, rad * sin
	}
	return 0, 0
}

// clampSlotXZ moves `target` to the nearest walkable surface cell when it
// lands inside a building footprint or another blocked cell. ok=false when
// no walkable cell exists in range (caller falls back to squad center).
//
// 8-direction sweep at r = 1..6 m; 48 lookups worst-case, each O(1).
func (sys *FormationSystem) clampSlotXZ(target components.WorldPos) (components.WorldPos, bool) {
	if sys.cellWalkableAt(target) {
		return target, true
	}
	dirs := [8][2]float32{
		{1, 0}, {0, 1}, {-1, 0}, {0, -1},
		{1, 1}, {-1, 1}, {-1, -1}, {1, -1},
	}
	for r := float32(1); r <= 6; r++ {
		for i := range dirs {
			dx, dz := dirs[i][0]*r, dirs[i][1]*r
			candidate := target.Add(rl.Vector3{X: dx, Y: 0, Z: dz})
			if sys.cellWalkableAt(candidate) {
				return candidate, true
			}
		}
	}
	return target, false
}

// cellInsideBuilding returns true when the surface NavGrid cell covering
// `pos` has the NavInBuilding bit set.
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
// is loaded, has Cost > 0, and is not flagged NavInBuilding.
func (sys *FormationSystem) cellWalkableAt(pos components.WorldPos) bool {
	if sys.navGridMap == nil {
		return true // pre-init: assume walkable.
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

// slotHash32 is a SplitMix-style 32-bit finalizer.
func slotHash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}
