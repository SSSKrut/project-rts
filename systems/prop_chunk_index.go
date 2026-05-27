package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// PropChunkIndex is a singleton: ChunkCoord → live prop entities born in
// that chunk. Lets the streaming system release every prop in
// O(props-in-chunk) instead of an O(all-props) filter scan.
type PropChunkIndex struct {
	Loaded map[components.ChunkCoord][]ecs.Entity
}

func NewPropChunkIndex() PropChunkIndex {
	return PropChunkIndex{
		Loaded: make(map[components.ChunkCoord][]ecs.Entity),
	}
}
