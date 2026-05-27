package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

const (
	// Close enough to a waypoint to advance Head. Lower than
	// UnitMovement.arrivalRadius (0.6) so pops happen without overshoot.
	microPathArrivalRadius float32 = 0.6
	// Goal drift threshold that flips Dirty for a replan.
	microPathGoalShift float32 = 2.0
	// Max replans per tick across all units (caps NavService.FindPath load).
	microPathReplanBudget int = 16
	// Unit hasn't covered stuckDist in stuckTime → force replan.
	microPathStuckDist float32 = 0.2
	microPathStuckTime float32 = 1.0
	// Per-unit minimum gap between replans.
	microPathReplanCooldown float32 = 0.5
)

// MicroPathSystem walks Units with a MicroPath: pops reached waypoints,
// detects goal drift / stuck conditions, and replans through
// NavService.FindPath under a per-unit cooldown + per-tick budget.
//
// Lives between FormationSystem (writer of ActionQueue.Head.Target) and the
// next UnitMovement tick (reader of MicroPath.Waypoints[Head]).
type MicroPathSystem struct {
	filter  *ecs.Filter4[components.Unit, components.WorldPos, components.MicroPath, components.ActionQueue]
	nav     *NavService
	pool    *core.WorkerPool
	elapsed float32
}

func NewMicroPathSystem(nav *NavService, pool *core.WorkerPool) *MicroPathSystem {
	return &MicroPathSystem{nav: nav, pool: pool}
}

func (sys *MicroPathSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.WorldPos, components.MicroPath, components.ActionQueue](w)
}

func (MicroPathSystem) Name() string { return "micro_path" }

func (MicroPathSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MicroPathSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	sys.elapsed += dt
	now := sys.elapsed

	budget := microPathReplanBudget

	q := sys.filter.Query()
	for q.Next() {
		_, pos, mp, aq := q.Get()
		sys.tickUnit(pos, mp, aq, now, &budget)
	}
}

// tickUnit applies the per-unit MicroPath lifecycle. Pulled out so future
// table-tests can exercise it without spinning a worker pool.
func (sys *MicroPathSystem) tickUnit(pos *components.WorldPos, mp *components.MicroPath, aq *components.ActionQueue, now float32, budget *int) {
	for mp.Head < mp.Count {
		d := mp.Waypoints[mp.Head].Sub(*pos)
		if d.X*d.X+d.Z*d.Z < microPathArrivalRadius*microPathArrivalRadius {
			mp.Head++
			continue
		}
		break
	}

	if aq.Count == 0 {
		clearMicroPath(mp)
		return
	}
	action := aq.Actions[aq.Head]
	if action.Kind != components.ActionMoveTo {
		clearMicroPath(mp)
		return
	}
	goal := action.Target

	if !mp.Dirty {
		d := goal.Sub(mp.GoalSnap)
		if d.X*d.X+d.Z*d.Z > microPathGoalShift*microPathGoalShift {
			mp.Dirty = true
		}
	}

	// Stuck detection — only while we have waypoints to walk.
	if mp.Count > 0 && mp.Head < mp.Count {
		dx := pos.Local.X - mp.LastPos.X
		dz := pos.Local.Z - mp.LastPos.Z
		if dx*dx+dz*dz > microPathStuckDist*microPathStuckDist {
			mp.LastPos = pos.Local
			mp.LastProgressAt = now
		} else if now-mp.LastProgressAt > microPathStuckTime {
			mp.Dirty = true
			mp.LastProgressAt = now
		}
	} else {
		mp.LastPos = pos.Local
		mp.LastProgressAt = now
	}

	// Force replan when the stored path is exhausted but the unit still
	// hasn't reached the final goal — otherwise the unit walks blindly
	// toward action.Target without ever refreshing waypoints.
	// Threshold 1 m sits above arrivalRadius (0.6) to avoid spurious
	// replans on the final tick before the action pops.
	if mp.Count == 0 || mp.Head >= mp.Count {
		d := goal.Sub(*pos)
		if d.X*d.X+d.Z*d.Z > 1.0*1.0 {
			mp.Dirty = true
		}
	}

	if !mp.Dirty || now < mp.ReplanAt || *budget <= 0 {
		return
	}
	waypoints := sys.nav.FindPath(*pos, goal, NavOpts{})
	mp.Head = 0
	mp.Count = 0
	for i, wp := range waypoints {
		if i >= components.MicroPathSize {
			break
		}
		mp.Waypoints[i] = wp
		mp.Count++
	}
	mp.GoalSnap = goal
	mp.Dirty = false
	mp.ReplanAt = now + microPathReplanCooldown
	mp.LastProgressAt = now
	mp.LastPos = pos.Local
	*budget--
}

func clearMicroPath(mp *components.MicroPath) {
	mp.Head = 0
	mp.Count = 0
	mp.Dirty = false
	mp.GoalSnap = components.WorldPos{}
}
