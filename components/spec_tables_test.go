package components

import "testing"

// A spec table is indexed by its enum, so a missing or misplaced row is a
// silent zero-value spec, not a compile error. Every row must name itself.
func TestWeaponSpecsAreSelfIdentifying(t *testing.T) {
	for k := WeaponKind(0); k < WeaponKindCount; k++ {
		spec := SpecForWeapon(k)
		if spec.Kind != k {
			t.Errorf("WeaponSpecs[%d].Kind = %d — row missing or misplaced", k, spec.Kind)
		}
		if spec.Name == "" {
			t.Errorf("WeaponSpecs[%d] has no name", k)
		}
	}
}

func TestWeaponSpecsCarrySaneNumbers(t *testing.T) {
	for k := WeaponKind(0); k < WeaponKindCount; k++ {
		s := SpecForWeapon(k)
		if s.RangeM <= 0 {
			t.Errorf("%s: range %.1f", s.Name, s.RangeM)
		}
		if s.RoF <= 0 {
			t.Errorf("%s: rate of fire %.2f", s.Name, s.RoF)
		}
		if s.Damage <= 0 {
			t.Errorf("%s: damage %d", s.Name, s.Damage)
		}
		if s.Ammo <= 0 {
			t.Errorf("%s: magazine %d", s.Name, s.Ammo)
		}
		if s.Dispersion < 0 {
			t.Errorf("%s: negative dispersion %.3f", s.Name, s.Dispersion)
		}
		if s.SplashRadius > 0 && s.SplashFalloff <= 0 {
			t.Errorf("%s: splash radius %.1f with falloff %.1f", s.Name, s.SplashRadius, s.SplashFalloff)
		}
	}
}

// pickTarget skips a class whose multiplier is ~0 and prefers the best one, so
// a weapon that can hurt nothing at all would simply never fire.
func TestEveryWeaponCanHurtSomething(t *testing.T) {
	for k := WeaponKind(0); k < WeaponKindCount; k++ {
		s := SpecForWeapon(k)
		// Air counts (Phase 20 M2): a MANPADS hurts nothing on the ground and
		// that is its entire identity.
		if s.VsSoft <= 0 && s.VsLight <= 0 && s.VsHeavy <= 0 && s.VsAir <= 0 {
			t.Errorf("%s has a zero multiplier against every target class", s.Name)
		}
		for name, mul := range map[string]float32{"soft": s.VsSoft, "light": s.VsLight, "heavy": s.VsHeavy, "air": s.VsAir} {
			if mul < 0 {
				t.Errorf("%s: negative %s multiplier %.2f", s.Name, name, mul)
			}
		}
	}
}

func TestSpecForWeaponFallsBackInsteadOfPanicking(t *testing.T) {
	if got := SpecForWeapon(WeaponKindCount); got.Kind != WeaponAK47 {
		t.Errorf("out-of-range kind returned %v, want the AK47 fallback", got.Kind)
	}
	if got := SpecForWeapon(WeaponKind(250)); got == nil {
		t.Error("a wild kind returned nil")
	}
}

func TestVehicleSpecsAreSelfIdentifying(t *testing.T) {
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		spec := SpecForVehicle(k)
		if spec.Kind != k {
			t.Errorf("VehicleSpecs[%d].Kind = %d — row missing or misplaced", k, spec.Kind)
		}
		if spec.Name == "" {
			t.Errorf("VehicleSpecs[%d] has no name", k)
		}
	}
}

func TestVehicleSpecsCarrySaneNumbers(t *testing.T) {
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		s := SpecForVehicle(k)
		if s.HP <= 0 {
			t.Errorf("%s: HP %.1f", s.Name, s.HP)
		}
		// An emplacement (Phase 20 M2) has ALL speeds at zero by design; what
		// stays a typo is a PARTIAL zero — a hull that drives forward but
		// cannot reverse is a config error, not a class.
		static := s.MaxSpeedRoad == 0 && s.MaxSpeedOffroad == 0 && s.MaxSpeedReverse == 0
		if !static && (s.MaxSpeedRoad <= 0 || s.MaxSpeedOffroad <= 0 || s.MaxSpeedReverse <= 0) {
			t.Errorf("%s: speeds road %.1f offroad %.1f reverse %.1f",
				s.Name, s.MaxSpeedRoad, s.MaxSpeedOffroad, s.MaxSpeedReverse)
		}
		// The driver divides by TurnRadiusM every tick.
		if s.TurnRadiusM <= 0 {
			t.Errorf("%s: turn radius %.1f", s.Name, s.TurnRadiusM)
		}
		if s.ColliderR <= 0 {
			t.Errorf("%s: collider radius %.1f", s.Name, s.ColliderR)
		}
		if s.BoxLen <= 0 || s.BoxWid <= 0 || s.BoxHgt <= 0 {
			t.Errorf("%s: box %.1f x %.1f x %.1f", s.Name, s.BoxLen, s.BoxWid, s.BoxHgt)
		}
	}
}

func TestVehicleRoadSpeedBeatsOffroad(t *testing.T) {
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		s := SpecForVehicle(k)
		if s.MaxSpeedRoad < s.MaxSpeedOffroad {
			t.Errorf("%s: road %.1f is slower than off-road %.1f — the road bias inverts",
				s.Name, s.MaxSpeedRoad, s.MaxSpeedOffroad)
		}
	}
}

// armorSectorMul reads these three; front must never be the softest face or
// flanking is a downgrade.
func TestVehicleArmourIsThickestAtTheFront(t *testing.T) {
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		s := SpecForVehicle(k)
		if s.ArmorFront <= 0 || s.ArmorSide <= 0 || s.ArmorRear <= 0 {
			t.Errorf("%s: armour %.2f / %.2f / %.2f — a zero multiplier makes a face immune",
				s.Name, s.ArmorFront, s.ArmorSide, s.ArmorRear)
			continue
		}
		// Multipliers scale incoming damage, so thicker armour = smaller number.
		if s.ArmorFront > s.ArmorSide || s.ArmorSide > s.ArmorRear {
			t.Errorf("%s: front %.2f side %.2f rear %.2f — not monotonic",
				s.Name, s.ArmorFront, s.ArmorSide, s.ArmorRear)
		}
	}
}

func TestVehicleLoadoutMatchesItsCount(t *testing.T) {
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		s := SpecForVehicle(k)
		if int(s.WeaponCount) > len(s.WeaponKinds) {
			t.Errorf("%s: WeaponCount %d exceeds the slot array", s.Name, s.WeaponCount)
			continue
		}
		for i := 0; i < int(s.WeaponCount); i++ {
			if s.WeaponKinds[i] >= WeaponKindCount {
				t.Errorf("%s: barrel %d is weapon kind %d, out of range", s.Name, i, s.WeaponKinds[i])
			}
		}
		// A turret that slews with nothing to aim, or barrels with no turret,
		// both read as an authoring slip.
		if s.TurretSlewDps > 0 && s.WeaponCount == 0 {
			t.Errorf("%s: slewing turret with no armament", s.Name)
		}
	}
}

func TestOrderKindSpecsAreSelfIdentifying(t *testing.T) {
	for c := OrderKindCode(0); c < OrderKindCount; c++ {
		spec := SpecForOrderKind(c)
		if spec.Code != c {
			t.Errorf("OrderKindSpecs[%d].Code = %d — row missing or misplaced", c, spec.Code)
		}
		if spec.Name == "" {
			t.Errorf("OrderKindSpecs[%d] has no name", c)
		}
	}
}

// The bundled raylib font has no Cyrillic glyphs: every player-visible string
// has to stay ASCII until an i18n pipeline lands.
func TestOrderLabelsAreASCII(t *testing.T) {
	for c := OrderKindCode(0); c < OrderKindCount; c++ {
		for _, r := range SpecForOrderKind(c).Name {
			if r > 127 {
				t.Errorf("order %d name %q holds a non-ASCII rune %q", c, SpecForOrderKind(c).Name, r)
				break
			}
		}
	}
}

func TestWeaponAndVehicleNamesAreASCII(t *testing.T) {
	for k := WeaponKind(0); k < WeaponKindCount; k++ {
		for _, r := range SpecForWeapon(k).Name {
			if r > 127 {
				t.Errorf("weapon %d name %q holds a non-ASCII rune %q", k, SpecForWeapon(k).Name, r)
				break
			}
		}
	}
	for k := VehicleKind(0); k < VehicleKindCount; k++ {
		for _, r := range SpecForVehicle(k).Name {
			if r > 127 {
				t.Errorf("vehicle %d name %q holds a non-ASCII rune %q", k, SpecForVehicle(k).Name, r)
				break
			}
		}
	}
}

func TestSpecForOrderKindFallsBackInsteadOfPanicking(t *testing.T) {
	if got := SpecForOrderKind(OrderKindCount); got.Code != OrderKindMoveTo {
		t.Errorf("out-of-range code returned %v, want the MoveTo fallback", got.Code)
	}
}

func TestSpecForVehicleFallsBackInsteadOfPanicking(t *testing.T) {
	if got := SpecForVehicle(VehicleKindCount); got.Kind != VehicleTruck {
		t.Errorf("out-of-range kind returned %v, want the Truck fallback", got.Kind)
	}
	if got := SpecForVehicle(VehicleKind(200)); got == nil {
		t.Error("a wild kind returned nil")
	}
}
