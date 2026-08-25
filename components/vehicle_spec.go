package components

// ArmorClass buckets targets for the weapon-vs-class table. Infantry is
// implicitly Soft.
type ArmorClass uint8

const (
	ArmorClassSoft ArmorClass = iota
	ArmorClassLight
	ArmorClassHeavy
)

// VsClassMul returns the weapon's damage multiplier against `class`.
func VsClassMul(w *WeaponSpec, class ArmorClass) float32 {
	switch class {
	case ArmorClassLight:
		return w.VsLight
	case ArmorClassHeavy:
		return w.VsHeavy
	}
	return w.VsSoft
}

// VehicleSpec is the per-class data row. Armor* are damage
// multipliers by hit sector (1.0 = unarmoured). Speeds m/s, slew deg/s.
// WeaponKinds[:WeaponCount] is the factory loadout — slot 0 becomes
// Equipment.Primary (turret main), slot 1 Secondary (coax).
type VehicleSpec struct {
	Kind            VehicleKind
	Name            string
	HP              float32
	MaxSpeedRoad    float32
	MaxSpeedOffroad float32
	MaxSpeedReverse float32
	TurnRadiusM     float32
	SeatCount       uint8
	ArmorFront      float32
	ArmorSide       float32
	ArmorRear       float32
	TurretSlewDps   float32
	Locomotion      Locomotion
	ColliderR       float32
	BoxLen          float32
	BoxWid          float32
	BoxHgt          float32
	SensorRangeM    float32
	DetectMul       float32
	NoiseRadiusM    float32
	Class           ArmorClass
	ReflexKind      VehicleReflexKind
	WeaponCount     uint8
	WeaponKinds     [2]WeaponKind

	// Passive receiver (Phase 20 M1). A hull with a warning set hears an
	// airborne radar far beyond anything it can see; a truck has no such kit
	// and 0 means the channel is not fitted at all.
	ESMRangeM float32
	// Active radar (Phase 20 M2, AA assets). Same bargain as the airborne
	// set: sees to RadarRangeM, heard by any ESM out to RadarEmitM, and the
	// emit range must exceed the seeing range or the dilemma collapses (P4).
	RadarRangeM float32
	RadarEmitM  float32
	// RelayRangeM > 0 makes this class a node of the radio net (block A).
	RelayRangeM float32
}

// VehicleSpecs — canonical table indexed by VehicleKind. Numbers are rough
// Cold-War placeholders; balance later.
var VehicleSpecs = [VehicleKindCount]VehicleSpec{
	VehicleTruck: {
		Kind: VehicleTruck, Name: "Truck", HP: 120,
		MaxSpeedRoad: 16, MaxSpeedOffroad: 6, MaxSpeedReverse: 3, TurnRadiusM: 8,
		SeatCount: 12, ArmorFront: 1.0, ArmorSide: 1.0, ArmorRear: 1.0,
		TurretSlewDps: 0, Locomotion: LocomotionWheeled, ColliderR: 3.2,
		BoxLen: 7.0, BoxWid: 2.5, BoxHgt: 2.7,
		SensorRangeM: 50, DetectMul: 1.6, NoiseRadiusM: 120,
		Class: ArmorClassSoft, ReflexKind: VehicleReflexFlee,
	},
	VehicleBTR: {
		Kind: VehicleBTR, Name: "BTR", HP: 220,
		MaxSpeedRoad: 22, MaxSpeedOffroad: 9, MaxSpeedReverse: 4, TurnRadiusM: 7,
		SeatCount: 10, ArmorFront: 0.5, ArmorSide: 0.65, ArmorRear: 0.8,
		TurretSlewDps: 60, Locomotion: LocomotionWheeled, ColliderR: 3.4,
		BoxLen: 7.7, BoxWid: 2.9, BoxHgt: 2.4,
		SensorRangeM: 60, DetectMul: 1.5, NoiseRadiusM: 110,
		Class: ArmorClassLight, ReflexKind: VehicleReflexSmokeAndReverse,
		WeaponCount: 1, WeaponKinds: [2]WeaponKind{WeaponKPVT},
		ESMRangeM: 700,
	},
	VehicleBMP: {
		Kind: VehicleBMP, Name: "BMP", HP: 260,
		MaxSpeedRoad: 18, MaxSpeedOffroad: 11, MaxSpeedReverse: 4, TurnRadiusM: 5,
		SeatCount: 10, ArmorFront: 0.45, ArmorSide: 0.6, ArmorRear: 0.75,
		TurretSlewDps: 55, Locomotion: LocomotionTracked, ColliderR: 3.0,
		BoxLen: 6.7, BoxWid: 3.15, BoxHgt: 2.45,
		SensorRangeM: 60, DetectMul: 1.5, NoiseRadiusM: 130,
		Class: ArmorClassLight, ReflexKind: VehicleReflexSmokeAndReverse,
		WeaponCount: 1, WeaponKinds: [2]WeaponKind{WeaponAutocannon30},
		ESMRangeM: 700,
	},
	VehicleTank: {
		Kind: VehicleTank, Name: "Tank", HP: 500,
		MaxSpeedRoad: 17, MaxSpeedOffroad: 11, MaxSpeedReverse: 2.5, TurnRadiusM: 4,
		SeatCount: 3, ArmorFront: 0.15, ArmorSide: 0.35, ArmorRear: 0.6,
		TurretSlewDps: 40, Locomotion: LocomotionTracked, ColliderR: 3.2,
		BoxLen: 6.9, BoxWid: 3.6, BoxHgt: 2.2,
		SensorRangeM: 55, DetectMul: 1.7, NoiseRadiusM: 150,
		Class: ArmorClassHeavy, ReflexKind: VehicleReflexFaceThreat,
		WeaponCount: 2, WeaponKinds: [2]WeaponKind{WeaponCannon125, WeaponPKM},
		ESMRangeM: 800,
	},
	VehicleATCarrier: {
		Kind: VehicleATCarrier, Name: "AT Carrier", HP: 180,
		MaxSpeedRoad: 17, MaxSpeedOffroad: 9, MaxSpeedReverse: 3.5, TurnRadiusM: 6,
		SeatCount: 4, ArmorFront: 0.6, ArmorSide: 0.75, ArmorRear: 0.9,
		TurretSlewDps: 30, Locomotion: LocomotionWheeled, ColliderR: 2.6,
		BoxLen: 5.7, BoxWid: 2.35, BoxHgt: 2.3,
		SensorRangeM: 65, DetectMul: 1.4, NoiseRadiusM: 100,
		Class: ArmorClassLight, ReflexKind: VehicleReflexSmokeAndReverse,
		WeaponCount: 1, WeaponKinds: [2]WeaponKind{WeaponATGM},
		ESMRangeM: 900,
	},
	// Protected patrol vehicle: fast on a road, poor off it, a ring-mounted MG
	// and armour that stops rifle fire and nothing heavier.
	VehicleCar: {
		Kind: VehicleCar, Name: "Car", HP: 160,
		MaxSpeedRoad: 22, MaxSpeedOffroad: 8, MaxSpeedReverse: 4, TurnRadiusM: 6.5,
		SeatCount: 5, ArmorFront: 0.7, ArmorSide: 0.85, ArmorRear: 1.0,
		TurretSlewDps: 60, Locomotion: LocomotionWheeled, ColliderR: 2.4,
		BoxLen: 6.7, BoxWid: 3.0, BoxHgt: 2.6,
		SensorRangeM: 60, DetectMul: 1.5, NoiseRadiusM: 110,
		Class: ArmorClassSoft, ReflexKind: VehicleReflexSmokeAndReverse,
		WeaponCount: 1, WeaponKinds: [2]WeaponKind{WeaponPKM},
	},
	// Towed twin-23mm on a static mount: every speed is zero, the crew is in
	// the open (Soft, no sector armour), and the radar is the long arm — the
	// gun outranges the optics only while it is emitting. Its ESM hears an
	// airborne radar; its own radar is heard even further (P4 both ways).
	VehicleAAGun: {
		Kind: VehicleAAGun, Name: "AA Gun", HP: 140,
		MaxSpeedRoad: 0, MaxSpeedOffroad: 0, MaxSpeedReverse: 0, TurnRadiusM: 1,
		SeatCount: 1, ArmorFront: 1.0, ArmorSide: 1.0, ArmorRear: 1.0,
		TurretSlewDps: 120, Locomotion: LocomotionWheeled, ColliderR: 1.6,
		BoxLen: 2.6, BoxWid: 2.2, BoxHgt: 2.4,
		SensorRangeM: 200, DetectMul: 1.2, NoiseRadiusM: 40,
		Class: ArmorClassSoft, ReflexKind: VehicleReflexGoDark,
		WeaponCount: 1, WeaponKinds: [2]WeaponKind{WeaponZU23},
		ESMRangeM: 900, RadarRangeM: 520, RadarEmitM: 1300,
	},
	VehicleCommand: {
		Kind: VehicleCommand, Name: "Command", HP: 160,
		MaxSpeedRoad: 20, MaxSpeedOffroad: 8, MaxSpeedReverse: 4, TurnRadiusM: 7,
		SeatCount: 4, ArmorFront: 0.8, ArmorSide: 0.9, ArmorRear: 1.0,
		TurretSlewDps: 0, Locomotion: LocomotionWheeled, ColliderR: 3.2,
		BoxLen: 6.8, BoxWid: 2.6, BoxHgt: 2.8,
		SensorRangeM: 70, DetectMul: 1.5, NoiseRadiusM: 100,
		Class: ArmorClassSoft, ReflexKind: VehicleReflexFlee,
		ESMRangeM: 700, RelayRangeM: RelayRangeCommandM,
	},
}

// Compile-time exhaustiveness: adding a VehicleKind without a row fails here.
var _ = [VehicleKindCount]VehicleSpec(VehicleSpecs)

func SpecForVehicle(kind VehicleKind) *VehicleSpec {
	if kind >= VehicleKindCount {
		kind = VehicleTruck
	}
	return &VehicleSpecs[kind]
}
