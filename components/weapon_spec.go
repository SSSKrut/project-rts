package components

import rl "github.com/gen2brain/raylib-go/raylib"

// WeaponSpec collapses the per-WeaponKind tables that previously lived in
// role_service.primaryStats and weapon.tracerColorFor, plus splash-damage
// knobs (SplashRadius / SplashFalloff).
//
// Readers index by WeaponKind. RoleService.spawnPrimary reads the row to
// construct a Weapon component; WeaponSystem.resolveShot reads tracer color
// + splash flags directly.

// WeaponSpec - per-weapon stats + visual + AoE parameters.
type WeaponSpec struct {
	Kind        WeaponKind
	Name        string  // "AK47" / "PKM" / ...
	Ammo        uint16  // default magazine capacity
	RangeM      float32 // effective range in metres
	RoF         float32 // rounds per second
	Damage      uint16  // per-shot damage HP
	Dispersion  float32 // small-angle radians (lateral spread at target)
	TracerColor rl.Color
	// SplashRadius > 0 turns the shot into an AoE damage event with linear
	// or quadratic falloff. RPG7 / GP25 set this.
	SplashRadius float32
	// SplashFalloff - exponent on the (1 - dSq/radiusSq) term. 1.0 = linear,
	// 2.0 = quadratic. Only meaningful when SplashRadius > 0.
	SplashFalloff float32
}

const WeaponKindCount WeaponKind = WeaponMakarov + 1

// WeaponSpecs - canonical table indexed by WeaponKind. Numbers are rough
// placeholders sized for 4-vs-8 firefights ending in 30-45 s at ~50 m.
var WeaponSpecs = [WeaponKindCount]WeaponSpec{
	WeaponAK47: {
		Kind: WeaponAK47, Name: "AK47",
		Ammo: 30, RangeM: 300, RoF: 4.0, Damage: 28, Dispersion: 0.03,
		TracerColor: rl.Color{R: 245, G: 245, B: 230, A: 255},
	},
	WeaponPKM: {
		Kind: WeaponPKM, Name: "PKM",
		Ammo: 100, RangeM: 500, RoF: 8.0, Damage: 30, Dispersion: 0.05,
		TracerColor: rl.Color{R: 255, G: 220, B: 100, A: 255},
	},
	WeaponSVD: {
		Kind: WeaponSVD, Name: "SVD",
		Ammo: 10, RangeM: 600, RoF: 0.5, Damage: 70, Dispersion: 0.005,
		TracerColor: rl.Color{R: 255, G: 90, B: 60, A: 255},
	},
	WeaponRPG7: {
		Kind: WeaponRPG7, Name: "RPG7",
		Ammo: 3, RangeM: 200, RoF: 0.1, Damage: 200, Dispersion: 0.02,
		TracerColor:   rl.Color{R: 255, G: 130, B: 40, A: 255},
		SplashRadius:  3.5,
		SplashFalloff: 2.0,
	},
	WeaponGP25: {
		Kind: WeaponGP25, Name: "GP25",
		Ammo: 8, RangeM: 150, RoF: 0.3, Damage: 50, Dispersion: 0.04,
		TracerColor:   rl.Color{R: 255, G: 160, B: 60, A: 255},
		SplashRadius:  2.5,
		SplashFalloff: 1.5,
	},
	WeaponMakarov: {
		Kind: WeaponMakarov, Name: "Makarov",
		Ammo: 8, RangeM: 30, RoF: 3.0, Damage: 18, Dispersion: 0.06,
		TracerColor: rl.Color{R: 220, G: 220, B: 220, A: 255},
	},
}

// Compile-time guard against WeaponKindCount drifting from table length.
var _ = [WeaponKindCount]WeaponSpec(WeaponSpecs)

// SpecForWeapon returns a pointer into the table. Defensive fallback to
// WeaponAK47 if kind is out of range.
func SpecForWeapon(kind WeaponKind) *WeaponSpec {
	if int(kind) >= len(WeaponSpecs) {
		return &WeaponSpecs[WeaponAK47]
	}
	return &WeaponSpecs[kind]
}
