package systems

import (
	"math"

	"rts-go/components"
)

// pointToSegment2D - distance from (px, pz) to segment (ax,az)-(bx,bz) in XZ.
// Standard projection-clamp formula. Degenerate (zero-length) segments fall
// through to point-distance.
func pointToSegment2D(px, pz, ax, az, bx, bz float32) float32 {
	dx := bx - ax
	dz := bz - az
	lenSq := dx*dx + dz*dz
	if lenSq <= 0 {
		ex := px - ax
		ez := pz - az
		return float32(math.Sqrt(float64(ex*ex + ez*ez)))
	}
	t := ((px-ax)*dx + (pz-az)*dz) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := ax + t*dx
	cz := az + t*dz
	ex := px - cx
	ez := pz - cz
	return float32(math.Sqrt(float64(ex*ex + ez*ez)))
}

// nearestRiverDistance - smallest 2D distance from (wx, wz) to any segment
// of any river polyline. +Inf when polylines is empty so callers don't need
// a separate "no rivers" branch.
//
// O(total-segments) per call. Fine with a few hand-authored polylines; for
// procedural river networks, replace with a chunk-bucketed spatial index.
func nearestRiverDistance(wx, wz float32, polylines []components.RiverPolyline) float32 {
	best := float32(math.Inf(1))
	for pi := range polylines {
		pts := polylines[pi].Points
		for i := 0; i+1 < len(pts); i++ {
			ax := float32(pts[i].Chunk.X)*components.ChunkSize + pts[i].Local.X
			az := float32(pts[i].Chunk.Z)*components.ChunkSize + pts[i].Local.Z
			bx := float32(pts[i+1].Chunk.X)*components.ChunkSize + pts[i+1].Local.X
			bz := float32(pts[i+1].Chunk.Z)*components.ChunkSize + pts[i+1].Local.Z
			d := pointToSegment2D(wx, wz, ax, az, bx, bz)
			if d < best {
				best = d
			}
		}
	}
	return best
}

// polylineWorldBBox - axis-aligned XZ bounding box of the polyline in world
// coords. Used by RiverSystem for cheap chunk-vs-river rejection.
func polylineWorldBBox(pl components.RiverPolyline) (minX, minZ, maxX, maxZ float32) {
	minX = float32(math.Inf(1))
	minZ = float32(math.Inf(1))
	maxX = float32(math.Inf(-1))
	maxZ = float32(math.Inf(-1))
	for i := range pl.Points {
		wx := float32(pl.Points[i].Chunk.X)*components.ChunkSize + pl.Points[i].Local.X
		wz := float32(pl.Points[i].Chunk.Z)*components.ChunkSize + pl.Points[i].Local.Z
		if wx < minX {
			minX = wx
		}
		if wx > maxX {
			maxX = wx
		}
		if wz < minZ {
			minZ = wz
		}
		if wz > maxZ {
			maxZ = wz
		}
	}
	return
}
