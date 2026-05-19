package components

import "math"

// AABB2D is an axis-aligned XZ rectangle in world coords. Used for building
// footprints and any other "this region of the surface" mask. Y is ignored -
// terrain height is sampled separately.
type AABB2D struct {
	MinX, MinZ, MaxX, MaxZ float32
}

func (a AABB2D) Contains(x, z float32) bool {
	return x >= a.MinX && x <= a.MaxX && z >= a.MinZ && z <= a.MaxZ
}

// DistanceXZ returns the shortest XZ distance from (x, z) to the rectangle.
// Returns 0 when the point is inside.
func (a AABB2D) DistanceXZ(x, z float32) float32 {
	dx := float32(0)
	if x < a.MinX {
		dx = a.MinX - x
	} else if x > a.MaxX {
		dx = x - a.MaxX
	}
	dz := float32(0)
	if z < a.MinZ {
		dz = a.MinZ - z
	} else if z > a.MaxZ {
		dz = z - a.MaxZ
	}
	if dx == 0 && dz == 0 {
		return 0
	}
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

func (a AABB2D) CenterX() float32 { return 0.5 * (a.MinX + a.MaxX) }
func (a AABB2D) CenterZ() float32 { return 0.5 * (a.MinZ + a.MaxZ) }
func (a AABB2D) SizeX() float32   { return a.MaxX - a.MinX }
func (a AABB2D) SizeZ() float32   { return a.MaxZ - a.MinZ }

// AABB3D is an axis-aligned world-space box. Used by Level volumes (Phase
// 16.A.3) - the loader reads it from a .glb level_<name> mesh bounds; the
// Phase 16.5 generator emits one explicitly per level.
type AABB3D struct {
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ float32
}

func (a AABB3D) CenterX() float32 { return 0.5 * (a.MinX + a.MaxX) }
func (a AABB3D) CenterY() float32 { return 0.5 * (a.MinY + a.MaxY) }
func (a AABB3D) CenterZ() float32 { return 0.5 * (a.MinZ + a.MaxZ) }
func (a AABB3D) SizeX() float32   { return a.MaxX - a.MinX }
func (a AABB3D) SizeY() float32   { return a.MaxY - a.MinY }
func (a AABB3D) SizeZ() float32   { return a.MaxZ - a.MinZ }

func (a AABB3D) AABB2D() AABB2D {
	return AABB2D{MinX: a.MinX, MinZ: a.MinZ, MaxX: a.MaxX, MaxZ: a.MaxZ}
}

func (a AABB3D) ContainsXZ(x, z float32) bool {
	return x >= a.MinX && x <= a.MaxX && z >= a.MinZ && z <= a.MaxZ
}

// OverlapsXZ returns true when projections to the XZ plane intersect.
func (a AABB3D) OverlapsXZ(b AABB3D) bool {
	return a.MinX <= b.MaxX && a.MaxX >= b.MinX && a.MinZ <= b.MaxZ && a.MaxZ >= b.MinZ
}
