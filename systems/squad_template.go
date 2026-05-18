package systems

import "rts-go/components"

// SquadTemplate enumerates the 7 starter rosters available to
// SquadService.CreateFromTemplate. The template is convenience only - it
// shapes the spawn-time role distribution, then disappears (no SquadTemplate
// component lives on the resulting Squad). Phase 13 / 15 may add a separate
// Doctrine concept on top.
type SquadTemplate uint8

const (
	// TmplLightInfantry - Leader + 3 Rifleman (4).
	TmplLightInfantry SquadTemplate = iota
	// TmplMotorRifle - Leader + 4 Rifleman + MG + Grenadier + Medic (8).
	TmplMotorRifle
	// TmplNATOInfantry - Leader + 4 Rifleman + MG + Grenadier + RadioOp (8).
	TmplNATOInfantry
	// TmplRecon - Leader + Sniper + 2 Rifleman + RadioOp (5).
	TmplRecon
	// TmplEngineering - Leader + 2 Engineer + DemoMan + 2 Rifleman (6).
	TmplEngineering
	// TmplATTeam - Leader + ATGunner + 2 Rifleman (4).
	TmplATTeam
	// TmplMGTeam - Leader + MG + 2 Rifleman (4).
	TmplMGTeam
)

// TemplateRoster returns the role list a CreateFromTemplate call will spawn,
// in slot order. Slot 0 is always the Leader (or the equivalent senior role)
// so it inherits the existing "slot 0 = commander" invariant the rest of the
// codebase relies on (formation offsets, map markers).
func TemplateRoster(t SquadTemplate) []components.UnitRoleKind {
	switch t {
	case TmplLightInfantry:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRifleman,
		}
	case TmplMotorRifle:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleMachineGunner,
			components.RoleGrenadier,
			components.RoleMedic,
		}
	case TmplNATOInfantry:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleMachineGunner,
			components.RoleGrenadier,
			components.RoleRadioOperator,
		}
	case TmplRecon:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleSniper,
			components.RoleRifleman,
			components.RoleRifleman,
			components.RoleRadioOperator,
		}
	case TmplEngineering:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleEngineer,
			components.RoleEngineer,
			components.RoleDemoMan,
			components.RoleRifleman,
			components.RoleRifleman,
		}
	case TmplATTeam:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleATGunner,
			components.RoleRifleman,
			components.RoleRifleman,
		}
	case TmplMGTeam:
		return []components.UnitRoleKind{
			components.RoleLeader,
			components.RoleMachineGunner,
			components.RoleRifleman,
			components.RoleRifleman,
		}
	}
	// Unknown template - fall back to a 4-rifleman squad so we never return
	// an empty roster (callers would crash trying to size unit spawn).
	return []components.UnitRoleKind{
		components.RoleRifleman,
		components.RoleRifleman,
		components.RoleRifleman,
		components.RoleRifleman,
	}
}
