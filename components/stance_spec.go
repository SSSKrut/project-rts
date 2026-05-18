package components

// Phase 14.5 P3 — StanceSpec collapses sibling tables that previously lived
// in different packages but all keyed by StanceCode:
//   - systems/unit_movement.go::unitMaxSpeed
//   - render_world.go::unitStanceHeight
//   - systems/weapon.go::stanceDamageMul
//   - systems/weapon.go::weaponTargetY
//   - ui/inspector.go::stanceLabel
//
// All readers index this table by StanceCode. Adding a new stance code is a
// single row append plus bumping StanceCount.

// StanceSpec — per-stance behavioural + visual parameters.
type StanceSpec struct {
	Code             StanceCode
	Name             string  // "Stand" / "Crouch" / "Prone".
	MaxSpeed         float32 // m/s at Walk pace.
	BodyHeight       float32 // render cube height (m).
	DamageMultiplier float32 // hit silhouette factor (smaller stance → less damage).
	TargetCenterY    float32 // weapon aim point Y above foot for opposing shooters.
}

// StanceCount — number of stance values. Update if StanceCode grows.
const StanceCount StanceCode = StanceProne + 1

// StanceSpecs — canonical table. Index by StanceCode.
var StanceSpecs = [StanceCount]StanceSpec{
	StanceStand: {
		Code: StanceStand, Name: "Stand",
		MaxSpeed: 5.0, BodyHeight: 1.8,
		DamageMultiplier: 1.0, TargetCenterY: 0.9,
	},
	StanceCrouch: {
		Code: StanceCrouch, Name: "Crouch",
		MaxSpeed: 3.0, BodyHeight: 1.1,
		DamageMultiplier: 0.8, TargetCenterY: 0.55,
	},
	StanceProne: {
		Code: StanceProne, Name: "Prone",
		MaxSpeed: 1.5, BodyHeight: 0.4,
		DamageMultiplier: 0.5, TargetCenterY: 0.25,
	},
}

// Compile-time guard against StanceCount drifting from table length.
var _ = [StanceCount]StanceSpec(StanceSpecs)

// SpecForStance returns a pointer into the table. Defensive fallback to
// StanceStand if code is out of range (should not happen at runtime).
func SpecForStance(code StanceCode) *StanceSpec {
	if int(code) >= len(StanceSpecs) {
		return &StanceSpecs[StanceStand]
	}
	return &StanceSpecs[code]
}
