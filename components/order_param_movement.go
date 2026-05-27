package components

// OrderParamMovementProfile is an optional component on an Order entity.
// When present, UnitMovementSystem and SquadMacroPathSystem read this
// profile in preference to the squad's standing MovementProfile for the
// duration of the order (Issued -> InProgress).
//
// On Order completion / cancellation, the override stops applying purely
// because the Order is no longer InProgress - no explicit "revert" step.
// The squad's standing rule (MovementProfile component) is untouched.
type OrderParamMovementProfile struct {
	Profile MovementProfile
}
