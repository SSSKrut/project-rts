package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 14.5 M14.5.1 — unitMaxSpeed table folded into components.StanceSpecs.
// Readers use components.SpecForStance(code).MaxSpeed.

// Separation parameters. Phase 7 P4: only separation force, no alignment /
// cohesion. Phase 9 layers formation logic on top via the same system.
const (
	separationRadius float32 = 1.5
	separationWeight float32 = 4.0
)

// arrivalRadius — how close the unit needs to be to its current MoveTo target
// before the action pops from the queue. Slightly bigger than the cell-centre
// dance to avoid jittering at the goal.
const arrivalRadius float32 = 0.6

// stopDuration — how long an ActionStop holds the unit in place before it
// pops off the queue. Phase 7 P7.
const stopDuration float32 = 0.1

// UnitMovementSystem advances units along their ActionQueue.
//
// Phase 11.5 M11.5.3 / M11.5.5: tier-gating removed and the per-unit step
// is dispatched through WorkerPool.ParallelFor. snapshot → parallel step →
// no post-pass (each worker writes only to its own unit's component pointers,
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
// auto-transitions stance toward the squad's standing default. StaminaExhausted
// marker add/remove uses per-worker buffers + serial post-pass to keep
// archetype mutations off the parallel critical path.
type UnitMovementSystem struct {
	unitFilter *ecs.Filter5[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance]
	pool       *core.WorkerPool
	// Phase 14.5 M14.5.2: separation pass reads from the shared SpatialHash
	// resource (rebuilt by SpatialHashRebuildSystem each tick before this
	// system runs). Replaces the per-tick O(N²) neighbour walk.
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

	// Phase 14.6 M14.6.1 — wall reflection. Walls snapshot bucketed by chunk
	// once per tick in the serial pre-pass; step() reads the 3x3 chunk window
	// around the unit to reflect velocity that would cross a wall this frame.
	wallFilter      *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap         *ecs.Map[components.Door]
	collisionWalls  map[components.ChunkCoord][]colWall

	// Reusable snapshot buffer. Filled in the serial pre-pass, read-only by
	// workers during ParallelFor — safe because writes are indexed and never
	// concurrent.
	workBuf []unitWork

	// Per-worker buffers for StaminaExhausted marker toggles. Workers can't
	// mutate archetypes concurrently, so they record pending Add / Remove
	// requests in their own buffer; serial post-pass applies them all.
	workerExhaustedAdds    [][]ecs.Entity
	workerExhaustedRemoves [][]ecs.Entity

	elapsed float32
}

// staminaRegenThreshold — Current/MaxLevel ratio at which the StaminaExhausted
// marker is cleared. PHASE-13.md P3 sets this at 0.30 so the unit must
// actually rest, not just touch zero.
const staminaRegenThreshold float32 = 0.30

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
	sys.unitFilter = ecs.NewFilter5[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.staminaMap = ecs.NewMap[components.Stamina](w)
	sys.staminaExhaustedMap = ecs.NewMap[components.StaminaExhausted](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderMovementOverrideMap = ecs.NewMap[components.OrderParamMovementProfile](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.spatialHash = ecs.NewResource[core.SpatialHash](w)
	sys.world = w
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
}

func (UnitMovementSystem) Name() string { return "unit_movement" }

func (UnitMovementSystem) LODPolicy() core.LODPolicy {
	// Phase 11.5 P1: universal simulation — every tick, no LOD gating.
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// unitWork — snapshot row for the parallel step pass. Pointers stay valid
// between snapshot and ParallelFor because nothing in this system changes
// archetype for snapshotted entities. Phase 13: profile/stamina cached in
// the snapshot so the hot loop never hits a map.
type unitWork struct {
	ent       ecs.Entity
	pos       *components.WorldPos
	mot       *components.Motion
	queue     *components.ActionQueue
	stance    *components.Stance
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
	// queries it directly inside step() — no per-tick O(N²) snapshot.
	hash := sys.spatialHash.Get()

	// Phase 14.6 M14.6.1 — wall snapshot for the per-tick reflection pass.
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

	// Snapshot units (component pointers) so the parallel pass can index into
	// a slice without holding the live ECS query. Phase 13: also snapshot the
	// effective MovementProfile + Stamina pointer for the unit so the parallel
	// step doesn't have to re-resolve squad / order / overrides per tick.
	sys.workBuf = sys.workBuf[:0]
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, mot, queue, stance := q.Get()
		profile := sys.resolveProfile(ent)
		stamina := sys.staminaMap.Get(ent)
		exhausted := sys.staminaExhaustedMap.Has(ent)
		sys.workBuf = append(sys.workBuf, unitWork{
			ent:       ent,
			pos:       pos,
			mot:       mot,
			queue:     queue,
			stance:    stance,
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
		// happen — NewUnitMovementSystem sizes against pool.Workers()), fall
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

// staminaMarkerOp is the result of one unit step's stamina decision.
type staminaMarkerOp uint8

const (
	staminaMarkerNone staminaMarkerOp = iota
	staminaMarkerAdd
	staminaMarkerRemove
)

// resolveProfile walks unit → squad → active order to determine the effective
// MovementProfile. Resolution order matches PHASE-13.md P5:
//
//  1. Active Order has OrderParamMovementProfile → use that profile.
//  2. Squad has MovementProfile → use that profile.
//  3. Fallback: system default (Walk / Stand / Standard / Direct).
//
// Called once per unit in the serial snapshot pass — keeps the parallel step
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

// step advances one unit by dt seconds. Race-safe: every write goes through
// the snapshot's per-unit pointers and never touches shared maps / resources.
// Returns the StaminaExhausted marker toggle decision (caller batches it into
// the per-worker buffer for the serial post-pass).
//
// Phase 14.5 M14.5.2: separation queries the shared SpatialHash (read-only
// during the parallel section).
func (sys *UnitMovementSystem) step(
	w unitWork,
	dt float32,
	hash *core.SpatialHash,
	walls map[components.ChunkCoord][]colWall,
) staminaMarkerOp {
	// Pace selection: StaminaExhausted forces Walk (P3). Otherwise use the
	// effective profile's Pace.
	effectivePace := w.profile.Pace
	if w.exhausted {
		effectivePace = components.PaceWalk
	}

	// Stance auto-transition (P9): when no explicit ActionStance is currently
	// at the queue head, snap toward the squad's standing default. Phase 13
	// transition is instant; Phase 25 may add a time cost.
	hasActiveStanceAction := w.queue.Count > 0 && w.queue.Actions[w.queue.Head].Kind == components.ActionStance
	if !hasActiveStanceAction && w.stance.Code != w.profile.Stance {
		w.stance.Code = w.profile.Stance
	}

	// Drain / regen Stamina each tick. Recovery only when Pace=Walk AND
	// Stance ∈ {Stand, Crouch} (Prone doesn't recover — P3). Open question 5
	// answered: crouch allows regen.
	markerOp := staminaMarkerNone
	if w.stamina != nil && w.stamina.MaxLevel > 0 {
		drain := components.PaceStaminaDrain[effectivePace] * dt
		switch effectivePace {
		case components.PaceWalk:
			if w.stance.Code != components.StanceProne {
				w.stamina.Current += w.stamina.RecoverRate * dt
				if w.stamina.Current > w.stamina.MaxLevel {
					w.stamina.Current = w.stamina.MaxLevel
				}
			}
		default:
			w.stamina.Current -= drain
			if w.stamina.Current < 0 {
				w.stamina.Current = 0
			}
		}
		// Marker decision: set when fully drained, clear once the unit has
		// rested past the regen threshold. Hysteresis keeps the marker from
		// flapping while the unit hovers at zero.
		switch {
		case !w.exhausted && w.stamina.Current <= 0:
			markerOp = staminaMarkerAdd
		case w.exhausted && w.stamina.Current >= staminaRegenThreshold*w.stamina.MaxLevel:
			markerOp = staminaMarkerRemove
		}
	}

	// Speed lookup uses the unit's current Stance × effective Pace.
	maxSpeed := components.SpecForStance(w.stance.Code).MaxSpeed * components.PaceSpeedMul[effectivePace]

	if w.queue.Count == 0 {
		w.mot.Speed = 0
		return markerOp
	}
	action := &w.queue.Actions[w.queue.Head]
	switch action.Kind {
	case components.ActionMoveTo:
		diff := action.Target.Sub(*w.pos)
		distSq := diff.X*diff.X + diff.Z*diff.Z
		if distSq < arrivalRadius*arrivalRadius {
			popAction(w.queue)
			return markerOp
		}
		dist := float32(math.Sqrt(float64(distSq)))
		invDist := 1 / dist
		desiredX := diff.X * invDist
		desiredZ := diff.Z * invDist

		// Phase 14.5 M14.5.2: separation force pulled from the SpatialHash.
		// Callback receives nearby entries inline — no intermediate slice
		// allocation. Self is filtered via ent check; alive-check is cheap
		// here (the hash may carry indices for entities removed since rebuild,
		// but the world.Alive guard skips dead reads).
		selfX := float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
		selfZ := float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
		var sepX, sepZ float32
		if hash != nil {
			hash.ForEachInRadius(selfX, selfZ, separationRadius, func(ent ecs.Entity, dSq float32) {
				if ent == w.ent || dSq <= 1e-4 {
					return
				}
				// Stale-entity guard: SpatialHash invariant — readers MUST
				// alive-check every callback target before further work.
				if !sys.world.Alive(ent) {
					return
				}
				// Recompute dx/dz so the falloff direction is from the live
				// position (SpatialEntry is a 1-tick stale snapshot, but the
				// resolution mismatch is ≤ a few cm at top speed — irrelevant
				// for separation force direction).
				np := sys.posMap.Get(ent)
				if np == nil {
					return
				}
				nx := float32(np.Chunk.X)*components.ChunkSize + np.Local.X
				nz := float32(np.Chunk.Z)*components.ChunkSize + np.Local.Z
				dx := selfX - nx
				dz := selfZ - nz
				dd := dx*dx + dz*dz
				if dd <= 1e-4 {
					return
				}
				inv := 1 / dd
				sepX += dx * inv
				sepZ += dz * inv
			})
		}

		vx := desiredX*maxSpeed + sepX*separationWeight
		vz := desiredZ*maxSpeed + sepZ*separationWeight
		speed := float32(math.Sqrt(float64(vx*vx + vz*vz)))
		if speed > maxSpeed {
			inv := maxSpeed / speed
			vx *= inv
			vz *= inv
			speed = maxSpeed
		}

		// Phase 14.6 M14.6.1 — wall reflection. If the predicted XZ step
		// would cross a wall (non-open-door segment) in the unit's 3x3 chunk
		// window, reflect velocity around the wall normal so the unit slides
		// off rather than tunnelling through. Reflection-only, no parallel
		// sliding — Phase 15.B will add the polished glide.
		if walls != nil {
			vx, vz = reflectAgainstWalls(selfX, selfZ, vx, vz, dt, walls, w.pos.Chunk)
		}

		// Y lerp toward target. Lets units climb stairs / drop into
		// bunkers without teleporting; GroundStick then picks the
		// floor whose Y is closest on the next tick.
		dy := action.Target.Local.Y - w.pos.Local.Y
		progress := float32(0)
		if dist > 0 {
			progress = dt * speed / dist
			if progress > 1 {
				progress = 1
			}
		}
		move := rl.Vector3{X: vx * dt, Y: dy * progress, Z: vz * dt}
		*w.pos = w.pos.Add(move)
		w.mot.Speed = speed
		if speed > 0.01 {
			w.mot.Yaw = float32(math.Atan2(float64(vx), float64(vz)))
		}

	case components.ActionStop:
		w.mot.Speed = 0
		if w.queue.StopUntil == 0 {
			w.queue.StopUntil = sys.elapsed + stopDuration
		} else if sys.elapsed >= w.queue.StopUntil {
			w.queue.StopUntil = 0
			popAction(w.queue)
		}

	case components.ActionStance:
		w.stance.Code = components.StanceCode(action.Target.Local.Y)
		popAction(w.queue)
	}
	return markerOp
}

// popAction advances the queue head past the current action.
func popAction(q *components.ActionQueue) {
	if q.Count == 0 {
		return
	}
	q.Actions[q.Head] = components.Action{}
	q.Head = (q.Head + 1) % components.ActionQueueSize
	q.Count--
	q.StopUntil = 0
}

// PushAction appends an action to the queue. If the queue is full, the oldest
// entry is dropped to make room (preserves the most recent intent). Exposed
// so main.go can wire orders without re-implementing the ring-buffer math.
func PushAction(q *components.ActionQueue, a components.Action) {
	if q.Count == components.ActionQueueSize {
		// Drop oldest.
		q.Head = (q.Head + 1) % components.ActionQueueSize
		q.Count--
	}
	q.Actions[q.Tail] = a
	q.Tail = (q.Tail + 1) % components.ActionQueueSize
	q.Count++
}

// ClearActions resets the queue to empty. Used by RMB immediate override.
func ClearActions(q *components.ActionQueue) {
	for i := range q.Actions {
		q.Actions[i] = components.Action{}
	}
	q.Head = 0
	q.Tail = 0
	q.Count = 0
	q.StopUntil = 0
}

// colWall is the movement-collision view of a WallSegment. Windows block
// movement (separate from LOS — losWall flips openingTransparent for windows
// too), open doors allow passage through the opening range, closed doors and
// plain walls fully block. World coords; trig pre-computed.
type colWall struct {
	fromX, fromZ       float32
	sa, ca             float32
	length             float32
	hasOpening         bool
	openPassable       bool // true ⇔ open door (window opening still blocks movement)
	openStart, openEnd float32
}

func makeColWall(pos components.WorldPos, w components.WallSegment, doorState components.DoorState) colWall {
	baseX := float32(pos.Chunk.X) * components.ChunkSize
	baseZ := float32(pos.Chunk.Z) * components.ChunkSize
	sa := float32(math.Sin(float64(w.Yaw)))
	ca := float32(math.Cos(float64(w.Yaw)))
	cw := colWall{
		fromX:      baseX + pos.Local.X,
		fromZ:      baseZ + pos.Local.Z,
		sa:         sa,
		ca:         ca,
		length:     w.Length,
		hasOpening: w.OpeningKind != components.OpeningNone,
		openStart:  w.OpeningCenterT*w.Length - w.OpeningWidth*0.5,
		openEnd:    w.OpeningCenterT*w.Length + w.OpeningWidth*0.5,
	}
	if w.OpeningKind == components.OpeningDoor && doorState == components.DoorOpen {
		cw.openPassable = true
	}
	return cw
}

// reflectAgainstWalls runs an XZ ray cast from `(curX, curZ)` along velocity
// `(velX, velZ) * dt` and reflects the velocity vector around the normal of
// every wall the predicted segment would cross this tick. Walls in the 3x3
// chunk window around `home` are considered; open-door openings pass through.
//
// Multiple wall hits chain: the first reflection updates the prediction, the
// next wall is tested against the new direction. Bound the loop at 4
// reflections to avoid pathological corners.
//
// Used by UnitMovementSystem.step. Reflection-only — Phase 15.B will swap to
// glide-along-wall for smoother movement.
func reflectAgainstWalls(curX, curZ, velX, velZ, dt float32,
	walls map[components.ChunkCoord][]colWall, home components.ChunkCoord,
) (float32, float32) {
	if velX*velX+velZ*velZ < 1e-6 {
		return velX, velZ
	}
	rvx, rvz := velX, velZ
	for pass := 0; pass < 4; pass++ {
		predX := curX + rvx*dt
		predZ := curZ + rvz*dt
		hit := false
		for dcZ := int32(-1); dcZ <= 1; dcZ++ {
			for dcX := int32(-1); dcX <= 1; dcX++ {
				cc := components.ChunkCoord{X: home.X + dcX, Z: home.Z + dcZ}
				bucket := walls[cc]
				for i := range bucket {
					w := &bucket[i]
					toX := w.fromX + w.sa*w.length
					toZ := w.fromZ + w.ca*w.length
					t1, t2, ok := segmentSegmentIntersect2D(curX, curZ, predX, predZ,
						w.fromX, w.fromZ, toX, toZ)
					if !ok || t1 < 0 || t1 > 1 || t2 < 0 || t2 > 1 {
						continue
					}
					if w.hasOpening && w.openPassable {
						wallT := t2 * w.length
						if wallT >= w.openStart && wallT <= w.openEnd {
							continue // pass through open door
						}
					}
					// Wall direction (sa, ca); normal perpendicular = (ca, -sa)
					// or its negation, whichever points toward the moving unit.
					nx, nz := w.ca, -w.sa
					if rvx*nx+rvz*nz > 0 {
						nx, nz = -nx, -nz
					}
					dot := rvx*nx + rvz*nz
					rvx -= 2 * dot * nx
					rvz -= 2 * dot * nz
					hit = true
					break
				}
				if hit {
					break
				}
			}
			if hit {
				break
			}
		}
		if !hit {
			return rvx, rvz
		}
	}
	return rvx, rvz
}
