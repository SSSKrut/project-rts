package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// TrenchSystem applies cosine-cut earthworks along every Trench polyline
// intersecting a pristine chunk. No water-prop spawn — earthworks are bare
// cuts. Filter excludes Modified to preserve player edits.
type TrenchSystem struct {
	chunkFilter        *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	trenchRes          ecs.Resource[components.TrenchNetwork]
	trenchProcessedMap *ecs.Map[components.TrenchProcessed]
	stamper            *Stamper
}

func (sys *TrenchSystem) InitUI(w *ecs.World) {
	sys.chunkFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.TrenchProcessed](),
			ecs.C[components.Modified](),
		)
	sys.trenchRes = ecs.NewResource[components.TrenchNetwork](w)
	sys.trenchProcessedMap = ecs.NewMap[components.TrenchProcessed](w)
	sys.stamper = NewStamper(w)
}

func (TrenchSystem) Name() string { return "trench" }

func (TrenchSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys TrenchSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	tn := sys.trenchRes.Get()
	if tn == nil || len(tn.Lines) == 0 {
		return
	}

	type plBBox struct {
		minX, minZ, maxX, maxZ float32
		idx                    int
	}
	bboxes := make([]plBBox, 0, len(tn.Lines))
	for i := range tn.Lines {
		minX, minZ, maxX, maxZ := trenchWorldBBox(tn.Lines[i])
		w := tn.Lines[i].Width
		bboxes = append(bboxes, plBBox{minX - w, minZ - w, maxX + w, maxZ + w, i})
	}

	type processedEnt struct{ id ecs.Entity }
	var processed []processedEnt
	q := sys.chunkFilter.Query()
	for q.Next() {
		cc, _, _ := q.Get()
		ccVal := *cc
		chunkMinX := float32(ccVal.X) * components.ChunkSize
		chunkMinZ := float32(ccVal.Z) * components.ChunkSize
		chunkMaxX := chunkMinX + components.ChunkSize
		chunkMaxZ := chunkMinZ + components.ChunkSize

		for _, b := range bboxes {
			if b.maxX < chunkMinX || b.minX > chunkMaxX ||
				b.maxZ < chunkMinZ || b.minZ > chunkMaxZ {
				continue
			}
			t := &tn.Lines[b.idx]
			sys.stamper.Trench(ccVal, t.Points, t.Width, t.Depth)
		}
		processed = append(processed, processedEnt{q.Entity()})
	}

	for _, p := range processed {
		if !sys.trenchProcessedMap.Has(p.id) {
			sys.trenchProcessedMap.Add(p.id, &components.TrenchProcessed{})
		}
	}
}

func trenchWorldBBox(t components.Trench) (minX, minZ, maxX, maxZ float32) {
	minX, minZ = float32(1e30), float32(1e30)
	maxX, maxZ = float32(-1e30), float32(-1e30)
	for i := range t.Points {
		wx := float32(t.Points[i].Chunk.X)*components.ChunkSize + t.Points[i].Local.X
		wz := float32(t.Points[i].Chunk.Z)*components.ChunkSize + t.Points[i].Local.Z
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
