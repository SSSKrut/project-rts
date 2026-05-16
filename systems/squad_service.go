package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SquadService is the only authorised mutator of Squad / CommandRoster /
// SquadMember. Keeps the bidirectional invariant from PHASE-9.md P2: for every
// SquadMember{s,i} on a unit, CommandRoster(s).Members[i] == that unit.
//
// Pattern mirrors Stamper / NavService — pre-built handles in NewSquadService,
// methods called from outside ECS queries (main.go input handlers; systems
// after closing their filter loops). Archetype mutations (Add/Remove of
// SquadMember) on member units may happen at any time, so callers must finish
// iterating before invoking Leave / CreateFromUnits.
type SquadService struct {
	world           *ecs.World
	squadMap        *ecs.Map[components.Squad]
	rosterMap       *ecs.Map[components.CommandRoster]
	formationMap    *ecs.Map[components.FormationData]
	macroPathMap    *ecs.Map[components.MacroPath]
	radioMap        *ecs.Map[components.RadioNetwork]
	memberMap       *ecs.Map[components.SquadMember]
	alwaysActiveMap *ecs.Map[components.AlwaysActive]
	// Phase 11 order maps. SquadService is the only path that issues / cancels
	// orders, mirroring the "service owns its archetype" pattern from Phase 9.
	orderQueueMap    *ecs.Map[components.OrderQueueHead]
	orderMap         *ecs.Map[components.Order]
	orderKindMap     *ecs.Map[components.OrderKind]
	orderStateMap    *ecs.Map[components.OrderState]
	orderOwnerMap    *ecs.Map[components.OrderOwner]
	orderTargetMap   *ecs.Map[components.OrderTarget]
	orderIssuedAtMap *ecs.Map[components.OrderIssuedAt]
	orderProgressMap *ecs.Map[components.OrderProgress]
	orderChainMap    *ecs.Map[components.OrderChain]
	orderFacingMap   *ecs.Map[components.OrderParamFacing]
	orderPatrolMap   *ecs.Map[components.OrderParamPatrol]
	// Phase 12 handles. hasRadiomanInRoster walks Equipment.Secondary on
	// every member and checks the Radio marker, so SquadService needs both
	// maps live.
	equipmentMap *ecs.Map[components.Equipment]
	radioGearMap *ecs.Map[components.Radio]
	// Session-time clock for OrderIssuedAt. Advanced by SetClock (called
	// from main.go each frame before input handlers run).
	clock float32
}

func NewSquadService(w *ecs.World) *SquadService {
	return &SquadService{
		world:            w,
		squadMap:         ecs.NewMap[components.Squad](w),
		rosterMap:        ecs.NewMap[components.CommandRoster](w),
		formationMap:     ecs.NewMap[components.FormationData](w),
		macroPathMap:     ecs.NewMap[components.MacroPath](w),
		radioMap:         ecs.NewMap[components.RadioNetwork](w),
		memberMap:        ecs.NewMap[components.SquadMember](w),
		alwaysActiveMap:  ecs.NewMap[components.AlwaysActive](w),
		orderQueueMap:    ecs.NewMap[components.OrderQueueHead](w),
		orderMap:         ecs.NewMap[components.Order](w),
		orderKindMap:     ecs.NewMap[components.OrderKind](w),
		orderStateMap:    ecs.NewMap[components.OrderState](w),
		orderOwnerMap:    ecs.NewMap[components.OrderOwner](w),
		orderTargetMap:   ecs.NewMap[components.OrderTarget](w),
		orderIssuedAtMap: ecs.NewMap[components.OrderIssuedAt](w),
		orderProgressMap: ecs.NewMap[components.OrderProgress](w),
		orderChainMap:    ecs.NewMap[components.OrderChain](w),
		orderFacingMap:   ecs.NewMap[components.OrderParamFacing](w),
		orderPatrolMap:   ecs.NewMap[components.OrderParamPatrol](w),
		equipmentMap:     ecs.NewMap[components.Equipment](w),
		radioGearMap:     ecs.NewMap[components.Radio](w),
	}
}

// SetClock updates the session-time used for OrderIssuedAt. main.go calls
// this once per frame before issuing any orders. Separate from app.elapsed
// because SquadService doesn't import core.
func (s *SquadService) SetClock(t float32) { s.clock = t }

// Clock returns the current session clock used for order timestamps. Used by
// OrderResolverSystem to compute progress / timeouts without re-importing
// elapsed.
func (s *SquadService) Clock() float32 { return s.clock }

// FormationSpacing returns the default spacing (metres) for each formation
// kind. F1-F4 hotkeys use this table; Phase 14 may swap it for a slider.
func FormationSpacing(k components.FormationKind) float32 {
	switch k {
	case components.FormationLine:
		return 2.0
	case components.FormationColumn:
		return 2.0
	case components.FormationWedge:
		return 3.0
	case components.FormationLoose:
		return 4.0
	}
	return 2.0
}

// CreateFromUnits spawns a new Squad entity, attaches every unit in `units` as
// a member (pulling them out of any prior squad first), and returns the new
// entity. Excess members beyond SquadRosterSize stay outside the roster as
// soloists. An empty (or all-invalid) input returns the zero Entity.
//
// The function is staged so no live pointer into the Squad archetype is held
// across an operation that might destroy another squad entity in the same
// archetype. Ark uses swap-on-remove storage compaction (storage.go:267),
// which would silently invalidate such a pointer — and this was the cause of
// the original "free(): invalid size" crash on repeated `T` presses.
func (s *SquadService) CreateFromUnits(units []ecs.Entity, kind components.FormationKind) ecs.Entity {
	if len(units) == 0 {
		return ecs.Entity{}
	}

	// Stage 1 — sanitise input: skip zero / dead / duplicate entities and
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

	// Stage 2 — detach every prepared unit from its prior squad. May despawn
	// old squads (cascading from Leave when their last member walks out), so
	// we cannot have spawned the new squad yet — Ark's storage compaction
	// would invalidate a held rosterMap pointer.
	for _, u := range prepared {
		if old := s.memberMap.Get(u); old != nil {
			s.leaveInternal(u, *old)
		}
	}

	// Stage 3 — spawn the new squad with the roster pre-populated as a value.
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

	// Stage 4 — attach SquadMember on every member. Each Add mutates the
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
	// Stage 1 — detach from prior squad if any. May destroy that squad.
	if old := s.memberMap.Get(unit); old != nil {
		if old.Squad == squad {
			return true
		}
		s.leaveInternal(unit, *old)
	}
	// Stage 2 — squad may itself have been the one destroyed (Leave cascaded
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

// CreateFromTemplate is the Phase 12 high-level spawn helper. It walks the
// template's role list, calls `unitFactory(spawnPos)` to materialise each
// unit's base archetype (Unit + Stance + Motion + ... — main.go owns the
// component list), stamps the role + equipment via RoleService.AssignRole,
// then bundles the result into a Squad through CreateFromUnits.
//
// `pos` is the squad centre; units fan out on a 2 m grid. `unitFactory` is a
// callback (rather than a SquadService method) because main.go is the
// canonical owner of the unit component graph — duplicating that list inside
// SquadService would re-implement half of unit.go.
//
// Returns the new Squad entity, or zero if the template was empty / the
// factory failed every slot.
func (s *SquadService) CreateFromTemplate(
	template SquadTemplate,
	pos components.WorldPos,
	formation components.FormationKind,
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
		units = append(units, u)
	}
	if len(units) == 0 {
		return ecs.Entity{}
	}
	return s.CreateFromUnits(units, formation)
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

// OrderParams bundles the optional per-kind fields IssueOrder accepts. Zero
// values are fine for kinds that don't read them.
type OrderParams struct {
	FacingYawRad float32
	HasFacing    bool
	PatrolLoop   bool
}

// IssueOrder spawns a new Order entity owned by `squad`. If append=false the
// existing chain (head + tails) is cancelled first; if append=true the new
// order is appended at the tail (Shift+RMB semantics, PHASE-11.md P9).
//
// kind / target / entityTarget describe what the order *means*; per-kind
// completion lives in OrderResolverSystem. Returns the new Order entity, or
// zero on a no-op (dead squad / missing OrderQueueHead).
func (s *SquadService) IssueOrder(
	squad ecs.Entity,
	kind components.OrderKindCode,
	target components.WorldPos,
	entityTarget ecs.Entity,
	appendToQueue bool,
	params OrderParams,
) ecs.Entity {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return ecs.Entity{}
	}
	head := s.orderQueueMap.Get(squad)
	if head == nil {
		return ecs.Entity{}
	}

	// Cancel mode — wipe the current chain so the new order is the sole head.
	// The wipe deletes order entities and walks the chain via OrderChain.Next.
	if !appendToQueue {
		s.cancelChain(head.First)
		head.First = ecs.Entity{}
	}

	// Spawn the new order entity.
	ord := s.world.NewEntity()
	s.orderMap.Add(ord, &components.Order{})
	s.orderKindMap.Add(ord, &components.OrderKind{Code: kind})
	s.orderStateMap.Add(ord, &components.OrderState{Code: components.OrderStateIssued})
	s.orderOwnerMap.Add(ord, &components.OrderOwner{Squad: squad})
	s.orderTargetMap.Add(ord, &components.OrderTarget{Pos: target, Entity: entityTarget})
	s.orderIssuedAtMap.Add(ord, &components.OrderIssuedAt{Time: s.clock})
	s.orderProgressMap.Add(ord, &components.OrderProgress{})
	s.orderChainMap.Add(ord, &components.OrderChain{})
	if params.HasFacing {
		s.orderFacingMap.Add(ord, &components.OrderParamFacing{YawRad: params.FacingYawRad})
	}
	if kind == components.OrderKindPatrol {
		s.orderPatrolMap.Add(ord, &components.OrderParamPatrol{Loop: params.PatrolLoop})
	}

	// Wire into the chain. The squad's MacroPath also gets ReplanAt=0 so
	// SquadMacroPathSystem replans immediately rather than waiting for the
	// 1 s throttle — this is the "fresh order → don't dawdle" guarantee from
	// PHASE-11.md notes.
	if head.First == (ecs.Entity{}) {
		head.First = ord
		if mp := s.macroPathMap.Get(squad); mp != nil {
			mp.ReplanAt = 0
		}
	} else {
		// Append at tail.
		tail := head.First
		for {
			ch := s.orderChainMap.Get(tail)
			if ch == nil || ch.Next == (ecs.Entity{}) {
				break
			}
			tail = ch.Next
		}
		if ch := s.orderChainMap.Get(tail); ch != nil {
			ch.Next = ord
		}
	}
	return ord
}

// CancelAllOrders aborts the squad's full chain (head + every Next). Used by
// H (Stop) hotkey and by the Phase 11 non-append RMB path.
func (s *SquadService) CancelAllOrders(squad ecs.Entity) {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return
	}
	head := s.orderQueueMap.Get(squad)
	if head == nil {
		return
	}
	s.cancelChain(head.First)
	head.First = ecs.Entity{}
	if mp := s.macroPathMap.Get(squad); mp != nil {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
	}
}

// cancelChain walks Next pointers starting at `start`, marks each order
// Cancelled, and removes the entity. OrderResolverSystem would catch
// Cancelled and remove anyway, but doing it here keeps a clear "no dangling
// orders after CancelAllOrders" invariant.
func (s *SquadService) cancelChain(start ecs.Entity) {
	cur := start
	for cur != (ecs.Entity{}) {
		if !s.world.Alive(cur) {
			break
		}
		next := ecs.Entity{}
		if ch := s.orderChainMap.Get(cur); ch != nil {
			next = ch.Next
		}
		s.world.RemoveEntity(cur)
		cur = next
	}
}

// OrderMoveTo is the Phase 9-compat wrapper. New callers should use
// IssueOrder; this stays so existing test code / hotkeys keep working.
func (s *SquadService) OrderMoveTo(squad ecs.Entity, goal components.WorldPos) {
	s.IssueOrder(squad, components.OrderKindMoveTo, goal, ecs.Entity{}, false, OrderParams{})
}

// Stop is the Phase 9-compat wrapper. Cancels the full chain.
func (s *SquadService) Stop(squad ecs.Entity) {
	s.CancelAllOrders(squad)
}

// hasRadiomanInRoster walks every unit and asks "does this soldier carry a
// Radio in Equipment.Secondary?". Phase 12 ships the real implementation —
// previously this was a Phase 9 placeholder returning false. The result
// goes into RadioNetwork.HasRadioman; Phase 20 (Comms) will read that to
// gate player input on radio-less squads.
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
