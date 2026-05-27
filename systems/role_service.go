package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// RoleService is the sole authorised mutator of UnitRole / per-role
// Equipment sub-entities. Pre-built handle object; no archetype mutations
// inside ECS queries. AssignRole also installs per-Unit Stamina and HP with
// role-specific limits. Squad-level standing rules go through
// SquadService.CreateFromTemplate, which reads the per-role default lookups
// exposed here.
type RoleService struct {
	world *ecs.World

	roleMap      *ecs.Map[components.UnitRole]
	equipmentMap *ecs.Map[components.Equipment]
	posMap       *ecs.Map[components.WorldPos]
	weaponMap    *ecs.Map[components.Weapon]
	radioMap     *ecs.Map[components.Radio]
	medkitMap    *ecs.Map[components.Medkit]
	spadeMap     *ecs.Map[components.Spade]
	ownedByMap   *ecs.Map[components.OwnedBy]
	staminaMap   *ecs.Map[components.Stamina]
	hpMap        *ecs.Map[components.HP]
}

func NewRoleService(w *ecs.World) *RoleService {
	return &RoleService{
		world:        w,
		roleMap:      ecs.NewMap[components.UnitRole](w),
		equipmentMap: ecs.NewMap[components.Equipment](w),
		posMap:       ecs.NewMap[components.WorldPos](w),
		weaponMap:    ecs.NewMap[components.Weapon](w),
		radioMap:     ecs.NewMap[components.Radio](w),
		medkitMap:    ecs.NewMap[components.Medkit](w),
		spadeMap:     ecs.NewMap[components.Spade](w),
		ownedByMap:   ecs.NewMap[components.OwnedBy](w),
		staminaMap:   ecs.NewMap[components.Stamina](w),
		hpMap:        ecs.NewMap[components.HP](w),
	}
}

// AssignRole stamps `unit` with `role`, replacing Primary / Secondary
// equipment. Idempotent: calling twice destroys previous equipment entities
// and respawns a fresh loadout, but preserves current Stamina / HP.
//
// Pre-condition: unit is alive and has a WorldPos.
func (s *RoleService) AssignRole(unit ecs.Entity, role components.UnitRoleKind) {
	if unit == (ecs.Entity{}) || !s.world.Alive(unit) {
		return
	}

	if s.roleMap.Has(unit) {
		s.roleMap.Get(unit).Kind = role
	} else {
		s.roleMap.Add(unit, &components.UnitRole{Kind: role})
	}

	// Snapshot pos so new sub-entities sit alongside the soldier. WorldPos
	// is by-value; the equipment entity's pos isn't kept in sync — consumers
	// read the owner via OwnedBy.
	pos := s.posMap.Get(unit)
	if pos == nil {
		return
	}
	carryPos := *pos

	eq := s.equipmentMap.Get(unit)
	if eq == nil {
		s.equipmentMap.Add(unit, &components.Equipment{})
		eq = s.equipmentMap.Get(unit)
	} else {
		s.destroyIfAlive(eq.Primary)
		s.destroyIfAlive(eq.Secondary)
	}

	primary := s.spawnPrimary(unit, role, carryPos)
	secondary := s.spawnSecondary(unit, role, carryPos)
	eq.Primary = primary
	eq.Secondary = secondary
	eq.Active = primary

	// Preserve Current value on role swap so a fatigued soldier doesn't
	// refill; only refresh MaxLevel / RecoverRate.
	maxLevel := StaminaMaxForRole(role)
	if existing := s.staminaMap.Get(unit); existing != nil {
		existing.MaxLevel = maxLevel
		existing.RecoverRate = staminaRecoverRate
		if existing.Current > maxLevel {
			existing.Current = maxLevel
		}
	} else {
		s.staminaMap.Add(unit, &components.Stamina{
			Current:     maxLevel,
			MaxLevel:    maxLevel,
			RecoverRate: staminaRecoverRate,
		})
	}

	// Same preserve-Current rule as Stamina; clamp down if new Max is lower.
	hpMax := HPMaxForRole(role)
	if existing := s.hpMap.Get(unit); existing != nil {
		existing.Max = hpMax
		if existing.Current > hpMax {
			existing.Current = hpMax
		}
	} else {
		s.hpMap.Add(unit, &components.HP{Current: hpMax, Max: hpMax})
	}
}

func (s *RoleService) destroyIfAlive(e ecs.Entity) {
	if e == (ecs.Entity{}) || !s.world.Alive(e) {
		return
	}
	s.world.RemoveEntity(e)
}

// spawnPrimary spawns the role's primary weapon entity. Per-WeaponKind stats
// come from components.WeaponSpecs (role → WeaponKind mapping lives here).
func (s *RoleService) spawnPrimary(unit ecs.Entity, role components.UnitRoleKind, pos components.WorldPos) ecs.Entity {
	weaponKind := primaryWeaponForRole(role)
	spec := components.SpecForWeapon(weaponKind)
	ent := s.world.NewEntity()
	s.weaponMap.Add(ent, &components.Weapon{
		Kind:       weaponKind,
		Ammo:       spec.Ammo,
		RangeM:     spec.RangeM,
		RoF:        spec.RoF,
		Damage:     spec.Damage,
		Dispersion: spec.Dispersion,
	})
	s.ownedByMap.Add(ent, &components.OwnedBy{Owner: unit})
	posCopy := pos
	s.posMap.Add(ent, &posCopy)
	return ent
}

// spawnSecondary picks the role's secondary item. Most roles share a
// Makarov sidearm; distinct roles get Radio / Medkit / Spade markers.
func (s *RoleService) spawnSecondary(unit ecs.Entity, role components.UnitRoleKind, pos components.WorldPos) ecs.Entity {
	ent := s.world.NewEntity()
	s.ownedByMap.Add(ent, &components.OwnedBy{Owner: unit})
	posCopy := pos
	s.posMap.Add(ent, &posCopy)

	switch role {
	case components.RoleRadioOperator:
		s.radioMap.Add(ent, &components.Radio{})
	case components.RoleMedic:
		s.medkitMap.Add(ent, &components.Medkit{})
	case components.RoleEngineer, components.RoleDemoMan:
		s.spadeMap.Add(ent, &components.Spade{})
	default:
		// All other roles carry a Makarov sidearm.
		mk := components.SpecForWeapon(components.WeaponMakarov)
		s.weaponMap.Add(ent, &components.Weapon{
			Kind: mk.Kind, Ammo: mk.Ammo, RangeM: mk.RangeM, RoF: mk.RoF,
			Damage: mk.Damage, Dispersion: mk.Dispersion,
		})
	}
	return ent
}

// staminaRecoverRate is the per-second Stamina regen at Pace=Walk +
// Stance in {Stand, Crouch}. Same across roles for now.
const staminaRecoverRate float32 = 0.05

// HPMaxForRole returns the per-role HP pool:
//   - MachineGunner: 110 (vest + extra mass).
//   - Sniper / ATGunner: 90 (lighter loadout).
//   - everyone else: 100.
func HPMaxForRole(role components.UnitRoleKind) float32 {
	switch role {
	case components.RoleMachineGunner:
		return 110
	case components.RoleSniper, components.RoleATGunner:
		return 90
	default:
		return 100
	}
}

// StaminaMaxForRole returns the per-role MaxLevel modifier. Heavy-equipment
// carriers (MG / AT / Engineer / DemoMan / Radio) have smaller tanks.
func StaminaMaxForRole(role components.UnitRoleKind) float32 {
	switch role {
	case components.RoleMachineGunner, components.RoleATGunner:
		return 0.7
	case components.RoleEngineer, components.RoleDemoMan:
		return 0.8
	case components.RoleRadioOperator:
		return 0.85
	default:
		return 1.0
	}
}

// MovementDefaultForRole returns the per-role default MovementProfile.
// Squad-level aggregation pulls from the leader's role.
func MovementDefaultForRole(role components.UnitRoleKind) components.MovementProfile {
	switch role {
	case components.RoleMachineGunner:
		// Deployed weapon — squad sits crouched once set up.
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceCrouch,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	case components.RoleSniper:
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceProne,
			Posture: components.PostureQuiet, PathStyle: components.PathStyleCoverSeek,
		}
	case components.RoleATGunner:
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceCrouch,
			Posture: components.PostureStandard, PathStyle: components.PathStyleCoverSeek,
		}
	case components.RoleMedic, components.RoleRadioOperator:
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceCrouch,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	default:
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceStand,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	}
}

// EngagementDefaultForRole returns the per-role default EngagementRules.
func EngagementDefaultForRole(role components.UnitRoleKind) components.EngagementRules {
	switch role {
	case components.RoleMachineGunner:
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: false}
	case components.RoleGrenadier:
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: true, FireOnStruct: true}
	case components.RoleSniper:
		// Sniper picks targets deliberately.
		return components.EngagementRules{Mode: components.HoldFire, FireOnInf: true, FireOnArm: false}
	case components.RoleATGunner:
		// Anti-armour only — wastes RPG on infantry.
		return components.EngagementRules{Mode: components.HoldFire, FireOnInf: false, FireOnArm: true}
	case components.RoleMedic, components.RoleRadioOperator, components.RoleEngineer, components.RoleDemoMan:
		return components.EngagementRules{Mode: components.ReturnFire, FireOnInf: true}
	default:
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: true}
	}
}

// BehaviorDefaultForRole returns the per-role default BehaviorRules.
func BehaviorDefaultForRole(role components.UnitRoleKind) components.BehaviorRules {
	const defaultThreshold = 0.30
	switch role {
	case components.RoleMachineGunner:
		// Deployed — don't reposition under fire (would lose Suppression).
		return components.BehaviorRules{
			AllowAutoReposition:  false,
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	case components.RoleSniper, components.RoleATGunner:
		return components.BehaviorRules{
			AllowAutoReposition:  false,
			AllowAutoStance:      false,
			HoldUntilOrdered:     true,
			AllowReturnFire:      false,
			SuppressionThreshold: defaultThreshold,
		}
	case components.RoleMedic:
		return components.BehaviorRules{
			AllowAutoReposition:  false, // stays with the wounded
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	case components.RoleRadioOperator:
		return components.BehaviorRules{
			AllowAutoReposition:  false,
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	case components.RoleEngineer:
		return components.BehaviorRules{
			AllowAutoReposition:  false, // pauses on building task
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	case components.RoleDemoMan:
		return components.BehaviorRules{
			AllowAutoReposition:  true,
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	default:
		return components.BehaviorRules{
			AllowAutoReposition:  true,
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	}
}

// primaryWeaponForRole maps a UnitRole to its primary WeaponKind. Keeping
// the mapping as a small switch (vs. a field on WeaponSpec) — roles and
// weapons are independent enums.
func primaryWeaponForRole(role components.UnitRoleKind) components.WeaponKind {
	switch role {
	case components.RoleMachineGunner:
		return components.WeaponPKM
	case components.RoleSniper:
		return components.WeaponSVD
	case components.RoleATGunner:
		return components.WeaponRPG7
	case components.RoleGrenadier:
		return components.WeaponGP25
	default:
		return components.WeaponAK47
	}
}
