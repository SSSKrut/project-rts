package entities

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// VehicleFactory bundles the Map handles for spawning a ground vehicle.
// Class differences come from components.VehicleSpecs; the factory only
// materialises the spec row into components.
type VehicleFactory struct {
	world  *ecs.World
	posMap *ecs.Map[components.WorldPos]

	VehicleMap      *ecs.Map[components.Vehicle]
	TurretMap       *ecs.Map[components.Turret]
	ActionQueueMap  *ecs.Map[components.ActionQueue]
	RoadFollowerMap *ecs.Map[components.RoadFollower]
	RoadRouteMap    *ecs.Map[components.RoadRoute]
	MotionMap       *ecs.Map[components.Motion]
	ColliderMap     *ecs.Map[components.Collider]
	HPMap           *ecs.Map[components.HP]
	FactionMap      *ecs.Map[components.Faction]
	ControllerMap   *ecs.Map[components.Controller]
	SensorsMap      *ecs.Map[components.Sensors]
	AwarenessMap    *ecs.Map[components.Awareness]
	EquipmentMap    *ecs.Map[components.Equipment]
	DetectMap       *ecs.Map[components.Detectability]
	OnGroundMap     *ecs.Map[components.OnGround]
	RadioMap        *ecs.Map[components.Radio]
	CommsMap        *ecs.Map[components.CommsState]
	RelayMap        *ecs.Map[components.Relay]
	WeaponMap       *ecs.Map[components.Weapon]
	OwnedByMap      *ecs.Map[components.OwnedBy]
	ThreatMap       *ecs.Map[components.Threat]
	DangerMap       *ecs.Map[components.DangerBuffer]
	OverrideMap     *ecs.Map[components.VehicleOverride]
}

func NewVehicleFactory(world *ecs.World, posMap *ecs.Map[components.WorldPos]) *VehicleFactory {
	return &VehicleFactory{
		world:           world,
		posMap:          posMap,
		VehicleMap:      ecs.NewMap[components.Vehicle](world),
		TurretMap:       ecs.NewMap[components.Turret](world),
		ActionQueueMap:  ecs.NewMap[components.ActionQueue](world),
		RoadFollowerMap: ecs.NewMap[components.RoadFollower](world),
		RoadRouteMap:    ecs.NewMap[components.RoadRoute](world),
		MotionMap:       ecs.NewMap[components.Motion](world),
		ColliderMap:     ecs.NewMap[components.Collider](world),
		HPMap:           ecs.NewMap[components.HP](world),
		FactionMap:      ecs.NewMap[components.Faction](world),
		ControllerMap:   ecs.NewMap[components.Controller](world),
		SensorsMap:      ecs.NewMap[components.Sensors](world),
		AwarenessMap:    ecs.NewMap[components.Awareness](world),
		EquipmentMap:    ecs.NewMap[components.Equipment](world),
		DetectMap:       ecs.NewMap[components.Detectability](world),
		OnGroundMap:     ecs.NewMap[components.OnGround](world),
		RadioMap:        ecs.NewMap[components.Radio](world),
		CommsMap:        ecs.NewMap[components.CommsState](world),
		RelayMap:        ecs.NewMap[components.Relay](world),
		WeaponMap:       ecs.NewMap[components.Weapon](world),
		OwnedByMap:      ecs.NewMap[components.OwnedBy](world),
		ThreatMap:       ecs.NewMap[components.Threat](world),
		DangerMap:       ecs.NewMap[components.DangerBuffer](world),
		OverrideMap:     ecs.NewMap[components.VehicleOverride](world),
	}
}

func vehicleSensors(spec *components.VehicleSpec) components.Sensors {
	var s components.Sensors
	s.Channels[0] = components.SensorChannel{
		Kind:        components.SensorOptical,
		BaseRangeM:  spec.SensorRangeM,
		FalloffKind: components.FalloffLinear,
		Facing:      components.VehicleOpticalProfile,
		DetectMask:  components.DimAll,
	}
	s.Count = 1
	// Unlike the airborne set, a ground radar arrives ON: it belongs to an
	// AI crew whose whole job is watching the sky, and going dark under fire
	// is the crew's reflex (GoDark), not a default.
	if spec.RadarRangeM > 0 {
		s.Channels[s.Count] = components.SensorChannel{
			Kind:        components.SensorRadar,
			BaseRangeM:  spec.RadarRangeM,
			EmitRangeM:  spec.RadarEmitM,
			FalloffKind: components.FalloffLinear,
			Facing:      components.OmniProfile,
			DetectMask:  components.DimVehicle | components.DimAir | components.DimNaval,
		}
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

// Spawn creates a vehicle of `kind` at `pos` with the spec-derived component
// set.
func (f *VehicleFactory) Spawn(pos components.WorldPos, kind components.VehicleKind,
	factionID, controller uint8) ecs.Entity {
	spec := components.SpecForVehicle(kind)
	ent := f.world.NewEntity()
	wp := pos
	f.posMap.Add(ent, &wp)
	f.VehicleMap.Add(ent, &components.Vehicle{Kind: kind})
	f.ActionQueueMap.Add(ent, &components.ActionQueue{})
	f.RoadFollowerMap.Add(ent, &components.RoadFollower{Edge: -1})
	f.RoadRouteMap.Add(ent, &components.RoadRoute{})
	f.MotionMap.Add(ent, &components.Motion{})
	f.ColliderMap.Add(ent, &components.Collider{Radius: spec.ColliderR})
	f.HPMap.Add(ent, &components.HP{Current: spec.HP, Max: spec.HP})
	f.FactionMap.Add(ent, &components.Faction{ID: factionID})
	f.ControllerMap.Add(ent, &components.Controller{Owner: controller})
	sensors := vehicleSensors(spec)
	f.SensorsMap.Add(ent, &sensors)
	f.AwarenessMap.Add(ent, &components.Awareness{})
	eq := components.Equipment{}
	for i := uint8(0); i < spec.WeaponCount && i < 2; i++ {
		wep := f.spawnWeapon(ent, wp, spec.WeaponKinds[i])
		if i == 0 {
			eq.Primary = wep
			eq.Active = wep
		} else {
			eq.Secondary = wep
		}
	}
	f.EquipmentMap.Add(ent, &eq)
	f.DetectMap.Add(ent, &components.Detectability{})
	f.OnGroundMap.Add(ent, &components.OnGround{})
	f.ThreatMap.Add(ent, &components.Threat{})
	f.DangerMap.Add(ent, &components.DangerBuffer{})
	f.OverrideMap.Add(ent, &components.VehicleOverride{})
	// Every hull carries a built-in set; the player switches it off to go
	// quiet, exactly as with the radar.
	f.RadioMap.Add(ent, &components.Radio{On: true, EmitRangeM: components.RadioEmitDefaultM})
	// Comms state rides with the radio: a hull is answerable the moment it
	// exists, not only once an order has made it commandable.
	f.CommsMap.Add(ent, &components.CommsState{})
	if spec.RelayRangeM > 0 {
		f.RelayMap.Add(ent, &components.Relay{RangeM: spec.RelayRangeM, Active: true})
	}
	if spec.TurretSlewDps > 0 {
		f.TurretMap.Add(ent, &components.Turret{})
	}
	return ent
}

func (f *VehicleFactory) spawnWeapon(owner ecs.Entity, pos components.WorldPos,
	kind components.WeaponKind) ecs.Entity {
	spec := components.SpecForWeapon(kind)
	ent := f.world.NewEntity()
	f.WeaponMap.Add(ent, &components.Weapon{
		Kind:       kind,
		Ammo:       spec.Ammo,
		RangeM:     spec.RangeM,
		RoF:        spec.RoF,
		Damage:     spec.Damage,
		Dispersion: spec.Dispersion,
	})
	f.OwnedByMap.Add(ent, &components.OwnedBy{Owner: owner})
	wp := pos
	f.posMap.Add(ent, &wp)
	return ent
}
