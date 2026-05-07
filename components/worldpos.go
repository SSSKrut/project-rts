package components

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// ChunkSize is the side length of a terrain chunk in world units (meters).
const ChunkSize float32 = 64.0

// ChunkResolution is the per-side vertex count of a chunk heightmap
// (= ChunkSize quads + 1 shared edge vertex with the neighbour).
const ChunkResolution int = 65

// ChunkCoord identifies a chunk on the X/Z plane. Y is not chunked —
// the world has a single vertical column per (X,Z).
type ChunkCoord struct {
	X, Z int32
}

// WorldPos is the canonical position type. Local.X and Local.Z must lie
// in [0, ChunkSize); a value outside that range is a bug — every operation
// that can break the invariant is responsible for renormalising. Local.Y
// is unbounded height.
type WorldPos struct {
	Chunk ChunkCoord
	Local rl.Vector3
}

// Add translates p by world-space vector v and renormalises chunk crossings
// on X/Z. Y is left untouched (height is not chunked).
func (p WorldPos) Add(v rl.Vector3) WorldPos {
	p.Local.X += v.X
	p.Local.Y += v.Y
	p.Local.Z += v.Z
	return Normalize(p)
}

// Sub returns the world-space vector pointing from other to p.
func (p WorldPos) Sub(other WorldPos) rl.Vector3 {
	dcx := float32(p.Chunk.X-other.Chunk.X) * ChunkSize
	dcz := float32(p.Chunk.Z-other.Chunk.Z) * ChunkSize
	return rl.Vector3{
		X: dcx + p.Local.X - other.Local.X,
		Y: p.Local.Y - other.Local.Y,
		Z: dcz + p.Local.Z - other.Local.Z,
	}
}

// ToRenderSpace converts p into a coordinate relative to origin's chunk.
// Render code passes systems.CurrentOriginChunk so float32 precision stays
// bounded near the camera regardless of how far the player has travelled.
func (p WorldPos) ToRenderSpace(origin ChunkCoord) rl.Vector3 {
	return rl.Vector3{
		X: float32(p.Chunk.X-origin.X)*ChunkSize + p.Local.X,
		Y: p.Local.Y,
		Z: float32(p.Chunk.Z-origin.Z)*ChunkSize + p.Local.Z,
	}
}

// DistanceSquared returns the squared world-space distance between a and b.
func DistanceSquared(a, b WorldPos) float32 {
	d := a.Sub(b)
	return d.X*d.X + d.Y*d.Y + d.Z*d.Z
}

// Distance returns the world-space distance between a and b.
func Distance(a, b WorldPos) float32 {
	return float32(math.Sqrt(float64(DistanceSquared(a, b))))
}

// Normalize re-establishes the Local.X/Local.Z ∈ [0, ChunkSize) invariant
// by folding overflow into Chunk. Safe to call on already-normal positions.
func Normalize(p WorldPos) WorldPos {
	// floor-division so negative locals push the chunk in the right direction
	// (e.g. Local.X = -1 with ChunkSize = 64 → chunk -1, Local.X = 63).
	if p.Local.X < 0 || p.Local.X >= ChunkSize {
		shift := int32(math.Floor(float64(p.Local.X / ChunkSize)))
		p.Chunk.X += shift
		p.Local.X -= float32(shift) * ChunkSize
	}
	if p.Local.Z < 0 || p.Local.Z >= ChunkSize {
		shift := int32(math.Floor(float64(p.Local.Z / ChunkSize)))
		p.Chunk.Z += shift
		p.Local.Z -= float32(shift) * ChunkSize
	}
	return p
}
