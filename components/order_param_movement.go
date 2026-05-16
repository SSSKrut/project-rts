package components

// OrderParamMovementProfile is an **optional** component on an Order entity.
// When present, UnitMovementSystem and SquadMacroPathSystem read this
// profile in preference to the squad's standing MovementProfile for the
// duration of the order (Issued → InProgress).
//
// On Order completion / cancellation, the override stops applying purely
// because the Order is no longer InProgress — no explicit "revert" step.
// The squad's standing rule (MovementProfile component) is untouched.
//
// Issued by:
//   - Ctrl+RMB hotkey → MoveTo with PresetStealth override
//   - Double-RMB hotkey → MoveTo with PresetSprint override
//   - Pie menu Sprint / Sneak / Crawl segments (M13.5 / Phase 21)
//
// Same component pattern as OrderParamFacing / OrderParamPatrol — sparsely
// attached so the base Order archetype stays small.
type OrderParamMovementProfile struct {
	Profile MovementProfile
}
