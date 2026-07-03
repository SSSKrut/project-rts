package components

// StanceSpec collapses sibling tables that all key by StanceCode:
// unit_movement.unitMaxSpeed, render_world.unitStanceHeight,
// weapon.stanceDamageMul, weapon.weaponTargetY, inspector.stanceLabel.
// Adding a new stance code is a single row append + bumping StanceCount.
type StanceSpec struct {
	Code             StanceCode
	Name             string
	MaxSpeed         float32 // m/s at Walk pace
	BodyHeight       float32 // render cube height (m)
	DamageMultiplier float32 // hit silhouette factor (smaller stance -> less damage)
	TargetCenterY    float32 // weapon aim point Y above foot for opposing shooters
	EyeHeight        float32 // sensor eye Y above foot (terrain-LOS origin)
}

const StanceCount StanceCode = StanceProne + 1

var StanceSpecs = [StanceCount]StanceSpec{
	StanceStand: {
		Code: StanceStand, Name: "Stand",
		MaxSpeed: 5.0, BodyHeight: 1.8,
		DamageMultiplier: 1.0, TargetCenterY: 0.9, EyeHeight: 1.5,
	},
	StanceCrouch: {
		Code: StanceCrouch, Name: "Crouch",
		MaxSpeed: 3.0, BodyHeight: 1.1,
		DamageMultiplier: 0.8, TargetCenterY: 0.55, EyeHeight: 1.0,
	},
	StanceProne: {
		Code: StanceProne, Name: "Prone",
		MaxSpeed: 1.5, BodyHeight: 0.4,
		DamageMultiplier: 0.5, TargetCenterY: 0.25, EyeHeight: 0.35,
	},
}

// Compile-time guard against StanceCount drifting from table length.
var _ = [StanceCount]StanceSpec(StanceSpecs)

func SpecForStance(code StanceCode) *StanceSpec {
	if int(code) >= len(StanceSpecs) {
		return &StanceSpecs[StanceStand]
	}
	return &StanceSpecs[code]
}
