package components

// VehicleSpec is the per-class data row (PHASE-19 P3). Armor* are damage
// multipliers by hit sector (1.0 = unarmoured). Speeds m/s, slew deg/s.
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
	},
	VehicleBTR: {
		Kind: VehicleBTR, Name: "BTR", HP: 220,
		MaxSpeedRoad: 22, MaxSpeedOffroad: 9, MaxSpeedReverse: 4, TurnRadiusM: 7,
		SeatCount: 10, ArmorFront: 0.5, ArmorSide: 0.65, ArmorRear: 0.8,
		TurretSlewDps: 60, Locomotion: LocomotionWheeled, ColliderR: 3.4,
		BoxLen: 7.7, BoxWid: 2.9, BoxHgt: 2.4,
		SensorRangeM: 60, DetectMul: 1.5, NoiseRadiusM: 110,
	},
	VehicleBMP: {
		Kind: VehicleBMP, Name: "BMP", HP: 260,
		MaxSpeedRoad: 18, MaxSpeedOffroad: 11, MaxSpeedReverse: 4, TurnRadiusM: 5,
		SeatCount: 10, ArmorFront: 0.45, ArmorSide: 0.6, ArmorRear: 0.75,
		TurretSlewDps: 55, Locomotion: LocomotionTracked, ColliderR: 3.0,
		BoxLen: 6.7, BoxWid: 3.15, BoxHgt: 2.45,
		SensorRangeM: 60, DetectMul: 1.5, NoiseRadiusM: 130,
	},
	VehicleTank: {
		Kind: VehicleTank, Name: "Tank", HP: 500,
		MaxSpeedRoad: 17, MaxSpeedOffroad: 11, MaxSpeedReverse: 2.5, TurnRadiusM: 4,
		SeatCount: 3, ArmorFront: 0.15, ArmorSide: 0.35, ArmorRear: 0.6,
		TurretSlewDps: 40, Locomotion: LocomotionTracked, ColliderR: 3.2,
		BoxLen: 6.9, BoxWid: 3.6, BoxHgt: 2.2,
		SensorRangeM: 55, DetectMul: 1.7, NoiseRadiusM: 150,
	},
	VehicleATCarrier: {
		Kind: VehicleATCarrier, Name: "AT Carrier", HP: 180,
		MaxSpeedRoad: 17, MaxSpeedOffroad: 9, MaxSpeedReverse: 3.5, TurnRadiusM: 6,
		SeatCount: 4, ArmorFront: 0.6, ArmorSide: 0.75, ArmorRear: 0.9,
		TurretSlewDps: 30, Locomotion: LocomotionWheeled, ColliderR: 2.6,
		BoxLen: 5.7, BoxWid: 2.35, BoxHgt: 2.3,
		SensorRangeM: 65, DetectMul: 1.4, NoiseRadiusM: 100,
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
