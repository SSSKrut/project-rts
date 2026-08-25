package components

type WeaponKind uint16

const (
	WeaponAK47 WeaponKind = iota
	WeaponPKM
	WeaponSVD
	WeaponRPG7
	WeaponGP25
	WeaponMakarov
	// Vehicle armament. Appended after infantry kinds so saved Weapon.Kind
	// values stay stable.
	WeaponCannon125
	WeaponAutocannon30
	WeaponKPVT
	WeaponATGM
	// Air defense (Phase 20 M2). ZU23 is the lead-computing gun; Igla spawns
	// a Missile entity instead of a hitscan shot.
	WeaponZU23
	WeaponIgla
	// Air-to-ground (Phase 20 M3). Vikhr is a Missile entity — the only way a
	// 1200 m weapon can exist while the raycast pipeline tops out at 192 m;
	// S8 is an ordinary rocket salvo through that same pipeline.
	WeaponVikhr
	WeaponS8
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

// Radio sits on a radioman's Equipment.Secondary entity, or directly on a
// hull / airframe that carries a built-in set. On is the player's dial: a
// working radio is an EMITTER and hands enemy ESM a bearing, so going quiet is
// a decision — the same bargain the radar offers, with the same two numbers.
type Radio struct {
	On         bool
	EmitRangeM float32
}

type Medkit struct{}

type Spade struct{}
