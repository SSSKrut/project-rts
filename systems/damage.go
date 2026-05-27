package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// DamageService is the sole authorised mutator of HP and the canonical death
// path for combat damage. Apply decrements HP; on <=0 it triggers ApplyDeath,
// which despawns the unit cleanly (Leave squad, destroy equipment sub-
// entities, RemoveEntity). Not a core.System — entry points run inline from
// the serial post-pass after the parallel raycast resolves hits.
//
// SquadService.Leave handles roster compaction and auto-despawns the squad
// when its last member dies.
type DamageService struct {
	world          *ecs.World
	hpMap          *ecs.Map[components.HP]
	equipmentMap   *ecs.Map[components.Equipment]
	factionMap     *ecs.Map[components.Faction]
	squadMemberMap *ecs.Map[components.SquadMember]
	posMap         *ecs.Map[components.WorldPos]
	// Awareness sweep on death wipes LastSeen entries pointing at the just-
	// killed unit, so readers can't dereference a recycled slot through stale
	// Awareness data.
	awarenessFilter *ecs.Filter1[components.Awareness]
	squadService    *SquadService
	eventLogRes     ecs.Resource[components.EventLog]
	clock           func() float32
	mapPings        *MapPingService
}

// NewDamageService must be called after the world exists and after
// SquadService is constructed.
func NewDamageService(w *ecs.World, squads *SquadService) *DamageService {
	return &DamageService{
		world:           w,
		hpMap:           ecs.NewMap[components.HP](w),
		equipmentMap:    ecs.NewMap[components.Equipment](w),
		factionMap:      ecs.NewMap[components.Faction](w),
		squadMemberMap:  ecs.NewMap[components.SquadMember](w),
		posMap:          ecs.NewMap[components.WorldPos](w),
		awarenessFilter: ecs.NewFilter1[components.Awareness](w),
		squadService:    squads,
		eventLogRes:     ecs.NewResource[components.EventLog](w),
	}
}

func (d *DamageService) SetClock(clock func() float32) { d.clock = clock }

// SetMapPings is nil-safe.
func (d *DamageService) SetMapPings(svc *MapPingService) { d.mapPings = svc }

// Apply decrements `target`'s HP by `dmg`. Returns true if this hit killed
// the unit (HP transitioned past 0). Dispatches ApplyDeath inline on a lethal
// hit. Safe to call from a serial post-pass; not safe inside a parallel
// query because of the archetype mutations it triggers.
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
		// in the same parallel batch); ApplyDeath was called once.
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

// ApplyDeath despawns a unit cleanly: sweeps Awareness FIFOs, detaches from
// squad, destroys equipment sub-entities, removes the entity. Idempotent.
func (d *DamageService) ApplyDeath(unit ecs.Entity) {
	if unit == (ecs.Entity{}) || !d.world.Alive(unit) {
		return
	}
	// Push KIA before tearing down — SquadMember + WorldPos must be sampled
	// before SquadService.Leave removes the membership.
	d.pushKIAEvent(unit)
	// Capture equipment IDs by value before SquadService.Leave touches the
	// archetype — Ark's swap-on-remove would invalidate a held pointer.
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

// pushKIAEvent records a KIA into the global EventLog and drops a red
// MapPing. Squad may be zero for soloists.
func (d *DamageService) pushKIAEvent(unit ecs.Entity) {
	log := d.eventLogRes.Get()
	var squad ecs.Entity
	if sm := d.squadMemberMap.Get(unit); sm != nil {
		squad = sm.Squad
	}
	var pos components.WorldPos
	if p := d.posMap.Get(unit); p != nil {
		pos = *p
	}
	now := float32(0)
	if d.clock != nil {
		now = d.clock()
	}
	if log != nil {
		log.Push(components.EventEntry{
			Kind:  components.EventKIA,
			At:    now,
			Pos:   pos,
			Squad: squad,
			Text:  "Unit KIA",
		})
	}
	if d.mapPings != nil {
		d.mapPings.Spawn(pos, components.MapPing{
			Kind:     components.MapPingKIA,
			SpawnAt:  now,
			TTL:      6.0,
			Color:    rl.Color{R: 230, G: 70, B: 70, A: 255},
			BaseRadM: 6.0,
		})
	}
}

// sweepAwareness clears every LastSeen slot whose Target is `dying`. Runs
// before RemoveEntity so readers iterating after death can't recover the
// stale id through Awareness.
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

// HPMap exposes the HP handle for read-only callers. Returned pointer must
// not be retained across archetype mutations.
func (d *DamageService) HPMap() *ecs.Map[components.HP] { return d.hpMap }

func (d *DamageService) FactionMap() *ecs.Map[components.Faction] { return d.factionMap }
