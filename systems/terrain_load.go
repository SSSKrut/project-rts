package systems

import (
	"fmt"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// TerrainLoadSystem fills the Heightmap of any chunk marked HeightmapDirty
// from disk if a saved blob exists. Runs BEFORE TerrainGenSystem in the
// pipeline so loaded chunks override procgen — the load system clears
// HeightmapDirty on hits, leaving only pristine chunks for the gen pass to
// pick up (P7).
//
// Misses (no file) are left alone: HeightmapDirty stays set, gen handles
// them next. Read errors (corrupt file, version mismatch) are logged and
// treated as misses, again falling back to procgen — never panic.
type TerrainLoadSystem struct {
	dirtyFilter    *ecs.Filter2[components.ChunkCoord, components.HeightmapDirty]
	heightmapMap   *ecs.Map[components.Heightmap]
	heightDirtyMap *ecs.Map[components.HeightmapDirty]
	modifiedMap    *ecs.Map[components.Modified]
}

func (sys *TerrainLoadSystem) InitUI(w *ecs.World) {
	sys.dirtyFilter = ecs.NewFilter2[components.ChunkCoord, components.HeightmapDirty](w)
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.heightDirtyMap = ecs.NewMap[components.HeightmapDirty](w)
	sys.modifiedMap = ecs.NewMap[components.Modified](w)
}

func (TerrainLoadSystem) Name() string { return "terrain_load" }

func (TerrainLoadSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys TerrainLoadSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	// Buffer reads until after the iterator closes — same deferred
	// archetype-mutation pattern used elsewhere in terrain code.
	type loaded struct {
		id      ecs.Entity
		heights [components.ChunkResolution * components.ChunkResolution]float32
	}
	var results []loaded

	q := sys.dirtyFilter.Query()
	for q.Next() {
		cc, _ := q.Get()
		id := q.Entity()
		var heights [components.ChunkResolution * components.ChunkResolution]float32
		found, err := ReadChunk(SaveDir, *cc, &heights)
		if err != nil {
			fmt.Printf("terrain_load: read %v: %v\n", *cc, err)
			continue
		}
		if !found {
			continue
		}
		results = append(results, loaded{id, heights})
	}

	for _, r := range results {
		if existing := sys.heightmapMap.Get(r.id); existing != nil {
			existing.Heights = r.heights
		} else {
			h := components.Heightmap{Heights: r.heights}
			sys.heightmapMap.Add(r.id, &h)
		}
		if !sys.modifiedMap.Has(r.id) {
			sys.modifiedMap.Add(r.id, &components.Modified{})
		}
		sys.heightDirtyMap.Remove(r.id)
	}
}
