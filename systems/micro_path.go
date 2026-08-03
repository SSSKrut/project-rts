package systems

import (
	"fmt"
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

const (
	// Close enough to a waypoint to advance Head. Lower than
	// UnitMovement.arrivalRadius (0.6) so pops happen without overshoot.
	microPathArrivalRadius float32 = 0.6
	// The mouth waypoint right before a gate needs a precise approach;
	// UnitMovement mirrors this radius so steering doesn't stop early.
	gateMouthRadius float32 = 0.3
	// Goal drift threshold that flips Dirty for a replan.
	microPathGoalShift float32 = 2.0
	// Max replans per tick across all units (caps NavService.FindPath load).
	microPathReplanBudget int = 16
	// Unit hasn't closed distance to its waypoint by ProgressFrac within
	// stuckTime → force replan.
	microPathProgressFrac float32 = 0.9
	microPathStuckTime    float32 = 1.0
	// Stuck within this of the waypoint = the spot is blocked by a body or
	// pressed against wall clearance, not unreachable: pop and steer for
	// the next waypoint — a replan returns the identical path forever (nav
	// is blind to units and to the collider-vs-clearance gap) and the
	// walker pins at ~0 m/s or saws between replanned heads. Sub-cell-
	// diagonal (1.41) so a pop never skips an entire nav cell; a parked-
	// body standoff is ~0.7 m, a wall-corner standoff ~1.2-1.3 m.
	microPathBlockedPopRadius float32 = 1.35
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
	filter *ecs.Filter4[components.Unit, components.WorldPos, components.MicroPath, components.ActionQueue]
	nav    *NavService
	pool   *core.WorkerPool
}

func NewMicroPathSystem(nav *NavService, pool *core.WorkerPool) *MicroPathSystem {
	return &MicroPathSystem{nav: nav, pool: pool}
}

func (sys *MicroPathSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.WorldPos, components.MicroPath, components.ActionQueue](w)
}

func (MicroPathSystem) Name() string { return "micro_path" }

func (MicroPathSystem) LODPolicy() core.LODPolicy {
	// Active-only: the filter is not tier-scoped, so one pass covers every
	// unit — a Relevant call would re-run the full pass in the same tick.
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MicroPathSystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)

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
		wp := mp.Waypoints[mp.Head]
		d := wp.Sub(*pos)
		distSq := d.X*d.X + d.Z*d.Z
		if distSq >= microPathArrivalRadius*microPathArrivalRadius {
			break
		}
		// Y-band: stacked storeys overlap in XZ (a cascade's upper flight
		// returns above the lower one), so XZ proximity alone pops upper-
		// floor waypoints from underneath and strands the climber.
		if d.Y > arrivalYBand || d.Y < -arrivalYBand {
			break
		}
		if mp.GateMask&(1<<mp.Head) != 0 && mp.Head > 0 {
			// Far endpoint of a transition (door / stairs / junction):
			// radius alone reaches across the wall from the wrong side and
			// strands the walker steering at the next interior waypoint
			// through solid wall beside the opening. Require crossing the
			// opening plane: projection onto prev→gate beyond the midpoint.
			prev := mp.Waypoints[mp.Head-1]
			u := pos.Sub(prev)
			v := wp.Sub(prev)
			vlen2 := v.X*v.X + v.Z*v.Z
			if u.X*v.X+u.Z*v.Z <= 0.5*vlen2 && vlen2 >= 1e-6 {
				break
			}
		}
		if mp.Head+1 < mp.Count && mp.GateMask&(1<<(mp.Head+1)) != 0 &&
			distSq >= gateMouthRadius*gateMouthRadius {
			// The waypoint BEFORE a gate is the opening's mouth: popping it
			// at full radius lets a walker beside the jamb switch to the
			// through-wall target and pin against the wall face
			// (ai_compound_main leader trap). Align with the mouth first.
			break
		}
		mp.Head++
		mp.BestDistSq = float32(math.MaxFloat32)
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

	// Stuck detection — only while we have waypoints to walk. Progress =
	// closing distance to the CURRENT waypoint; raw displacement lets a
	// wall-pinned walker oscillate (±0.3 m of wall slide) hard enough to
	// keep resetting the timer forever.
	if mp.Count > 0 && mp.Head < mp.Count {
		d := mp.Waypoints[mp.Head].Sub(*pos)
		cur := d.X*d.X + d.Z*d.Z
		if cur < mp.BestDistSq*microPathProgressFrac {
			mp.BestDistSq = cur
			mp.LastProgressAt = now
		} else if now-mp.LastProgressAt > microPathStuckTime {
			if cur < microPathBlockedPopRadius*microPathBlockedPopRadius &&
				d.Y > -arrivalYBand && d.Y < arrivalYBand &&
				mp.GateMask&(1<<mp.Head) == 0 &&
				(mp.Head+1 >= mp.Count || mp.GateMask&(1<<(mp.Head+1)) == 0) {
				// Blocked-pop: gates and gate mouths are excluded — they
				// need the precise approach the pops above enforce.
				mp.Head++
			} else {
				mp.Dirty = true
			}
			mp.LastProgressAt = now
			mp.BestDistSq = float32(math.MaxFloat32)
		}
	} else {
		mp.BestDistSq = float32(math.MaxFloat32)
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
	waypoints, gates := sys.nav.FindPathGates(*pos, goal, NavOpts{})
	if debugLog && len(waypoints) == 0 {
		fmt.Printf("[mp-empty] from=(%.1f,%.1f,Y%.1f) to=(%.1f,%.1f,Y%.1f)\n",
			float32(pos.Chunk.X)*components.ChunkSize+pos.Local.X,
			float32(pos.Chunk.Z)*components.ChunkSize+pos.Local.Z, pos.Local.Y,
			float32(goal.Chunk.X)*components.ChunkSize+goal.Local.X,
			float32(goal.Chunk.Z)*components.ChunkSize+goal.Local.Z, goal.Local.Y)
	}
	mp.Head = 0
	mp.Count = 0
	mp.GateMask = 0
	for i, wp := range waypoints {
		if i >= components.MicroPathSize {
			break
		}
		mp.Waypoints[i] = wp
		if i < len(gates) && gates[i] {
			mp.GateMask |= 1 << i
		}
		mp.Count++
	}
	mp.GoalSnap = goal
	mp.Dirty = false
	mp.ReplanAt = now + microPathReplanCooldown
	mp.LastProgressAt = now
	mp.BestDistSq = float32(math.MaxFloat32)
	*budget--
}

func clearMicroPath(mp *components.MicroPath) {
	mp.Head = 0
	mp.Count = 0
	mp.Dirty = false
	mp.GateMask = 0
	mp.GoalSnap = components.WorldPos{}
}
