package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// HeightKernel returns the per-vertex height delta at offset (dx, dz) from
// the stamp centre. Implementations return 0 outside their effective radius.
type HeightKernel func(dx, dz float32) float32

// Crater returns a half-cosine bowl kernel:
//
//	delta(d) = -depth * 0.5 * (1 + cos(π * d/radius))  for d ≤ radius
//	delta(d) = 0                                       for d > radius
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

// Stamper applies HeightKernels to live terrain chunks. Service object,
// not a System; pre-built handles via NewStamper.
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
// of center. Touched chunks get MeshDirty + Modified (survive eviction).
// Unloaded chunks are silently skipped.
//
// At a chunk seam, both adjacent chunks own a copy of the shared edge vertex
// and both get the same delta from kernel(dx, dz), keeping the seam continuous.
func (s *Stamper) StampHeightmap(center components.WorldPos, kernel HeightKernel, radius float32) {
	idx := s.indexRes.Get()
	if idx == nil {
		return
	}

	cx := float32(center.Chunk.X)*components.ChunkSize + center.Local.X
	cz := float32(center.Chunk.Z)*components.ChunkSize + center.Local.Z

	// Floor-divide so negatives land in the right chunk (x = -0.5 → chunk -1).
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

// RiverCut applies a cosine half-falloff cut along a polyline. Does NOT set
// Modified — derivable on respawn.
func (s *Stamper) RiverCut(cc components.ChunkCoord, polyline []components.WorldPos, width, depth float32) {
	s.cutAlongPolyline(cc, polyline, width, depth)
}

// Trench cuts a defensive earthwork along a polyline. Same kernel as
// RiverCut, distinct API surface for distinct intent.
func (s *Stamper) Trench(cc components.ChunkCoord, polyline []components.WorldPos, width, depth float32) {
	s.cutAlongPolyline(cc, polyline, width, depth)
}

// cutAlongPolyline: cosine-falloff polyline cut. For each vertex, distance
// d to the nearest segment drives a ramp from -depth at line centre to 0
// at width.
func (s *Stamper) cutAlongPolyline(cc components.ChunkCoord, polyline []components.WorldPos, width, depth float32) {
	if width <= 0 || len(polyline) < 2 {
		return
	}
	idx := s.indexRes.Get()
	if idx == nil {
		return
	}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return
	}
	hm := s.heightmapMap.Get(ent)
	if hm == nil {
		return
	}

	step := components.ChunkSize / float32(components.ChunkResolution-1)
	baseX := float32(cc.X) * components.ChunkSize
	baseZ := float32(cc.Z) * components.ChunkSize
	invW := 1.0 / width

	type seg struct{ ax, az, bx, bz float32 }
	segs := make([]seg, 0, len(polyline)-1)
	for i := 0; i+1 < len(polyline); i++ {
		ax := float32(polyline[i].Chunk.X)*components.ChunkSize + polyline[i].Local.X
		az := float32(polyline[i].Chunk.Z)*components.ChunkSize + polyline[i].Local.Z
		bx := float32(polyline[i+1].Chunk.X)*components.ChunkSize + polyline[i+1].Local.X
		bz := float32(polyline[i+1].Chunk.Z)*components.ChunkSize + polyline[i+1].Local.Z
		segs = append(segs, seg{ax, az, bx, bz})
	}

	touched := false
	for j := 0; j < components.ChunkResolution; j++ {
		wz := baseZ + float32(j)*step
		row := j * components.ChunkResolution
		for i := 0; i < components.ChunkResolution; i++ {
			wx := baseX + float32(i)*step

			best := float32(math.Inf(1))
			for k := range segs {
				d := pointToSegment2D(wx, wz, segs[k].ax, segs[k].az, segs[k].bx, segs[k].bz)
				if d < best {
					best = d
					if best == 0 {
						break
					}
				}
			}
			if best >= width {
				continue
			}
			factor := 0.5 * (1 + float32(math.Cos(math.Pi*float64(best*invW))))
			hm.Heights[row+i] += -depth * factor
			touched = true
		}
	}

	if touched && !s.meshDirtyMap.Has(ent) {
		s.meshDirtyMap.Add(ent, &components.MeshDirty{})
	}
}

// LevelTo flattens the heightmap to `targetY` inside the footprint, cosine-
// blends back outside over `falloffWidth`. Does NOT set Modified.
func (s *Stamper) LevelTo(cc components.ChunkCoord, footprint components.AABB2D, targetY, falloffWidth float32) {
	if falloffWidth < 0 {
		falloffWidth = 0
	}
	idx := s.indexRes.Get()
	if idx == nil {
		return
	}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return
	}
	hm := s.heightmapMap.Get(ent)
	if hm == nil {
		return
	}

	step := components.ChunkSize / float32(components.ChunkResolution-1)
	baseX := float32(cc.X) * components.ChunkSize
	baseZ := float32(cc.Z) * components.ChunkSize

	touched := false
	for j := 0; j < components.ChunkResolution; j++ {
		wz := baseZ + float32(j)*step
		row := j * components.ChunkResolution
		for i := 0; i < components.ChunkResolution; i++ {
			wx := baseX + float32(i)*step
			d := footprint.DistanceXZ(wx, wz)
			if d == 0 {
				hm.Heights[row+i] = targetY
				touched = true
				continue
			}
			if falloffWidth <= 0 || d >= falloffWidth {
				continue
			}
			w := 0.5 * (1 + float32(math.Cos(math.Pi*float64(d/falloffWidth))))
			cur := hm.Heights[row+i]
			hm.Heights[row+i] = cur*(1-w) + targetY*w
			touched = true
		}
	}

	if touched && !s.meshDirtyMap.Has(ent) {
		s.meshDirtyMap.Add(ent, &components.MeshDirty{})
	}
}

// RectCut sinks a bunker pad — flat plate at (surface_at_centre − depth)
// inside the footprint, cosine skirt outside.
func (s *Stamper) RectCut(cc components.ChunkCoord, footprint components.AABB2D, depth, falloffWidth float32) {
	referenceY := GroundHeight(footprint.CenterX(), footprint.CenterZ())
	s.LevelTo(cc, footprint, referenceY-depth, falloffWidth)
}

// RoadFlatten blends the heightmap toward a linear road profile between two
// endpoints over a strip of given width:
//
//	w(d) = 0.5 * (1 + cos(π * d / (width/2)))
//	h    = lerp(h, target, w)
//
// where target is interpolated linearly along the road. Unlike RiverCut
// (additive), this is a blend toward target — flattens a strip and fades
// back at the edges. Does NOT set Modified.
func (s *Stamper) RoadFlatten(cc components.ChunkCoord,
	fromWX, fromWZ, fromY, toWX, toWZ, toY, width float32) {
	if width <= 0 {
		return
	}
	idx := s.indexRes.Get()
	if idx == nil {
		return
	}
	ent, ok := idx.Loaded[cc]
	if !ok {
		return
	}
	hm := s.heightmapMap.Get(ent)
	if hm == nil {
		return
	}

	dx := toWX - fromWX
	dz := toWZ - fromWZ
	lenSq := dx*dx + dz*dz
	if lenSq <= 0 {
		return
	}

	step := components.ChunkSize / float32(components.ChunkResolution-1)
	baseX := float32(cc.X) * components.ChunkSize
	baseZ := float32(cc.Z) * components.ChunkSize
	halfW := width * 0.5
	invHalfW := 1.0 / halfW

	touched := false
	for j := 0; j < components.ChunkResolution; j++ {
		wz := baseZ + float32(j)*step
		row := j * components.ChunkResolution
		for i := 0; i < components.ChunkResolution; i++ {
			wx := baseX + float32(i)*step
			t := ((wx-fromWX)*dx + (wz-fromWZ)*dz) / lenSq
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
			cx := fromWX + t*dx
			cz := fromWZ + t*dz
			ddx := wx - cx
			ddz := wz - cz
			d := float32(math.Sqrt(float64(ddx*ddx + ddz*ddz)))
			if d >= halfW {
				continue
			}
			targetY := fromY + t*(toY-fromY)
			w := 0.5 * (1 + float32(math.Cos(math.Pi*float64(d*invHalfW))))
			hm.Heights[row+i] = hm.Heights[row+i]*(1-w) + targetY*w
			touched = true
		}
	}

	if touched && !s.meshDirtyMap.Has(ent) {
		s.meshDirtyMap.Add(ent, &components.MeshDirty{})
	}
}
