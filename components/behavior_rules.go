package components

// BehaviorRules is the per-Squad standing rule covering AI override gates.
// Written by RoleService / SquadService at spawn and the Inspector quick-bar
// at player edit. Reader is SurvivalInstinctSystem / CoverEvaluationSystem.
// COMMAND-MODEL.md S7 documents the writer / reader contract.
type BehaviorRules struct {
	// AllowAutoReposition - SurvivalInstinct may move the unit to a better
	// cover slot when suppressed. False locks the unit at its current cell
	// (deployed MG, sniper on overwatch).
	AllowAutoReposition bool

	// AllowAutoStance - may drop the unit into Prone under fire without an
	// explicit order. False means stance only changes via player or the
	// squad's MovementProfile.Stance.
	AllowAutoStance bool

	// HoldUntilOrdered - disables every reactive behaviour. The unit does
	// nothing without an explicit Order. Sniper / ATGunner default.
	HoldUntilOrdered bool

	// AllowReturnFire - separate from EngagementRules.Mode: even if Mode is
	// HoldFire, a true here lets the unit return fire when shot at. Used by
	// support roles (Medic / Engineer / RadioOp) who normally hold fire but
	// shouldn't go silent under direct attack.
	AllowReturnFire bool

	// SuppressionThreshold - SurvivalInstinct gate. When Suppression.Level
	// exceeds this value, the unit becomes a candidate for TacticalOverride
	// (find cover, reposition, prone).
	SuppressionThreshold float32
}
