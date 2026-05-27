package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// UtilityContext bundles per-tick perception state for scoring functions.
// Built once per evaluation; scoring functions read fields directly without
// re-querying maps.
type UtilityContext struct {
	Threat      float32 // [0,1]
	ThreatState components.ThreatState

	// At least one hostile in Awareness FIFO, alive, in range.
	HasTarget bool

	// SurvivalInstinct wrote blackboard.AssignedCover.
	CoverAvail bool

	// Primary weapon Ammo / spec.MagSize (1.0 full, 0 empty).
	AmmoFraction float32

	// Squad has an active head order.
	HasGoal bool

	// Placeholder until WeaponSystem plumbs a last-shot timestamp.
	JustFired bool

	// Direct-fire suppression channel (distinct from Total).
	Suppression float32

	// XZ distance to Blackboard.GoalSlot (FormationSystem writes it). Zero
	// for soloists / idle units.
	DistToSlot float32
}

// buildContext populates a UtilityContext from the unit's components.
func (sys *UtilityEvaluatorSystem) buildContext(ent ecs.Entity, b *components.LocalBlackboard) UtilityContext {
	var c UtilityContext

	if t := sys.threatMap.Get(ent); t != nil {
		c.Threat = t.Total
		c.ThreatState = t.State
		c.Suppression = t.Suppression
	}

	if aware := sys.awarenessMap.Get(ent); aware != nil {
		c.HasTarget = sys.awarenessHasUsableTarget(ent, aware)
	}

	c.CoverAvail = b.AssignedCover != (ecs.Entity{})

	// Primary weapon ammo; secondary doesn't change mode.
	if eq := sys.equipmentMap.Get(ent); eq != nil && eq.Primary != (ecs.Entity{}) {
		if w := sys.weaponMap.Get(eq.Primary); w != nil {
			spec := components.SpecForWeapon(w.Kind)
			if spec.Ammo > 0 {
				c.AmmoFraction = float32(w.Ammo) / float32(spec.Ammo)
			}
		}
	}

	if sm := sys.squadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		if head := sys.orderQueueMap.Get(sm.Squad); head != nil && head.First != (ecs.Entity{}) {
			c.HasGoal = true
		}
	}

	if b.GoalSlot != (components.WorldPos{}) {
		if selfPos := sys.posMap.Get(ent); selfPos != nil {
			dSq := worldDistSq(*selfPos, b.GoalSlot)
			if dSq > 0 {
				c.DistToSlot = float32(math.Sqrt(float64(dSq)))
			}
		}
	}

	return c
}

// awarenessHasUsableTarget returns true when at least one Awareness slot
// holds a still-alive hostile within weapon range.
func (sys *UtilityEvaluatorSystem) awarenessHasUsableTarget(seer ecs.Entity, aware *components.Awareness) bool {
	var ownFaction uint8
	if f := sys.factionMap.Get(seer); f != nil {
		ownFaction = f.ID
	}
	var rangeSq float32
	if eq := sys.equipmentMap.Get(seer); eq != nil && eq.Primary != (ecs.Entity{}) {
		if w := sys.weaponMap.Get(eq.Primary); w != nil {
			rangeSq = w.RangeM * w.RangeM
		}
	}
	if rangeSq <= 0 {
		return false // no usable weapon → Engaging can't win
	}
	selfPos := sys.posMap.Get(seer)
	if selfPos == nil {
		return false
	}
	for i := range aware.LastSeen {
		slot := aware.LastSeen[i]
		if slot.Time == 0 || slot.Target == (ecs.Entity{}) {
			continue
		}
		f := sys.factionMap.Get(slot.Target)
		if f == nil || f.ID == ownFaction {
			continue
		}
		tpos := sys.posMap.Get(slot.Target)
		if tpos == nil {
			continue
		}
		dSq := worldDistSq(*selfPos, *tpos)
		if dSq <= rangeSq {
			return true
		}
	}
	return false
}

// scoreForMode is the per-mode score dispatch. Calibrated to [0, 1.2] so
// the ModeSwitchScoreDelta (0.15) gate filters near-ties.
//
// Calibration shape:
//   - Threat = 0   → Following dominates.
//   - Threat = 0.5 → TakingCover catches up; Following still wins with HasGoal.
//   - Threat = 0.8 → TakingCover / Suppressed dominate.
//   - AmmoFraction < 0.2 → Reloading spikes.
//   - HasTarget + ammo ≥ 0.2 + threat ≤ 0.7 → Engaging wins.
func scoreForMode(mode components.ActionMode, c *UtilityContext) float32 {
	switch mode {
	case components.ModeFollowing:
		s := float32(0.5) - c.Threat*0.3
		if c.HasGoal {
			s += 0.2
		} else {
			s -= 0.2
		}
		// Far-from-slot bonus: unit is genuinely en-route.
		if c.DistToSlot > 5 {
			s += 0.05
		}
		return s

	case components.ModeEngaging:
		if !c.HasTarget {
			return 0
		}
		return 0.7 - c.Threat*0.2 + c.AmmoFraction*0.2

	case components.ModeTakingCover:
		base := c.Threat * 0.9
		if c.CoverAvail {
			return base + 0.2
		}
		return base - 0.3

	case components.ModeRepositioning:
		if !c.JustFired {
			return 0
		}
		// Only "relocating" when the unit is already roughly in place.
		if c.DistToSlot > 3 {
			return 0
		}
		return 0.6 - c.Threat*0.5

	case components.ModeReloading:
		if c.AmmoFraction > 0.2 {
			return 0
		}
		// Threat suppresses reload — heavy fire → TakingCover wins.
		return 0.8 - c.Threat*0.6

	case components.ModeSuppressed:
		// Reads the direct-fire channel, distinct from TakingCover (Total).
		return c.Suppression*1.2 - 0.3
	}
	return 0
}

// applyReason writes a short human-readable cause into the blackboard's
// fixed-size Reason buffer. Strings stay < 32 chars.
func applyReason(b *components.LocalBlackboard, mode components.ActionMode, c *UtilityContext) {
	switch mode {
	case components.ModeFollowing:
		if c.HasGoal {
			b.SetReason("Following waypoint")
		} else {
			b.SetReason("Idle - no order")
		}
	case components.ModeEngaging:
		b.SetReason("Engaging visible target")
	case components.ModeTakingCover:
		if c.CoverAvail {
			b.SetReason("Taking cover")
		} else {
			b.SetReason("Seeking cover (none yet)")
		}
	case components.ModeRepositioning:
		b.SetReason("Repositioning after shot")
	case components.ModeReloading:
		b.SetReason("Reloading")
	case components.ModeSuppressed:
		b.SetReason("Suppressed by fire")
	default:
		b.SetReason("")
	}
}
