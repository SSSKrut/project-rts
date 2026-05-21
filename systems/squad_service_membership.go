package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// CreateFromUnits spawns a new Squad entity, attaches every unit in `units`
// as a member (pulling them out of any prior squad first), and returns the
// new entity. Excess members beyond SquadRosterSize stay outside the roster
// as soloists. An empty (or all-invalid) input returns the zero Entity.
//
// The function is staged so no live pointer into the Squad archetype is held
// across an operation that might destroy another squad entity in the same
// archetype. Ark uses swap-on-remove storage compaction (storage.go:267),
// which would silently invalidate such a pointer - and this was the cause of
// the original "free(): invalid size" crash on repeated `T` presses.
func (s *SquadService) CreateFromUnits(units []ecs.Entity, kind components.FormationKind) ecs.Entity {
	if len(units) == 0 {
		return ecs.Entity{}
	}

	// Stage 1 - sanitise input: skip zero / dead / duplicate entities and
	// clamp to the roster size. Doing this up front means later stages can
	// trust every entry without re-checking.
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

	// Stage 2 - detach every prepared unit from its prior squad. May despawn
	// old squads (cascading from Leave when their last member walks out), so
	// we cannot have spawned the new squad yet - Ark's storage compaction
	// would invalidate a held rosterMap pointer.
	for _, u := range prepared {
		if old := s.memberMap.Get(u); old != nil {
			s.leaveInternal(u, *old)
		}
	}

	// Stage 3 - spawn the new squad with the roster pre-populated as a value.
	// rosterMap.Add takes a pointer-to-temporary, the ECS stores the value;
	// no long-lived pointer escapes this scope.
	var roster components.CommandRoster
	for i, u := range prepared {
		roster.Members[i] = u
		roster.Count = uint8(i + 1)
	}
	squad := s.world.NewEntity()
	s.squadMap.Add(squad, &components.Squad{})
	s.rosterMap.Add(squad, &roster)
	s.formationMap.Add(squad, &components.FormationData{
		Type:    kind,
		Forward: rl.Vector3{X: 0, Y: 0, Z: 1},
		Spacing: FormationSpacing(kind),
	})
	s.macroPathMap.Add(squad, &components.MacroPath{})
	s.radioMap.Add(squad, &components.RadioNetwork{
		Frequency:   0,
		HasRadioman: s.hasRadiomanInRoster(prepared),
		HQReachable: false,
	})
	s.orderQueueMap.Add(squad, &components.OrderQueueHead{})
	s.alwaysActiveMap.Add(squad, &components.AlwaysActive{})

	// Stage 4 - attach SquadMember on every member. Each Add mutates the
	// unit's archetype; the Squad archetype is untouched, so no other squad
	// row can shift underneath us here.
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

// Join appends a unit to an existing squad. Returns false if the squad or
// unit is missing/dead, or the roster is full. No-op when the unit is already
// in this squad; transparently pulls it out of any other squad first.
//
// Like CreateFromUnits, the implementation is staged to avoid holding a
// pointer into the Squad archetype across a leaveInternal call that might
// destroy another squad of the same archetype (see CreateFromUnits comment).
func (s *SquadService) Join(squad, unit ecs.Entity) bool {
	if squad == (ecs.Entity{}) || unit == (ecs.Entity{}) {
		return false
	}
	if !s.world.Alive(squad) || !s.world.Alive(unit) {
		return false
	}
	// Stage 1 - detach from prior squad if any. May destroy that squad.
	if old := s.memberMap.Get(unit); old != nil {
		if old.Squad == squad {
			return true
		}
		s.leaveInternal(unit, *old)
	}
	// Stage 2 - squad may itself have been the one destroyed (Leave cascaded
	// to RemoveEntity); re-check after the call.
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
	// Defensive: SquadMember could point at a squad that was already torn
	// down (resource lifecycle invariants can race during cascaded leaves).
	// Don't trust m.Squad until we've confirmed it's alive.
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
	// Compact: slide the tail one slot left so Members[i<Count] stays packed.
	// Clamp SlotIndex to the current Count to handle stale m values gracefully.
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

// Despawn detaches every member, then removes the squad entity. Use when the
// caller explicitly wants the squad gone (Phase 11 wipeout, debug clear).
func (s *SquadService) Despawn(squad ecs.Entity) {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return
	}
	r := s.rosterMap.Get(squad)
	if r == nil {
		s.world.RemoveEntity(squad)
		return
	}
	// Copy roster off the squad's storage so we don't read freed memory after
	// RemoveEntity. Members[i] is an entity ID; the copy is cheap (64 bytes).
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

// hasRadiomanInRoster walks every unit and asks "does this soldier carry a
// Radio in Equipment.Secondary?". Phase 12 ships the real implementation -
// previously this was a Phase 9 placeholder returning false. The result goes
// into RadioNetwork.HasRadioman; Phase 20 (Comms) will read that to gate
// player input on radio-less squads.
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
