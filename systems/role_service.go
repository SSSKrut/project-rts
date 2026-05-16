package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// RoleService is the sole authorised mutator of UnitRole / per-role Equipment
// sub-entities (Primary weapon, Secondary gear). Mirrors the SquadService /
// Stamper pattern — pre-built handles, no archetype mutations inside ECS
// queries. main.go owns one instance and passes it into spawn helpers.
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
}

func (s *RoleService) destroyIfAlive(e ecs.Entity) {
	if e == (ecs.Entity{}) || !s.world.Alive(e) {
		return
	}
	s.world.RemoveEntity(e)
}

// spawnPrimary spawns the role's primary weapon entity. Stats here are
// placeholders — Phase 14 (Combat) will overwrite with real numbers.
func (s *RoleService) spawnPrimary(unit ecs.Entity, role components.UnitRoleKind, pos components.WorldPos) ecs.Entity {
	weaponKind, ammo, rangeM, rof, dmg := primaryStats(role)
	ent := s.world.NewEntity()
	s.weaponMap.Add(ent, &components.Weapon{
		Kind:   weaponKind,
		Ammo:   ammo,
		RangeM: rangeM,
		RoF:    rof,
		Damage: dmg,
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
		// All other roles carry a Makarov sidearm.
		s.weaponMap.Add(ent, &components.Weapon{
			Kind:   components.WeaponMakarov,
			Ammo:   8,
			RangeM: 50,
			RoF:    4,
			Damage: 20,
		})
	}
	return ent
}

// primaryStats returns the placeholder Weapon fields for each role's primary.
// Numbers are coarse — real balance lives in Phase 14.
func primaryStats(role components.UnitRoleKind) (kind components.WeaponKind, ammo uint16, rangeM, rof float32, dmg uint16) {
	switch role {
	case components.RoleMachineGunner:
		return components.WeaponPKM, 100, 800, 10, 35
	case components.RoleSniper:
		return components.WeaponSVD, 10, 800, 1, 80
	case components.RoleATGunner:
		return components.WeaponRPG7, 3, 300, 0.2, 400
	case components.RoleGrenadier:
		return components.WeaponGP25, 30, 400, 0.5, 60
	default:
		// Leader / Rifleman / Medic / RadioOperator / Engineer / DemoMan all
		// carry an AK47 as their primary in the Phase 12 placeholder.
		return components.WeaponAK47, 30, 400, 10, 30
	}
}
