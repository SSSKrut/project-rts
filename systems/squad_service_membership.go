package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// CreateFromUnits spawns a new Squad, attaches every unit in `units` as a
// member (pulling out of any prior squad first), and returns the new entity.
// Excess members beyond SquadRosterSize stay outside as soloists.
//
// Staged to avoid holding a pointer into the Squad archetype across an
// operation that might destroy another squad in the same archetype — Ark
// uses swap-on-remove storage compaction (cause of the original crash on
// repeated `T` presses).
func (s *SquadService) CreateFromUnits(units []ecs.Entity, kind components.FormationKind) ecs.Entity {
	if len(units) == 0 {
		return ecs.Entity{}
	}

	// Stage 1 — sanitise: skip zero / dead / duplicate, clamp to roster size.
	prepared := make([]ecs.Entity, 0, len(units))
	seen := make(map[ecs.Entity]struct{}, len(units))
	for _, u := range units {
		if u == (ecs.Entity{}) {
			continue
		}
		if !s.world.Alive(u) {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		prepared = append(prepared, u)
		if len(prepared) == components.SquadRosterSize {
			break
		}
	}
	if len(prepared) == 0 {
		return ecs.Entity{}
	}

	// Stage 2 — detach each unit from its prior squad. May despawn old squads
	// via cascaded Leave; new squad must NOT be spawned yet, otherwise Ark's
	// storage compaction would invalidate a held rosterMap pointer.
	// Stale personal actions die here too: a T-merge on the move otherwise
	// leaves every member racing its old solo MoveTo — the fresh squad has
	// no order yet, so FormationSystem never overwrites the queues and the
	// group scatters until the player stops it by hand (owner 2026-07-31).
	for _, u := range prepared {
		if old := s.memberMap.Get(u); old != nil {
			s.leaveInternal(u, *old)
		}
		if aq := s.actionQueueMap.Get(u); aq != nil {
			ClearActions(aq)
		}
	}

	// Stage 3 — spawn the new squad with the roster pre-populated as a value;
	// no long-lived pointer escapes this scope.
	var roster components.CommandRoster
	for i, u := range prepared {
		roster.Members[i] = u
		roster.Count = uint8(i + 1)
	}
	squad := s.world.NewEntity()
	s.squadMap.Add(squad, &components.Squad{})
	s.rosterMap.Add(squad, &roster)
	spacing := FormationSpacing(kind)
	// Hull-scale spacing: infantry gaps (1.2-2 m) stack 3 m-radius vehicles
	// into one another and formation fights the collision resolve forever.
	for _, u := range prepared {
		if !s.vehicleMap.Has(u) {
			continue
		}
		if col := s.colliderMap.Get(u); col != nil {
			if need := col.Radius*2 + 1; need > spacing {
				spacing = need
			}
		}
	}
	s.formationMap.Add(squad, &components.FormationData{
		Type:    kind,
		Forward: rl.Vector3{X: 0, Y: 0, Z: 1},
		Spacing: spacing,
	})
	s.macroPathMap.Add(squad, &components.MacroPath{})
	s.radioMap.Add(squad, &components.RadioNetwork{
		Frequency:   0,
		HasRadioman: s.hasRadiomanInRoster(prepared),
		HQReachable: false,
	})
	s.orderQueueMap.Add(squad, &components.OrderQueueHead{})
	s.alwaysActiveMap.Add(squad, &components.AlwaysActive{})

	// Squad allegiance mirrors its first member so map / inspector / input
	// never fall back to implicit defaults (DP-4).
	if f := s.factionMap.Get(prepared[0]); f != nil {
		s.upsertFaction(squad, *f)
	}
	if c := s.controllerMap.Get(prepared[0]); c != nil {
		s.upsertController(squad, *c)
	}

	// Stage 4 — attach SquadMember on every member. Each Add mutates the
	// unit archetype; the Squad archetype is untouched.
	for i, u := range prepared {
		sm := components.SquadMember{Squad: squad, SlotIndex: uint8(i)}
		if s.memberMap.Has(u) {
			*s.memberMap.Get(u) = sm
		} else {
			s.memberMap.Add(u, &sm)
		}
	}
	return squad
}

// Join appends a unit to an existing squad. Returns false on missing/dead
// squad/unit or full roster. No-op when unit is already in this squad;
// transparently pulls it out of any other squad first.
//
// Staged like CreateFromUnits to avoid the Squad archetype pointer trap.
func (s *SquadService) Join(squad, unit ecs.Entity) bool {
	if squad == (ecs.Entity{}) || unit == (ecs.Entity{}) {
		return false
	}
	if !s.world.Alive(squad) || !s.world.Alive(unit) {
		return false
	}
	if old := s.memberMap.Get(unit); old != nil {
		if old.Squad == squad {
			return true
		}
		s.leaveInternal(unit, *old)
	}
	// Squad itself may have been destroyed via cascaded Leave; re-check.
	if !s.world.Alive(squad) {
		return false
	}
	r := s.rosterMap.Get(squad)
	if r == nil || r.Count == components.SquadRosterSize {
		return false
	}
	idx := r.Count
	r.Members[idx] = unit
	r.Count++
	sm := components.SquadMember{Squad: squad, SlotIndex: idx}
	if s.memberMap.Has(unit) {
		*s.memberMap.Get(unit) = sm
	} else {
		s.memberMap.Add(unit, &sm)
	}
	return true
}

// Leave removes the unit from its squad (no-op if it has no SquadMember, or
// if its referenced squad has already been despawned). The roster is
// compacted so Members[0..Count-1] stays packed. An emptied squad is
// auto-despawned.
func (s *SquadService) Leave(unit ecs.Entity) {
	if unit == (ecs.Entity{}) || !s.world.Alive(unit) {
		return
	}
	m := s.memberMap.Get(unit)
	if m == nil {
		return
	}
	s.leaveInternal(unit, *m)
}

func (s *SquadService) leaveInternal(unit ecs.Entity, m components.SquadMember) {
	// SquadMember could point at a squad already torn down (race during
	// cascaded leaves); don't trust m.Squad until confirmed alive.
	if m.Squad == (ecs.Entity{}) || !s.world.Alive(m.Squad) {
		if s.world.Alive(unit) && s.memberMap.Has(unit) {
			s.memberMap.Remove(unit)
		}
		return
	}
	r := s.rosterMap.Get(m.Squad)
	if r == nil {
		if s.world.Alive(unit) && s.memberMap.Has(unit) {
			s.memberMap.Remove(unit)
		}
		return
	}
	// Slide the tail one slot left so Members[i<Count] stays packed; clamp
	// SlotIndex to current Count to handle stale m values.
	start := int(m.SlotIndex)
	if start >= int(r.Count) {
		start = int(r.Count) - 1
	}
	if start < 0 {
		start = 0
	}
	for i := start; i+1 < int(r.Count); i++ {
		r.Members[i] = r.Members[i+1]
		nb := r.Members[i]
		if nb == (ecs.Entity{}) || !s.world.Alive(nb) {
			continue
		}
		if mm := s.memberMap.Get(nb); mm != nil {
			mm.SlotIndex = uint8(i)
		}
	}
	if r.Count > 0 {
		r.Members[r.Count-1] = ecs.Entity{}
		r.Count--
	}
	emptyNow := r.Count == 0
	if s.world.Alive(unit) && s.memberMap.Has(unit) {
		s.memberMap.Remove(unit)
	}
	if emptyNow {
		s.world.RemoveEntity(m.Squad)
	}
}

// Despawn detaches every member, then removes the squad entity.
func (s *SquadService) Despawn(squad ecs.Entity) {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return
	}
	r := s.rosterMap.Get(squad)
	if r == nil {
		s.world.RemoveEntity(squad)
		return
	}
	// Copy roster off squad storage before RemoveEntity (avoid reading freed
	// memory; entity IDs are cheap to copy).
	count := int(r.Count)
	members := r.Members
	s.world.RemoveEntity(squad)
	for i := 0; i < count; i++ {
		u := members[i]
		if u == (ecs.Entity{}) || !s.world.Alive(u) {
			continue
		}
		if s.memberMap.Has(u) {
			s.memberMap.Remove(u)
		}
	}
}

// hasRadiomanInRoster returns true if any unit's Equipment.Secondary is a
// Radio. Goes into RadioNetwork.HasRadioman.
func (s *SquadService) hasRadiomanInRoster(units []ecs.Entity) bool {
	for _, u := range units {
		if u == (ecs.Entity{}) || !s.world.Alive(u) {
			continue
		}
		eq := s.equipmentMap.Get(u)
		if eq == nil || eq.Secondary == (ecs.Entity{}) {
			continue
		}
		if !s.world.Alive(eq.Secondary) {
			continue
		}
		if s.radioGearMap.Has(eq.Secondary) {
			return true
		}
	}
	return false
}
