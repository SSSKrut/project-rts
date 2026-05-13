package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MaxSpeed by stance (m/s). Phase 7 P4.
var unitMaxSpeed = [...]float32{
	components.StanceStand:  5.0,
	components.StanceCrouch: 3.0,
	components.StanceProne:  1.5,
}

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
// no shared map mutation, no archetype changes). On a 12-unit scene the
// ParallelFor overhead is ~50 μs of net loss; the chassis pays off at 1000+
// units.
type UnitMovementSystem struct {
	unitFilter      *ecs.Filter5[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance]
	neighbourFilter *ecs.Filter2[components.Unit, components.WorldPos]
	pool            *core.WorkerPool
	elapsed         float32
}

// NewUnitMovementSystem wires the system with a worker pool. nil pool falls
// back to serial execution (useful for unit tests).
func NewUnitMovementSystem(pool *core.WorkerPool) *UnitMovementSystem {
	return &UnitMovementSystem{pool: pool}
}

func (sys *UnitMovementSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter5[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance](w)
	sys.neighbourFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
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

// unitNeighbour — XZ-only positional record used for the separation pass.
type unitNeighbour struct {
	ent ecs.Entity
	wx  float32
	wz  float32
}

// unitWork — snapshot row for the parallel step pass. Pointers stay valid
// between snapshot and ParallelFor because nothing in this system changes
// archetype for snapshotted entities.
type unitWork struct {
	ent    ecs.Entity
	pos    *components.WorldPos
	mot    *components.Motion
	queue  *components.ActionQueue
	stance *components.Stance
}

func (sys *UnitMovementSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	sys.elapsed += dt

	// Snapshot neighbours (XZ-only) for the separation pass. Phase 14 will
	// swap this O(N²) scan for a spatial hash.
	neighbours := make([]unitNeighbour, 0, 64)
	qN := sys.neighbourFilter.Query()
	for qN.Next() {
		_, pos := qN.Get()
		neighbours = append(neighbours, unitNeighbour{
			ent: qN.Entity(),
			wx:  float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			wz:  float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
		})
	}

	// Snapshot units (component pointers) so the parallel pass can index into
	// a slice without holding the live ECS query.
	work := make([]unitWork, 0, 64)
	q := sys.unitFilter.Query()
	for q.Next() {
		_, pos, mot, queue, stance := q.Get()
		work = append(work, unitWork{ent: q.Entity(), pos: pos, mot: mot, queue: queue, stance: stance})
	}

	sys.pool.ParallelFor(len(work), func(start, end int) {
		for i := start; i < end; i++ {
			w := work[i]
			sys.step(w.ent, w.pos, w.mot, w.queue, w.stance, dt, neighbours)
		}
	})
}

// step advances one unit by dt seconds. Race-safe: every write goes through
// the snapshot's per-unit pointers and never touches shared maps / resources.
func (sys *UnitMovementSystem) step(
	ent ecs.Entity,
	pos *components.WorldPos,
	mot *components.Motion,
	queue *components.ActionQueue,
	stance *components.Stance,
	dt float32,
	neighbours []unitNeighbour,
) {
	if queue.Count == 0 {
		mot.Speed = 0
		return
	}
	action := &queue.Actions[queue.Head]
	switch action.Kind {
	case components.ActionMoveTo:
		diff := action.Target.Sub(*pos)
		distSq := diff.X*diff.X + diff.Z*diff.Z
		if distSq < arrivalRadius*arrivalRadius {
			popAction(queue)
			return
		}
		dist := float32(math.Sqrt(float64(distSq)))
		invDist := 1 / dist
		desiredX := diff.X * invDist
		desiredZ := diff.Z * invDist

		// Separation force from neighbours in XZ.
		selfX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		selfZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		var sepX, sepZ float32
		for _, n := range neighbours {
			if n.ent == ent {
				continue
			}
			dx := selfX - n.wx
			dz := selfZ - n.wz
			dSq := dx*dx + dz*dz
			if dSq <= 1e-4 || dSq > separationRadius*separationRadius {
				continue
			}
			invD := 1 / dSq
			sepX += dx * invD
			sepZ += dz * invD
		}

		maxSpeed := unitMaxSpeed[stance.Code]
		vx := desiredX*maxSpeed + sepX*separationWeight
		vz := desiredZ*maxSpeed + sepZ*separationWeight
		speed := float32(math.Sqrt(float64(vx*vx + vz*vz)))
		if speed > maxSpeed {
			inv := maxSpeed / speed
			vx *= inv
			vz *= inv
			speed = maxSpeed
		}

		// Y lerp toward target. Lets units climb stairs / drop into
		// bunkers without teleporting; GroundStick then picks the
		// floor whose Y is closest on the next tick.
		dy := action.Target.Local.Y - pos.Local.Y
		progress := float32(0)
		if dist > 0 {
			progress = dt * speed / dist
			if progress > 1 {
				progress = 1
			}
		}
		move := rl.Vector3{X: vx * dt, Y: dy * progress, Z: vz * dt}
		*pos = pos.Add(move)
		mot.Speed = speed
		if speed > 0.01 {
			mot.Yaw = float32(math.Atan2(float64(vx), float64(vz)))
		}

	case components.ActionStop:
		mot.Speed = 0
		if queue.StopUntil == 0 {
			queue.StopUntil = sys.elapsed + stopDuration
		} else if sys.elapsed >= queue.StopUntil {
			queue.StopUntil = 0
			popAction(queue)
		}

	case components.ActionStance:
		stance.Code = components.StanceCode(action.Target.Local.Y)
		popAction(queue)
	}
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
