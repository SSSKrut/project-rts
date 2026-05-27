package systems

import "github.com/mlange-42/ark/ecs"

// CoverSlotIndex is a singleton: host entity (prop / wall / corner anchor) →
// the cover-slot entities that grew from it. Gives SpatialBakeSystem an O(1)
// "does this host already have slots?" check on chunk-respawn (slot generators
// are not idempotent — calling them twice would double-spawn).
//
// Slot lifecycle is also pinned to PropChunkIndex / BuildingChildIndex so an
// evicted chunk takes its slots with it. ByHost is updated in lockstep with
// those indices; both views must agree.
type CoverSlotIndex struct {
	ByHost map[ecs.Entity][]ecs.Entity
}

func NewCoverSlotIndex() CoverSlotIndex {
	return CoverSlotIndex{
		ByHost: make(map[ecs.Entity][]ecs.Entity),
	}
}
