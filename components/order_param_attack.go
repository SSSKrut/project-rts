package components

// OrderParamAttackMove is an **optional** marker on an Order entity. Present
// when the order was issued via Alt+RMB (or pie-menu AttackMove). PHASE-13.md
// scaffolds the flag; Phase 14 WeaponSystem is the canonical reader — it
// permits opportunistic firing on visible enemies *without* cancelling the
// MoveTo order. Phase 13 itself has no behavioural effect; the Inspector
// queued-orders chip surfaces an "AT" indicator (M13.6) so the player can
// see the flag is set.
type OrderParamAttackMove struct{}
