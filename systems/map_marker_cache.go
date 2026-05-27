package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MapMarkerCacheSystem refreshes the per-squad marker positions consumed by
// the 2D map renderer. Run every 250 ms (vs. per-frame SquadCenter); the
// cache also serves as the renderer's smoothing anchor.
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
	// Survivors are refreshed below; the rest are dropped at the end.
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
	for k := range cache.Position {
		if _, ok := seen[k]; !ok {
			delete(cache.Position, k)
		}
	}
}
