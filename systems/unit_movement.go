package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 14.5 M14.5.1 - unitMaxSpeed table folded into components.StanceSpecs.
// Readers use components.SpecForStance(code).MaxSpeed.

// Separation parameters. Phase 7 P4: only separation force, no alignment /
// cohesion. Phase 9 layers formation logic on top via the same system.
const (
	separationRadius float32 = 1.5
	separationWeight float32 = 4.0
)

// Phase 15 M15.B.5 - living-movement polish constants.
//
// stanceAccel - per-stance acceleration cap (m/s^2). Stand 4, Crouch 2, Prone
// 1: same shape as MaxSpeed table, prone units take longer to spin up.
// Applied symmetrically to speed-up and brake.
//
// maxYawRate - turn cap (rad/s) ~ 170 deg/s, realistic for infantry. Cap
// applies to the wrapped delta between current and desired yaw.
var stanceAccel = [...]float32{
	components.StanceStand:  4.0,
	components.StanceCrouch: 2.0,
	components.StanceProne:  1.0,
}

const maxYawRate float32 = 3.0

// perUnitSpeedSpread - half-width of the per-unit personality SpeedMul. With
// 0.05 the multiplier sits in [0.95, 1.05] - members run at slightly
// different paces so a squad doesn't lock-step.
const perUnitSpeedSpread float32 = 0.05

// arrivalRadius - how close the unit needs to be to its current MoveTo
// target before the action pops from the queue. Slightly bigger than the
// cell-centre dance to avoid jittering at the goal.
const arrivalRadius float32 = 0.6

// stopDuration - how long an ActionStop holds the unit in place before it
// pops off the queue. Phase 7 P7.
const stopDuration float32 = 0.1

// staminaRegenThreshold - Current/MaxLevel ratio at which the
// StaminaExhausted marker is cleared. PHASE-13.md P3 sets this at 0.30 so
// the unit must actually rest, not just touch zero.
const staminaRegenThreshold float32 = 0.30

// UnitMovementSystem advances units along their ActionQueue.
//
// Phase 11.5 M11.5.3 / M11.5.5: tier-gating removed and the per-unit step is
// dispatched through WorkerPool.ParallelFor. snapshot -> parallel step -> no
// post-pass (each worker writes only to its own unit's component pointers,
// no shared map mutation, no archetype changes).
//
// Phase 11.6 M11.6.2: snapshot buffers live on the struct and are reset to
// len=0 each Update instead of allocated fresh. After the first Update the
// underlying capacity is stable, so steady-state runs no longer hit the
// allocator on this hot path.
//
// Phase 13 M13.3: per-unit step now reads the effective MovementProfile
// (OrderParamMovementProfile on the squad's active order > squad's standing
// MovementProfile > system default), drains/regenerates Stamina, and
// auto-transitions stance toward the squad's standing default.
// StaminaExhausted marker add/remove uses per-worker buffers + serial
// post-pass to keep archetype mutations off the parallel critical path.
//
// The per-unit body of `step()` lives in unit_movement_step.go; wall
// collision helpers (colWall, makeColWall, reflectAgainstWalls) live in
// unit_movement_walls.go.
type UnitMovementSystem struct {
	unitFilter *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.MicroPath]
	pool       *core.WorkerPool
	// Phase 14.5 M14.5.2: separation pass reads from the shared SpatialHash
	// resource (rebuilt by SpatialHashRebuildSystem each tick before this
	// system runs). Replaces the per-tick O(N^2) neighbour walk.
	spatialHash ecs.Resource[core.SpatialHash]
	// Phase 14.5: world handle for stale-entity alive-check in the
	// separation callback.
	world *ecs.World

	// Phase 13 handles.
	memberMap                *ecs.Map[components.SquadMember]
	staminaMap               *ecs.Map[components.Stamina]
	staminaExhaustedMap      *ecs.Map[components.StaminaExhausted]
	orderQueueMap            *ecs.Map[components.OrderQueueHead]
	orderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	movementProfileMap       *ecs.Map[components.MovementProfile]
	posMap                   *ecs.Map[components.WorldPos]
	// Phase 17 M17.B.4 - read for combat-move facing decoupling.
	threatMap *ecs.Map[components.Threat]
	// Phase 17.8 M17.8.5 — ORCA local avoidance reads neighbour velocity
	// + collider radius for the agent constraint.
	motionMap   *ecs.Map[components.Motion]
	colliderMap *ecs.Map[components.Collider]
	// Phase 17.8 M17.8.6 — replan-trigger counters live on the
	// LocalBlackboard. step() accumulates Overcrowded / Stuck dt and
	// flips MicroPath.Dirty when either crosses threshold.
	blackboardMap *ecs.Map[components.LocalBlackboard]

	// Phase 14.6 M14.6.1 - wall reflection. Walls snapshot bucketed by chunk
	// once per tick in the serial pre-pass; step() reads the 3x3 chunk
	// window around the unit to reflect velocity that would cross a wall
	// this frame.
	wallFilter     *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap        *ecs.Map[components.Door]
	collisionWalls map[components.ChunkCoord][]colWall

	// Reusable snapshot buffer. Filled in the serial pre-pass, read-only by
	// workers during ParallelFor - safe because writes are indexed and never
	// concurrent.
	workBuf []unitWork

	// Per-worker buffers for StaminaExhausted marker toggles. Workers can't
	// mutate archetypes concurrently, so they record pending Add / Remove
	// requests in their own buffer; serial post-pass applies them all.
	workerExhaustedAdds    [][]ecs.Entity
	workerExhaustedRemoves [][]ecs.Entity

	elapsed float32
}

// NewUnitMovementSystem wires the system with a worker pool. nil pool falls
// back to serial execution (useful for unit tests).
func NewUnitMovementSystem(pool *core.WorkerPool) *UnitMovementSystem {
	workers := 1
	if pool != nil && pool.Workers() > 0 {
		workers = pool.Workers()
	}
	return &UnitMovementSystem{
		pool:                   pool,
		workBuf:                make([]unitWork, 0, 64),
		workerExhaustedAdds:    make([][]ecs.Entity, workers),
		workerExhaustedRemoves: make([][]ecs.Entity, workers),
		collisionWalls:         make(map[components.ChunkCoord][]colWall, 32),
	}
}

func (sys *UnitMovementSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.MicroPath](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.staminaMap = ecs.NewMap[components.Stamina](w)
	sys.staminaExhaustedMap = ecs.NewMap[components.StaminaExhausted](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderMovementOverrideMap = ecs.NewMap[components.OrderParamMovementProfile](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.spatialHash = ecs.NewResource[core.SpatialHash](w)
	sys.world = w
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
}

func (UnitMovementSystem) Name() string { return "unit_movement" }

func (UnitMovementSystem) LODPolicy() core.LODPolicy {
	// Phase 11.5 P1: universal simulation - every tick, no LOD gating.
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// unitWork - snapshot row for the parallel step pass. Pointers stay valid
// between snapshot and ParallelFor because nothing in this system changes
// archetype for snapshotted entities. Phase 13: profile/stamina cached in
// the snapshot so the hot loop never hits a map.
type unitWork struct {
	ent       ecs.Entity
	pos       *components.WorldPos
	mot       *components.Motion
	queue     *components.ActionQueue
	stance    *components.Stance
	microPath *components.MicroPath // M17.A short-term waypoint stream
	threat    *components.Threat    // M17.B - read for combat-move facing
	profile   components.MovementProfile
	stamina   *components.Stamina // nil if unit has no Stamina component
	exhausted bool                // current StaminaExhausted marker presence
}

func (sys *UnitMovementSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	sys.elapsed += dt

	// Phase 14.5 M14.5.2: neighbour data comes from the shared SpatialHash
	// rebuilt this tick by SpatialHashRebuildSystem. The separation pass
	// queries it directly inside step() - no per-tick O(N^2) snapshot.
	hash := sys.spatialHash.Get()

	// Phase 14.6 M14.6.1 - wall snapshot for the per-tick reflection pass.
	// Cleared and refilled each tick; workers read read-only inside step().
	for k := range sys.collisionWalls {
		sys.collisionWalls[k] = sys.collisionWalls[k][:0]
	}
	if sys.wallFilter != nil {
		qW := sys.wallFilter.Query()
		for qW.Next() {
			pos, w := qW.Get()
			doorState := components.DoorClosed
			if d := sys.doorMap.Get(qW.Entity()); d != nil {
				doorState = d.State
			}
			sys.collisionWalls[pos.Chunk] = append(sys.collisionWalls[pos.Chunk],
				makeColWall(*pos, *w, doorState))
		}
	}
	walls := sys.collisionWalls

	// Snapshot units (component pointers) so the parallel pass can index
	// into a slice without holding the live ECS query. Phase 13: also
	// snapshot the effective MovementProfile + Stamina pointer for the unit
	// so the parallel step doesn't have to re-resolve squad / order /
	// overrides per tick.
	sys.workBuf = sys.workBuf[:0]
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, mot, queue, stance, mp := q.Get()
		profile := sys.resolveProfile(ent)
		stamina := sys.staminaMap.Get(ent)
		exhausted := sys.staminaExhaustedMap.Has(ent)
		threat := sys.threatMap.Get(ent)
		sys.workBuf = append(sys.workBuf, unitWork{
			ent:       ent,
			pos:       pos,
			mot:       mot,
			queue:     queue,
			stance:    stance,
			microPath: mp,
			threat:    threat,
			profile:   profile,
			stamina:   stamina,
			exhausted: exhausted,
		})
	}
	work := sys.workBuf

	// Reset per-worker marker buffers (capacity preserved, length zeroed).
	for i := range sys.workerExhaustedAdds {
		sys.workerExhaustedAdds[i] = sys.workerExhaustedAdds[i][:0]
		sys.workerExhaustedRemoves[i] = sys.workerExhaustedRemoves[i][:0]
	}

	sys.pool.ParallelForIndexed(len(work), func(chunkIdx, start, end int) {
		// Defensive: if chunkIdx exceeds the buffer allocation (shouldn't
		// happen - NewUnitMovementSystem sizes against pool.Workers()), fall
		// back to slot 0 to avoid an out-of-range panic.
		if chunkIdx >= len(sys.workerExhaustedAdds) {
			chunkIdx = 0
		}
		for i := start; i < end; i++ {
			w := work[i]
			markerOp := sys.step(w, dt, hash, walls)
			switch markerOp {
			case staminaMarkerAdd:
				sys.workerExhaustedAdds[chunkIdx] = append(sys.workerExhaustedAdds[chunkIdx], w.ent)
			case staminaMarkerRemove:
				sys.workerExhaustedRemoves[chunkIdx] = append(sys.workerExhaustedRemoves[chunkIdx], w.ent)
			}
		}
	})

	// Serial post-pass: apply StaminaExhausted marker toggles. Archetype
	// mutations must run outside the parallel section (Ark forbids in-loop
	// mutations on archetypes other workers may have been reading).
	for _, batch := range sys.workerExhaustedAdds {
		for _, e := range batch {
			if !sys.staminaExhaustedMap.Has(e) {
				sys.staminaExhaustedMap.Add(e, &components.StaminaExhausted{})
			}
		}
	}
	for _, batch := range sys.workerExhaustedRemoves {
		for _, e := range batch {
			if sys.staminaExhaustedMap.Has(e) {
				sys.staminaExhaustedMap.Remove(e)
			}
		}
	}
}

// resolveProfile walks unit -> squad -> active order to determine the
// effective MovementProfile. Resolution order matches PHASE-13.md P5:
//
//  1. Active Order has OrderParamMovementProfile -> use that profile.
//  2. Squad has MovementProfile -> use that profile.
//  3. Fallback: system default (Walk / Stand / Standard / Direct).
//
// Called once per unit in the serial snapshot pass - keeps the parallel step
// hot-path free of map lookups + handle indirections.
func (sys *UnitMovementSystem) resolveProfile(unit ecs.Entity) components.MovementProfile {
	member := sys.memberMap.Get(unit)
	if member == nil || member.Squad == (ecs.Entity{}) {
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceStand,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	}
	// Order-level override wins.
	if head := sys.orderQueueMap.Get(member.Squad); head != nil && head.First != (ecs.Entity{}) {
		if override := sys.orderMovementOverrideMap.Get(head.First); override != nil {
			return override.Profile
		}
	}
	// Squad-level standing rule.
	if profile := sys.movementProfileMap.Get(member.Squad); profile != nil {
		return *profile
	}
	return components.MovementProfile{
		Pace: components.PaceWalk, Stance: components.StanceStand,
		Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
	}
}
