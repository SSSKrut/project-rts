package systems

import "rts-go/components"

// SquadTemplate enumerates the starter rosters available to
// SquadService.CreateFromTemplate. Convenience-only — shapes spawn-time
// role distribution then disappears.
type SquadTemplate uint8

const (
	TmplLightInfantry SquadTemplate = iota // Leader + 3 Rifleman
	TmplMotorRifle                         // Leader + 4 Rifleman + MG + Grenadier + Medic
	TmplNATOInfantry                       // Leader + 4 Rifleman + MG + Grenadier + RadioOp
	TmplRecon                              // Leader + Sniper + 2 Rifleman + RadioOp
	TmplEngineering                        // Leader + 2 Engineer + DemoMan + 2 Rifleman
	TmplATTeam                             // Leader + ATGunner + 2 Rifleman
	TmplMGTeam                             // Leader + MG + 2 Rifleman
)

// TemplateRoster returns the role list CreateFromTemplate spawns, in slot
// order. Slot 0 is always Leader (preserves the slot-0=commander invariant).
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
	// Unknown template — fall back to 4 riflemen so we never return empty.
	return []components.UnitRoleKind{
		components.RoleRifleman,
		components.RoleRifleman,
		components.RoleRifleman,
		components.RoleRifleman,
	}
}
