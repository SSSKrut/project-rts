package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// HeightKernel returns the per-vertex height delta at offset (dx, dz) from
// the stamp centre, in world units. Implementations are responsible for
// returning 0 outside their effective radius (the caller's bounding box
// only serves to skip far-away vertices cheaply).
type HeightKernel func(dx, dz float32) float32

// Crater returns a kernel that subtracts a smooth round bowl of the given
// depth at the given radius. Profile is half a cosine wave —
//
//	delta(d) = -depth * 0.5 * (1 + cos(pi * d/radius))    for d <= radius
//	delta(d) = 0                                          for d >  radius
//
// — so the lip blends to zero with no sharp edges. Good fit for shell
// impacts and any future explosion crater (P9).
func Crater(depth, radius float32) HeightKernel {
	if radius <= 0 {
		return func(dx, dz float32) float32 { return 0 }
	}
	invR := 1.0 / radius
	return func(dx, dz float32) float32 {
		d2 := dx*dx + dz*dz
		r2 := radius * radius
		if d2 >= r2 {
			return 0
		}
		d := float32(math.Sqrt(float64(d2)))
		factor := 0.5 * (1 + float32(math.Cos(math.Pi*float64(d*invR))))
		return -depth * factor
	}
}

// Stamper applies HeightKernels to live terrain chunks. Service object —
// not a System; its handles are pre-built once via NewStamper and reused so
// each stamp doesn't rebuild Maps. Created in main.go and called from
// interactive sites (debug keys today, ballistics later) (P8).
type Stamper struct {
	indexRes     ecs.Resource[TerrainChunkIndex]
	heightmapMap *ecs.Map[components.Heightmap]
	meshDirtyMap *ecs.Map[components.MeshDirty]
	modifiedMap  *ecs.Map[components.Modified]
}

func NewStamper(w *ecs.World) *Stamper {
	return &Stamper{
		indexRes:     ecs.NewResource[TerrainChunkIndex](w),
		heightmapMap: ecs.NewMap[components.Heightmap](w),
		meshDirtyMap: ecs.NewMap[components.MeshDirty](w),
		modifiedMap:  ecs.NewMap[components.Modified](w),
	}
}

// StampHeightmap applies kernel to every loaded-chunk vertex within radius
// of center (in world XZ). Touched chunks get MeshDirty (so the mesh
// rebuilds next frame) and Modified (so they survive eviction).
//
// Chunks not currently loaded are silently skipped — the streaming layer
// owns load lifecycle, not us. Callers that need a guaranteed effect must
// ensure the affected chunks are within the streaming radius first.
//
// At a chunk seam, both adjacent chunks own a copy of the shared edge
// vertex; both fall inside our chunk-bounds loop so both get the same
// delta from kernel(dx, dz), keeping the seam continuous.
func (s *Stamper) StampHeightmap(center components.WorldPos, kernel HeightKernel, radius float32) {
	idx := s.indexRes.Get()
	if idx == nil {
		return
	}

	cx := float32(center.Chunk.X)*components.ChunkSize + center.Local.X
	cz := float32(center.Chunk.Z)*components.ChunkSize + center.Local.Z

	// Affected-chunk bounding box in chunk coords. Floor-divide so negatives
	// land in the right chunk (e.g. x = -0.5 → chunk -1).
	minCX := int32(math.Floor(float64((cx - radius) / components.ChunkSize)))
	maxCX := int32(math.Floor(float64((cx + radius) / components.ChunkSize)))
	minCZ := int32(math.Floor(float64((cz - radius) / components.ChunkSize)))
	maxCZ := int32(math.Floor(float64((cz + radius) / components.ChunkSize)))

	step := components.ChunkSize / float32(components.ChunkResolution-1)
	r2 := radius * radius

	for ccZ := minCZ; ccZ <= maxCZ; ccZ++ {
		for ccX := minCX; ccX <= maxCX; ccX++ {
			cc := components.ChunkCoord{X: ccX, Z: ccZ}
			ent, ok := idx.Loaded[cc]
			if !ok {
				continue
			}
			hm := s.heightmapMap.Get(ent)
			if hm == nil {
				continue
			}

			baseX := float32(cc.X) * components.ChunkSize
			baseZ := float32(cc.Z) * components.ChunkSize
			touched := false
			for j := 0; j < components.ChunkResolution; j++ {
				wz := baseZ + float32(j)*step
				dz := wz - cz
				if dz > radius || dz < -radius {
					continue
				}
				row := j * components.ChunkResolution
				for i := 0; i < components.ChunkResolution; i++ {
					wx := baseX + float32(i)*step
					dx := wx - cx
					if dx > radius || dx < -radius {
						continue
					}
					if dx*dx+dz*dz > r2 {
						continue
					}
					delta := kernel(dx, dz)
					if delta != 0 {
						hm.Heights[row+i] += delta
						touched = true
					}
				}
			}

			if touched {
				if !s.meshDirtyMap.Has(ent) {
					s.meshDirtyMap.Add(ent, &components.MeshDirty{})
				}
				if !s.modifiedMap.Has(ent) {
					s.modifiedMap.Add(ent, &components.Modified{})
				}
			}
		}
	}
}
