package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// PropChunkIndex is a singleton: ChunkCoord -> live prop entities born in that
// chunk. Populated by PropSpawnSystem and consumed by TerrainStreamingSystem
// at chunk eviction; lets us release every prop in O(props-in-chunk) instead
// of an O(all-props) filter scan.
//
// Bridges and water-props go through the same index - the host chunk owns the
// lifecycle, so when it evicts the prop disappears with it. Deterministic
// respawn on re-entry.
type PropChunkIndex struct {
	Loaded map[components.ChunkCoord][]ecs.Entity
}

func NewPropChunkIndex() PropChunkIndex {
	return PropChunkIndex{
		Loaded: make(map[components.ChunkCoord][]ecs.Entity),
	}
}
