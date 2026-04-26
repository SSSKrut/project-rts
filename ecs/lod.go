package ecs

import "time"

// LODLevel defines update frequency buckets.
type LODLevel int

const (
	LODActive LODLevel = iota
	LODRelevant
	LODDormant
)

// LODDisabled disables updates for a specific LOD bucket.
const LODDisabled time.Duration = -1

// LOD marks an entity's current level of detail.
type LOD struct {
	Level LODLevel
}

// LODPolicy defines how often a system runs per LOD bucket.
type LODPolicy struct {
	ActiveEvery   time.Duration
	RelevantEvery time.Duration
	DormantEvery  time.Duration
}

// DefaultLODPolicy provides a conservative default schedule.
func DefaultLODPolicy() LODPolicy {
	return LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  time.Second,
	}
}

// Interval returns the update interval for a given LOD level.
func (p LODPolicy) Interval(level LODLevel) time.Duration {
	switch level {
	case LODActive:
		return p.ActiveEvery
	case LODRelevant:
		return p.RelevantEvery
	case LODDormant:
		return p.DormantEvery
	default:
		return LODDisabled
	}
}

// LODLevelForEntity returns the LOD level for an entity.
func LODLevelForEntity(state *WorldState, id EntityID) LODLevel {
	lod, ok := Get[LOD](state, id)
	if !ok {
		return LODActive
	}
	return lod.Level
}

// QueryLOD returns entities matching component types and the given LOD level.
// This is the legacy API, prefer ForEachLOD variants for better performance.
func QueryLOD(state *WorldState, level LODLevel, types ...ComponentType) []EntityID {
	lodCT := TypeOf[LOD]()
	allTypes := append([]ComponentType{lodCT}, types...)

	var ids []EntityID
	for _, arch := range state.graph.ArchetypesWith(allTypes...) {
		lodIdx, ok := arch.typeIndex[lodCT]
		if !ok {
			continue
		}

		for row := 0; row < arch.Len(); row++ {
			lodComp := arch.columns[lodIdx][row]
			if lod, ok := lodComp.(LOD); ok && lod.Level == level {
				ids = append(ids, arch.EntityAt(row))
			}
		}
	}
	return ids
}

var lodLevels = []LODLevel{LODActive, LODRelevant, LODDormant}
