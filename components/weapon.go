package components

// WeaponKind enumerates available weapon archetypes. Phase 7 shipped one
// placeholder (AK47); Phase 12 expands with stubs for the rest of the role
// loadout (no combat logic — Phase 14 fills in real ballistic numbers).
type WeaponKind uint16

const (
	WeaponAK47 WeaponKind = iota
	WeaponPKM
	WeaponSVD
	WeaponRPG7
	WeaponGP25
	WeaponMakarov
)

// Weapon — per-instance state for a weapon entity. Sits on a separate entity
// (alongside OwnedBy + WorldPos) so a soldier can pass, drop, or pick up a
// weapon without touching the Unit archetype.
//
// Phase 14 M14.2 fields: LastFiredAt + Dispersion. RoF gates per-tick firing
// (one shot per 1/RoF seconds); Dispersion is the small-angle aiming spread
// in radians (multiplied by range to get lateral deflection at the target).
type Weapon struct {
	Kind        WeaponKind
	Ammo        uint16
	RangeM      float32
	RoF         float32
	Damage      uint16
	LastFiredAt float32 // session-time of the most recent shot; 0 = never fired
	Dispersion  float32 // radians (small-angle); 0 = perfect aim
}

// Radio is a marker placed on the Equipment.Secondary entity of a
// RadioOperator. Phase 20 (Comms) will read it for RadioNetwork.HasRadioman
// gating; Phase 12 only makes the marker exist so hasRadiomanInRoster can
// stop returning false.
type Radio struct{}

// Medkit is a marker placed on the Equipment.Secondary entity of a Medic.
// Phase 14 / 25 will give it actual heal mechanics.
type Medkit struct{}

// Spade is a marker placed on the Equipment.Secondary entity of an Engineer
// / DemoMan. Phase 18 (Engineering) will give it build-speed semantics.
type Spade struct{}
