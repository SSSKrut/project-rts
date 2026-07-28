package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// RiverSystem cuts the river bed into every pristine chunk a polyline
// crosses. Runs after terrain_gen and before prop_spawn (trees see the river
// exclusion) and terrain_mesh (the cut is in the mesh on first build). The
// water surface itself is a ribbon mesh built once at boot, not per-chunk
// props. Filter excludes Modified so player edits aren't re-stamped.
type RiverSystem struct {
	chunkFilter       *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	riversRes         ecs.Resource[components.Rivers]
	riverProcessedMap *ecs.Map[components.RiverProcessed]
	stamper           *Stamper
}

func (sys *RiverSystem) InitUI(w *ecs.World) {
	sys.chunkFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.RiverProcessed](),
			ecs.C[components.Modified](),
		)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
	sys.riverProcessedMap = ecs.NewMap[components.RiverProcessed](w)
	sys.stamper = NewStamper(w)
}

func (RiverSystem) Name() string { return "river" }

func (RiverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys RiverSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	rivers := sys.riversRes.Get()
	if rivers == nil || len(rivers.Polylines) == 0 {
		return
	}

	type plBBox struct {
		minX, minZ, maxX, maxZ float32
		idx                    int
	}
	bboxes := make([]plBBox, 0, len(rivers.Polylines))
	for i := range rivers.Polylines {
		minX, minZ, maxX, maxZ := polylineWorldBBox(rivers.Polylines[i])
		// Inflate by Width so segments whose strip clips into the chunk while
		// their centre line is outside still match.
		w := rivers.Polylines[i].Width
		bboxes = append(bboxes, plBBox{
			minX: minX - w, minZ: minZ - w,
			maxX: maxX + w, maxZ: maxZ + w,
			idx: i,
		})
	}

	var processed []ecs.Entity
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
			pl := &rivers.Polylines[b.idx]
			sys.stamper.RiverCut(ccVal, pl.Points, pl.Width, pl.Depth)
		}

		// Mark even non-intersecting chunks — keeps the filter cheap. Without
		// this we'd re-test every pristine chunk vs every river each tick.
		processed = append(processed, q.Entity())
	}

	for _, e := range processed {
		if !sys.riverProcessedMap.Has(e) {
			sys.riverProcessedMap.Add(e, &components.RiverProcessed{})
		}
	}
}
