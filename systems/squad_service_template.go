package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// CreateFromTemplate is the Phase 12 high-level spawn helper. It walks the
// template's role list, calls `unitFactory(spawnPos)` to materialise each
// unit's base archetype (Unit + Stance + Motion + ... - main.go owns the
// component list), stamps the role + equipment via RoleService.AssignRole,
// then bundles the result into a Squad through CreateFromUnits.
//
// `pos` is the squad centre; units fan out on a 2 m grid. `unitFactory` is a
// callback (rather than a SquadService method) because main.go is the
// canonical owner of the unit component graph - duplicating that list inside
// SquadService would re-implement half of unit.go.
//
// Returns the new Squad entity, or zero if the template was empty / the
// factory failed every slot.
func (s *SquadService) CreateFromTemplate(
	template SquadTemplate,
	pos components.WorldPos,
	formation components.FormationKind,
	faction components.Faction,
	roleService *RoleService,
	unitFactory func(spawn components.WorldPos) ecs.Entity,
) ecs.Entity {
	roster := TemplateRoster(template)
	if len(roster) == 0 || unitFactory == nil || roleService == nil {
		return ecs.Entity{}
	}

	// Layout: column-major 2 m grid centred on `pos`. Slot 0 (leader) sits
	// dead-centre, the rest fill outwards row by row. Keeping the leader on
	// the centre matches the slot-0=commander invariant the rest of the code
	// expects (formation offsets, map-marker projection).
	const spacing = 2.0
	cols := 4
	if len(roster) < cols {
		cols = len(roster)
	}

	units := make([]ecs.Entity, 0, len(roster))
	for i, role := range roster {
		offX, offZ := templateGridOffset(i, cols, spacing)
		spawn := pos
		spawn.Local.X += offX
		spawn.Local.Z += offZ
		u := unitFactory(spawn)
		if u == (ecs.Entity{}) {
			continue
		}
		roleService.AssignRole(u, role)
		// Phase 14 M14.1: stamp Faction onto the unit. Idempotent - if the
		// factory already wrote one (unlikely but harmless), overwrite so the
		// template's intent wins.
		if existing := s.factionMap.Get(u); existing != nil {
			*existing = faction
		} else {
			s.factionMap.Add(u, &faction)
		}
		units = append(units, u)
	}
	if len(units) == 0 {
		return ecs.Entity{}
	}
	squad := s.CreateFromUnits(units, formation)
	if squad == (ecs.Entity{}) {
		return squad
	}

	// Phase 14 M14.6: stamp Faction onto the squad entity as well so the map /
	// inspector colour-picker can tint by hostility without walking to a
	// roster member. Idempotent - repeated template calls overwrite.
	if s.factionMap.Has(squad) {
		*s.factionMap.Get(squad) = faction
	} else {
		s.factionMap.Add(squad, &faction)
	}

	// Phase 13 M13.2: install squad-level standing rules. Aggregation rule
	// (PHASE-13.md P7): defaults come from the leader's role (slot 0 in the
	// template roster), then template-specific tweaks override individual
	// fields. Idempotent (P8) - if the player has already edited the squad's
	// rules (gameplay reassignment, hot-reload), preserve those values.
	s.applyTemplateStandingRules(squad, template, roster)
	return squad
}

// applyTemplateStandingRules writes MovementProfile / EngagementRules /
// BehaviorRules onto the squad entity using per-role defaults from
// RoleService helpers + template-specific overrides. Skips any component
// that already exists on the squad (P8: don't clobber player edits).
func (s *SquadService) applyTemplateStandingRules(
	squad ecs.Entity,
	template SquadTemplate,
	roster []components.UnitRoleKind,
) {
	leader := components.RoleRifleman
	if len(roster) > 0 {
		leader = roster[0]
	}
	movement := MovementDefaultForRole(leader)
	engagement := EngagementDefaultForRole(leader)
	behavior := BehaviorDefaultForRole(leader)

	// Template-specific overrides - capture squad-level character that pure
	// leader-role aggregation can't (e.g. Recon = stealth, AT = no-inf).
	switch template {
	case TmplRecon:
		movement.PathStyle = components.PathStyleCoverSeek
		movement.Posture = components.PostureQuiet
		engagement.Mode = components.HoldFire
	case TmplATTeam:
		// AT team holds fire until armour appears.
		engagement.Mode = components.HoldFire
		engagement.FireOnInf = false
		engagement.FireOnArm = true
	case TmplMGTeam:
		// MG team starts crouched - they're a fire base, not assault.
		movement.Stance = components.StanceCrouch
	case TmplEngineering:
		// Engineers stay cautious - building tasks are slow and exposed.
		movement.Pace = components.PaceWalk
		engagement.Mode = components.ReturnFire
	}

	if !s.movementProfileMap.Has(squad) {
		s.movementProfileMap.Add(squad, &movement)
	}
	if !s.engagementRulesMap.Has(squad) {
		s.engagementRulesMap.Add(squad, &engagement)
	}
	if !s.behaviorRulesMap.Has(squad) {
		s.behaviorRulesMap.Add(squad, &behavior)
	}
}

// templateGridOffset spreads slot indices onto a small grid. Slot 0 stays at
// (0,0); slot 1 goes right of centre, slot 2 below, etc. Output is XZ offset
// in metres.
func templateGridOffset(slot, cols int, spacing float32) (float32, float32) {
	if slot == 0 || cols <= 0 {
		return 0, 0
	}
	idx := slot - 1
	row := idx / cols
	col := idx % cols
	// Centre the row: half-width pulled left so the leader sits roughly in
	// the middle of the formation, not at one corner.
	halfCols := float32(cols-1) * 0.5
	x := (float32(col) - halfCols) * spacing
	z := float32(row+1) * spacing
	return x, z
}
