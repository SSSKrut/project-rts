package components

// HP is the per-Unit hit point pool. float32 so future armour / penetration /
// cover-shadow models can produce fractional damage without integer rounding
// artefacts.
//
// Orthogonal to Stamina: HP is the "alive" axis, Stamina is the "tired" axis.
type HP struct {
	Current float32
	Max     float32
}
