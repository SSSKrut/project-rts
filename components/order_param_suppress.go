package components

// OrderParamSuppress is an **optional** component on an Order entity. Present
// only on OrderKindSuppressFire orders that the player issued with explicit
// area / ammo / timer parameters. Missing values fall back to the WeaponSystem
// defaults documented in PHASE-14.md M14.4 ("simple 30 sec engagement").
//
// Phase 14 ships timer-based completion only - AmmoCap is written here for
// forward-compat (Phase 21 UI will surface a progress bar), but the resolver
// does not yet sum per-shot ammo consumption against it.
type OrderParamSuppress struct {
	// Radius of the suppression sector (metres) around OrderTarget.Pos.
	// Phase 14 simple: 8 m default. WeaponSystem reader lands when fire-on-
	// area (vs fire-on-entity) selection is wired in M14.4.
	Radius float32
	// AmmoCap - soft cap on the total rounds the squad will spend on this
	// order. Phase 14 scaffold; no reader yet.
	AmmoCap uint16
	// StartTime - session-time when the order was issued. Resolver
	// completes the order when (clock - StartTime) > suppressDuration.
	StartTime float32
}
