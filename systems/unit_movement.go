package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Separation parameters. Only separation force here; formation logic
// layers on top via the same system.
const (
	separationRadius float32 = 1.5
	separationWeight float32 = 4.0
)

// Vehicle-yield tuning (Phase 19 M6): hulls shove overlapped units aside,
// units inside a moving hull's projected corridor sidestep before contact,
// and hulls enter the ORCA neighbour set with full responsibility on the
// unit.
const (
	vehYieldQueryR         float32 = 16.0 // covers the corridor horizon
	vehYieldHorizon        float32 = 1.5  // s of hull travel projected ahead
	vehYieldLatPad         float32 = 0.7  // extra corridor half-width
	vehShoveCap            float32 = 5.0  // m/s — scramble, not stroll
	orcaVehNeighbourRadius float32 = 14.0
	vehOrcaPad             float32 = 0.3
)

// Per-stance acceleration cap (m/s²). Prone units take longer to spin up.
var stanceAccel = [...]float32{
	components.StanceStand:  4.0,
	components.StanceCrouch: 2.0,
	components.StanceProne:  1.0,
}

// Turn cap (rad/s) ~170°/s.
const maxYawRate float32 = 3.0

// Per-unit personality SpeedMul half-width: members run at slightly
// different paces so a squad doesn't lock-step.
const perUnitSpeedSpread float32 = 0.05

// Distance to MoveTo target where the action pops. Slightly bigger than the
// cell-centre dance to avoid jittering at the goal.
const arrivalRadius float32 = 0.6

// Max |Y delta| to the MoveTo target for XZ arrival to count while the
// micro path still has waypoints — half a storey (3.0) with margin, so a
// stair climber under its slot doesn't pop the action a floor early.
const arrivalYBand float32 = 1.6

// How long ActionStop holds the unit in place before it pops.
const stopDuration float32 = 0.1

// Current/MaxLevel ratio at which StaminaExhausted clears (forces rest, not
// just touching zero).
const staminaRegenThreshold float32 = 0.30

// UnitMovementSystem advances units along their ActionQueue.
//
// Parallel: per-unit step runs through ParallelFor. Each worker writes only
// to its own unit's component pointers — no shared map mutation, no
// archetype changes inside the parallel section. Snapshot buffers live on
// the struct and reset to len=0 each Update.
//
// Per-unit step reads the effective MovementProfile (order override > squad
// standing > system default), drains/regenerates Stamina, and auto-
// transitions stance toward the squad's standing default. StaminaExhausted
// toggles use per-worker buffers + serial post-pass.
//
// The per-unit body lives in unit_movement_step.go; wall collision helpers
// (colWall / reflectAgainstWalls) live in unit_movement_walls.go.
type UnitMovementSystem struct {
	unitFilter *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.MicroPath]
	pool       *core.WorkerPool
	// Neighbour data comes from the frozen SpatialEntry snapshot, never live maps.
	spatialHash ecs.Resource[core.SpatialHash]
	// Vehicle hulls: ORCA obstacles for moving units + shove source for idle
	// ones (P5 — infantry yields, the hull never dodges).
	vehHash ecs.Resource[core.VehicleSpatialHash]

	memberMap                *ecs.Map[components.SquadMember]
	staminaMap               *ecs.Map[components.Stamina]
	staminaExhaustedMap      *ecs.Map[components.StaminaExhausted]
	orderQueueMap            *ecs.Map[components.OrderQueueHead]
	orderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	movementProfileMap       *ecs.Map[components.MovementProfile]
	threatMap                *ecs.Map[components.Threat]
	// step() reads the unit's OWN collider for the ORCA self radius.
	colliderMap *ecs.Map[components.Collider]
	// Replan-trigger counters: step() accumulates Overcrowded / Stuck dt and
	// flips MicroPath.Dirty when either crosses threshold.
	blackboardMap *ecs.Map[components.LocalBlackboard]

	// Walls snapshot bucketed by chunk once per tick; step() reads the 3×3
	// window around the unit.
	wallFilter     *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap        *ecs.Map[components.Door]
	collisionWalls map[components.ChunkCoord][]colWall

	workBuf []unitWork

	// Per-worker buffers for StaminaExhausted toggles; serial post-pass
	// applies them (workers can't mutate archetypes concurrently).
	workerExhaustedAdds    [][]ecs.Entity
	workerExhaustedRemoves [][]ecs.Entity
}

// NewUnitMovementSystem. nil pool falls back to serial execution.
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
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.spatialHash = ecs.NewResource[core.SpatialHash](w)
	sys.vehHash = ecs.NewResource[core.VehicleSpatialHash](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
}

func (UnitMovementSystem) Name() string { return "unit_movement" }

func (UnitMovementSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// unitWork — snapshot row for the parallel step. Profile/stamina cached so
// the hot loop never hits a map.
type unitWork struct {
	ent       ecs.Entity
	pos       *components.WorldPos
	mot       *components.Motion
	queue     *components.ActionQueue
	stance    *components.Stance
	microPath *components.MicroPath
	threat    *components.Threat
	profile   components.MovementProfile
	stamina   *components.Stamina
	exhausted bool
}

func (sys *UnitMovementSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	now := float32(ctx.SimNow)

	hash := sys.spatialHash.Get()
	var vehHash *core.SpatialHash
	if vh := sys.vehHash.Get(); vh != nil {
		vehHash = &vh.SpatialHash
	}

	// Wall snapshot for the reflection pass: cleared + refilled each tick,
	// workers read read-only.
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

	// Snapshot units (component pointers + cached profile / stamina) so the
	// parallel pass doesn't re-resolve squad / order / overrides per tick.
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

	for i := range sys.workerExhaustedAdds {
		sys.workerExhaustedAdds[i] = sys.workerExhaustedAdds[i][:0]
		sys.workerExhaustedRemoves[i] = sys.workerExhaustedRemoves[i][:0]
	}

	sys.pool.ParallelForIndexed(len(work), func(chunkIdx, start, end int) {
		if chunkIdx >= len(sys.workerExhaustedAdds) {
			chunkIdx = 0
		}
		for i := start; i < end; i++ {
			w := work[i]
			markerOp := sys.step(w, dt, now, hash, vehHash, walls)
			switch markerOp {
			case staminaMarkerAdd:
				sys.workerExhaustedAdds[chunkIdx] = append(sys.workerExhaustedAdds[chunkIdx], w.ent)
			case staminaMarkerRemove:
				sys.workerExhaustedRemoves[chunkIdx] = append(sys.workerExhaustedRemoves[chunkIdx], w.ent)
			}
		}
	})

	// Serial post-pass: apply StaminaExhausted toggles outside the parallel
	// section (Ark forbids archetype mutations workers may be reading).
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

// resolveProfile picks the effective MovementProfile (order override > squad
// standing > system default). Called once per unit in the serial snapshot.
func (sys *UnitMovementSystem) resolveProfile(unit ecs.Entity) components.MovementProfile {
	member := sys.memberMap.Get(unit)
	if member == nil || member.Squad == (ecs.Entity{}) {
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceStand,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	}
	if head := sys.orderQueueMap.Get(member.Squad); head != nil && head.First != (ecs.Entity{}) {
		if override := sys.orderMovementOverrideMap.Get(head.First); override != nil {
			return override.Profile
		}
	}
	if profile := sys.movementProfileMap.Get(member.Squad); profile != nil {
		return *profile
	}
	return components.MovementProfile{
		Pace: components.PaceWalk, Stance: components.StanceStand,
		Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
	}
}
