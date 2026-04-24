package ecs

import (
	"math"
)

// ChunkID represents a unique spatial bucket in 3D space.
type ChunkID [3]int32

// SpatialGrid maps entities into cubic chunks to avoid O(N) distance checks.
type SpatialGrid struct {
	CellSize float32
	buckets  map[ChunkID]*SparseSet
	entityTo ChunkID // Could also be a component. Kept here for clarity.
}

// NewSpatialGrid creates a new spatial grid with specified cell size.
func NewSpatialGrid(cellSize float32) *SpatialGrid {
	return &SpatialGrid{
		CellSize: cellSize,
		buckets:  make(map[ChunkID]*SparseSet),
	}
}

// Clone returns a shallow copy of the grid for double buffering.
func (g *SpatialGrid) Clone() *SpatialGrid {
	clone := NewSpatialGrid(g.CellSize)
	for chunk, bucket := range g.buckets {
		clone.buckets[chunk] = bucket.Clone()
	}
	return clone
}

// PosToChunk converts a 3D position to a ChunkID.
func (g *SpatialGrid) PosToChunk(x, y, z float32) ChunkID {
	return ChunkID{
		int32(math.Floor(float64(x / g.CellSize))),
		int32(math.Floor(float64(y / g.CellSize))),
		int32(math.Floor(float64(z / g.CellSize))),
	}
}

// Add places an entity into the grid bucket.
func (g *SpatialGrid) Add(id EntityID, x, y, z float32) {
	chunk := g.PosToChunk(x, y, z)
	g.ensureBucket(chunk).Set(id, nil)
}

// Remove removes an entity from a specific bucket.
func (g *SpatialGrid) Remove(id EntityID, x, y, z float32) {
	chunk := g.PosToChunk(x, y, z)
	if bucket, ok := g.buckets[chunk]; ok {
		bucket.Remove(id)
	}
}

// Update moves an entity from its old position bucket to its new one.
func (g *SpatialGrid) Update(id EntityID, oldX, oldY, oldZ, newX, newY, newZ float32) {
	oldC := g.PosToChunk(oldX, oldY, oldZ)
	newC := g.PosToChunk(newX, newY, newZ)
	if oldC != newC {
		if bucket, ok := g.buckets[oldC]; ok {
			bucket.Remove(id)
		}
		g.ensureBucket(newC).Set(id, nil)
	}
}

// QueryRadius returns all entities in chunks that intersect the given sphere.
func (g *SpatialGrid) QueryRadius(x, y, z, radius float32) []EntityID {
	minChunk := g.PosToChunk(x-radius, y-radius, z-radius)
	maxChunk := g.PosToChunk(x+radius, y+radius, z+radius)

	var result []EntityID
	for cx := minChunk[0]; cx <= maxChunk[0]; cx++ {
		for cy := minChunk[1]; cy <= maxChunk[1]; cy++ {
			for cz := minChunk[2]; cz <= maxChunk[2]; cz++ {
				if bucket, ok := g.buckets[ChunkID{cx, cy, cz}]; ok {
					result = append(result, bucket.Entities()...)
				}
			}
		}
	}
	return result
}

func (g *SpatialGrid) ensureBucket(id ChunkID) *SparseSet {
	bucket, ok := g.buckets[id]
	if !ok {
		bucket = NewSparseSet()
		g.buckets[id] = bucket
	}
	return bucket
}
