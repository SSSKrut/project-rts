package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

const (
	losSampleStep float32 = 1.0
	losClearance  float32 = 0.05
)

// snapshotHeightmaps refills dst with slice views over every loaded chunk's
// Heights array. Read-only during the parallel pass; terrain systems run
// earlier in the tick, so the data is stable.
func snapshotHeightmaps(idx *TerrainChunkIndex, hmMap *ecs.Map[components.Heightmap], dst map[components.ChunkCoord][]float32) {
	clear(dst)
	if idx == nil {
		return
	}
	for cc, ent := range idx.Loaded {
		if hm := hmMap.Get(ent); hm != nil {
			dst[cc] = hm.Heights[:]
		}
	}
}

func terrainHeightAt(hm map[components.ChunkCoord][]float32, wx, wz float32) float32 {
	gx := floorI32(wx)
	gz := floorI32(wz)
	cc := components.ChunkCoord{X: gx >> navGridShift, Z: gz >> navGridShift}
	hs, ok := hm[cc]
	if !ok {
		return GroundHeight(wx, wz)
	}
	li := int(gx & navGridMask)
	lj := int(gz & navGridMask)
	res := components.ChunkResolution
	h00 := hs[lj*res+li]
	h10 := hs[lj*res+li+1]
	h01 := hs[(lj+1)*res+li]
	h11 := hs[(lj+1)*res+li+1]
	fx := wx - float32(gx)
	fz := wz - float32(gz)
	return h00*(1-fx)*(1-fz) + h10*fx*(1-fz) + h01*(1-fx)*fz + h11*fx*fz
}

// terrainBlocksLOS samples the sight line every ~losSampleStep metre and
// reports whether terrain rises above it.
func terrainBlocksLOS(hm map[components.ChunkCoord][]float32, x0, z0, y0, x1, z1, y1 float32) bool {
	_, blocked := terrainHitT(hm, x0, z0, y0, x1, z1, y1)
	return blocked
}

// terrainHitT returns the earliest t in (0,1) where the segment dips below
// the terrain surface.
func terrainHitT(hm map[components.ChunkCoord][]float32, x0, z0, y0, x1, z1, y1 float32) (float32, bool) {
	dx := x1 - x0
	dz := z1 - z0
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	steps := int(dist / losSampleStep)
	if steps < 2 {
		return 0, false
	}
	dy := y1 - y0
	inv := 1 / float32(steps)
	for i := 1; i < steps; i++ {
		t := float32(i) * inv
		if terrainHeightAt(hm, x0+dx*t, z0+dz*t) > y0+dy*t+losClearance {
			return t, true
		}
	}
	return 0, false
}
