package components

import rl "github.com/gen2brain/raylib-go/raylib"

// WeaponSpec collapses the per-WeaponKind tables that previously lived in
// role_service.primaryStats and weapon.tracerColorFor, plus splash-damage
// knobs (SplashRadius / SplashFalloff).
type WeaponSpec struct {
	Kind        WeaponKind
	Name        string
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
	// Weapon-vs-class damage multipliers. Soft = infantry and unarmoured
	// vehicles; Light = APC-tier; Heavy = tanks. Gunners skip targets whose
	// multiplier is ~0 (a PKM never wastes ammo on a tank).
	VsSoft  float32
	VsLight float32
	VsHeavy float32
	// VsAir is the anti-air multiplier and is zero for every weapon in the
	// roster today, which is the point: an airframe entered Awareness in Phase
	// 20 M1 and would otherwise read as a soft target, so a hull would empty
	// its belt at a helicopter it cannot touch. M2 fills this in for the AA
	// gun and the MANPADS; until then the zero IS the gate.
	VsAir float32
}

const WeaponKindCount WeaponKind = WeaponATGM + 1

// WeaponSpecs - canonical table indexed by WeaponKind. Numbers are rough
// placeholders sized for 4-vs-8 firefights ending in 30-45 s at ~50 m.
var WeaponSpecs = [WeaponKindCount]WeaponSpec{
	WeaponAK47: {
		Kind: WeaponAK47, Name: "AK47",
		Ammo: 30, RangeM: 300, RoF: 4.0, Damage: 28, Dispersion: 0.03,
		TracerColor: rl.Color{R: 245, G: 245, B: 230, A: 255},
		VsSoft:      1.0, VsLight: 0.1, VsHeavy: 0,
	},
	WeaponPKM: {
		Kind: WeaponPKM, Name: "PKM",
		Ammo: 100, RangeM: 500, RoF: 8.0, Damage: 30, Dispersion: 0.05,
		TracerColor: rl.Color{R: 255, G: 220, B: 100, A: 255},
		VsSoft:      1.0, VsLight: 0.15, VsHeavy: 0,
	},
	WeaponSVD: {
		Kind: WeaponSVD, Name: "SVD",
		Ammo: 10, RangeM: 600, RoF: 0.5, Damage: 70, Dispersion: 0.005,
		TracerColor: rl.Color{R: 255, G: 90, B: 60, A: 255},
		VsSoft:      1.0, VsLight: 0.1, VsHeavy: 0,
	},
	WeaponRPG7: {
		Kind: WeaponRPG7, Name: "RPG7",
		Ammo: 3, RangeM: 200, RoF: 0.1, Damage: 200, Dispersion: 0.02,
		TracerColor:   rl.Color{R: 255, G: 130, B: 40, A: 255},
		SplashRadius:  3.5,
		SplashFalloff: 2.0,
		VsSoft:        0.5, VsLight: 1.2, VsHeavy: 1.0,
	},
	WeaponGP25: {
		Kind: WeaponGP25, Name: "GP25",
		Ammo: 8, RangeM: 150, RoF: 0.3, Damage: 50, Dispersion: 0.04,
		TracerColor:   rl.Color{R: 255, G: 160, B: 60, A: 255},
		SplashRadius:  2.5,
		SplashFalloff: 1.5,
		VsSoft:        1.0, VsLight: 0.3, VsHeavy: 0.05,
	},
	WeaponMakarov: {
		Kind: WeaponMakarov, Name: "Makarov",
		Ammo: 8, RangeM: 30, RoF: 3.0, Damage: 18, Dispersion: 0.06,
		TracerColor: rl.Color{R: 220, G: 220, B: 220, A: 255},
		VsSoft:      1.0, VsLight: 0, VsHeavy: 0,
	},
	// Vehicle armament. Ranges hit the weaponMaxRange clamp (192 m LOS
	// window) — real reach returns with the Maps LOS track.
	WeaponCannon125: {
		Kind: WeaponCannon125, Name: "125mm",
		Ammo: 40, RangeM: 1800, RoF: 0.12, Damage: 450, Dispersion: 0.008,
		TracerColor:   rl.Color{R: 255, G: 200, B: 120, A: 255},
		SplashRadius:  4.0,
		SplashFalloff: 2.0,
		VsSoft:        1.0, VsLight: 1.3, VsHeavy: 1.0,
	},
	WeaponAutocannon30: {
		Kind: WeaponAutocannon30, Name: "30mm AC",
		Ammo: 160, RangeM: 1200, RoF: 3.0, Damage: 60, Dispersion: 0.02,
		TracerColor: rl.Color{R: 255, G: 170, B: 80, A: 255},
		VsSoft:      0.8, VsLight: 1.0, VsHeavy: 0.15,
	},
	WeaponKPVT: {
		Kind: WeaponKPVT, Name: "KPVT",
		Ammo: 200, RangeM: 900, RoF: 6.0, Damage: 35, Dispersion: 0.04,
		TracerColor: rl.Color{R: 255, G: 230, B: 120, A: 255},
		VsSoft:      1.0, VsLight: 0.7, VsHeavy: 0.05,
	},
	WeaponATGM: {
		Kind: WeaponATGM, Name: "ATGM",
		Ammo: 4, RangeM: 1500, RoF: 0.07, Damage: 550, Dispersion: 0.004,
		TracerColor:   rl.Color{R: 255, G: 110, B: 90, A: 255},
		SplashRadius:  3.0,
		SplashFalloff: 2.0,
		VsSoft:        0.3, VsLight: 1.4, VsHeavy: 1.2,
	},
}

// Compile-time guard against WeaponKindCount drifting from table length.
var _ = [WeaponKindCount]WeaponSpec(WeaponSpecs)

func SpecForWeapon(kind WeaponKind) *WeaponSpec {
	if int(kind) >= len(WeaponSpecs) {
		return &WeaponSpecs[WeaponAK47]
	}
	return &WeaponSpecs[kind]
}
