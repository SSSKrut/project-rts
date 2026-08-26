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
	commsMap    *ecs.Map[components.CommsState]
	cacheRes    ecs.Resource[components.MapMarkerCache]
}

func (sys *MapMarkerCacheSystem) InitUI(w *ecs.World) {
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.commsMap = ecs.NewMap[components.CommsState](w)
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
		seen[squad] = struct{}{}
		// A squad off the net stops reporting. The marker holds its last known
		// place and starts ageing — the same treatment an enemy contact gets,
		// because it is the same kind of fact. A squad with no cached marker at
		// all is seeded regardless: after a load the cache is empty, and an
		// invisible friendly reads as a bug rather than as radio silence.
		_, cached := cache.Position[squad]
		cs := sys.commsMap.Get(squad)
		if cached && cs != nil && cs.Band.LeashM() > 0 {
			if _, already := cache.StaleSince[squad]; !already {
				cache.StaleSince[squad] = float32(ctx.SimNow)
			}
			continue
		}
		delete(cache.StaleSince, squad)
		cache.Position[squad] = center
	}
	for k := range cache.Position {
		if _, ok := seen[k]; !ok {
			delete(cache.Position, k)
			delete(cache.StaleSince, k)
		}
	}
}
