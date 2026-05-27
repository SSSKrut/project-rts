package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// CreateFromTemplate walks the template's role list, calls unitFactory for
// each, stamps role + equipment via RoleService.AssignRole, and bundles
// the result into a Squad. unitFactory is a callback because main.go owns
// the canonical unit component graph.
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

	// Column-major 2 m grid centred on `pos`. Slot 0 (leader) sits dead-
	// centre — preserves the slot-0=commander invariant.
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
		// Template's faction wins over any factory-set value.
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

	// Stamp Faction onto squad entity too so map / inspector can tint
	// without walking to a roster member.
	if s.factionMap.Has(squad) {
		*s.factionMap.Get(squad) = faction
	} else {
		s.factionMap.Add(squad, &faction)
	}

	s.applyTemplateStandingRules(squad, template, roster)
	return squad
}

// applyTemplateStandingRules writes MovementProfile / EngagementRules /
// BehaviorRules onto the squad. Defaults come from the leader's role with
// template-specific overrides. Skips components already present so
// player edits are preserved.
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

	// Squad-level character that leader-role aggregation can't capture
	// (e.g. Recon = stealth, AT = no-inf).
	switch template {
	case TmplRecon:
		movement.PathStyle = components.PathStyleCoverSeek
		movement.Posture = components.PostureQuiet
		engagement.Mode = components.HoldFire
	case TmplATTeam:
		engagement.Mode = components.HoldFire
		engagement.FireOnInf = false
		engagement.FireOnArm = true
	case TmplMGTeam:
		// Fire base, not assault.
		movement.Stance = components.StanceCrouch
	case TmplEngineering:
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

// templateGridOffset spreads slot indices onto a small grid. Slot 0 stays
// at (0,0); output is XZ offset in metres.
func templateGridOffset(slot, cols int, spacing float32) (float32, float32) {
	if slot == 0 || cols <= 0 {
		return 0, 0
	}
	idx := slot - 1
	row := idx / cols
	col := idx % cols
	// Centre the row so the leader sits in the middle, not at a corner.
	halfCols := float32(cols-1) * 0.5
	x := (float32(col) - halfCols) * spacing
	z := float32(row+1) * spacing
	return x, z
}
