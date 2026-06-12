// Package entities holds ECS-spawn helpers that bundle per-archetype Map
// handles + init defaults for a single entity kind. Map handles are built
// once in NewXxxFactory(world); Spawn() reuses them without re-resolving
// archetypes per call site. Factories are NOT systems - they don't tick.
package entities

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// UnitFactory bundles every Map handle needed to spawn a "naked" infantry
// unit. Spawn() emits the unit-side components only; RoleService.AssignRole
// stamps the role + spawns weapon/gear afterwards. Read-handles are exported
// so the Inspector / input layer can reuse them instead of allocating a
// duplicate NewMap.
type UnitFactory struct {
	world  *ecs.World
	posMap *ecs.Map[components.WorldPos]

	StanceMap       *ecs.Map[components.Stance]
	MotionMap       *ecs.Map[components.Motion]
	ThreatMap       *ecs.Map[components.Threat]
	ActionQueueMap  *ecs.Map[components.ActionQueue]
	AwarenessMap    *ecs.Map[components.Awareness]
	MicroPathMap    *ecs.Map[components.MicroPath]
	DangerBufMap    *ecs.Map[components.DangerBuffer]
	BlackboardMap   *ecs.Map[components.LocalBlackboard]
	ColliderMap     *ecs.Map[components.Collider]
	SensorsMap      *ecs.Map[components.Sensors]
	UnitMap         *ecs.Map[components.Unit]
}

// NewUnitFactory pre-builds every Map handle so per-spawn cost is just Add
// calls. Pass in the shared `posMap` (also used by terrain anchors / building
// roots) so we don't fork the WorldPos archetype writer.
func NewUnitFactory(world *ecs.World, posMap *ecs.Map[components.WorldPos]) *UnitFactory {
	return &UnitFactory{
		world:          world,
		posMap:         posMap,
		StanceMap:      ecs.NewMap[components.Stance](world),
		MotionMap:      ecs.NewMap[components.Motion](world),
		ThreatMap:      ecs.NewMap[components.Threat](world),
		ActionQueueMap: ecs.NewMap[components.ActionQueue](world),
		AwarenessMap:   ecs.NewMap[components.Awareness](world),
		MicroPathMap:   ecs.NewMap[components.MicroPath](world),
		DangerBufMap:   ecs.NewMap[components.DangerBuffer](world),
		BlackboardMap:  ecs.NewMap[components.LocalBlackboard](world),
		ColliderMap:    ecs.NewMap[components.Collider](world),
		SensorsMap:     ecs.NewMap[components.Sensors](world),
		UnitMap:        ecs.NewMap[components.Unit](world),
	}
}

// defaultInfantrySensors - single Optical channel matching the legacy Vision
// values (range 40 m, ~120° forward cone). Linear falloff so detection half-
// range = 20 m at high concealment, full 40 m at no concealment.
func defaultInfantrySensors() components.Sensors {
	var s components.Sensors
	s.Channels[0] = components.SensorChannel{
		Kind:        components.SensorOptical,
		BaseRangeM:  40,
		FalloffKind: components.FalloffLinear,
		Facing:      components.InfantryOpticalProfile,
		DetectMask:  components.DimAll,
	}
	s.Count = 1
	return s
}

// Spawn creates a unit at `pos` with the canonical starter set: standing,
// idle, vision range 40 m / cone ~60 deg half-angle, empty action queue.
func (f *UnitFactory) Spawn(pos components.WorldPos) ecs.Entity {
	ent := f.world.NewEntity()
	wp := pos
	f.posMap.Add(ent, &wp)
	f.UnitMap.Add(ent, &components.Unit{})
	f.StanceMap.Add(ent, &components.Stance{Code: components.StanceStand})
	f.MotionMap.Add(ent, &components.Motion{})
	f.ColliderMap.Add(ent, &components.Collider{Radius: 0.35})
	sensors := defaultInfantrySensors()
	f.SensorsMap.Add(ent, &sensors)
	f.ThreatMap.Add(ent, &components.Threat{})
	f.DangerBufMap.Add(ent, &components.DangerBuffer{})
	f.MicroPathMap.Add(ent, &components.MicroPath{})
	f.AwarenessMap.Add(ent, &components.Awareness{})
	f.BlackboardMap.Add(ent, &components.LocalBlackboard{})
	f.ActionQueueMap.Add(ent, &components.ActionQueue{})
	return ent
}
