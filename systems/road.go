package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// The carriageway is drawn as a ribbon mesh that carries its own embankment,
// so terrain is only ever cut — never raised — and only where it would poke
// through. roadSubgrade is how far below the deck the cut floor lands;
// roadCarveFalloff softens the cut walls outside the strip.
const (
	roadSubgrade     float32 = 0.30
	roadCarveFalloff float32 = 1.5
)

// RoadSystem cuts the heightmap under the road network for every pristine
// chunk. Gated by Without[Modified] so player edits aren't re-cut, and by
// Without[RoadProcessed] so it runs once per chunk life.
type RoadSystem struct {
	chunkFilter      *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	surfaceRes       ecs.Resource[components.RoadSurface]
	roadProcessedMap *ecs.Map[components.RoadProcessed]
	stamper          *Stamper
}

func (sys *RoadSystem) InitUI(w *ecs.World) {
	sys.chunkFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.Modified](),
			ecs.C[components.RoadProcessed](),
		)
	sys.surfaceRes = ecs.NewResource[components.RoadSurface](w)
	sys.roadProcessedMap = ecs.NewMap[components.RoadProcessed](w)
	sys.stamper = NewStamper(w)
}

func (RoadSystem) Name() string { return "road" }

func (RoadSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys RoadSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	surface := sys.surfaceRes.Get()
	if surface == nil || len(surface.Samples) == 0 {
		return
	}

	var processed []ecs.Entity
	q := sys.chunkFilter.Query()
	for q.Next() {
		cc, _, _ := q.Get()
		sys.stamper.RoadCarve(*cc, surface.Samples, roadSubgrade, roadCarveFalloff)
		processed = append(processed, q.Entity())
	}
	for _, e := range processed {
		if !sys.roadProcessedMap.Has(e) {
			sys.roadProcessedMap.Add(e, &components.RoadProcessed{})
		}
	}
}
