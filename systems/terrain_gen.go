package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// TerrainGenSystem fills the Heightmap of every chunk marked HeightmapDirty
// by sampling GroundHeight at each vertex's world (X, Z). It does not touch
// LOD markers or the GPU — that's downstream (Р8).
//
// Runs every tick (ActiveEvery: 0): we want freshly-spawned chunks to have
// heights ready for the mesh system on the same frame. With ChunkResolution
// = 65 that's 4225 noise evals per chunk; 25 chunks at world startup is
// ~100k evals — acceptable for a one-time spike on world load (MVP).
type TerrainGenSystem struct {
	dirtyFilter    *ecs.Filter2[components.ChunkCoord, components.HeightmapDirty]
	heightmapMap   *ecs.Map[components.Heightmap]
	heightDirtyMap *ecs.Map[components.HeightmapDirty]
}

func (sys *TerrainGenSystem) InitUI(w *ecs.World) {
	sys.dirtyFilter = ecs.NewFilter2[components.ChunkCoord, components.HeightmapDirty](w)
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.heightDirtyMap = ecs.NewMap[components.HeightmapDirty](w)
}

func (TerrainGenSystem) Name() string { return "terrain_gen" }

func (TerrainGenSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys TerrainGenSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	// Step is 1 m at the current settings (64 m / 64 quads). Compute it from
	// the constants so the math stays correct if ChunkResolution changes.
	step := components.ChunkSize / float32(components.ChunkResolution-1)

	// Generated heights buffered until after the query closes — we add the
	// Heightmap component (archetype mutation) once the iterator is done.
	type genResult struct {
		id      ecs.Entity
		heights [components.ChunkResolution * components.ChunkResolution]float32
	}
	var results []genResult

	q := sys.dirtyFilter.Query()
	for q.Next() {
		cc, _ := q.Get()
		id := q.Entity()
		var hm [components.ChunkResolution * components.ChunkResolution]float32
		baseX := float32(cc.X) * components.ChunkSize
		baseZ := float32(cc.Z) * components.ChunkSize
		for j := 0; j < components.ChunkResolution; j++ {
			wz := baseZ + float32(j)*step
			row := j * components.ChunkResolution
			for i := 0; i < components.ChunkResolution; i++ {
				wx := baseX + float32(i)*step
				hm[row+i] = GroundHeight(wx, wz)
			}
		}
		results = append(results, genResult{id, hm})
	}

	// Apply results: write heights, clear HeightmapDirty.
	for _, r := range results {
		if existing := sys.heightmapMap.Get(r.id); existing != nil {
			existing.Heights = r.heights
		} else {
			h := components.Heightmap{Heights: r.heights}
			sys.heightmapMap.Add(r.id, &h)
		}
		sys.heightDirtyMap.Remove(r.id)
	}
}
