package components

// BehaviorRules is the per-Squad standing rule covering AI override gates.
// Phase 13 ships this as scaffold-only: fields are written by RoleService /
// SquadService at spawn (defaults) and by the Inspector quick-bar (M13.6)
// at player edit, but **no system in Phase 13 reads them**.
//
// Phase 15 (Tactical AI) is the canonical reader: SurvivalInstinctSystem
// checks HoldUntilOrdered / AllowAutoReposition / AllowAutoStance before
// setting the TacticalOverride marker on a unit; CoverEvaluationSystem
// reads SuppressionThreshold to know when to engage cover selection.
//
// This is the same scaffold pattern as Phase 9's Suppression.Level field
// (Phase 14 wrote the first real values). The "contract between phases"
// from COMMAND-MODEL.md §7 documents the writer / reader split explicitly
// so a Phase 15 audit can find every gate by grepping BehaviorRules.
type BehaviorRules struct {
	// AllowAutoReposition — Phase 15 SurvivalInstinct may move the unit to
	// a better cover slot when suppressed. False locks the unit at its
	// current cell (deployed MG, sniper on overwatch).
	AllowAutoReposition bool

	// AllowAutoStance — Phase 15 may drop the unit into Prone under fire
	// without an explicit order. False means stance only changes when the
	// player or the squad's MovementProfile.Stance says so.
	AllowAutoStance bool

	// HoldUntilOrdered — disables every reactive behaviour. The unit does
	// nothing without an explicit Order. Sniper / ATGunner default for
	// careful target selection.
	HoldUntilOrdered bool

	// AllowReturnFire — separate from EngagementRules.Mode: even if Mode is
	// HoldFire, a true here lets the unit return fire when shot at. Used by
	// support roles (Medic / Engineer / RadioOp) who normally hold fire but
	// shouldn't go silent under direct attack.
	AllowReturnFire bool

	// SuppressionThreshold — Phase 15 SurvivalInstinct gate. When
	// Suppression.Level > this value, the unit becomes a candidate for
	// TacticalOverride (find cover, reposition, prone). 0.30 is the
	// Phase 13 default — Phase 15 will tune empirically.
	SuppressionThreshold float32
}
