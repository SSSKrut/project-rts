package entities

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// AircraftFactory materialises an AircraftSpec row into components. It is the
// single seam every source of an airframe goes through: a scenario arrival
// today, a pad or a FARP later (Phase 20 P9). Keep it that way — the second
// source must add a caller, not a parallel spawner.
//
// Note what is absent: OnGround (GroundStick would clamp the airframe to the
// dirt), Collider (no ground avoidance participates), RoadFollower and
// RoadRoute (an aircraft has no relationship with the road graph). Those
// omissions ARE the P10 exclusion — an airframe that quietly grew a Collider
// would start blocking doorways from 300 m up.
type AircraftFactory struct {
	world  *ecs.World
	posMap *ecs.Map[components.WorldPos]

	AircraftMap    *ecs.Map[components.Aircraft]
	ArrivalMap     *ecs.Map[components.AirArrival]
	ActionQueueMap *ecs.Map[components.ActionQueue]
	MotionMap      *ecs.Map[components.Motion]
	HPMap          *ecs.Map[components.HP]
	FactionMap     *ecs.Map[components.Faction]
	ControllerMap  *ecs.Map[components.Controller]
	SensorsMap     *ecs.Map[components.Sensors]
	AwarenessMap   *ecs.Map[components.Awareness]
	DetectMap      *ecs.Map[components.Detectability]
	ThreatMap      *ecs.Map[components.Threat]
	DangerMap      *ecs.Map[components.DangerBuffer]
	OverrideMap    *ecs.Map[components.AircraftOverride]
}

func NewAircraftFactory(world *ecs.World, posMap *ecs.Map[components.WorldPos]) *AircraftFactory {
	return &AircraftFactory{
		world:          world,
		posMap:         posMap,
		AircraftMap:    ecs.NewMap[components.Aircraft](world),
		ArrivalMap:     ecs.NewMap[components.AirArrival](world),
		ActionQueueMap: ecs.NewMap[components.ActionQueue](world),
		MotionMap:      ecs.NewMap[components.Motion](world),
		HPMap:          ecs.NewMap[components.HP](world),
		FactionMap:     ecs.NewMap[components.Faction](world),
		ControllerMap:  ecs.NewMap[components.Controller](world),
		SensorsMap:     ecs.NewMap[components.Sensors](world),
		AwarenessMap:   ecs.NewMap[components.Awareness](world),
		DetectMap:      ecs.NewMap[components.Detectability](world),
		ThreatMap:      ecs.NewMap[components.Threat](world),
		DangerMap:      ecs.NewMap[components.DangerBuffer](world),
		OverrideMap:    ecs.NewMap[components.AircraftOverride](world),
	}
}

// aircraftSensors builds the channel set. The radar arrives SWITCHED OFF: an
// emitter that has to be turned on is a decision, and a decision the player
// never made is not one he can be blamed for (P4).
func aircraftSensors(spec *components.AircraftSpec) components.Sensors {
	var s components.Sensors
	s.Channels[0] = components.SensorChannel{
		Kind:        components.SensorOptical,
		BaseRangeM:  spec.SensorRangeM,
		FalloffKind: components.FalloffLinear,
		Facing:      components.AircraftOpticalProfile,
		DetectMask:  components.DimAll,
	}
	s.Count = 1
	if spec.RadarRangeM > 0 {
		s.Channels[s.Count] = components.SensorChannel{
			Kind:        components.SensorRadar,
			BaseRangeM:  spec.RadarRangeM,
			EmitRangeM:  spec.RadarEmitM,
			FalloffKind: components.FalloffLinear,
			Facing:      components.OmniProfile,
			DetectMask:  components.DimVehicle | components.DimAir | components.DimNaval,
		}
		s.SetChannel(s.Count, false)
		s.Count++
	}
	if spec.ESMRangeM > 0 {
		s.Channels[s.Count] = components.SensorChannel{
			Kind:        components.SensorESM,
			BaseRangeM:  spec.ESMRangeM,
			FalloffKind: components.FalloffStep,
			Facing:      components.OmniProfile,
			DetectMask:  components.DimAll,
		}
		s.Count++
	}
	return s
}

// Arrival files a scheduled appearance. The arrival is an entity so it saves,
// hashes and dies like anything else, instead of being a flag inside a
// resource nobody can serialise.
func (f *AircraftFactory) Arrival(a components.AirArrival) ecs.Entity {
	ent := f.world.NewEntity()
	rec := a
	f.ArrivalMap.Add(ent, &rec)
	return ent
}

// Spawn releases an airframe into the world at `pos`, heading for `exit` when
// its work is done.
func (f *AircraftFactory) Spawn(pos, exit components.WorldPos, kind components.AircraftKind,
	altRef components.AltRef, altSet, speedSet float32,
	factionID, controller uint8) ecs.Entity {
	spec := components.SpecForAircraft(kind)
	if altSet <= 0 {
		altSet = spec.DefaultAltAGL
	}
	if speedSet <= 0 {
		speedSet = spec.CruiseSpeed
	}
	ent := f.world.NewEntity()
	wp := pos
	f.posMap.Add(ent, &wp)
	f.AircraftMap.Add(ent, &components.Aircraft{
		Kind:     kind,
		AltRef:   altRef,
		AltSet:   altSet,
		SpeedSet: speedSet,
		Fuel:     spec.FuelSec,
		Exit:     exit,
	})
	f.ActionQueueMap.Add(ent, &components.ActionQueue{})
	f.MotionMap.Add(ent, &components.Motion{})
	f.HPMap.Add(ent, &components.HP{Current: spec.HP, Max: spec.HP})
	f.FactionMap.Add(ent, &components.Faction{ID: factionID})
	f.ControllerMap.Add(ent, &components.Controller{Owner: controller})
	sensors := aircraftSensors(spec)
	f.SensorsMap.Add(ent, &sensors)
	f.AwarenessMap.Add(ent, &components.Awareness{})
	f.DetectMap.Add(ent, &components.Detectability{})
	f.ThreatMap.Add(ent, &components.Threat{})
	f.DangerMap.Add(ent, &components.DangerBuffer{})
	f.OverrideMap.Add(ent, &components.AircraftOverride{})
	return ent
}
