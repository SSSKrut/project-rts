package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// UtilityContext bundles per-tick perception state for scoring functions.
// Built once per unit per evaluation (in buildContext); scoring functions
// read fields directly without re-querying maps. Keeps scoring O(1) per
// mode and isolates allocation from the hot scoring loop.
type UtilityContext struct {
	// Threat from per-unit Threat component. Total ∈ [0,1] is the canonical
	// danger scalar; State is the discretised band (Safe / Vigilant /
	// Alerted / Threatened).
	Threat      float32
	ThreatState components.ThreatState

	// HasTarget — at least one hostile in Awareness FIFO that's still
	// alive, in weapon range, and recent enough to engage.
	HasTarget bool

	// CoverAvail — blackboard.AssignedCover is non-zero. SurvivalInstinct
	// writes this; UtilityEvaluator only reads (no cover pick here).
	CoverAvail bool

	// AmmoFraction — primary weapon Ammo / spec.MagSize. 1.0 = full, 0 =
	// empty. Used by Reloading score and as multiplier on Engaging.
	AmmoFraction float32

	// HasGoal — squad has an active head order; otherwise the unit is
	// idle (Following score drops without a destination).
	HasGoal bool

	// JustFired — placeholder. M17.8.6 will plumb a "last-shot timestamp"
	// from WeaponSystem into the blackboard; until then this stays false
	// and Repositioning effectively never scores high. Phase 17.8 leaves
	// the score gate in place so the scoring shape is final.
	JustFired bool

	// Suppression band shortcuts — Suppressed mode wants a high signal
	// here distinct from Total (Total includes ambient threat too).
	Suppression float32

	// DistToSlot — XZ distance from unit position to Blackboard.GoalSlot in
	// metres. FormationSystem writes GoalSlot every tick. Used by
	// Following score (large dist + HasGoal → "I'm still moving") and by
	// Repositioning (small dist + JustFired → "in place, time to relocate").
	// Zero when no GoalSlot set (soloists, idle units).
	DistToSlot float32
}

// buildContext populates a UtilityContext from the unit's components.
// Returns a value (not pointer) — small struct, stack-allocated, no GC
// pressure on the hot path.
func (sys *UtilityEvaluatorSystem) buildContext(ent ecs.Entity, b *components.LocalBlackboard) UtilityContext {
	var c UtilityContext

	if t := sys.threatMap.Get(ent); t != nil {
		c.Threat = t.Total
		c.ThreatState = t.State
		c.Suppression = t.Suppression
	}

	// HasTarget — walk Awareness FIFO; first slot with a recent hostile
	// in weapon range satisfies. Cheap because FIFO size ∈ [0,8].
	if aware := sys.awarenessMap.Get(ent); aware != nil {
		c.HasTarget = sys.awarenessHasUsableTarget(ent, aware)
	}

	// Cover assignment lives on the blackboard, written by SurvivalInstinct.
	c.CoverAvail = b.AssignedCover != (ecs.Entity{})

	// Weapon ammo. Primary weapon is the canonical fire-source; secondary
	// not considered for the Reloading gate (sidearm doesn't change mode).
	if eq := sys.equipmentMap.Get(ent); eq != nil && eq.Primary != (ecs.Entity{}) {
		if w := sys.weaponMap.Get(eq.Primary); w != nil {
			spec := components.SpecForWeapon(w.Kind)
			if spec.Ammo > 0 {
				c.AmmoFraction = float32(w.Ammo) / float32(spec.Ammo)
			}
		}
	}

	// HasGoal — unit's squad has a head order. Soloists (no squad) default
	// to HasGoal=false; Following.Score drops, they tend toward TakingCover
	// or Engaging based on threat.
	if sm := sys.squadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		if head := sys.orderQueueMap.Get(sm.Squad); head != nil && head.First != (ecs.Entity{}) {
			c.HasGoal = true
		}
	}

	// DistToSlot — XZ distance to GoalSlot when FormationSystem has written
	// one. Soloists / idle units leave GoalSlot zero; DistToSlot stays 0
	// which suppresses Following.Score's distance term (it's not "still
	// moving" if it has no slot).
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

// awarenessHasUsableTarget walks the Awareness FIFO and returns true when
// at least one slot holds a still-alive hostile within weapon range. Cheap:
// FIFO ≤ 8, single Map.Get per slot.
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
		// No usable weapon — treat as no target so Engaging never wins.
		return false
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
		// Check liveness + faction.
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

// scoreForMode is the spec-table dispatch — one row per ActionMode. Score
// values are calibrated to overlap in the band [0, 1.2] so the
// ModeSwitchScoreDelta (0.15) gate filters near-ties without missing
// genuine transitions.
//
// Tuning lives in M17.8.9 (playtest pass). Initial formulas chosen so that:
//   - Threat = 0   → Following dominates (other modes mostly negative).
//   - Threat = 0.5 → TakingCover catches up; Following still wins if
//                    HasGoal and no Engaging target.
//   - Threat = 0.8 → TakingCover / Suppressed dominate; Following sinks.
//   - AmmoFraction < 0.2 → Reloading spikes; matches Reloading priority
//                    over Following (low-ammo + low-threat unit reloads).
//   - HasTarget + ammo ≥ 0.2 + threat ≤ 0.7 → Engaging wins over Following.
func scoreForMode(mode components.ActionMode, c *UtilityContext) float32 {
	switch mode {
	case components.ModeFollowing:
		s := float32(0.5) - c.Threat*0.3
		if c.HasGoal {
			s += 0.2
		} else {
			s -= 0.2
		}
		// Phase 17.8 M17.8.4 — DistToSlot signal. Far-from-slot adds a
		// small bonus (unit is genuinely en-route), close-to-slot
		// neutral (it's already in position; other modes can win).
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
		// Only score high when the unit is roughly in place (close to its
		// slot) — far-from-slot units are still in Following, not
		// "relocating after a shot".
		if c.DistToSlot > 3 {
			return 0
		}
		return 0.6 - c.Threat*0.5

	case components.ModeReloading:
		// Only when ammo is genuinely low — full-mag units never score here.
		if c.AmmoFraction > 0.2 {
			return 0
		}
		// Higher reload pressure with less ammo, but threat suppresses it
		// (don't reload under heavy fire — TakingCover wins).
		return 0.8 - c.Threat*0.6

	case components.ModeSuppressed:
		// Naturally low unless suppression specifically is high. Distinct
		// from TakingCover which reads Threat.Total; Suppressed reads the
		// direct-fire suppression channel.
		return c.Suppression*1.2 - 0.3
	}
	return 0
}

// applyReason composes a short human-readable line describing why the
// unit is in `mode` given context `c`, and writes it into the blackboard's
// fixed-size Reason buffer. Inspector reads via b.ReasonString() in
// M17.8.8 — but other systems can already query CurrentMode + Reason for
// debug logging.
//
// Strings stay short (< 32 chars) so the fixed buffer fits without
// truncation. Where signals overlap we pick the dominant cause.
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
