package components

import "github.com/mlange-42/ark/ecs"

// MapMarkerCache holds pre-computed squad-marker positions for the 2D map
// panel. Refreshed at coarse cadence (250 ms) so the map renderer doesn't pay
// SquadCenter every frame for every squad - at 100 squads that's 6000
// calls/sec, here it's 400.
//
// The map renderer reads `Position` and interpolates inter-frame toward the
// latest cached value.
type MapMarkerCache struct {
	Position map[ecs.Entity]WorldPos
	// StaleSince is the sim time a marker stopped being refreshed because the
	// net no longer reaches its squad; 0 = live. The map draws its own units
	// exactly as honestly as it draws the enemy's — what you see is the last
	// report, and it ages.
	StaleSince map[ecs.Entity]float32
}

func NewMapMarkerCache() MapMarkerCache {
	return MapMarkerCache{
		Position:   make(map[ecs.Entity]WorldPos, 16),
		StaleSince: make(map[ecs.Entity]float32, 16),
	}
}
