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

// SquadMacroPathSystem builds and refreshes the MacroPath of each Squad.
// A* runs through NavService for the squad center (computed on the fly).
// Replan triggers:
//
//  1. ReplanAt = 0 (set by SquadService.OrderMoveTo) — immediate.
//  2. elapsed ≥ mp.ReplanAt — 1 s throttle.
//  3. Center drifted > SquadReplanCenterDrift from the next waypoint.
//
// Parallel per-squad: each FindPath call builds local A* state, read-only
// resources are immutable across the tick, and the MacroPath write touches
// only that squad's component (disjoint across workers).
type SquadMacroPathSystem struct {
	filter         *ecs.Filter5[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData, components.OrderQueueHead]
	posMap         *ecs.Map[components.WorldPos]
	orderKindMap   *ecs.Map[components.OrderKind]
	orderTargetMap *ecs.Map[components.OrderTarget]
	orderStateMap *ecs.Map[components.OrderState]
	// MovementProfile.PathStyle (or order-level override) feeds FindPath.
	movementProfileMap       *ecs.Map[components.MovementProfile]
	orderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	nav                      *NavService
	pool                     *core.WorkerPool

	workBuf []macroPathWork
	elapsed float32
}

// NewSquadMacroPathSystem. nil pool falls back to serial execution.
func NewSquadMacroPathSystem(nav *NavService, pool *core.WorkerPool) *SquadMacroPathSystem {
	return &SquadMacroPathSystem{
		nav:     nav,
		pool:    pool,
		workBuf: make([]macroPathWork, 0, 16),
	}
}

func (sys *SquadMacroPathSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter5[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData, components.OrderQueueHead](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderTargetMap = ecs.NewMap[components.OrderTarget](w)
	sys.orderStateMap = ecs.NewMap[components.OrderState](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.orderMovementOverrideMap = ecs.NewMap[components.OrderParamMovementProfile](w)
}

func (SquadMacroPathSystem) Name() string { return "squad_macro_path" }

func (SquadMacroPathSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   1 * time.Second,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

const (
	// Throttle (seconds) between unforced A* calls for the same squad.
	SquadReplanInterval float32 = 1.0
	// Force a replan when center drifts further than this from the next
	// waypoint (bunch reorganised around an obstacle).
	SquadReplanCenterDrift float32 = 5.0
	// SquadArrivalCoeff × Spacing = "close enough, squad idle".
	SquadArrivalCoeff float32 = 1.5
	// Slightly larger than unit arrivalRadius so the macro path doesn't
	// outpace its members.
	SquadWaypointReached float32 = 2.0
)

type macroPathWork struct {
	roster    *components.CommandRoster
	mp        *components.MacroPath
	fd        *components.FormationData
	head      *components.OrderQueueHead
	pathStyle components.PathStyle
}

func (sys *SquadMacroPathSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())

	sys.workBuf = sys.workBuf[:0]
	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd, head := q.Get()
		sq := q.Entity()
		// Resolve PathStyle from the squad's MovementProfile or the active
		// Order's override. Done serially here, once per squad.
		pathStyle := components.PathStyleDirect
		if profile := sys.movementProfileMap.Get(sq); profile != nil {
			pathStyle = profile.PathStyle
		}
		if head.First != (ecs.Entity{}) {
			if override := sys.orderMovementOverrideMap.Get(head.First); override != nil {
				pathStyle = override.Profile.PathStyle
			}
		}
		sys.workBuf = append(sys.workBuf, macroPathWork{
			roster: roster, mp: mp, fd: fd, head: head, pathStyle: pathStyle,
		})
	}
	work := sys.workBuf

	world := ctx.World
	elapsed := sys.elapsed
	sys.pool.ParallelFor(len(work), func(start, end int) {
		for i := start; i < end; i++ {
			sys.processSquad(world, work[i], elapsed)
		}
	})
}

func (sys *SquadMacroPathSystem) processSquad(world *ecs.World, w macroPathWork, elapsed float32) {
	roster := w.roster
	mp := w.mp
	fd := w.fd
	head := w.head
	if roster.Count == 0 {
		return
	}

	center, ok := SquadCenter(world, roster, sys.posMap)
	if !ok {
		return
	}

	// MacroPath is derived from the current Order. Idle squad → mp.HasGoal
	// stays false; FormationSystem sits this one out.
	var orderKind components.OrderKindCode
	var orderTargetPos components.WorldPos
	hasOrder := false
	if head.First != (ecs.Entity{}) && world.Alive(head.First) {
		if k := sys.orderKindMap.Get(head.First); k != nil {
			if t := sys.orderTargetMap.Get(head.First); t != nil {
				orderKind = k.Code
				orderTargetPos = t.Pos
				hasOrder = true
			}
		}
	}
	if !hasOrder {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}
	// Spec-driven hold-in-place: orders with DrivesMacroPath == false
	// (AttackTarget, SuppressFire) keep the squad stationary.
	if !components.SpecForOrderKind(orderKind).DrivesMacroPath {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}

	mp.Goal = orderTargetPos
	mp.HasGoal = true
	_ = orderKind

	// Pop head waypoints already crossed by the center. Skip while waiting
	// for stragglers so the FormationSystem gate stays consistent across
	// the slower 1 s SquadMacroPath cadence.
	if !mp.WaitingForStragglers {
		for mp.Head < mp.Count {
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z < SquadWaypointReached*SquadWaypointReached {
				mp.Head++
			} else {
				break
			}
		}
	}

	arrival := SquadArrivalCoeff * fd.Spacing
	if dSq := centerXZDistSq(center, mp.Goal); dSq < arrival*arrival {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}

	needReplan := false
	switch {
	case mp.ReplanAt == 0:
		needReplan = true
	case elapsed >= mp.ReplanAt:
		needReplan = true
	case mp.Count > 0 && mp.Head < mp.Count:
		d := center.Sub(mp.Waypoints[mp.Head])
		if d.X*d.X+d.Z*d.Z > SquadReplanCenterDrift*SquadReplanCenterDrift {
			needReplan = true
		}
	}
	if !needReplan {
		return
	}

	if debugLog {
		fmt.Printf("[macro] replan squad kind=%d goal=(%.1f,%.1f) center=(%.1f,%.1f)\n",
			orderKind,
			mp.Goal.Local.X+float32(mp.Goal.Chunk.X)*components.ChunkSize,
			mp.Goal.Local.Z+float32(mp.Goal.Chunk.Z)*components.ChunkSize,
			center.Local.X+float32(center.Chunk.X)*components.ChunkSize,
			center.Local.Z+float32(center.Chunk.Z)*components.ChunkSize)
	}

	path := sys.nav.FindPath(center, mp.Goal, NavOpts{
		Locomotion: components.LocomotionFoot,
		PathStyle:  w.pathStyle,
	})

	mp.Head = 0
	mp.Count = 0
	if len(path) == 0 {
		// No path — fall back to the raw goal so FormationSystem still drags
		// the squad in the right direction.
		mp.Waypoints[0] = mp.Goal
		mp.Count = 1
	} else {
		step := decimationStep(fd.Spacing)
		for i := step - 1; i < len(path); i += step {
			if int(mp.Count) >= components.SquadMacroPathSize {
				break
			}
			mp.Waypoints[mp.Count] = path[i]
			mp.Count++
		}
		// Always end on Goal so the squad doesn't park at the last decimated
		// waypoint instead of the target.
		if mp.Count == 0 || centerXZDistSq(mp.Waypoints[mp.Count-1], mp.Goal) > 1 {
			if int(mp.Count) < components.SquadMacroPathSize {
				mp.Waypoints[mp.Count] = mp.Goal
				mp.Count++
			} else {
				mp.Waypoints[components.SquadMacroPathSize-1] = mp.Goal
			}
		}
	}

	// Forward = unit XZ-vector from center toward the next waypoint. Falls
	// back to existing Forward when the squad is on top of the waypoint.
	var target components.WorldPos
	if mp.Count > 0 {
		target = mp.Waypoints[0]
	} else {
		target = mp.Goal
	}
	diff := target.Sub(center)
	mag := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
	if mag > 0.01 {
		fd.Forward = rl.Vector3{X: diff.X / mag, Y: 0, Z: diff.Z / mag}
	}

	mp.LastPlanned = elapsed
	mp.ReplanAt = elapsed + SquadReplanInterval
}

// SquadCenter returns the XZ-averaged WorldPos of every live roster member.
// Y is averaged too — falls out naturally as the squad ascends stairs / bunkers.
// `world` is required for the per-member Alive check (Ark's Map.Get panics on
// a dead entity).
func SquadCenter(world *ecs.World, roster *components.CommandRoster, posMap *ecs.Map[components.WorldPos]) (components.WorldPos, bool) {
	if roster.Count == 0 {
		return components.WorldPos{}, false
	}
	// First valid member becomes the reference chunk so accumulated Local
	// values stay bounded; offsets fold through WorldPos.Sub.
	var ref components.WorldPos
	found := false
	var sumX, sumY, sumZ float32
	var n float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		p := posMap.Get(mem)
		if p == nil {
			continue
		}
		if !found {
			ref = *p
			found = true
		}
		d := p.Sub(ref)
		sumX += d.X
		sumY += d.Y
		sumZ += d.Z
		n++
	}
	if !found {
		return components.WorldPos{}, false
	}
	inv := 1 / n
	out := ref.Add(rl.Vector3{X: sumX * inv, Y: sumY * inv, Z: sumZ * inv})
	return out, true
}

// centerXZDistSq — squared XZ distance between two WorldPos. Used in tight
// loops where DistanceSquared would also fold Y.
func centerXZDistSq(a, b components.WorldPos) float32 {
	d := a.Sub(b)
	return d.X*d.X + d.Z*d.Z
}

// SquadSpread returns (maxDistance, caughtUp, total): largest XZ distance
// from `center` to any live member, count within caughtThreshold, total live.
func SquadSpread(
	world *ecs.World,
	roster *components.CommandRoster,
	center components.WorldPos,
	posMap *ecs.Map[components.WorldPos],
	caughtThreshold float32,
) (float32, uint8, uint8) {
	var maxSq float32
	var caught, total uint8
	thresholdSq := caughtThreshold * caughtThreshold
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		p := posMap.Get(mem)
		if p == nil {
			continue
		}
		total++
		dSq := centerXZDistSq(*p, center)
		if dSq > maxSq {
			maxSq = dSq
		}
		if dSq <= thresholdSq {
			caught++
		}
	}
	return float32(math.Sqrt(float64(maxSq))), caught, total
}

// decimationStep — how many fine-grained NavService waypoints to skip
// between macro waypoints. Spacing 2 → step 4, Spacing 4 → step 6 etc.
func decimationStep(spacing float32) int {
	step := int(spacing) + 2
	if step < 4 {
		step = 4
	}
	if step > 8 {
		step = 8
	}
	return step
}
