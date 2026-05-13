package systems

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SquadMacroPathSystem builds and refreshes the MacroPath of each Squad
// entity. A* runs through NavService for the *center* of the squad (PHASE-9.md
// P4 — center is computed on the fly, not stored). Replan triggers (P5):
//
//  1. ReplanAt = 0 (set by SquadService.OrderMoveTo) — immediate.
//  2. sys.elapsed >= mp.ReplanAt — throttle of 1 s.
//  3. Center drifted further than SquadReplanCenterDrift from the next
//     waypoint — bunch reorganised around an obstacle, replan to the goal.
//
// Tier routing (P7): squad inherits the LOD tier of its commander (slot 0).
// The system itself runs Update for both Active (1 s) and Relevant (3 s)
// tiers but only processes squads whose commander carries the matching LOD
// marker.
type SquadMacroPathSystem struct {
	filter         *ecs.Filter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData]
	posMap         *ecs.Map[components.WorldPos]
	lodActiveMap   *ecs.Map[components.LODActive]
	lodRelevantMap *ecs.Map[components.LODRelevant]
	nav            *NavService
	elapsed        float32
}

func NewSquadMacroPathSystem(nav *NavService) *SquadMacroPathSystem {
	return &SquadMacroPathSystem{nav: nav}
}

func (sys *SquadMacroPathSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.lodActiveMap = ecs.NewMap[components.LODActive](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
}

func (SquadMacroPathSystem) Name() string { return "squad_macro_path" }

func (SquadMacroPathSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   1 * time.Second,
		RelevantEvery: 3 * time.Second,
		DormantEvery:  core.LODDisabled,
	}
}

const (
	// SquadReplanInterval — throttle (seconds) between unforced A* calls for
	// the same squad. PHASE-9.md P5 default.
	SquadReplanInterval float32 = 1.0
	// SquadReplanCenterDrift — if center is further than this from the next
	// waypoint, force a replan. Models bunch-around-obstacle drift.
	SquadReplanCenterDrift float32 = 5.0
	// SquadArrivalCoeff × Spacing = "close enough to the goal — squad idle".
	SquadArrivalCoeff float32 = 1.5
	// SquadWaypointReached — center within this distance pops the head
	// waypoint. Slightly larger than unit arrivalRadius so the macro path
	// doesn't outpace its members.
	SquadWaypointReached float32 = 2.0
)

func (sys *SquadMacroPathSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())

	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd := q.Get()
		if roster.Count == 0 {
			continue
		}
		commander := roster.Members[0]
		// Squad inherits the commander's LOD tier — see P7.
		switch ctx.Tier {
		case core.LODTierActive:
			if !sys.lodActiveMap.Has(commander) {
				continue
			}
		case core.LODTierRelevant:
			if !sys.lodRelevantMap.Has(commander) {
				continue
			}
		default:
			continue
		}

		center, ok := SquadCenter(ctx.World, roster, sys.posMap)
		if !ok {
			continue
		}

		if !mp.HasGoal {
			continue
		}

		// Pop head waypoints already crossed by the center. Also covers the
		// case where FormationSystem hasn't advanced Head yet (e.g. during the
		// first tick after a replan).
		for mp.Head < mp.Count {
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z < SquadWaypointReached*SquadWaypointReached {
				mp.Head++
			} else {
				break
			}
		}

		arrival := SquadArrivalCoeff * fd.Spacing
		if dSq := centerXZDistSq(center, mp.Goal); dSq < arrival*arrival {
			mp.HasGoal = false
			mp.Head = 0
			mp.Count = 0
			continue
		}

		needReplan := false
		switch {
		case mp.ReplanAt == 0:
			needReplan = true
		case sys.elapsed >= mp.ReplanAt:
			needReplan = true
		case mp.Count > 0 && mp.Head < mp.Count:
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z > SquadReplanCenterDrift*SquadReplanCenterDrift {
				needReplan = true
			}
		}
		if !needReplan {
			continue
		}

		path := sys.nav.FindPath(center, mp.Goal, NavOpts{Locomotion: components.LocomotionFoot})

		mp.Head = 0
		mp.Count = 0
		if len(path) == 0 {
			// No path — push the raw goal as a single fallback waypoint so
			// FormationSystem still drags the squad in the right direction.
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
			// Always end on Goal — otherwise the squad parks at the last
			// decimated waypoint instead of pulling up at the target.
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

		mp.LastPlanned = sys.elapsed
		mp.ReplanAt = sys.elapsed + SquadReplanInterval
	}
}

// SquadCenter returns the XZ-averaged WorldPos of every live member in the
// roster. Y is averaged too — it falls out naturally as the squad ascends
// stairs or descends into a bunker, no special floor logic needed. (false)
// when the roster has no resolvable positions.
//
// `world` is taken for the per-member Alive check; Ark's Map.Get panics on a
// dead entity so we must filter first. Phase 9 doesn't kill units but Phase 11
// will, and SquadService doesn't currently watch entity-death events.
func SquadCenter(world *ecs.World, roster *components.CommandRoster, posMap *ecs.Map[components.WorldPos]) (components.WorldPos, bool) {
	if roster.Count == 0 {
		return components.WorldPos{}, false
	}
	// Use the first valid member as the reference chunk so accumulated Local
	// values stay bounded; offsets from other members fold through WorldPos.Sub.
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

// centerXZDistSq is squared XZ distance between two WorldPos. Helper for tight
// inner loops where DistanceSquared would also fold Y (irrelevant when both
// points sit on the same plane during movement).
func centerXZDistSq(a, b components.WorldPos) float32 {
	d := a.Sub(b)
	return d.X*d.X + d.Z*d.Z
}

// decimationStep — how many fine-grained NavService waypoints to skip between
// macro waypoints. Spacing 2 → step 4, Spacing 4 → step 6 etc.
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
