package components

import rl "github.com/gen2brain/raylib-go/raylib"

// WeaponSpec collapses the per-WeaponKind tables that previously lived in
// role_service.primaryStats and weapon.tracerColorFor, plus splash-damage
// knobs (SplashRadius / SplashFalloff).
// WeaponRelease says who pulls the trigger. The zero value MUST be Auto: the
// day this field landed, every existing barrel had to keep firing on its own,
// and a zero that meant "waits for the player" would have struck the whole
// roster mute (the Sensors.OffMask lesson). A manual barrel is invisible to
// pickTarget — no RoE check reaches it, which is what makes "hold fire" mean
// "silence the AI's weapons" while the player's missile still works.
type WeaponRelease uint8

const (
	ReleaseAuto WeaponRelease = iota
	ReleaseManual
)

type WeaponSpec struct {
	Kind        WeaponKind
	Release     WeaponRelease
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
	// VsAir is the anti-air multiplier; zero (the entire ground roster) keeps
	// a weapon out of the air fight entirely — the zero IS the gate. Phase 20
	// M2 fills it in for the AA gun and the MANPADS.
	VsAir float32
	// ProjSpeedM > 0 marks a lead-computing gun (Phase 20 M2): air shots
	// resolve analytically — intercept point from target velocity, hit
	// probability falling with range and angular rate — instead of raycast.
	ProjSpeedM float32
	// MissileSpeedM > 0 makes firing spawn a Missile entity instead of a
	// shot. Turn rate and fuel bound the chase; a hard-turning target at
	// close range beats the missile with geometry, not dice.
	MissileSpeedM  float32
	MissileTurnDps float32
	MissileFuelS   float32
}

const WeaponKindCount WeaponKind = WeaponS8 + 1

// WeaponSpecs - canonical table indexed by WeaponKind.
//
// A rifle's VsLight is ZERO, not "small" (2026-08-26 balance pass). Eight
// rifles at 4 rounds/s is 32 hits a second, and 0.1 of a rifle bullet times
// that is 90 damage/s — a squad shredding a BMP with small arms. Zero is also
// the target gate (pickTarget skips multipliers under 0.05), so riflemen now
// ignore hulls entirely and armour is the AT gunner's job. Measured by
// lite_balance_open / _ambush; change one, re-run both.
var WeaponSpecs = [WeaponKindCount]WeaponSpec{
	WeaponAK47: {
		Kind: WeaponAK47, Name: "AK47",
		Ammo: 30, RangeM: 300, RoF: 4.0, Damage: 28, Dispersion: 0.03,
		TracerColor: rl.Color{R: 245, G: 245, B: 230, A: 255},
		VsSoft:      1.0, VsLight: 0, VsHeavy: 0,
	},
	WeaponPKM: {
		Kind: WeaponPKM, Name: "PKM",
		Ammo: 100, RangeM: 500, RoF: 8.0, Damage: 30, Dispersion: 0.05,
		TracerColor: rl.Color{R: 255, G: 220, B: 100, A: 255},
		VsSoft:      1.0, VsLight: 0, VsHeavy: 0,
	},
	WeaponSVD: {
		Kind: WeaponSVD, Name: "SVD",
		Ammo: 10, RangeM: 600, RoF: 0.5, Damage: 70, Dispersion: 0.005,
		TracerColor: rl.Color{R: 255, G: 90, B: 60, A: 255},
		VsSoft:      1.0, VsLight: 0, VsHeavy: 0,
	},
	WeaponRPG7: {
		Kind: WeaponRPG7, Name: "RPG7",
		Ammo: 3, RangeM: 200, RoF: 0.2, Damage: 200, Dispersion: 0.02,
		TracerColor:   rl.Color{R: 255, G: 130, B: 40, A: 255},
		SplashRadius:  3.5,
		SplashFalloff: 2.0,
		VsSoft:        0.35, VsLight: 1.6, VsHeavy: 1.0,
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
	// Air defense (Phase 20 M2). The ZU23's RangeM is inside its host's radar
	// reach (520) and beyond a heli's optics (140): a dark helicopter is shot
	// before it sees the gun, an emitting one hears the gun's radar first.
	WeaponZU23: {
		Kind: WeaponZU23, Name: "ZU-23",
		Ammo: 400, RangeM: 380, RoF: 8.0, Damage: 22, Dispersion: 0.03,
		TracerColor: rl.Color{R: 255, G: 120, B: 90, A: 255},
		VsSoft:      0.4, VsLight: 0.25, VsHeavy: 0,
		VsAir:       1.0, ProjSpeedM: 600,
	},
	WeaponIgla: {
		Kind: WeaponIgla, Name: "Igla",
		Ammo: 3, RangeM: 900, RoF: 0.12, Damage: 200, Dispersion: 0,
		TracerColor: rl.Color{R: 255, G: 240, B: 200, A: 255},
		VsAir:       1.0,
		MissileSpeedM: 180, MissileTurnDps: 120, MissileFuelS: 7,
	},
	// Air-to-ground (Phase 20 M3). Vikhr reaches 1200 m because a missile
	// entity needs no LOS window; S8 states 190 m because a raycast weapon
	// cannot exceed weaponMaxRange, and a spec card that claims otherwise is
	// the silent ceiling all over again. That gap IS the loadout trade: stand
	// off with eight rounds, or come inside the ZU-23 with forty.
	WeaponVikhr: {
		Kind: WeaponVikhr, Name: "Vikhr", Release: ReleaseManual,
		Ammo: 8, RangeM: 1200, RoF: 0.15, Damage: 500, Dispersion: 0,
		TracerColor: rl.Color{R: 255, G: 150, B: 110, A: 255},
		VsSoft:      0.4, VsLight: 1.5, VsHeavy: 1.3,
		MissileSpeedM: 300, MissileTurnDps: 55, MissileFuelS: 6,
	},
	WeaponS8: {
		Kind: WeaponS8, Name: "S-8 rockets", Release: ReleaseManual,
		Ammo: 40, RangeM: 190, RoF: 2.5, Damage: 90, Dispersion: 0.05,
		TracerColor:   rl.Color{R: 255, G: 180, B: 90, A: 255},
		SplashRadius:  5.0,
		SplashFalloff: 1.5,
		VsSoft:        1.0, VsLight: 0.9, VsHeavy: 0.3,
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
