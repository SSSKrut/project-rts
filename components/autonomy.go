package components

// AutonomyCode names the per-squad reactive-AI freedom level. Strict at the
// silent end ("do exactly what I said, never react"), Survival at the
// permissive end ("scatter / retreat as needed to survive"). Adjacent to
// Doctrine - Doctrine is "what the squad does", Autonomy is "how much
// freedom the AI has to deviate".
type AutonomyCode uint8

const (
	AutonomyNone AutonomyCode = iota
	AutonomyStrict
	AutonomyCautious
	AutonomyAdaptive
	AutonomySurvival
)

// ActiveAutonomy records the autonomy level last applied to a squad via the
// Inspector chip. Stays as a label even if the player hand-edits any
// underlying BehaviorRules field - BehaviorRulesEdit's DirtyMask tracks the
// fine-grained per-field divergence.
type ActiveAutonomy struct {
	Code AutonomyCode
}

// BehaviorRulesField is a bit position in BehaviorRulesEdit.DirtyMask. Each
// player-tweaked field flips a bit so a subsequent Autonomy chip click can
// skip it ("respect my edit on this one").
type BehaviorRulesField uint8

const (
	DirtyAutoReposition BehaviorRulesField = 1 << iota
	DirtyAutoStance
	DirtyHoldUntilOrdered
	DirtyAllowReturnFire
	DirtySuppressionThreshold
)

// BehaviorRulesEdit carries the dirty mask for one squad's BehaviorRules.
// Scaffold for Phase 15.C - the chip click checks each bit before
// overwriting that field; the player-edit handler in
// drawBehaviorSection flips the bit. Detailed UX (reset chip, visual
// dirty indicators) lands in a follow-up.
type BehaviorRulesEdit struct {
	DirtyMask BehaviorRulesField
}

// AutonomySpec is one row in the per-code spec table. Mirrors DoctrineSpec
// shape but the Inspector chip writes only the Behavior field - Autonomy
// is the AI-freedom dial, Doctrine is the macro stance.
type AutonomySpec struct {
	Behavior BehaviorRules
}

// AutonomySpecs - Phase 15 C-P1 initial values. None stays zero.
var AutonomySpecs = [5]AutonomySpec{
	AutonomyNone: {},
	AutonomyStrict: {
		Behavior: BehaviorRules{
			AllowAutoReposition: false, AllowAutoStance: false,
			HoldUntilOrdered: true, AllowReturnFire: false,
			SuppressionThreshold: 1.0,
		},
	},
	AutonomyCautious: {
		Behavior: BehaviorRules{
			AllowAutoReposition: false, AllowAutoStance: true,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.5,
		},
	},
	AutonomyAdaptive: {
		Behavior: BehaviorRules{
			AllowAutoReposition: true, AllowAutoStance: true,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.3,
		},
	},
	AutonomySurvival: {
		Behavior: BehaviorRules{
			AllowAutoReposition: true, AllowAutoStance: true,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.15,
		},
	},
}

func AutonomyName(c AutonomyCode) string {
	switch c {
	case AutonomyStrict:
		return "Strict"
	case AutonomyCautious:
		return "Cautious"
	case AutonomyAdaptive:
		return "Adaptive"
	case AutonomySurvival:
		return "Survival"
	}
	return "-"
}

// AutonomyMatches returns true when every non-dirty field of `br` equals the
// spec for `c`. Dirty fields are excluded (the player asserted their value;
// the chip stays highlighted on the autonomy that matches the rest).
func AutonomyMatches(c AutonomyCode, br BehaviorRules, dirty BehaviorRulesField) bool {
	if c == AutonomyNone {
		return false
	}
	spec := AutonomySpecs[c].Behavior
	if dirty&DirtyAutoReposition == 0 && br.AllowAutoReposition != spec.AllowAutoReposition {
		return false
	}
	if dirty&DirtyAutoStance == 0 && br.AllowAutoStance != spec.AllowAutoStance {
		return false
	}
	if dirty&DirtyHoldUntilOrdered == 0 && br.HoldUntilOrdered != spec.HoldUntilOrdered {
		return false
	}
	if dirty&DirtyAllowReturnFire == 0 && br.AllowReturnFire != spec.AllowReturnFire {
		return false
	}
	if dirty&DirtySuppressionThreshold == 0 && br.SuppressionThreshold != spec.SuppressionThreshold {
		return false
	}
	return true
}

// ApplyAutonomy writes the spec into `br` skipping dirty bits.
func ApplyAutonomy(c AutonomyCode, br *BehaviorRules, dirty BehaviorRulesField) {
	if c == AutonomyNone || br == nil {
		return
	}
	spec := AutonomySpecs[c].Behavior
	if dirty&DirtyAutoReposition == 0 {
		br.AllowAutoReposition = spec.AllowAutoReposition
	}
	if dirty&DirtyAutoStance == 0 {
		br.AllowAutoStance = spec.AllowAutoStance
	}
	if dirty&DirtyHoldUntilOrdered == 0 {
		br.HoldUntilOrdered = spec.HoldUntilOrdered
	}
	if dirty&DirtyAllowReturnFire == 0 {
		br.AllowReturnFire = spec.AllowReturnFire
	}
	if dirty&DirtySuppressionThreshold == 0 {
		br.SuppressionThreshold = spec.SuppressionThreshold
	}
}
