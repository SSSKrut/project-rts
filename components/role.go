package components

import rl "github.com/gen2brain/raylib-go/raylib"

// UnitRoleKind enumerates the base infantry roles. RoleRifleman = 0 is the
// zero-value fallback so a unit missing UnitRole defaults to plain rifleman
// behaviour.
type UnitRoleKind uint8

const (
	RoleRifleman UnitRoleKind = iota
	RoleLeader
	RoleMachineGunner
	RoleGrenadier
	RoleSniper
	RoleATGunner
	RoleMedic
	RoleRadioOperator
	RoleEngineer
	RoleDemoMan
)

// UnitRole stamps a soldier with their role. Resolved at spawn by
// RoleService.AssignRole and persists across squad merges / splits.
type UnitRole struct {
	Kind UnitRoleKind
}

func (k UnitRoleKind) String() string {
	switch k {
	case RoleLeader:
		return "Leader"
	case RoleMachineGunner:
		return "MachineGunner"
	case RoleGrenadier:
		return "Grenadier"
	case RoleSniper:
		return "Sniper"
	case RoleATGunner:
		return "ATGunner"
	case RoleMedic:
		return "Medic"
	case RoleRadioOperator:
		return "RadioOperator"
	case RoleEngineer:
		return "Engineer"
	case RoleDemoMan:
		return "DemoMan"
	default:
		return "Rifleman"
	}
}

// ShortLabel is the 1-2 character abbreviation used by the 3D billboard
// label, the Inspector roster, and the map commander icon (NATO style:
// L / R / MG / GL / SN / AT / MD / RO / EN / DM).
func (k UnitRoleKind) ShortLabel() string {
	switch k {
	case RoleLeader:
		return "L"
	case RoleMachineGunner:
		return "MG"
	case RoleGrenadier:
		return "GL"
	case RoleSniper:
		return "SN"
	case RoleATGunner:
		return "AT"
	case RoleMedic:
		return "MD"
	case RoleRadioOperator:
		return "RO"
	case RoleEngineer:
		return "EN"
	case RoleDemoMan:
		return "DM"
	default:
		return "R"
	}
}

// RoleColor returns the cap / tint colour. Read by render_world (cap cube),
// Inspector (roster-row tint), map_render (commander label).
func RoleColor(k UnitRoleKind) rl.Color {
	switch k {
	case RoleLeader:
		return rl.Color{R: 255, G: 220, B: 60, A: 255}
	case RoleMachineGunner:
		return rl.Color{R: 200, G: 50, B: 50, A: 255}
	case RoleGrenadier:
		return rl.Color{R: 230, G: 130, B: 50, A: 255}
	case RoleSniper:
		return rl.Color{R: 40, G: 100, B: 40, A: 255}
	case RoleATGunner:
		return rl.Color{R: 140, G: 30, B: 30, A: 255}
	case RoleMedic:
		return rl.Color{R: 240, G: 240, B: 240, A: 255}
	case RoleRadioOperator:
		return rl.Color{R: 60, G: 100, B: 200, A: 255}
	case RoleEngineer:
		return rl.Color{R: 130, G: 100, B: 50, A: 255}
	case RoleDemoMan:
		return rl.Color{R: 100, G: 100, B: 100, A: 255}
	default:
		return rl.Color{R: 80, G: 95, B: 55, A: 255}
	}
}
