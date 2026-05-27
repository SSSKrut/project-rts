package components

// BehaviorRules is the per-Squad standing rule covering AI override gates.
// Written by RoleService / SquadService at spawn and the Inspector quick-bar
// at player edit. Reader is SurvivalInstinctSystem / CoverEvaluationSystem.
type BehaviorRules struct {
	// AllowAutoReposition - SurvivalInstinct may move the unit to a better
	// cover slot when suppressed. False locks the unit at its current cell
	// (deployed MG, sniper on overwatch).
	AllowAutoReposition bool

	// AllowAutoStance - may drop the unit into Prone under fire without an
	// explicit order.
	AllowAutoStance bool

	// HoldUntilOrdered - disables every reactive behaviour. The unit does
	// nothing without an explicit Order. Sniper / ATGunner default.
	HoldUntilOrdered bool

	// AllowReturnFire - separate from EngagementRules.Mode: even if Mode is
	// HoldFire, a true here lets the unit return fire when shot at. Used by
	// support roles (Medic / Engineer / RadioOp).
	AllowReturnFire bool

	// SuppressionThreshold - SurvivalInstinct gate. When Threat.Suppression
	// exceeds this value, the unit becomes a candidate for TacticalOverride.
	SuppressionThreshold float32
}
