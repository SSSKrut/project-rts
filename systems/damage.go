package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// DamageService is the sole authorised mutator of HP and the canonical death
// path for combat damage. Phase 14 M14.1 ships the minimal viable shape:
//
//   - Apply(target, dmg) decrements HP.Current; on <= 0 it triggers
//     ApplyDeath, which despawns the unit cleanly (Leave squad, destroy
//     equipment sub-entities, RemoveEntity).
//   - No wounded state, no corpses, no lootable equipment — those land in
//     Phase 15 / 25.
//
// Pattern mirrors Stamper / NavService / SquadService: a pre-built handle
// object created in main.go after the ECS world and the SquadService exist,
// then handed to WeaponSystem (M14.2) for use from its serial post-pass.
// It is intentionally *not* a core.System — there's no per-tick Update; the
// only entry points are the public Apply / ApplyDeath methods, which run
// inline at the call site after the parallel raycast phase resolves hits.
//
// SquadService.Leave handles roster compaction and auto-despawns the squad
// when its last member dies, so callers don't have to special-case empty
// squads.
type DamageService struct {
	world          *ecs.World
	hpMap          *ecs.Map[components.HP]
	equipmentMap   *ecs.Map[components.Equipment]
	factionMap     *ecs.Map[components.Faction]
	squadMemberMap *ecs.Map[components.SquadMember]
	// Phase 14.6 M14.6.0 — awareness sweep on death wipes any LastSeen entry
	// pointing at the just-killed unit, so readers (WeaponSystem.pickTarget,
	// future tactical AI) can't dereference a recycled slot through stale
	// Awareness data.
	awarenessFilter *ecs.Filter1[components.Awareness]
	squadService    *SquadService
}

// NewDamageService wires the map handles. Must be called after the world
// exists and after SquadService is constructed.
func NewDamageService(w *ecs.World, squads *SquadService) *DamageService {
	return &DamageService{
		world:           w,
		hpMap:           ecs.NewMap[components.HP](w),
		equipmentMap:    ecs.NewMap[components.Equipment](w),
		factionMap:      ecs.NewMap[components.Faction](w),
		squadMemberMap:  ecs.NewMap[components.SquadMember](w),
		awarenessFilter: ecs.NewFilter1[components.Awareness](w),
		squadService:    squads,
	}
}

// Apply decrements `target`'s HP by `dmg`. Returns true if this hit killed
// the unit (HP transitioned past 0); false on glancing damage or no-op (dead
// target, no HP component). Dispatches ApplyDeath inline on a lethal hit so
// callers don't need to chain calls.
//
// Safe to call from a serial post-pass: archetype mutations (equipment
// destroy, squad leave, RemoveEntity) happen here, not inside a parallel
// query.
func (d *DamageService) Apply(target ecs.Entity, dmg float32) bool {
	if target == (ecs.Entity{}) || !d.world.Alive(target) {
		return false
	}
	hp := d.hpMap.Get(target)
	if hp == nil {
		return false
	}
	if hp.Current <= 0 {
		// Already dead this tick but not yet reaped (multiple hits landed
		// in the same parallel batch). Skip — ApplyDeath was called once.
		return false
	}
	hp.Current -= dmg
	if hp.Current > 0 {
		return false
	}
	hp.Current = 0
	d.ApplyDeath(target)
	return true
}

// ApplyDeath despawns a unit cleanly:
//  1. Sweep every live unit's Awareness FIFO to clear LastSeen slots that
//     point at this entity (Phase 14.6 M14.6.0 — closes Issue #11 class).
//  2. Detach from squad (SquadService.Leave compacts the roster and
//     auto-despawns the squad when emptied).
//  3. Destroy Primary / Secondary equipment sub-entities (mirrors the
//     RoleService.AssignRole teardown logic — no leaked weapon entities).
//  4. world.RemoveEntity(unit).
//
// Idempotent: a dead / zero entity short-circuits to no-op.
func (d *DamageService) ApplyDeath(unit ecs.Entity) {
	if unit == (ecs.Entity{}) || !d.world.Alive(unit) {
		return
	}
	// Capture equipment IDs by value before SquadService.Leave touches the
	// archetype — Ark's swap-on-remove compaction would invalidate a held
	// pointer otherwise.
	var primary, secondary ecs.Entity
	if eq := d.equipmentMap.Get(unit); eq != nil {
		primary = eq.Primary
		secondary = eq.Secondary
	}
	d.sweepAwareness(unit)
	if d.squadService != nil && d.squadMemberMap.Has(unit) {
		d.squadService.Leave(unit)
	}
	if primary != (ecs.Entity{}) && d.world.Alive(primary) {
		d.world.RemoveEntity(primary)
	}
	if secondary != (ecs.Entity{}) && d.world.Alive(secondary) {
		d.world.RemoveEntity(secondary)
	}
	if d.world.Alive(unit) {
		d.world.RemoveEntity(unit)
	}
}

// sweepAwareness walks every live unit's Awareness FIFO and clears every
// LastSeen slot whose Target is `dying`. Cheap one-pass O(units * 8). Runs
// before RemoveEntity so readers iterating after death can't recover the
// stale id through Awareness; defensive readers (pickTarget alive-check)
// catch the rest.
func (d *DamageService) sweepAwareness(dying ecs.Entity) {
	if d.awarenessFilter == nil || dying == (ecs.Entity{}) {
		return
	}
	q := d.awarenessFilter.Query()
	for q.Next() {
		aware := q.Get()
		for i := range aware.LastSeen {
			if aware.LastSeen[i].Target == dying {
				aware.LastSeen[i] = components.AwarenessEntry{}
			}
		}
	}
}

// HPMap exposes the HP handle for read-only callers (Inspector single-unit
// view, render-time HP bar). Returned pointer must not be retained across
// archetype mutations — caller uses it inline.
func (d *DamageService) HPMap() *ecs.Map[components.HP] { return d.hpMap }

// FactionMap exposes the Faction handle for read-only callers (WeaponSystem
// hostility gate, map renderer faction tint).
func (d *DamageService) FactionMap() *ecs.Map[components.Faction] { return d.factionMap }
