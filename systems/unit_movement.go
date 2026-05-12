package systems

import (
	"math"
	"time"

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

// UnitMovementSystem advances units along their ActionQueue. Per Phase 7 P4:
// only separation steering; no alignment / cohesion / formation (those land
// in Phase 9).
type UnitMovementSystem struct {
	activeFilter    *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.LODActive]
	relevantFilter  *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.LODRelevant]
	neighbourActive *ecs.Filter3[components.Unit, components.WorldPos, components.LODActive]
	neighbourRelev  *ecs.Filter3[components.Unit, components.WorldPos, components.LODRelevant]
	elapsed         float32
}

func (sys *UnitMovementSystem) InitUI(w *ecs.World) {
	sys.activeFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.LODActive](w)
	sys.relevantFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.ActionQueue, components.Stance, components.LODRelevant](w)
	sys.neighbourActive = ecs.NewFilter3[components.Unit, components.WorldPos, components.LODActive](w)
	sys.neighbourRelev = ecs.NewFilter3[components.Unit, components.WorldPos, components.LODRelevant](w)
}

func (UnitMovementSystem) Name() string { return "unit_movement" }

func (UnitMovementSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  core.LODDisabled,
	}
}

// neighbour — XZ-only positional record used for the separation pass.
type unitNeighbour struct {
	ent ecs.Entity
	wx  float32
	wz  float32
}

func (sys *UnitMovementSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	sys.elapsed += dt

	// Snapshot neighbours (XZ-only) for the separation pass. Phase 7 has
	// at most 12 units; an O(N²) scan is trivial. Phase 9 swaps in a spatial
	// hash when N grows past ~50.
	neighbours := make([]unitNeighbour, 0, 16)
	collectNeighbours := func() {
		qN := sys.neighbourActive.Query()
		for qN.Next() {
			_, pos, _ := qN.Get()
			neighbours = append(neighbours, unitNeighbour{
				ent: qN.Entity(),
				wx:  float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
				wz:  float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			})
		}
		qN2 := sys.neighbourRelev.Query()
		for qN2.Next() {
			_, pos, _ := qN2.Get()
			neighbours = append(neighbours, unitNeighbour{
				ent: qN2.Entity(),
				wx:  float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
				wz:  float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			})
		}
	}
	collectNeighbours()

	step := func(ent ecs.Entity, pos *components.WorldPos, mot *components.Motion, queue *components.ActionQueue, stance *components.Stance) {
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

	if ctx.Tier == core.LODTierActive {
		q := sys.activeFilter.Query()
		for q.Next() {
			_, pos, mot, queue, stance, _ := q.Get()
			step(q.Entity(), pos, mot, queue, stance)
		}
	} else if ctx.Tier == core.LODTierRelevant {
		q := sys.relevantFilter.Query()
		for q.Next() {
			_, pos, mot, queue, stance, _ := q.Get()
			step(q.Entity(), pos, mot, queue, stance)
		}
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
