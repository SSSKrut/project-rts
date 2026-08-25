package components

// AircraftSpec is the per-class data row, same shape of contract as
// VehicleSpec: behaviour code is shared, classes differ only by numbers.
// Speeds m/s, rates deg/s or m/s, distances metres.
//
// TurnRadiusM is the radius in level cruise; PivotDps is the yaw authority
// that survives at a hover, where a radius means nothing. Both matter: a
// gunship swinging its nose onto a target while stationary is the same
// manoeuvre a tracked hull makes, and one number cannot express both.
type AircraftSpec struct {
	Kind          AircraftKind
	Name          string
	HP            float32
	MaxSpeed      float32
	CruiseSpeed   float32
	TurnRadiusM   float32
	PivotDps      float32
	AccelMs2      float32
	ClimbRateMs   float32
	DefaultAltAGL float32
	CeilingM      float32
	FuelSec       float32
	SeatCount     uint8
	ColliderR     float32
	BoxLen        float32
	BoxWid        float32
	BoxHgt        float32
	SensorRangeM  float32
	DetectMul     float32
	NoiseRadiusM  float32
	Class         ArmorClass

	// The emissions triad (P4). RadarRangeM is what the set sees when it is
	// running; RadarEmitM is how far away it is heard doing so, and is larger
	// on purpose. ESMRangeM is this airframe's own receiver — the passive half
	// that makes the enemy's choice cost him too. Zero means "no such kit".
	RadarRangeM float32
	RadarEmitM  float32
	ESMRangeM   float32

	// Loadouts are chosen when the task is set, before the airframe exists
	// (P9) — AirArrival.Loadout indexes this. A class with none flies unarmed.
	Loadouts     [3]AircraftLoadout
	LoadoutCount uint8
}

// AircraftLoadout is one armament option: up to two barrels, same shape as
// VehicleSpec.WeaponKinds. Slot 0 becomes Equipment.Primary + Active.
type AircraftLoadout struct {
	Name        string
	WeaponKinds [2]WeaponKind
	WeaponCount uint8
}

// AircraftSpecs — canonical table indexed by AircraftKind. Cold-War rotary
// placeholders; balance later.
var AircraftSpecs = [AircraftKindCount]AircraftSpec{
	AircraftHeliAttack: {
		Kind: AircraftHeliAttack, Name: "Gunship", HP: 300,
		MaxSpeed: 78, CruiseSpeed: 55, TurnRadiusM: 90, PivotDps: 45,
		AccelMs2: 4.5, ClimbRateMs: 9, DefaultAltAGL: 30, CeilingM: 1800,
		FuelSec: 900, SeatCount: 2, ColliderR: 6.0,
		BoxLen: 13.4, BoxWid: 3.6, BoxHgt: 4.5,
		SensorRangeM: 140, DetectMul: 2.2, NoiseRadiusM: 400,
		Class:       ArmorClassLight,
		RadarRangeM: 420, RadarEmitM: 1100, ESMRangeM: 1400,
		LoadoutCount: 3,
		Loadouts: [3]AircraftLoadout{
			{Name: "ATGM", WeaponCount: 2,
				WeaponKinds: [2]WeaponKind{WeaponVikhr, WeaponAutocannon30}},
			{Name: "Rockets", WeaponCount: 2,
				WeaponKinds: [2]WeaponKind{WeaponS8, WeaponAutocannon30}},
			{Name: "Guns", WeaponCount: 1,
				WeaponKinds: [2]WeaponKind{WeaponAutocannon30}},
		},
	},
	AircraftHeliTransport: {
		Kind: AircraftHeliTransport, Name: "Transport Heli", HP: 260,
		MaxSpeed: 70, CruiseSpeed: 50, TurnRadiusM: 110, PivotDps: 35,
		AccelMs2: 3.5, ClimbRateMs: 7, DefaultAltAGL: 60, CeilingM: 1600,
		FuelSec: 1100, SeatCount: 10, ColliderR: 7.0,
		BoxLen: 25.8, BoxWid: 5.2, BoxHgt: 6.4,
		SensorRangeM: 110, DetectMul: 2.4, NoiseRadiusM: 520,
		Class:     ArmorClassSoft,
		ESMRangeM: 900,
	},
}

// Compile-time exhaustiveness: adding an AircraftKind without a row fails here.
var _ = [AircraftKindCount]AircraftSpec(AircraftSpecs)

// LoadoutFor clamps an arrival's loadout index to what the class actually
// carries; an unarmed class returns the zero loadout and spawns no barrels.
func LoadoutFor(spec *AircraftSpec, idx uint8) AircraftLoadout {
	if spec == nil || spec.LoadoutCount == 0 {
		return AircraftLoadout{}
	}
	if idx >= spec.LoadoutCount {
		idx = 0
	}
	return spec.Loadouts[idx]
}

func SpecForAircraft(kind AircraftKind) *AircraftSpec {
	if kind >= AircraftKindCount {
		kind = AircraftHeliAttack
	}
	return &AircraftSpecs[kind]
}

// AircraftAssetName binds a class and a side to a baked model, mirroring
// VehicleAssetName. Empty means "no model" and the renderer draws its box.
var AircraftAssetName = [AircraftKindCount][AssetSideCount]string{
	AircraftHeliAttack:    {SideWest: "gunship_w", SideEast: "gunship_e"},
	AircraftHeliTransport: {SideWest: "heli_w", SideEast: "heli_e"},
}
