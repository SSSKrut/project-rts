package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ControlPointSystem resolves capture by presence and keeps each point's relay
// honest. It runs BEFORE comms so that losing a point drops the net in the same
// tick the point changes hands — that immediacy is the whole reason points and
// radios are one mechanic rather than two.
type ControlPointSystem struct {
	pointFilter *ecs.Filter2[components.ControlPoint, components.WorldPos]
	unitFilter  *ecs.Filter3[components.Unit, components.WorldPos, components.Faction]
	vehFilter   *ecs.Filter3[components.Vehicle, components.WorldPos, components.Faction]
	hpMap       *ecs.Map[components.HP]
	relayMap    *ecs.Map[components.Relay]
	factionMap  *ecs.Map[components.Faction]
	eventLogRes ecs.Resource[components.EventLog]
	world       *ecs.World

	bodies []bodySnap
}

type bodySnap struct {
	x, z    float32
	faction uint8
}

func NewControlPointSystem() *ControlPointSystem { return &ControlPointSystem{} }

func (sys *ControlPointSystem) InitUI(w *ecs.World) {
	sys.pointFilter = ecs.NewFilter2[components.ControlPoint, components.WorldPos](w)
	sys.unitFilter = ecs.NewFilter3[components.Unit, components.WorldPos, components.Faction](w)
	sys.vehFilter = ecs.NewFilter3[components.Vehicle, components.WorldPos, components.Faction](w)
	sys.hpMap = ecs.NewMap[components.HP](w)
	sys.relayMap = ecs.NewMap[components.Relay](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.eventLogRes = ecs.NewResource[components.EventLog](w)
	sys.world = w
}

func (ControlPointSystem) Name() string { return "control_point" }

func (ControlPointSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ControlPointSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	sys.snapshotBodies()
	if len(sys.bodies) == 0 {
		return
	}
	dt := float32(ctx.Delta.Seconds())

	q := sys.pointFilter.Query()
	for q.Next() {
		cp, pos := q.Get()
		sys.step(q.Entity(), cp, pos, dt, float32(ctx.SimNow))
	}
}

// snapshotBodies flattens every live GROUND combatant once per tick. Aircraft
// are absent on purpose: a point is ground, and a machine that cannot stand on
// it cannot hold it.
func (sys *ControlPointSystem) snapshotBodies() {
	sys.bodies = sys.bodies[:0]
	qU := sys.unitFilter.Query()
	for qU.Next() {
		_, pos, fac := qU.Get()
		if hp := sys.hpMap.Get(qU.Entity()); hp != nil && hp.Current <= 0 {
			continue
		}
		x, z := worldXZ(*pos)
		sys.bodies = append(sys.bodies, bodySnap{x: x, z: z, faction: fac.ID})
	}
	qV := sys.vehFilter.Query()
	for qV.Next() {
		_, pos, fac := qV.Get()
		if hp := sys.hpMap.Get(qV.Entity()); hp != nil && hp.Current <= 0 {
			continue
		}
		x, z := worldXZ(*pos)
		sys.bodies = append(sys.bodies, bodySnap{x: x, z: z, faction: fac.ID})
	}
}

func (sys *ControlPointSystem) step(ent ecs.Entity, cp *components.ControlPoint,
	pos *components.WorldPos, dt, now float32) {

	px, pz := worldXZ(*pos)
	rSq := cp.Radius * cp.Radius
	var counts [components.FactionCount]int
	for i := range sys.bodies {
		b := &sys.bodies[i]
		dx, dz := b.x-px, b.z-pz
		if dx*dx+dz*dz > rSq || int(b.faction) >= len(counts) {
			continue
		}
		counts[b.faction]++
	}

	// The strongest presence that is not the owner is the challenger; the
	// owner's own count is what it has to beat.
	best, bestN := components.FactionNone, 0
	for f, n := range counts {
		if n == 0 || uint8(f) == cp.Owner {
			continue
		}
		if n > bestN {
			best, bestN = uint8(f), n
		}
	}
	ownerN := 0
	if int(cp.Owner) < len(counts) {
		ownerN = counts[cp.Owner]
	}

	cp.Contested = bestN > 0 && ownerN > 0
	sys.syncRelay(ent, cp)

	if cp.Contested {
		return // both sides standing on it: nothing moves
	}
	if bestN == 0 {
		// Nobody pushing. Progress bleeds back so a half-taken point does not
		// sit at 0.9 forever waiting for the same man to walk past again.
		if cp.Progress > 0 {
			cp.Progress -= components.CaptureRate(1) * dt
			if cp.Progress <= 0 {
				cp.Progress = 0
				cp.Challenger = components.FactionNone
			}
		}
		return
	}
	if cp.Challenger != best {
		cp.Challenger = best
		cp.Progress = 0
	}
	cp.Progress += components.CaptureRate(bestN) * dt
	if cp.Progress < 1 {
		return
	}
	cp.Owner = best
	cp.Progress = 0
	cp.Challenger = components.FactionNone
	sys.syncRelay(ent, cp)
	sys.pushCaptureEvent(ent, cp, *pos, now)
}

// syncRelay keeps the point's node matched to who holds it. A contested point
// is silent: the moment an enemy sets foot on it the net in that sector starts
// failing, before the point has actually changed hands.
func (sys *ControlPointSystem) syncRelay(ent ecs.Entity, cp *components.ControlPoint) {
	if cp.RelayRangeM <= 0 {
		return
	}
	r := sys.relayMap.Get(ent)
	if r == nil {
		return
	}
	r.RangeM = cp.RelayRangeM
	r.Active = !cp.Contested && cp.Owner != components.FactionNone
	if f := sys.factionMap.Get(ent); f != nil {
		f.ID = cp.Owner
	}
}

func (sys *ControlPointSystem) pushCaptureEvent(ent ecs.Entity,
	cp *components.ControlPoint, pos components.WorldPos, now float32) {
	log := sys.eventLogRes.Get()
	if log == nil {
		return
	}
	text := "Point taken"
	if cp.Owner != components.FactionPlayer {
		text = "Point lost"
	}
	log.Push(components.EventEntry{
		Kind: components.EventOrderCompleted,
		At:   now,
		Pos:  pos,
		Text: text,
	})
}

// SpawnControlPoint files a point on the map. A point with a relay range gets
// the Relay component up front so ControlPointSystem only ever has to flip it,
// never to add a component mid-query.
func SpawnControlPoint(w *ecs.World, pos components.WorldPos, owner uint8,
	radiusM, relayRangeM float32) ecs.Entity {
	ent := w.NewEntity()
	p := pos
	ecs.NewMap[components.WorldPos](w).Add(ent, &p)
	ecs.NewMap[components.Faction](w).Add(ent, &components.Faction{ID: owner})
	ecs.NewMap[components.ControlPoint](w).Add(ent, &components.ControlPoint{
		Radius:      radiusM,
		Owner:       owner,
		Challenger:  components.FactionNone,
		RelayRangeM: relayRangeM,
	})
	if relayRangeM > 0 {
		ecs.NewMap[components.Relay](w).Add(ent, &components.Relay{
			RangeM: relayRangeM,
			Active: owner != components.FactionNone,
		})
	}
	ecs.NewMap[components.AlwaysActive](w).Add(ent, &components.AlwaysActive{})
	return ent
}
