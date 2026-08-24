package systems

import (
	"fmt"
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
	// Slot-Y resample (#13): a flank slot metres from the centre needs the
	// height under ITS OWN XZ, not the centre's. Stateless — parallel-safe.
	sampler *HeightSampler
	// Read the squad's head order kind to skip the outside-walkable clamp
	// when the player wants the squad INSIDE (Garrison / OccupyBuilding /
	// ClearBuilding); otherwise the clamp pushes inside slots back outside
	// and units pile up against the wall instead of entering through the door.
	orderQueueMap  *ecs.Map[components.OrderQueueHead]
	orderKindMap   *ecs.Map[components.OrderKind]
	orderTargetMap *ecs.Map[components.OrderTarget]
	levelMap       *ecs.Map[components.Level]
	// Building orders resolve per-member slots through the shared planner
	// (windows / rooms / hidden / ground-floor per order kind) — the same
	// call the ghost preview draws, so execution matches the promise.
	slotPlanner        *BuildingSlotPlanner
	orderFacingMap     *ecs.Map[components.OrderParamFacing]
	orderEngagementMap *ecs.Map[components.OrderParamEngagementOverride]
	motionMap          *ecs.Map[components.Motion]
	vehicleMap         *ecs.Map[components.Vehicle]
	aircraftMap        *ecs.Map[components.Aircraft]
	colliderMap        *ecs.Map[components.Collider]

	// Members carrying a TacticalOverride are AI-driven (e.g. SurvivalInstinct
	// moving them to cover). FormationSystem reads but does not write their
	// ActionQueue while the marker is held.
	tacticalOverrideMap *ecs.Map[components.TacticalOverride]
	// A vehicle running a reflex (VehicleOverride.Kind != None) owns its own
	// locomotion — the same contract TacticalOverride gives infantry.
	vehicleOverrideMap *ecs.Map[components.VehicleOverride]
	// Members with IndividualPosition override their slot goal with the
	// player-placed target. TacticalOverride still wins over this.
	individualPosMap *ecs.Map[components.IndividualPosition]

	orientMap      *ecs.Map[components.FormationOrientation]
	customSlotsMap *ecs.Map[components.FormationCustomSlots]
	// SquadBrain's plan changes HOW the slots are driven: bounding waves,
	// a paused march during a relocation, the ClearSeq phase targets.
	planMap *ecs.Map[components.SquadPlan]

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
	sys.vehicleOverrideMap = ecs.NewMap[components.VehicleOverride](w)
	sys.individualPosMap = ecs.NewMap[components.IndividualPosition](w)
	sys.microPathMap = ecs.NewMap[components.MicroPath](w)
	sys.orientMap = ecs.NewMap[components.FormationOrientation](w)
	sys.customSlotsMap = ecs.NewMap[components.FormationCustomSlots](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderTargetMap = ecs.NewMap[components.OrderTarget](w)
	sys.levelMap = ecs.NewMap[components.Level](w)
	sys.slotPlanner = NewBuildingSlotPlanner(w)
	sys.orderFacingMap = ecs.NewMap[components.OrderParamFacing](w)
	sys.orderEngagementMap = ecs.NewMap[components.OrderParamEngagementOverride](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.vehicleMap = ecs.NewMap[components.Vehicle](w)
	sys.aircraftMap = ecs.NewMap[components.Aircraft](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.planMap = ecs.NewMap[components.SquadPlan](w)
	sys.sampler = NewHeightSampler(w)
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

// Retarget tolerance for leader-wake targets: at or above the 1 m nav
// quantization step, so a re-quantized leader path can't thrash the head
// target (MA1 P7-a).
const wakeRetargetTolerance float32 = 1.1

// formationForwardLockDist — once the center is within this many metres of
// the macro target, freeze Forward instead of recomputing it from
// (target - center). Avoids unit-vector swing near arrival twisting offsets
// into circular motion.
const formationForwardLockDist float32 = 5.0

// Forward slew (#12): the march heading rotates toward the desired direction
// at most this many rad/s, with a deadband below which it doesn't move at
// all. Unlimited per-tick recompute from (target - live centroid) let
// centroid noise wobble Forward, slots orbited, members chased them sideways.
// The deadband must eat replan noise: every 1 s a fresh path from the
// (ORCA-wiggling) leader re-quantizes on the 1 m nav grid, swinging the
// first-segment direction by up to ~12deg. Real course changes exceed it.
const (
	formationTurnRate    float32 = 1.2
	formationYawDeadband float32 = 0.26
)

// formationWork — snapshot row for the parallel per-squad pass. Pointers are
// stable between snapshot and ParallelFor since neither branch changes
// archetype for snapshotted entities.
type formationWork struct {
	squad  ecs.Entity
	roster *components.CommandRoster
	mp     *components.MacroPath
	fd     *components.FormationData
	plan   *components.SquadPlan
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
			plan:   sys.planMap.Get(q.Entity()),
		})
	}
	work := sys.workBuf

	// Reset per-worker straggler buffers; drained serially after the
	// parallel pass.
	for i := range sys.workerLeaveBufs {
		sys.workerLeaveBufs[i] = sys.workerLeaveBufs[i][:0]
	}

	world := ctx.World
	dt := float32(ctx.Delta.Seconds())
	sys.pool.ParallelForIndexed(len(work), func(chunkIdx, start, end int) {
		if chunkIdx >= len(sys.workerLeaveBufs) {
			chunkIdx = len(sys.workerLeaveBufs) - 1
		}
		buf := &sys.workerLeaveBufs[chunkIdx]
		for i := start; i < end; i++ {
			sys.processSquad(world, work[i], dt, buf)
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
func (sys *FormationSystem) processSquad(world *ecs.World, w formationWork, dt float32, leaveBuffer *[]ecs.Entity) {
	roster := w.roster
	mp := w.mp
	fd := w.fd
	if roster.Count == 0 {
		return
	}

	center, ok := SquadAnchorPos(world, roster, sys.posMap)
	if !ok {
		return
	}

	// Hull floor on spacing: any writer (editor kind reset, F1-F4) can stomp
	// Spacing back to infantry scale, and 3 m-radius hulls on 2 m slots fight
	// the collision resolve forever.
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) || !sys.vehicleMap.Has(mem) {
			continue
		}
		if col := sys.colliderMap.Get(mem); col != nil {
			if need := col.Radius*2 + 1; need > fd.Spacing {
				fd.Spacing = need
			}
		}
	}

	// The leader does NOT wait for stragglers — lagging members regain their
	// slots at raised Pace instead (bb.CatchUp). Spread stats stay for HUD.
	_, caughtUp, totalLive := SquadSpread(world, roster, center, sys.posMap, fd.Spacing)
	mp.WaitingForStragglers = false
	mp.StragglerCaughtUp = caughtUp
	mp.StragglerTotal = totalLive

	// SquadMacroPathSystem runs every 1 s, FormationSystem at 100 ms is the
	// responsive pace for head advance. Reach widens for a vehicle anchor —
	// see SquadWaypointReach (dead-ring livelock).
	reach := SquadWaypointReach(world, roster, sys.vehicleMap, sys.aircraftMap)
	for mp.Head < mp.Count {
		d := center.Sub(mp.Waypoints[mp.Head])
		if d.X*d.X+d.Z*d.Z < reach*reach {
			mp.Head++
		} else {
			break
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

	// Bounding: the moving wave runs to the brain's bound instead of the macro
	// waypoint; the holding wave stays put and covers it.
	plan := w.plan
	if plan != nil && plan.Mode == components.SquadPlanBounding && mp.HasGoal {
		centerTarget = plan.Anchor
		haveTarget = true
	}

	// Player layout edit (editor drag / kind / preset): a marching squad
	// picks the new slots up through the normal pass, so the flag only has to
	// survive until everyone is actually back in place — dropping it on sight
	// of a macro goal strands whoever is still out of position when the order
	// completes a moment later (a hull released from a Flee reflex). An IDLE
	// squad re-forms in place: hold slot targets until every driven member
	// parks, then release.
	reformHold := false
	reformDone := true
	if fd.ReformPending && !haveTarget {
		centerTarget = center
		haveTarget = true
		reformHold = true
	}

	// Interior intent: when the squad's head order is Garrison /
	// OccupyBuilding / ClearBuilding, the slot clamp must be disabled even
	// while the squad is still outside the building — otherwise units stop
	// at outside-the-wall slots instead of pathing through the door.
	interiorIntent := false
	var interiorBuilding ecs.Entity
	interiorPolicy := SlotRooms
	interiorHasFacing := false
	var interiorFacingYaw float32
	if head := sys.orderQueueMap.Get(w.squad); head != nil && head.First != (ecs.Entity{}) {
		if kind := sys.orderKindMap.Get(head.First); kind != nil {
			switch kind.Code {
			case components.OrderKindGarrison,
				components.OrderKindOccupyBuilding,
				components.OrderKindClearBuilding:
				interiorIntent = true
				if tgt := sys.orderTargetMap.Get(head.First); tgt != nil {
					interiorBuilding = tgt.Entity
				}
				switch kind.Code {
				case components.OrderKindGarrison:
					interiorPolicy = SlotWindows
				case components.OrderKindClearBuilding:
					interiorPolicy = SlotGroundFloor
				default:
					// "Hidden position" = OccupyBuilding + HoldFire override.
					if sys.orderEngagementMap.Has(head.First) {
						interiorPolicy = SlotHidden
					}
				}
				if f := sys.orderFacingMap.Get(head.First); f != nil {
					interiorHasFacing = true
					interiorFacingYaw = f.YawRad
				}
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

	// Anchor the in-place reform so the LEADER'S OWN SLOT lands on the
	// leader's current position — anchoring on the leader itself makes the
	// formation chase its own reference point and drift (slot 0 is
	// draggable in vehicle squads).
	if reformHold {
		fw := fd.Forward
		if o := sys.orientMap.Get(w.squad); o != nil && o.Mode == components.OrientNorth {
			fw = rl.Vector3{X: 0, Y: 0, Z: 1}
		}
		var offX0, offZ0 float32
		if cs := sys.customSlotsMap.Get(w.squad); cs != nil {
			offX0, offZ0 = customSlotWorld(cs.Slots[0], fw)
		} else {
			offX0, offZ0 = FormationOffset(fd.Type, 0, fd.Spacing, fw)
		}
		centerTarget = centerTarget.Add(rl.Vector3{X: -offX0, Y: 0, Z: -offZ0})
	}

	// Per-member building slots via the shared planner — the same call the
	// ghost preview draws. Empty when the target isn't a building root
	// (Occupy L<n> targets a Level) or its chunk isn't streamed in yet.
	var interiorSlots []BuildingSlot
	if interiorBuilding != (ecs.Entity{}) {
		if plan != nil && plan.Mode == components.SquadPlanClearSeq {
			interiorSlots = sys.clearSeqSlots(interiorBuilding, plan, center, int(roster.Count))
		} else {
			interiorSlots = sys.slotPlanner.PlanSlots(interiorBuilding,
				interiorPolicy, int(roster.Count), interiorHasFacing, interiorFacingYaw)
		}
	}
	// Who takes which position is a matching problem, not the roster order:
	// index-order hands the far window to the far man and crosses every path
	// in the doorway, and one death compacts the roster so everybody swaps.
	slotOf := sys.matchInteriorSlots(world, roster, interiorSlots)

	// Desired march direction: the current macro SEGMENT when one exists
	// (stable between replans), else (target - center) — geometrically noisy,
	// so it stays locked inside formationForwardLockDist. Forward then SLEWS
	// toward the desired direction instead of snapping (#12).
	forwardZero := fd.Forward.X == 0 && fd.Forward.Z == 0
	if haveTarget {
		var dirX, dirZ float32
		haveDir := false
		if mp.Head < mp.Count && mp.Count > 1 {
			// Pure path geometry — never a live position (leader wiggle
			// through the deadband re-created the churn), over a two-segment
			// baseline: single 4 m decimated segments quantize direction in
			// ~14deg grid steps and alternate between replans.
			lo := int(mp.Head) - 1
			if lo < 0 {
				lo = 0
			}
			hi := lo + 2
			if last := int(mp.Count) - 1; hi > last {
				hi = last
			}
			if hi > lo {
				d := mp.Waypoints[hi].Sub(mp.Waypoints[lo])
				if magSq := d.X*d.X + d.Z*d.Z; magSq > 0.0025 {
					inv := 1 / float32(math.Sqrt(float64(magSq)))
					dirX, dirZ = d.X*inv, d.Z*inv
					haveDir = true
				}
			}
		}
		if !haveDir {
			diff := centerTarget.Sub(center)
			mag := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
			if mag > 0.05 && (forwardZero || mag > formationForwardLockDist) {
				dirX, dirZ = diff.X/mag, diff.Z/mag
				haveDir = true
			}
		}
		switch {
		case haveDir && forwardZero:
			fd.Forward = rl.Vector3{X: dirX, Y: 0, Z: dirZ}
		case haveDir:
			cur := float32(math.Atan2(float64(fd.Forward.X), float64(fd.Forward.Z)))
			want := float32(math.Atan2(float64(dirX), float64(dirZ)))
			delta := wrapAngle(want - cur)
			if debugLog && (delta > formationYawDeadband || delta < -formationYawDeadband) {
				fmt.Printf("[fwd] head=%d/%d cur=%.0f want=%.0f\n",
					mp.Head, mp.Count, cur*180/math.Pi, want*180/math.Pi)
			}
			if delta > formationYawDeadband || delta < -formationYawDeadband {
				maxStep := formationTurnRate * dt
				if delta > maxStep {
					delta = maxStep
				} else if delta < -maxStep {
					delta = -maxStep
				}
				cur += delta
				fd.Forward = rl.Vector3{
					X: float32(math.Sin(float64(cur))),
					Y: 0,
					Z: float32(math.Cos(float64(cur))),
				}
			}
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
		// marker is held; a reflexing hull owns its own driver the same way
		// (P8-f: a squadded truck's Flee used to be overwritten by the slot
		// push within 100 ms, so it never actually left).
		if sys.tacticalOverrideMap.Has(mem) {
			continue
		}
		if ov := sys.vehicleOverrideMap.Get(mem); ov != nil &&
			ov.Kind != components.VehicleReflexNone {
			continue
		}
		// The brain owns who moves: a relocating squad is clearing a beaten
		// zone under its own overrides, and the covering wave of a bound holds
		// its ground until the phase flips.
		if plan != nil && sys.individualPosMap.Get(mem) == nil {
			switch plan.Mode {
			case components.SquadPlanRelocate:
				continue
			case components.SquadPlanBounding:
				if !plan.InMovingWave(i) {
					if aq := sys.actionQueueMap.Get(mem); aq != nil && aq.Count > 0 {
						ClearActions(aq)
					}
					continue
				}
			}
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
		wakeTarget := false
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
			const interiorSpreadSpacing float32 = 1.2
			if si := slotIndexAt(slotOf, i); si != noSlot && int(si) < len(interiorSlots) {
				slot := interiorSlots[si]
				target = slot.Pos
				// Parked at a window slot → face the opening. UnitMovement
				// owns yaw while walking; once idle nothing else writes it.
				if slot.HasYaw {
					d := mPos.Sub(slot.Pos)
					if d.X*d.X+d.Z*d.Z < SlotParkRadius*SlotParkRadius {
						if m := sys.motionMap.Get(mem); m != nil && m.Speed < 0.5 {
							m.Yaw = slot.Yaw
						}
					}
				}
			} else if mp.HasGoal {
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
			// Leader-wake bias: when the leader has a MicroPath in flight,
			// slot i>0 anchors its target i metres along the leader's path
			// polyline FROM THE LEADER'S LIVE POSITION (arc-length lerp).
			// Pulls trailing members into a column-like file through
			// corridors; lateral offset still spreads them out in the open.
			// The anchor advances CONTINUOUSLY with the leader — the old
			// discrete waypoint pick jumped ~1 m on every leader pop and the
			// retarget+replan storm was the march jitter source (MA1 P7-a).
			// The walk is a pure path-bending transport: subtract it back
			// along forward so a straight march keeps the slot grid at TRUE
			// spacing — leaving it in compressed a column's interval to
			// Spacing−1 and shoved a line's flanks a rank ahead (the MA2
			// column-pack regression once the stale-aim lag stopped hiding it).
			target = centerTarget.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
			if i > 0 {
				leader := roster.Members[0]
				if leader != (ecs.Entity{}) && world.Alive(leader) {
					if leaderMP := sys.microPathMap.Get(leader); leaderMP != nil && leaderMP.Count > leaderMP.Head {
						if lp := sys.posMap.Get(leader); lp != nil {
							fwx, fwz := forward.X, forward.Z
							if fwx*fwx+fwz*fwz < 1e-4 {
								fwx, fwz = 0, 1
							}
							anchor, walked := wakeAnchor(*lp, leaderMP, float32(i))
							target = anchor.Add(rl.Vector3{
								X: offX - fwx*walked, Y: 0, Z: offZ - fwz*walked})
							wakeTarget = true
						}
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
			// Slot Y = live surface under the slot's own XZ (#13): the
			// centre's Y is metres off for flank slots on a cross-slope.
			wx := float32(target.Chunk.X)*components.ChunkSize + target.Local.X
			wz := float32(target.Chunk.Z)*components.ChunkSize + target.Local.Z
			target.Local.Y = sys.sampler.Sample(wx, wz)
		}

		if fd.ReformPending && ip == nil {
			d := mPos.Sub(target)
			tol := float32(1.5)
			if veh := sys.vehicleMap.Get(mem); veh != nil {
				tol = rampPopRadius(vehArrivalRadius, components.SpecForVehicle(veh.Kind)) + 0.5
			}
			if d.X*d.X+d.Z*d.Z > tol*tol {
				reformDone = false
			}
		}

		// Mirror the per-unit slot target into the blackboard so
		// UtilityEvaluator's DistToSlot signal sees the same goal. CatchUp
		// hysteresis: engage beyond 2xSpacing+2, relax within Spacing+1.
		// The leader (slot 0) sets the squad's pace and never catch-ups.
		if bb := sys.blackboardMap.Get(mem); bb != nil {
			bb.GoalSlot = target
			if i > 0 && ip == nil {
				d := mPos.Sub(target)
				lagSq := d.X*d.X + d.Z*d.Z
				if bb.CatchUp {
					relax := fd.Spacing + 1
					bb.CatchUp = lagSq > relax*relax
				} else {
					engage := 2*fd.Spacing + 2
					bb.CatchUp = lagSq > engage*engage
				}
			} else {
				bb.CatchUp = false
			}
		}

		// Retarget the existing MoveTo head in place instead of
		// ClearActions+PushAction. No forced MicroPath.Dirty here:
		// MicroPathSystem's own goal-drift check decides whether the path
		// is actually invalidated — a wake target advancing along the march
		// is an extension, not a new route, and forcing a replan on every
		// retarget was the storm (43.7 churn replans/unit/min on a straight
		// march). Wake targets also take a coarser retarget tolerance ≥ the
		// nav quantization step so leader wiggle can't thrash the head.
		if aq.Count > 0 {
			head := &aq.Actions[aq.Head]
			if head.Kind == components.ActionMoveTo {
				tol := formationPushTolerance
				if wakeTarget {
					tol = wakeRetargetTolerance
				}
				d := head.Target.Sub(target)
				if d.X*d.X+d.Z*d.Z < tol*tol {
					continue
				}
				head.Target = target
				continue
			}
		}
		// IDLE squad only: parked on an empty queue is a valid state —
		// re-push only when the unit actually drifted off its slot, else
		// arrival pops the MoveTo and this pass re-pushes it forever (idle
		// 10 Hz churn). Marching squads keep the unconditional push: early
		// leader-wake targets sit within park tolerance of the followers and
		// the gate would hold them at the start line until catch-up sprints.
		// Y-band keeps the recovery path for storey slots: a walker stranded
		// a floor below its slot must be re-pushed so MicroPath replans the
		// stairs.
		if !mp.HasGoal {
			dPark := mPos.Sub(target)
			parkTol := SlotParkRadius
			if veh := sys.vehicleMap.Get(mem); veh != nil {
				parkTol = rampPopRadius(vehArrivalRadius, components.SpecForVehicle(veh.Kind)) + 0.5
			}
			if dPark.X*dPark.X+dPark.Z*dPark.Z <= parkTol*parkTol &&
				dPark.Y > -arrivalYBand && dPark.Y < arrivalYBand {
				continue
			}
		}
		ClearActions(aq)
		PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: target})
		if mp := sys.microPathMap.Get(mem); mp != nil {
			mp.Dirty = true
		}
	}

	if fd.ReformPending && reformDone {
		fd.ReformPending = false
	}
}

// matchInteriorSlots decides who takes which planned position. A man under a
// TacticalOverride is not a candidate — his slot goes to someone who can
// actually walk to it, and the empty window gets manned instead of waiting
// for him. His stored HeldSlot is left alone so he prefers it back on return.
func (sys *FormationSystem) matchInteriorSlots(world *ecs.World,
	roster *components.CommandRoster, slots []BuildingSlot) []uint8 {

	if len(slots) == 0 {
		return nil
	}
	n := int(roster.Count)
	cands := make([]SlotCandidate, n)
	for i := 0; i < n; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) || sys.tacticalOverrideMap.Has(mem) {
			continue
		}
		p := sys.posMap.Get(mem)
		if p == nil {
			continue
		}
		c := SlotCandidate{Pos: *p, Live: true}
		if bb := sys.blackboardMap.Get(mem); bb != nil {
			c.Held = bb.HeldSlot
		}
		cands[i] = c
	}
	out := assignSlots(cands, slots, make([]uint8, 0, n))
	for i := 0; i < n; i++ {
		if !cands[i].Live {
			continue
		}
		bb := sys.blackboardMap.Get(roster.Members[i])
		if bb == nil {
			continue
		}
		if out[i] == noSlot {
			bb.ClearHeldSlot()
		} else {
			bb.SetHeldSlot(out[i])
		}
	}
	return out
}

// slotIndexAt reads an assignment safely; a nil table means "no interior
// slots this pass".
func slotIndexAt(a []uint8, i uint8) uint8 {
	if int(i) < len(a) {
		return a[i]
	}
	return noSlot
}

// wakeAnchor returns the point `dist` metres along the leader's remaining
// path polyline, measured from the leader's LIVE position (arc-length lerp).
// Clamps to the last waypoint when the path is shorter. Continuous in the
// leader's motion — no ±1 m jump when the leader pops a waypoint.
// wakeAnchor also reports the distance actually walked: near the path's end
// the walk clamps, and the caller must subtract the CLAMPED distance — the
// requested one stretched the parked file to Spacing+1 intervals and the
// column's centroid never entered the MoveTo completion radius.
func wakeAnchor(leaderPos components.WorldPos, mp *components.MicroPath, dist float32) (components.WorldPos, float32) {
	prev := leaderPos
	walked := float32(0)
	for k := mp.Head; k < mp.Count; k++ {
		wp := mp.Waypoints[k]
		d := wp.Sub(prev)
		segLen := float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
		if dist <= segLen {
			if segLen < 1e-4 {
				return prev, walked
			}
			t := dist / segLen
			return prev.Add(rl.Vector3{X: d.X * t, Y: d.Y * t, Z: d.Z * t}), walked + dist
		}
		dist -= segLen
		walked += segLen
		prev = wp
	}
	return prev, walked
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

// clearSeqSlots maps a ClearSeq phase onto per-member goals: the stack file
// outside the door, the point pair through it, then the storey under sweep.
func (sys *FormationSystem) clearSeqSlots(building ecs.Entity, plan *components.SquadPlan,
	center components.WorldPos, count int) []BuildingSlot {

	switch plan.Phase {
	case components.ClearPhaseStackUp:
		return sys.slotPlanner.PlanStackUp(building, center, count)
	case components.ClearPhaseEnter:
		stack := sys.slotPlanner.PlanStackUp(building, center, count)
		entry := sys.slotPlanner.PlanFloorSlots(building, 0, count)
		if len(stack) == 0 {
			return entry
		}
		for i := 0; i < clearEntryPair && i < len(stack) && i < len(entry); i++ {
			stack[i] = entry[i]
		}
		return stack
	}
	return sys.slotPlanner.PlanFloorSlots(building, plan.Floor, count)
}

// How many men go through the door first.
const clearEntryPair = 2
