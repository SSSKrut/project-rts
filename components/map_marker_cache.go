package components

import "github.com/mlange-42/ark/ecs"

// MapMarkerCache holds pre-computed squad-marker positions for the 2D map
// panel. Refreshed by MapMarkerCacheSystem at a coarse cadence (250 ms) so the
// map renderer doesn't pay SquadCenter every frame for every squad — at 100
// squads that's 6000 calls/sec, here it's 400.
//
// The map renderer reads `Position` and interpolates inter-frame toward the
// latest cached value (see ui/map_render.go drawSquadMarkers). Stale entries
// are pruned by MapMarkerCacheSystem when the squad no longer matches its
// filter.
type MapMarkerCache struct {
	Position map[ecs.Entity]WorldPos
}

// NewMapMarkerCache returns a ready-to-register resource value.
func NewMapMarkerCache() MapMarkerCache {
	return MapMarkerCache{Position: make(map[ecs.Entity]WorldPos, 16)}
}
