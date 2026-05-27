package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 17 M17.A constants.
const (
	// microPathArrivalRadius - close enough to a waypoint to advance Head.
	// Lower than UnitMovement.arrivalRadius (0.6) so the unit pops waypoints
	// without overshooting the next one.
	microPathArrivalRadius float32 = 0.6
	// microPathGoalShift - goal drifted this far from the planned GoalSnap?
	// Replan. Plan A-P1: "if new goal > 2 m from GoalSnap, mark Dirty".
	microPathGoalShift float32 = 2.0
	// microPathReplanBudget - max replans per tick across all units. Caps
	// NavService.FindPath load.
	microPathReplanBudget int = 16
	// microPathStuckDist / microPathStuckTime - if a unit hasn't covered
	// stuckDist in stuckTime, force a replan (last segment likely blocked
	// by an obstacle the planner didn't see).
	microPathStuckDist float32 = 0.2
	microPathStuckTime float32 = 1.0
	// microPathReplanCooldown - per-unit minimum gap between replans.
	microPathReplanCooldown float32 = 0.5
)

// MicroPathSystem - Phase 17 M17.A. Each tick walks Units with a MicroPath
// component and:
//
//  1. Pops Head waypoints the unit has already reached.
//  2. Compares the live ActionQueue MoveTo goal against MicroPath.GoalSnap;
//     a drift > microPathGoalShift sets Dirty.
//  3. Stuck detection - if the unit hasn't moved microPathStuckDist in
//     microPathStuckTime seconds, force Dirty.
//  4. Plans / replans through NavService.FindPath, respecting a per-unit
//     cooldown and a global per-tick budget.
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
	// 1. Drain reached waypoints.
	for mp.Head < mp.Count {
		d := mp.Waypoints[mp.Head].Sub(*pos)
		if d.X*d.X+d.Z*d.Z < microPathArrivalRadius*microPathArrivalRadius {
			mp.Head++
			continue
		}
		break
	}

	// 2. No active MoveTo -> wipe path.
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

	// 3. Detect goal drift.
	if !mp.Dirty {
		d := goal.Sub(mp.GoalSnap)
		if d.X*d.X+d.Z*d.Z > microPathGoalShift*microPathGoalShift {
			mp.Dirty = true
		}
	}

	// 4. Stuck detection - only while we have waypoints to walk.
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

	// 4.5 Phase 17.9 — force replan when the stored path is exhausted but
	// the unit still hasn't reached the final goal. Without this the unit
	// drops into "walk blindly toward action.Target" mode for the rest of
	// the route: pathfinder never refreshes the next chunk of waypoints,
	// reflectAgainstWalls slides on walls instead of routing through doors,
	// and the debug Unit-paths overlay shows nothing because Head==Count.
	// Threshold 1 m is comfortably above arrivalRadius (0.6) so we don't
	// trigger spurious replans on the final tick before the action pops.
	if mp.Count == 0 || mp.Head >= mp.Count {
		d := goal.Sub(*pos)
		if d.X*d.X+d.Z*d.Z > 1.0*1.0 {
			mp.Dirty = true
		}
	}

	// 5. Replan if Dirty (with cooldown + budget).
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
