package components

// HP is the per-Unit hit point pool. float32 Current / Max so future armour /
// penetration / cover-shadow models can produce fractional damage without
// integer rounding artefacts.
//
// Writers: RoleService.AssignRole sets Max from the per-role table and seeds
// Current=Max on first assignment. DamageService.Apply decrements Current;
// when Current <= 0 the unit is despawned.
//
// Orthogonal to Stamina: HP is the "alive" axis, Stamina is the "tired" axis.
type HP struct {
	Current float32
	Max     float32
}
