package systems

import (
	"math"
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// CommsSystem computes how well the radio net reaches each commander. That is
// the whole of block A M0 — the number and its band, with no consumers yet;
// order delivery, the autonomy leash and direction finding come in M1-M3.
//
// A commander is anything that can hold an order of its own: a squad, or a
// soloist vehicle / airframe that commands itself (Phase 20.7 L0). One
// component covers all three, so no call site has to ask which kind it has.
//
// The model is deliberately thin (block A P9): quality is a live radio times
// the reach of the nearest friendly relay. No line of sight, no terrain, no
// antenna height — a net that behaves unpredictably is a net the player cannot
// plan around, and planning around it is the game.
type CommsSystem struct {
	commsFilter *ecs.Filter1[components.CommsState]
	relayFilter *ecs.Filter3[components.Relay, components.WorldPos, components.Faction]

	rosterMap    *ecs.Map[components.CommandRoster]
	posMap       *ecs.Map[components.WorldPos]
	equipmentMap *ecs.Map[components.Equipment]
	radioMap     *ecs.Map[components.Radio]
	factionMap   *ecs.Map[components.Faction]
	aircraftMap  *ecs.Map[components.Aircraft]
	hpMap        *ecs.Map[components.HP]
	world        *ecs.World

	relays []relaySnap
}

type relaySnap struct {
	x, z    float32
	rangeM  float32
	faction uint8
}

// commsCadence: comms is a state indicator, not a hot loop. Hash buckets
// spread the recompute the way SquadBrainSystem spreads its decisions.
const (
	commsCadence     = 60 // ticks between recomputes of one commander
	commsBucketCount = 15
)

func NewCommsSystem() *CommsSystem { return &CommsSystem{} }

func (sys *CommsSystem) InitUI(w *ecs.World) {
	sys.commsFilter = ecs.NewFilter1[components.CommsState](w)
	sys.relayFilter = ecs.NewFilter3[components.Relay, components.WorldPos, components.Faction](w)
	sys.rosterMap = ecs.NewMap[components.CommandRoster](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.equipmentMap = ecs.NewMap[components.Equipment](w)
	sys.radioMap = ecs.NewMap[components.Radio](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.aircraftMap = ecs.NewMap[components.Aircraft](w)
	sys.hpMap = ecs.NewMap[components.HP](w)
	sys.world = w
}

func (CommsSystem) Name() string { return "comms" }

func (CommsSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  1 * time.Second,
	}
}

func (sys *CommsSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	sys.snapshotRelays()

	q := sys.commsFilter.Query()
	for q.Next() {
		cs := q.Get()
		ent := q.Entity()
		if !core.ShouldProcessBucket(ent.ID(), commsBucketCount,
			uint32(ctx.TickIndex/(commsCadence/commsBucketCount))) {
			continue
		}
		pos, ok := sys.commanderPos(ent)
		if !ok {
			continue
		}
		fac := uint8(0)
		if f := sys.factionMap.Get(ent); f != nil {
			fac = f.ID
		}
		cs.Quality = sys.quality(ent, &pos, fac)
		cs.Band = components.BandFor(cs.Quality, cs.Band)
	}
}

// snapshotRelays flattens every active relay to XZ + range once per tick. The
// list is tiny (spawns, captured points, command vehicles), so the per-commander
// lookup stays a linear scan instead of a spatial structure nobody needs.
func (sys *CommsSystem) snapshotRelays() {
	sys.relays = sys.relays[:0]
	q := sys.relayFilter.Query()
	for q.Next() {
		r, pos, fac := q.Get()
		if !r.Active || r.RangeM <= 0 {
			continue
		}
		// A destroyed command vehicle carries its node down with it.
		if hp := sys.hpMap.Get(q.Entity()); hp != nil && hp.Current <= 0 {
			continue
		}
		sys.relays = append(sys.relays, relaySnap{
			x:       float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			z:       float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			rangeM:  r.RangeM,
			faction: fac.ID,
		})
	}
}

// commanderPos: a soloist stands somewhere, a squad does not — it is an
// abstract entity with no WorldPos at all, and its place is its roster's.
// Asking the entity first is what keeps a soloist (which carries BOTH its own
// position and a self-roster) reading its own body.
func (sys *CommsSystem) commanderPos(cmd ecs.Entity) (components.WorldPos, bool) {
	if p := sys.posMap.Get(cmd); p != nil {
		return *p, true
	}
	if r := sys.rosterMap.Get(cmd); r != nil {
		return SquadAnchorPos(sys.world, r, sys.posMap)
	}
	return components.WorldPos{}, false
}

func (sys *CommsSystem) quality(cmd ecs.Entity, pos *components.WorldPos, faction uint8) float32 {
	relay := sys.relayFactor(pos, faction, sys.aircraftMap.Has(cmd))
	if relay <= 0 {
		return 0
	}
	return sys.radioFactor(cmd) * relay
}

// radioFactor: a live, switched-on set is full strength; without one the
// commander still hears at half, which is what makes a dead radioman an order
// to fall back rather than a mute unit.
func (sys *CommsSystem) radioFactor(cmd ecs.Entity) float32 {
	if sys.radioOn(cmd) {
		return 1
	}
	if r := sys.rosterMap.Get(cmd); r != nil {
		for i := uint8(0); i < r.Count; i++ {
			m := r.Members[i]
			if m == (ecs.Entity{}) || m == cmd || !sys.world.Alive(m) {
				continue
			}
			if sys.radioOn(m) {
				return 1
			}
		}
	}
	return components.CommsNoRadioFactor
}

// radioOn checks the entity's own built-in set and the one in its equipment.
// A hull carries its radio directly; a rifleman carries his radioman's.
func (sys *CommsSystem) radioOn(ent ecs.Entity) bool {
	if r := sys.radioMap.Get(ent); r != nil && r.On {
		return true
	}
	eq := sys.equipmentMap.Get(ent)
	if eq == nil {
		return false
	}
	for _, item := range [3]ecs.Entity{eq.Secondary, eq.Primary, eq.Active} {
		if item == (ecs.Entity{}) || !sys.world.Alive(item) {
			continue
		}
		if r := sys.radioMap.Get(item); r != nil && r.On {
			return true
		}
	}
	return false
}

// relayFactor is the reach of the best friendly node: linear falloff to zero
// at its range. An airframe multiplies every range — altitude is line of
// sight, and one coefficient is cheaper than modelling it (P10).
func (sys *CommsSystem) relayFactor(pos *components.WorldPos, faction uint8, airborne bool) float32 {
	px := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	pz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	best := float32(0)
	for i := range sys.relays {
		r := &sys.relays[i]
		if r.faction != faction {
			continue
		}
		reach := r.rangeM
		if airborne {
			reach *= components.CommsAirRelayMul
		}
		dx, dz := px-r.x, pz-r.z
		d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		f := 1 - d/reach
		if f > best {
			best = f
		}
	}
	if best > 1 {
		best = 1
	}
	return best
}

// SpawnRelay files a node of the net: a faction spawn today, a captured
// control point in block B. Boot-time helper — it resolves its handles per
// call rather than living on a system nobody else needs.
func SpawnRelay(w *ecs.World, pos components.WorldPos, faction uint8, rangeM float32) ecs.Entity {
	ent := w.NewEntity()
	p := pos
	ecs.NewMap[components.WorldPos](w).Add(ent, &p)
	ecs.NewMap[components.Faction](w).Add(ent, &components.Faction{ID: faction})
	ecs.NewMap[components.Relay](w).Add(ent, &components.Relay{RangeM: rangeM, Active: true})
	ecs.NewMap[components.AlwaysActive](w).Add(ent, &components.AlwaysActive{})
	return ent
}
