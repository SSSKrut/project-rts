package components

type WeaponKind uint16

const (
	WeaponAK47 WeaponKind = iota
	WeaponPKM
	WeaponSVD
	WeaponRPG7
	WeaponGP25
	WeaponMakarov
)

// Weapon - per-instance state for a weapon entity. Lives on its own entity
// (alongside OwnedBy + WorldPos) so a soldier can pass, drop, or pick up a
// weapon without touching the Unit archetype.
//
// RoF gates per-tick firing (one shot per 1/RoF seconds); Dispersion is the
// small-angle aiming spread in radians (multiplied by range to get lateral
// deflection at the target).
type Weapon struct {
	Kind        WeaponKind
	Ammo        uint16
	RangeM      float32
	RoF         float32
	Damage      uint16
	LastFiredAt float32 // session-time of the most recent shot; 0 = never fired
	Dispersion  float32 // radians; 0 = perfect aim
}

// Radio is the marker placed on the Equipment.Secondary entity of a
// RadioOperator. Read by RadioNetwork.HasRadioman gating.
type Radio struct{}

type Medkit struct{}

type Spade struct{}
