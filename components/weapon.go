package components

// WeaponKind enumerates available weapon archetypes. Phase 7 ships one
// placeholder (AK47); Phase 11 expands once ballistics + damage tables land.
type WeaponKind uint16

const (
	WeaponAK47 WeaponKind = iota
)

// Weapon — per-instance state for a weapon entity. Sits on a separate entity
// (alongside OwnedBy + WorldPos) so a soldier can pass, drop, or pick up a
// weapon without touching the Unit archetype. Phase 7 wires only Kind +
// ownership; ballistic fields are populated for forward-compat, no system
// reads them yet.
type Weapon struct {
	Kind   WeaponKind
	Ammo   uint16
	RangeM float32
	RoF    float32
	Damage uint16
}
