package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// HeightSampler returns the surface height under a world point: the live
// chunk heightmap when loaded (includes procedural cuts and player edits),
// procgen GroundHeight otherwise.
type HeightSampler struct {
	index ecs.Resource[TerrainChunkIndex]
	hm    *ecs.Map[components.Heightmap]
}

func NewHeightSampler(w *ecs.World) *HeightSampler {
	return &HeightSampler{
		index: ecs.NewResource[TerrainChunkIndex](w),
		hm:    ecs.NewMap[components.Heightmap](w),
	}
}

func (s *HeightSampler) Sample(wx, wz float32) float32 {
	gx := floorI32(wx)
	gz := floorI32(wz)
	idx := s.index.Get()
	if idx == nil {
		return GroundHeight(wx, wz)
	}
	cc := components.ChunkCoord{X: gx >> navGridShift, Z: gz >> navGridShift}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return GroundHeight(wx, wz)
	}
	hm := s.hm.Get(ent)
	if hm == nil {
		return GroundHeight(wx, wz)
	}
	li := int(gx & navGridMask)
	lj := int(gz & navGridMask)
	res := components.ChunkResolution
	h00 := hm.Heights[lj*res+li]
	h10 := hm.Heights[lj*res+li+1]
	h01 := hm.Heights[(lj+1)*res+li]
	h11 := hm.Heights[(lj+1)*res+li+1]
	fx := wx - float32(gx)
	fz := wz - float32(gz)
	return h00*(1-fx)*(1-fz) + h10*fx*(1-fz) + h01*(1-fx)*fz + h11*fx*fz
}

func floorI32(v float32) int32 {
	i := int32(v)
	if float32(i) > v {
		i--
	}
	return i
}
