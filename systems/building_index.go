package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// BuildingChildIndex maps a building root entity to its currently-live child
// entities. Populated by BuildingSystem on chunk-respawn, drained by
// TerrainStreamingSystem.evict so children are torn down with their host
// chunk while the root (AlwaysActive) lives on. Building-keyed (not
// chunk-keyed) so destruction can address one building.
type BuildingChildIndex struct {
	Loaded map[ecs.Entity][]ecs.Entity
}

func NewBuildingChildIndex() BuildingChildIndex {
	return BuildingChildIndex{
		Loaded: make(map[ecs.Entity][]ecs.Entity),
	}
}

// BuildingPlanIndex maps a building root entity to the full layout spec
// (BuildingPlan) used by BuildingSystem child-spawn pass, plus the Level
// entities spawned once at startup. Plans are written once at startup
// and read each time a chunk hosting the building re-enters stream range.
//
// Level entities are AlwaysActive — they survive chunk eviction so that
// children spawned in any chunk of a multi-chunk building can reference
// them via Furniture.Level / Marker.Level / LevelTransition refs.
type BuildingPlanIndex struct {
	Plans  map[ecs.Entity]*components.BuildingPlan
	Levels map[ecs.Entity][]ecs.Entity
}

func NewBuildingPlanIndex() BuildingPlanIndex {
	return BuildingPlanIndex{
		Plans:  make(map[ecs.Entity]*components.BuildingPlan),
		Levels: make(map[ecs.Entity][]ecs.Entity),
	}
}
