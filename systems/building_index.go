package systems

import (
	"github.com/mlange-42/ark/ecs"
)

// BuildingChildIndex maps a building root entity to its currently-live child
// entities. Populated by BuildingSystem on chunk-respawn, drained by
// TerrainStreamingSystem.evict so the children are torn down with their host
// chunk while the root (AlwaysActive) lives on. Building-keyed (not
// chunk-keyed) so Phase 11 destruction can address one building.
type BuildingChildIndex struct {
	Loaded map[ecs.Entity][]ecs.Entity
}

func NewBuildingChildIndex() BuildingChildIndex {
	return BuildingChildIndex{
		Loaded: make(map[ecs.Entity][]ecs.Entity),
	}
}
