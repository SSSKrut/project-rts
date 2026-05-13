package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MapMarkerCacheSystem refreshes the per-squad marker positions consumed by
// the 2D map renderer (Phase 11.5 P7). Run every 250 ms — much cheaper than
// calling SquadCenter on every map-render frame for every squad (which would
// scale poorly past ~100 squads). The cache also serves as the smoothing
// anchor: the renderer lerps inter-frame from the previous on-screen position
// toward the cache's current value.
type MapMarkerCacheSystem struct {
	squadFilter *ecs.Filter2[components.Squad, components.CommandRoster]
	posMap      *ecs.Map[components.WorldPos]
	cacheRes    ecs.Resource[components.MapMarkerCache]
}

func (sys *MapMarkerCacheSystem) InitUI(w *ecs.World) {
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.cacheRes = ecs.NewResource[components.MapMarkerCache](w)
}

func (MapMarkerCacheSystem) Name() string { return "map_marker_cache" }

func (MapMarkerCacheSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MapMarkerCacheSystem) Update(ctx core.UpdateContext) {
	cache := sys.cacheRes.Get()
	if cache == nil {
		return
	}
	// Mark current entries as stale; survivors are refreshed below, the rest
	// are dropped at the end of the pass. Cheaper than building a new map
	// each tick — at 100 squads the survivor set is the same size 99% of the
	// time.
	seen := make(map[ecs.Entity]struct{}, len(cache.Position))
	q := sys.squadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		squad := q.Entity()
		if roster.Count == 0 {
			continue
		}
		center, ok := SquadCenter(ctx.World, roster, sys.posMap)
		if !ok {
			continue
		}
		cache.Position[squad] = center
		seen[squad] = struct{}{}
	}
	// Drop entries for squads that no longer exist / have an empty roster.
	for k := range cache.Position {
		if _, ok := seen[k]; !ok {
			delete(cache.Position, k)
		}
	}
}
