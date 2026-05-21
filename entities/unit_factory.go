// Package entities holds ECS-spawn helpers that bundle the per-archetype map
// handles + the init defaults for a single entity kind. main.go uses a
// factory in place of a long NewMap + Add boilerplate block per spawn site.
//
// Factories are NOT systems - they don't tick. They construct entities on
// demand from external callers (main.go starter scene, SquadService.
// CreateFromTemplate, future spawn rules). Map handles are built once in
// NewXxxFactory(world); Spawn() reuses them without re-resolving archetypes.
package entities

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// UnitFactory bundles every Map handle needed to spawn a "naked" infantry
// unit - every component the simulation reads (Unit / Stance / Motion /
// Vision / Threat / DangerBuffer / MicroPath / Awareness / LocalBlackboard /
// ActionQueue / Collider / WorldPos) but no UnitRole and no Equipment
// sub-entities. RoleService.AssignRole stamps the role + spawns the
// weapon/gear afterwards.
//
// External callers occasionally need to read certain unit components (the
// Inspector and the RMB handler look at Stance / Motion / Threat /
// ActionQueue). The factory exposes them as exported pointer fields so a
// single line in main.go (`units := entities.NewUnitFactory(world, posMap)`)
// replaces ~22 lines of per-component plumbing.
type UnitFactory struct {
	world  *ecs.World
	posMap *ecs.Map[components.WorldPos]

	// Public read-handles - kept exported so the Inspector / input layer can
	// avoid a duplicate NewMap call (handle is a thin wrapper, but the
	// duplication added noise).
	StanceMap       *ecs.Map[components.Stance]
	MotionMap       *ecs.Map[components.Motion]
	ThreatMap       *ecs.Map[components.Threat]
	ActionQueueMap  *ecs.Map[components.ActionQueue]
	AwarenessMap    *ecs.Map[components.Awareness]
	MicroPathMap    *ecs.Map[components.MicroPath]
	DangerBufMap    *ecs.Map[components.DangerBuffer]
	BlackboardMap   *ecs.Map[components.LocalBlackboard]
	ColliderMap     *ecs.Map[components.Collider]
	VisionMap       *ecs.Map[components.Vision]
	UnitMap         *ecs.Map[components.Unit]
}

// NewUnitFactory pre-builds every Map handle. Pass in the shared `posMap`
// the rest of main.go uses for terrain anchors / building roots so we don't
// fork the WorldPos archetype writer.
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
		VisionMap:      ecs.NewMap[components.Vision](world),
		UnitMap:        ecs.NewMap[components.Unit](world),
	}
}

// Spawn creates a unit entity at `pos` with the canonical starter component
// set: standing, idle, vision range 40 m / cone ~60 deg half-angle, empty
// action queue. Caller (typically RoleService) stamps the role and weapon
// afterwards.
func (f *UnitFactory) Spawn(pos components.WorldPos) ecs.Entity {
	ent := f.world.NewEntity()
	wp := pos
	f.posMap.Add(ent, &wp)
	f.UnitMap.Add(ent, &components.Unit{})
	f.StanceMap.Add(ent, &components.Stance{Code: components.StanceStand})
	f.MotionMap.Add(ent, &components.Motion{})
	f.ColliderMap.Add(ent, &components.Collider{Radius: 0.35})
	f.VisionMap.Add(ent, &components.Vision{RangeM: 40, AngleDot: 0.5})
	f.ThreatMap.Add(ent, &components.Threat{})
	f.DangerBufMap.Add(ent, &components.DangerBuffer{})
	f.MicroPathMap.Add(ent, &components.MicroPath{})
	f.AwarenessMap.Add(ent, &components.Awareness{})
	f.BlackboardMap.Add(ent, &components.LocalBlackboard{})
	f.ActionQueueMap.Add(ent, &components.ActionQueue{})
	return ent
}
