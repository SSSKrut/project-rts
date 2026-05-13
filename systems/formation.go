package systems

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// FormationSystem drives every rostered unit to its formation offset around
// the squad center (PHASE-9.md P3). The contract with Phase 7
// UnitMovementSystem stays the same: we keep writing the unit's ActionQueue,
// the low-level controller hasn't been told squads exist. That means Phase 10
// tactical AI can later preempt FormationSystem by also writing to the queue
// at higher priority — no architectural change.
//
// Cohesion (P6) is integrated here, not a separate system: we already walk
// every member to compute the offset, so the "is this member too far from
// center?" test is one extra subtract. Stragglers go through SquadService.Leave
// after the filter loop closes.
//
// Tier routing — same as SquadMacroPathSystem: the squad inherits the
// commander's LOD tier; the system itself runs Update for both tiers but
// only processes squads whose commander has the matching LOD marker.
type FormationSystem struct {
	filter         *ecs.Filter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData]
	posMap         *ecs.Map[components.WorldPos]
	actionQueueMap *ecs.Map[components.ActionQueue]
	lodActiveMap   *ecs.Map[components.LODActive]
	lodRelevantMap *ecs.Map[components.LODRelevant]
	squadService   *SquadService
}

func NewFormationSystem(squadService *SquadService) *FormationSystem {
	return &FormationSystem{squadService: squadService}
}

func (sys *FormationSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.actionQueueMap = ecs.NewMap[components.ActionQueue](w)
	sys.lodActiveMap = ecs.NewMap[components.LODActive](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
}

func (FormationSystem) Name() string { return "formation" }

func (FormationSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   100 * time.Millisecond,
		RelevantEvery: 500 * time.Millisecond,
		DormantEvery:  core.LODDisabled,
	}
}

// CohesionLeashCoeff × Spacing = max distance member-to-center before the
// member falls out of the roster (PHASE-9.md P6 "эластичный поводок").
const CohesionLeashCoeff float32 = 4.0

// formationPushTolerance — minimum target shift that re-pushes the unit's
// MoveTo. Below this, the unit keeps its existing queue. Without the gate the
// formation re-writes ActionQueue every 100 ms, the unit's
// arrival-pop / new-push cycle oscillates around the destination, and
// rotating Forward sweeps targets sideways. Slightly larger than
// UnitMovementSystem.arrivalRadius (0.6) so we don't re-issue on the cusp.
const formationPushTolerance float32 = 0.7

// formationForwardLockDist — once the center is within this many metres of
// the macro target, freeze Forward instead of recomputing it from
// (target - center). Avoids the unit-vector swing near arrival that twisted
// offsets into circular motion in the original Phase 9 build.
const formationForwardLockDist float32 = 5.0

func (sys *FormationSystem) Update(ctx core.UpdateContext) {
	// Stragglers are collected and applied after the filter loop closes —
	// SquadService.Leave mutates the archetype of the member unit and the
	// CommandRoster of the squad, both of which would break a live query.
	var leaveBuffer []ecs.Entity

	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd := q.Get()
		if roster.Count == 0 {
			continue
		}
		commander := roster.Members[0]
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

		// Pop reached waypoints — SquadMacroPathSystem runs only every 1 s,
		// FormationSystem at 100 ms is the responsive pace for head advance.
		for mp.Head < mp.Count {
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z < SquadWaypointReached*SquadWaypointReached {
				mp.Head++
			} else {
				break
			}
		}

		// Resolve the center's current macro target.
		var centerTarget components.WorldPos
		haveTarget := false
		if mp.HasGoal {
			if mp.Head < mp.Count {
				centerTarget = mp.Waypoints[mp.Head]
			} else {
				centerTarget = mp.Goal
			}
			haveTarget = true
		}

		// Update Forward toward the macro target — but only while the squad
		// is still far enough out that the unit vector (target - center) /
		// mag is geometrically stable. Once we're inside formationForwardLockDist
		// (or Forward was never set), the existing Forward stays.
		forwardZero := fd.Forward.X == 0 && fd.Forward.Z == 0
		if haveTarget {
			diff := centerTarget.Sub(center)
			mag := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
			if mag > 0.05 && (forwardZero || mag > formationForwardLockDist) {
				fd.Forward = rl.Vector3{X: diff.X / mag, Y: 0, Z: diff.Z / mag}
			}
		}

		// Cohesion is only enforced while the squad has an active order
		// (PHASE-9.md P6 wording — "эластичный поводок" — describes movement,
		// not a no-op idle state). The previous behaviour ejected naturally-
		// spread members the moment a fresh squad was formed with `T`, even
		// before the player issued any order.
		leash := CohesionLeashCoeff * fd.Spacing
		leashSq := leash * leash

		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
				continue
			}
			mPos := sys.posMap.Get(mem)
			if mPos == nil {
				continue
			}

			if haveTarget {
				d := mPos.Sub(center)
				distSq := d.X*d.X + d.Z*d.Z
				if distSq > leashSq {
					leaveBuffer = append(leaveBuffer, mem)
					continue
				}
			}

			if !haveTarget {
				continue
			}
			aq := sys.actionQueueMap.Get(mem)
			if aq == nil {
				continue
			}
			offX, offZ := formationOffset(fd.Type, i, fd.Spacing, fd.Forward)
			target := centerTarget.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})

			// Re-push only if the new target meaningfully differs from the
			// last queued MoveTo. Cuts the per-tick pop-push cycle at the
			// destination and stops the rotating-offset chase that some
			// formations could fall into.
			if aq.Count > 0 {
				lastIdx := (int(aq.Tail) + components.ActionQueueSize - 1) % components.ActionQueueSize
				last := aq.Actions[lastIdx]
				if last.Kind == components.ActionMoveTo {
					d := last.Target.Sub(target)
					if d.X*d.X+d.Z*d.Z < formationPushTolerance*formationPushTolerance {
						continue
					}
				}
			}
			ClearActions(aq)
			PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: target})
		}
	}

	for _, e := range leaveBuffer {
		sys.squadService.Leave(e)
	}
}

// formationOffset returns the XZ offset of `slot` from the squad center for
// each formation kind. Slot 0 always sits on center (commander). Output is in
// world-space metres.
func formationOffset(kind components.FormationKind, slot uint8, spacing float32, forward rl.Vector3) (float32, float32) {
	if slot == 0 {
		return 0, 0
	}
	// Right = rotate Forward 90° clockwise around +Y. Pre-normalised by
	// SquadMacroPathSystem / FormationSystem (mag check); falls back to a
	// sensible default if Forward is degenerate.
	fx, fz := forward.X, forward.Z
	if fx*fx+fz*fz < 1e-4 {
		fx, fz = 0, 1
	}
	rx, rz := fz, -fx
	k := int(slot)

	switch kind {
	case components.FormationLine:
		// Slot 1=+1, 2=-1, 3=+2, 4=-2 ... ranks fan outward from commander.
		side := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		d := side * sign * spacing
		return rx * d, rz * d

	case components.FormationColumn:
		// Slot k sits k*spacing behind the commander along -Forward.
		d := -float32(k) * spacing
		return fx * d, fz * d

	case components.FormationWedge:
		// Pairs fan back-left / back-right at 45°. Row index = (k+1)/2,
		// so 1-2 are the first row behind, 3-4 the second, etc.
		row := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		ox := (-fx*row + rx*row*sign) * spacing
		oz := (-fz*row + rz*row*sign) * spacing
		return ox, oz

	case components.FormationLoose:
		// Deterministic radial scatter — hash only on slot index so the
		// pattern doesn't shimmer when members swap squads (P-note in
		// PHASE-9.md "Что НЕ делать в formationOffset").
		h := slotHash32(uint32(slot))
		ang := float64(h&0xFFFF) / float64(0x10000) * 2 * math.Pi
		radF := float32((h>>16)&0xFFFF) / float32(0x10000)
		// Slot 1+ pushed outward at least 0.5×(2×spacing) so members don't
		// pile on the commander.
		rad := (0.5 + 0.5*radF) * 2 * spacing
		cos := float32(math.Cos(ang))
		sin := float32(math.Sin(ang))
		return rad * cos, rad * sin
	}
	return 0, 0
}

// slotHash32 — SplitMix-style 32-bit finalizer used by FormationLoose.
func slotHash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}
