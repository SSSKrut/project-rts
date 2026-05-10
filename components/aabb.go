package components

import "math"

// AABB2D is an axis-aligned XZ rectangle in world coords. Used for building
// footprints and any other "this region of the surface" mask. Y is ignored —
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
