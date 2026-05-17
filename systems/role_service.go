package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// RoleService is the sole authorised mutator of UnitRole / per-role Equipment
// sub-entities (Primary weapon, Secondary gear). Mirrors the SquadService /
// Stamper pattern — pre-built handles, no archetype mutations inside ECS
// queries. main.go owns one instance and passes it into spawn helpers.
//
// Phase 13 extension: AssignRole also installs a per-Unit Stamina component
// with role-specific MaxLevel (heavy-equipment carriers fatigue faster).
// Squad-level standing rules (MovementProfile, EngagementRules, BehaviorRules)
// are written by SquadService.CreateFromTemplate from per-role helper lookups
// exposed here — RoleService does not touch the Squad archetype directly.
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

// NewRoleService wires the map handles. Must be called after world creation
// (same constraint as every other ecs.NewMap call).
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

// AssignRole stamps `unit` with `role`, replacing its Primary / Secondary
// equipment with the role-appropriate kit. Idempotent: calling twice with
// different roles destroys the previous equipment entities and respawns a
// fresh loadout.
//
// Pre-condition: `unit` is alive and already has a WorldPos. Equipment will
// be added if missing. UnitRole is added or overwritten.
func (s *RoleService) AssignRole(unit ecs.Entity, role components.UnitRoleKind) {
	if unit == (ecs.Entity{}) || !s.world.Alive(unit) {
		return
	}

	// Stamp / overwrite the role marker.
	if s.roleMap.Has(unit) {
		s.roleMap.Get(unit).Kind = role
	} else {
		s.roleMap.Add(unit, &components.UnitRole{Kind: role})
	}

	// Snapshot current pos so the new sub-entities can sit alongside the
	// soldier. WorldPos here is by-value — the equipment entity's pos isn't
	// kept in sync after this (Phase 7 weapon entities never tracked the
	// owner's pos either; consumer systems read the owner via OwnedBy).
	pos := s.posMap.Get(unit)
	if pos == nil {
		return
	}
	carryPos := *pos

	// Destroy whatever Equipment already exists so AssignRole is idempotent.
	// Same teardown is reused if we ever wire a "drop loadout on death" hook.
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
	// Active defaults to Primary — Phase 7 used the same rule for AK47.
	eq.Active = primary

	// Phase 13 M13.2: install / refresh Stamina with the per-role MaxLevel.
	// Idempotent — if a unit already carries Stamina (e.g. AssignRole was
	// called twice with different roles), we keep its Current value so a
	// fatigued soldier doesn't magically refill on a role swap. Only
	// MaxLevel and RecoverRate are recomputed.
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

	// Phase 14 M14.1: per-role HP pool. Same idempotency rule as Stamina —
	// don't refill a damaged unit because of a role swap; only refresh Max
	// (and clamp Current down if the new Max is lower).
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

// spawnPrimary spawns the role's primary weapon entity with the Phase 14
// M14.7 stats (damage / range / RoF / dispersion).
func (s *RoleService) spawnPrimary(unit ecs.Entity, role components.UnitRoleKind, pos components.WorldPos) ecs.Entity {
	weaponKind, ammo, rangeM, rof, dmg, dispersion := primaryStats(role)
	ent := s.world.NewEntity()
	s.weaponMap.Add(ent, &components.Weapon{
		Kind:       weaponKind,
		Ammo:       ammo,
		RangeM:     rangeM,
		RoF:        rof,
		Damage:     dmg,
		Dispersion: dispersion,
	})
	s.ownedByMap.Add(ent, &components.OwnedBy{Owner: unit})
	posCopy := pos
	s.posMap.Add(ent, &posCopy)
	return ent
}

// spawnSecondary picks the role's secondary item. Many roles share a simple
// Makarov sidearm — distinct roles get the placeholder Radio / Medkit / Spade
// markers so Phase 20 / 14 / 18 consumers have something concrete to read.
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
		// All other roles carry a Makarov sidearm. Phase 14 M14.7: lower
		// range / damage / wider dispersion than the AK47 primary.
		s.weaponMap.Add(ent, &components.Weapon{
			Kind:       components.WeaponMakarov,
			Ammo:       8,
			RangeM:     30,
			RoF:        3,
			Damage:     18,
			Dispersion: 0.06,
		})
	}
	return ent
}

// staminaRecoverRate is the per-second Stamina regen at Pace=Walk + Stance ∈
// {Stand, Crouch}. PHASE-13.md P2 sets this as a fixed value across all roles;
// Phase 15 doctrines may introduce per-doctrine scaling later.
const staminaRecoverRate float32 = 0.05

// HPMaxForRole returns the per-role HP pool. PHASE-14.md P1 table:
//   - MachineGunner: 110 (vest + extra mass).
//   - Sniper / ATGunner: 90 (lighter loadout).
//   - everyone else: 100 (Rifleman / Leader / Grenadier / Medic /
//     Radio / Engineer / Demo).
//
// Numbers are placeholders; final balance lands in M14.7 playtest.
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
// carriers (MG / AT / Engineer / DemoMan / Radio) have a smaller tank because
// they're hauling more weight. PHASE-13.md P2 table.
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

// MovementDefaultForRole returns the per-role default MovementProfile applied
// at squad creation. Squad-level aggregation pulls from the leader's role
// (see SquadService.CreateFromTemplate), then template-specific tweaks may
// override individual fields. PHASE-13.md P7 + COMMAND-MODEL.md §4 table.
func MovementDefaultForRole(role components.UnitRoleKind) components.MovementProfile {
	switch role {
	case components.RoleMachineGunner:
		// Deployed weapon — squad sits crouched once setup.
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
		// Leader / Rifleman / Grenadier / Engineer / DemoMan — standing default.
		return components.MovementProfile{
			Pace: components.PaceWalk, Stance: components.StanceStand,
			Posture: components.PostureStandard, PathStyle: components.PathStyleDirect,
		}
	}
}

// EngagementDefaultForRole returns the per-role default EngagementRules.
// Same per-role table as COMMAND-MODEL.md §4. Phase 14 WeaponSystem is the
// real reader; Phase 13 only surfaces these in the Inspector quick-bar.
func EngagementDefaultForRole(role components.UnitRoleKind) components.EngagementRules {
	switch role {
	case components.RoleMachineGunner:
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: false}
	case components.RoleGrenadier:
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: true, FireOnStruct: true}
	case components.RoleSniper:
		// Hold fire until ordered — sniper picks targets deliberately.
		return components.EngagementRules{Mode: components.HoldFire, FireOnInf: true, FireOnArm: false}
	case components.RoleATGunner:
		// Anti-armour only — wastes RPG on infantry otherwise.
		return components.EngagementRules{Mode: components.HoldFire, FireOnInf: false, FireOnArm: true}
	case components.RoleMedic, components.RoleRadioOperator, components.RoleEngineer, components.RoleDemoMan:
		return components.EngagementRules{Mode: components.ReturnFire, FireOnInf: true}
	default:
		// Leader / Rifleman.
		return components.EngagementRules{Mode: components.FreeFire, FireOnInf: true, FireOnArm: true}
	}
}

// BehaviorDefaultForRole returns the per-role default BehaviorRules. These
// are gate flags; Phase 15 SurvivalInstinct will read them. SuppressionThreshold
// stays at 0.30 for everyone (Phase 15 tunes empirically).
func BehaviorDefaultForRole(role components.UnitRoleKind) components.BehaviorRules {
	const defaultThreshold = 0.30
	switch role {
	case components.RoleMachineGunner:
		// Deployed — don't reposition under fire (would lose Suppression
		// effect). Auto-stance ok.
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
		// Leader / Rifleman / Grenadier — full reactive set.
		return components.BehaviorRules{
			AllowAutoReposition:  true,
			AllowAutoStance:      true,
			HoldUntilOrdered:     false,
			AllowReturnFire:      true,
			SuppressionThreshold: defaultThreshold,
		}
	}
}

// primaryStats returns the per-role Weapon fields. PHASE-14.md M14.7 table
// (P10 lock-in): realistic damage / range / RoF / dispersion. Numbers stay
// rough placeholders — real balance pass is Phase 25 polish, but these are
// close enough that a 4-vs-8 firefight at ~50 m feels like a tactical
// engagement (target: ~30-45 s to wipe one side, per M14.7 closure).
//
// Dispersion = small-angle radians; lateral deflection at the target is
// dispersion × range. Examples: AK47 0.030 at 100 m → 3 m spread; SVD 0.005
// at 100 m → 0.5 m (precision shot).
func primaryStats(role components.UnitRoleKind) (kind components.WeaponKind, ammo uint16, rangeM, rof float32, dmg uint16, dispersion float32) {
	switch role {
	case components.RoleMachineGunner:
		// PKM: 7.62×54 belt — high RoF, wider dispersion (burst fire),
		// good range. Numbers a touch above AK to reflect role weight.
		return components.WeaponPKM, 100, 500, 8.0, 30, 0.05
	case components.RoleSniper:
		// SVD: long range, low RoF, tight dispersion (precision rifle).
		return components.WeaponSVD, 10, 600, 0.5, 70, 0.005
	case components.RoleATGunner:
		// RPG7: rare shots, AP/HE — vs Inf splash placeholder. Phase 14.5
		// will add real splash radius; for now single-target only.
		return components.WeaponRPG7, 3, 200, 0.1, 200, 0.02
	case components.RoleGrenadier:
		// GP25: under-barrel grenade launcher. Splash placeholder.
		return components.WeaponGP25, 8, 150, 0.3, 50, 0.04
	default:
		// Leader / Rifleman / Medic / RadioOperator / Engineer / DemoMan
		// all carry an AK47 as their primary.
		return components.WeaponAK47, 30, 300, 4.0, 28, 0.03
	}
}
