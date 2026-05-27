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

	// Phase 17 M17.A.3 - leader-wake bias and Dirty-flag retarget. Formation
	// writes the squad's slot targets into ActionQueue.Head.Target and flips
	// MicroPath.Dirty whenever the goal shifts; MicroPathSystem picks that up
	// and replans through NavService.
	microPathMap *ecs.Map[components.MicroPath]
	// Phase 17.8 M17.8.4 — mirror per-unit slot target into the unit's
	// LocalBlackboard.GoalSlot so UtilityEvaluator can read DistToSlot
	// without duplicating the formation-offset math.
	blackboardMap *ecs.Map[components.LocalBlackboard]
	// Phase 17.8 M17.8.6 follow-up — read squad's head order kind to skip
	// the outside-walkable clamp when the player wants the squad INSIDE
	// (Garrison / OccupyBuilding / ClearBuilding). Without this gate the
	// clamp pushes every inside-building slot back to outside surface, so
	// units pile up against the wall instead of entering through the door.
	orderQueueMap *ecs.Map[components.OrderQueueHead]
	orderKindMap  *ecs.Map[components.OrderKind]

	// Phase 15 M15.A.0 - members carrying a TacticalOverride are AI-driven
	// (e.g. SurvivalInstinct moving them to cover). FormationSystem reads but
	// does not write their ActionQueue while the marker is held.
	tacticalOverrideMap *ecs.Map[components.TacticalOverride]
	// Phase 15 M15.B.0 - members with IndividualPosition override their slot
	// goal with the player-placed target (Absolute world pos or Relative XZ
	// offset against squad center). TacticalOverride still wins over this.
	individualPosMap *ecs.Map[components.IndividualPosition]

	// Phase 18 formation editor: orientation lock (north / movement) and
	// arbitrary per-slot offsets in squad-local frame.
	orientMap      *ecs.Map[components.FormationOrientation]
	customSlotsMap *ecs.Map[components.FormationCustomSlots]

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
	sys.tacticalOverrideMap = ecs.NewMap[components.TacticalOverride](w)
	sys.individualPosMap = ecs.NewMap[components.IndividualPosition](w)
	sys.microPathMap = ecs.NewMap[components.MicroPath](w)
	sys.orientMap = ecs.NewMap[components.FormationOrientation](w)
	sys.customSlotsMap = ecs.NewMap[components.FormationCustomSlots](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
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

// CohesionLeashCoeff x Spacing = max distance member-to-center before the
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
	squad  ecs.Entity
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
		sys.workBuf = append(sys.workBuf, formationWork{
			squad:  q.Entity(),
			roster: roster,
			mp:     mp,
			fd:     fd,
		})
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

	// Phase 15 M15.A.5 - measure roster spread. When the max distance from
	// any member to the center exceeds 2*Spacing the squad pauses waypoint
	// advance so the commander doesn't outrun stragglers. The gate clears
	// itself once everyone is within Spacing again.
	stragglerThreshold := 2.0 * fd.Spacing
	spread, caughtUp, totalLive := SquadSpread(world, roster, center, sys.posMap, fd.Spacing)
	waiting := spread > stragglerThreshold
	mp.WaitingForStragglers = waiting
	mp.StragglerCaughtUp = caughtUp
	mp.StragglerTotal = totalLive

	// Pop reached waypoints - SquadMacroPathSystem runs only every 1 s,
	// FormationSystem at 100 ms is the responsive pace for head advance.
	// Skipping the loop while waiting keeps the commander parked at the
	// current waypoint until the squad regroups.
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

	// Phase 17.8 M17.8.6 follow-up — interior intent detection. When the
	// squad's head order kind is Garrison / OccupyBuilding / ClearBuilding
	// the slot clamp must be disabled, even while the squad is still
	// outside the building. clampSlotXZ rejects NavInBuilding cells as
	// "not walkable" and pushes them to the nearest outside surface;
	// without this gate units stop at outside-the-wall slots instead of
	// pathing through the door.
	interiorIntent := false
	if head := sys.orderQueueMap.Get(w.squad); head != nil && head.First != (ecs.Entity{}) {
		if kind := sys.orderKindMap.Get(head.First); kind != nil {
			switch kind.Code {
			case components.OrderKindGarrison,
				components.OrderKindOccupyBuilding,
				components.OrderKindClearBuilding:
				interiorIntent = true
			}
		}
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
		// Phase 15 M15.A.0 - skip AI-driven members. SurvivalInstinct owns
		// their ActionQueue while the override marker is held.
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

		// Phase 15 M15.B.0 - IndividualPosition runs even when the squad has
		// no macro target (player parked a unit behind cover while the squad
		// is idle - the unit still moves to its placed position). Slot-driven
		// members need haveTarget; override-driven members don't.
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
			// Phase 17.8 M17.8.6 follow-up — interior intent: each member
			// targets a slightly-spread point around mp.Goal (firstFloorPos)
			// in a Loose formation. Pure-collapse to mp.Goal would
			// bottleneck all 8 units through the door at the same XZ →
			// ORCA queues + corner sliding slows them to ~0 m/s. A small
			// loose offset (1.2 m spacing) lets pathfinder give each unit
			// a different route (spreading the bottleneck), and once
			// inside the units naturally spread by the same offset.
			if mp.HasGoal {
				const interiorSpreadSpacing float32 = 1.2
				offX, offZ := FormationOffset(components.FormationLoose, i, interiorSpreadSpacing, rl.Vector3{X: 0, Y: 0, Z: 1})
				target = mp.Goal.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
			} else {
				target = centerTarget
			}
		} else {
			// Phase 18 orientation lock: OrientNorth overrides the live
			// motion-derived forward with the world +Z axis so the
			// formation keeps its compass alignment regardless of march
			// direction. OrientMovement (default) uses the running forward
			// we computed above.
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
			// Phase 17 M17.A.3 leader-wake bias: when the squad's leader has a
			// MicroPath in flight, slot i > 0 anchors its target on a waypoint
			// from the leader's queue (k = min(i, remaining-1)) instead of the
			// raw squad-center projection. Pulls trailing members into a
			// column-like file when the leader is threading a corridor; in
			// open ground the lateral offset still spreads them out because
			// the same FormationOffset is applied.
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
		// Phase 14.6 M14.6.1 / Phase 15 M15.B.4 / Phase 17.8 M17.8.6 fix —
		// keep slot targets out of blocked cells when approaching from
		// outside a building, UNLESS the player explicitly wants the
		// squad inside (Garrison / OccupyBuilding / ClearBuilding).
		// Without the interiorIntent gate, the clamp pushed inside-
		// building slots back to outside surface — squad arrived at
		// outside cells and never entered. NavService routes through
		// doors via TransitionEdge, so the raw inside slot is correct;
		// MicroPath handles per-unit routing.
		if !interiorIntent && ip == nil && !sys.cellInsideBuilding(centerTarget) {
			if clamped, ok := sys.clampSlotXZ(target); ok {
				target = clamped
			} else {
				target = center
			}
		}

		// Phase 17.8 M17.8.4 — mirror the final per-unit slot target into
		// the blackboard so UtilityEvaluator's DistToSlot signal sees the
		// same goal MicroPath is pathing toward. Cheap (no extra math).
		if bb := sys.blackboardMap.Get(mem); bb != nil {
			bb.GoalSlot = target
		}

		// Phase 17 M17.A.2 - retarget the existing MoveTo head in place
		// instead of ClearActions + PushAction. Flip MicroPath.Dirty when the
		// goal shifts > formationPushTolerance so MicroPathSystem replans.
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
// Y=along forward) into world XZ given the current forward direction.
// Mirrors the right/forward basis used by FormationOffset so editor-set
// slots end up in the same coordinate frame as kind-based ones.
func customSlotWorld(slot rl.Vector2, forward rl.Vector3) (float32, float32) {
	fx, fz := forward.X, forward.Z
	if fx*fx+fz*fz < 1e-4 {
		fx, fz = 0, 1
	}
	rx, rz := fz, -fx
	return slot.X*rx + slot.Y*fx, slot.X*rz + slot.Y*fz
}

// FormationOffset returns the XZ offset of `slot` from the squad center for
// each formation kind. Slot 0 always sits on center (commander). Output is in
// world-space metres. Phase 13.6 M13.6.1: exported so ghost-preview code in
// main.go can reuse the same slot layout that FormationSystem writes.
func FormationOffset(kind components.FormationKind, slot uint8, spacing float32, forward rl.Vector3) (float32, float32) {
	if slot == 0 {
		return 0, 0
	}
	// Right = rotate Forward 90 deg clockwise around +Y. Pre-normalised by
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
		// Pairs fan back-left / back-right at 45 deg. Row index = (k+1)/2,
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
		// Slot 1+ pushed outward at least 0.5x(2xspacing) so members don't
		// pile on the commander.
		rad := (0.5 + 0.5*radF) * 2 * spacing
		cos := float32(math.Cos(ang))
		sin := float32(math.Sin(ang))
		return rad * cos, rad * sin
	}
	return 0, 0
}

// clampSlotXZ moves `target` to the nearest walkable surface cell when it
// lands inside a building footprint, on a river strip, or on any other blocked
// cell. Phase 14.6 M14.6.1: commander macro path already routes through doors
// via NavService, but each member's individual slot is a raw offset from the
// squad center - without this clamp a side-slot whose XZ falls inside the
// building would make the member walk straight at the wall.
//
// Phase 15 M15.B.4 - search radius widened from 3 m to 6 m and returns
// ok=false when no walkable cell exists in range. Callers use that to drop
// the unit back to the squad center rather than pushing it toward an
// unreachable cell.
//
// Algorithm: 8-direction sweep at r = 1..6 m. 48 cell lookups worst-case;
// each lookup is O(1) into the NavGrid, so the whole walk costs nothing
// even at 100 ms cadence across every member.
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
