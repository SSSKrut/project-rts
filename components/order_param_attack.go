package components

// OrderParamAttackMove is an optional marker on an Order entity. Present
// when the order was issued via Alt+RMB (or pie-menu AttackMove). WeaponSystem
// reads it to permit opportunistic firing on visible enemies *without*
// cancelling the MoveTo order.
type OrderParamAttackMove struct{}
