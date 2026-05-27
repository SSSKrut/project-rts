package components

// OrderParamSuppress is an optional component on an Order entity. Present
// only on OrderKindSuppressFire orders that the player issued with explicit
// area / ammo / timer parameters.
type OrderParamSuppress struct {
	// Radius of the suppression sector (metres) around OrderTarget.Pos.
	Radius float32
	// AmmoCap - soft cap on the total rounds the squad will spend on this
	// order. No reader yet.
	AmmoCap uint16
	// StartTime - session-time when the order was issued. Resolver
	// completes the order when (clock - StartTime) > suppressDuration.
	StartTime float32
}
