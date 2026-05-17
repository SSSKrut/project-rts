package components

// HP — per-Unit hit point pool. Phase 14 P1: float32 Current / Max so future
// armour / penetration / cover-shadow models (Phase 15 / 24) can produce
// fractional damage without integer rounding artefacts.
//
// Writers: RoleService.AssignRole sets Max from the per-role table and seeds
// Current=Max on first assignment. DamageSystem.Apply decrements Current;
// when Current <= 0 the unit is despawned (Phase 14 simplification —
// wounded state / corpses / lootable equipment land in Phase 15 / 25).
//
// HP is orthogonal to Stamina: HP is the "alive" axis, Stamina is the
// "tired" axis — two independent rows in the Inspector.
type HP struct {
	Current float32
	Max     float32
}
