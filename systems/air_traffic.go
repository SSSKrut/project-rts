package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AirTrafficSystem owns the two ends of an airframe's life on the map:
// release (a scheduled arrival comes due) and clearance (an egressing
// airframe reaches its exit and leaves).
//
// The release path is deliberately one call — factory.Spawn — reached from
// one place. A pad or a FARP is a SECOND SOURCE of the same release, not a
// second spawner (Phase 20 P9): when it lands it files the same arrival, or
// calls the same seam, and nothing here has to learn about it.
type AirTrafficSystem struct {
	arrivalFilter   *ecs.Filter1[components.AirArrival]
	airFilter       *ecs.Filter2[components.Aircraft, components.WorldPos]
	awarenessFilter *ecs.Filter1[components.Awareness]
	arrivals      *ecs.Map[components.AirArrival]
	factory       airSpawner
	world         *ecs.World
	due           []ecs.Entity
	clear         []ecs.Entity
}

// airSpawner is the seam, narrowed to what this system needs. Keeping it an
// interface rather than the concrete factory keeps systems/ from importing
// entities/ (which imports components/ and would close a cycle).
type airSpawner interface {
	Spawn(pos, exit components.WorldPos, kind components.AircraftKind,
		altRef components.AltRef, altSet, speedSet float32,
		factionID, controller uint8) ecs.Entity
}

func NewAirTrafficSystem() *AirTrafficSystem { return &AirTrafficSystem{} }

// SetSpawner injects the release seam. Separate from the constructor because
// the factory is built with the gameplay handles, after registerSystems has
// already put this system in the pipeline. Until it is set the system still
// runs — it just has nothing to release.
func (sys *AirTrafficSystem) SetSpawner(f airSpawner) { sys.factory = f }

func (sys *AirTrafficSystem) InitUI(w *ecs.World) {
	sys.world = w
	sys.arrivalFilter = ecs.NewFilter1[components.AirArrival](w)
	sys.airFilter = ecs.NewFilter2[components.Aircraft, components.WorldPos](w)
	sys.arrivals = ecs.NewMap[components.AirArrival](w)
	sys.awarenessFilter = ecs.NewFilter1[components.Awareness](w)
}

func (AirTrafficSystem) Name() string { return "air_traffic" }

func (AirTrafficSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *AirTrafficSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	now := float32(ctx.SimNow)

	// Collect first, mutate after: releasing inside the query would change
	// archetypes while Ark holds it locked.
	sys.due = sys.due[:0]
	qa := sys.arrivalFilter.Query()
	for qa.Next() {
		if qa.Get().At <= now {
			sys.due = append(sys.due, qa.Entity())
		}
	}
	qa.Close()

	sys.clear = sys.clear[:0]
	qf := sys.airFilter.Query()
	for qf.Next() {
		ac, pos := qf.Get()
		if !ac.Egressing {
			continue
		}
		if d := pos.Sub(ac.Exit); d.X*d.X+d.Z*d.Z <= airExitRadius*airExitRadius {
			sys.clear = append(sys.clear, qf.Entity())
		}
	}
	qf.Close()

	sys.release()
	for _, ent := range sys.clear {
		if !sys.world.Alive(ent) {
			continue
		}
		// An egress despawn must sweep Awareness exactly like a death does:
		// anyone still watching the departing airframe would otherwise hold a
		// recycled id in its FIFO (found by ai_air_manpads — the MANPADS
		// gunner's utility pass touched the slot one tick after departure).
		sweepAwarenessOf(sys.awarenessFilter, ent)
		sys.world.RemoveEntity(ent)
	}
}

// release turns due arrivals into airframes. The arrival entity dies with the
// release: it is a one-shot record, and leaving it alive would re-spawn the
// airframe every tick.
func (sys *AirTrafficSystem) release() {
	if sys.factory == nil {
		return
	}
	for _, ent := range sys.due {
		if !sys.world.Alive(ent) {
			continue
		}
		a := sys.arrivals.Get(ent)
		if a == nil {
			continue
		}
		rec := *a
		sys.world.RemoveEntity(ent)
		sys.factory.Spawn(rec.Entry, rec.Exit, rec.Kind, rec.AltRef,
			rec.AltSet, rec.SpeedSet, rec.FactionID, rec.Controller)
	}
}

// SendHome puts an airframe on its egress leg. The single public verb for
// "this one is done" — task completion, a player abort and bingo fuel all
// have to end here so an airframe can never be left flying with no reason to.
func SendHome(ac *components.Aircraft, aq *components.ActionQueue) {
	if ac == nil || ac.Egressing {
		return
	}
	ac.Egressing = true
	if aq != nil {
		ClearActions(aq)
	}
}

// AirExitFor picks a map-edge exit for an entry point: straight back out the
// way the airframe came in, `span` metres beyond. Scenario authoring helper —
// it keeps a scene from having to hand-place a point nobody will ever look at.
func AirExitFor(entry components.WorldPos, span float32) components.WorldPos {
	wx := float32(entry.Chunk.X)*components.ChunkSize + entry.Local.X
	wz := float32(entry.Chunk.Z)*components.ChunkSize + entry.Local.Z
	len := float32(math.Sqrt(float64(wx*wx + wz*wz)))
	if len < 1 {
		return entry
	}
	scale := (len + span) / len
	out := components.WorldPos{}
	out.Local.X = wx * scale
	out.Local.Z = wz * scale
	out.Local.Y = entry.Local.Y
	return components.Normalize(out)
}
