package systems

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Streaming radii in chunks (Р4). Active = full-detail mesh, Relevant =
// decimated mesh. Chebyshev distance (max of |dx|, |dz|) so the loaded region
// is a square ring rather than a circle — matches a grid streaming pattern
// and keeps neighbour lookup trivially correct.
const (
	terrainActiveRadius   = 2
	terrainRelevantRadius = 4
	terrainHysteresis     = 1
)

// TerrainChunkIndex is a singleton resource: ChunkCoord -> entity. Used for
// O(1) "is this chunk already loaded?" queries during streaming and for
// collecting the eviction set without a full scan. Held by reference (added
// via ecs.AddResource) and mutated in place — no per-tick allocation.
type TerrainChunkIndex struct {
	Loaded map[components.ChunkCoord]ecs.Entity
}

// NewTerrainChunkIndex builds an empty index. Call this once at app init
// before passing the address to ecs.AddResource.
func NewTerrainChunkIndex() TerrainChunkIndex {
	return TerrainChunkIndex{
		Loaded: make(map[components.ChunkCoord]ecs.Entity),
	}
}

// TerrainStreamingSystem owns the lifecycle of terrain chunk entities: it
// creates them when the anchor moves into range, evicts them (releasing GPU
// resources) when the anchor moves away, and toggles the LOD marker as the
// anchor crosses tier boundaries. It does NOT generate heights or build
// meshes — it only sets HeightmapDirty / MeshDirty for downstream systems
// (Р8). Runs at 4 Hz (every 250 ms) — the anchor can't outrun a chunk in
// less than that even at high speed.
type TerrainStreamingSystem struct {
	anchorFilter   *ecs.Filter2[components.LODAnchor, components.WorldPos]
	chunkFilter    *ecs.Filter2[components.ChunkCoord, components.TerrainChunk]
	indexRes       ecs.Resource[TerrainChunkIndex]
	posMap         *ecs.Map[components.WorldPos]
	chunkCoordMap  *ecs.Map[components.ChunkCoord]
	chunkMarkerMap *ecs.Map[components.TerrainChunk]
	heightDirtyMap *ecs.Map[components.HeightmapDirty]
	meshDirtyMap   *ecs.Map[components.MeshDirty]
	chunkMeshMap   *ecs.Map[components.ChunkMesh]
	lodActiveMap   *ecs.Map[components.LODActive]
	lodRelevantMap *ecs.Map[components.LODRelevant]
}

func (sys *TerrainStreamingSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	sys.chunkFilter = ecs.NewFilter2[components.ChunkCoord, components.TerrainChunk](w)
	sys.indexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.chunkCoordMap = ecs.NewMap[components.ChunkCoord](w)
	sys.chunkMarkerMap = ecs.NewMap[components.TerrainChunk](w)
	sys.heightDirtyMap = ecs.NewMap[components.HeightmapDirty](w)
	sys.meshDirtyMap = ecs.NewMap[components.MeshDirty](w)
	sys.chunkMeshMap = ecs.NewMap[components.ChunkMesh](w)
	sys.lodActiveMap = ecs.NewMap[components.LODActive](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
}

func (TerrainStreamingSystem) Name() string { return "terrain_streaming" }

func (TerrainStreamingSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// chebyshev returns max(|dx|, |dz|) — the square-ring "distance" we use for
// chunk LOD tiering.
func chebyshev(a, b components.ChunkCoord) int32 {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dz := a.Z - b.Z
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// tierForDistance maps a Chebyshev chunk distance to the desired LOD tier,
// applying inward/outward hysteresis bands so a chunk doesn't oscillate when
// the anchor sits right on a boundary. We need the current tier to know
// which band to test against.
func tierForDistance(dist int32, current core.LODTier) core.LODTier {
	activeIn := int32(terrainActiveRadius - terrainHysteresis)
	activeOut := int32(terrainActiveRadius + terrainHysteresis)
	relevantIn := int32(terrainRelevantRadius - terrainHysteresis)
	relevantOut := int32(terrainRelevantRadius + terrainHysteresis)

	switch current {
	case core.LODTierActive:
		if dist <= activeOut {
			return core.LODTierActive
		}
		if dist <= relevantOut {
			return core.LODTierRelevant
		}
		return core.LODTierDormant
	case core.LODTierRelevant:
		if dist <= activeIn {
			return core.LODTierActive
		}
		if dist <= relevantOut {
			return core.LODTierRelevant
		}
		return core.LODTierDormant
	default: // Dormant or unknown
		if dist <= activeIn {
			return core.LODTierActive
		}
		if dist <= relevantIn {
			return core.LODTierRelevant
		}
		return core.LODTierDormant
	}
}

// initialTier returns the tier a brand-new chunk should be born into,
// purely from distance. No hysteresis to consult yet.
func initialTier(dist int32) core.LODTier {
	if dist <= int32(terrainActiveRadius) {
		return core.LODTierActive
	}
	if dist <= int32(terrainRelevantRadius) {
		return core.LODTierRelevant
	}
	return core.LODTierDormant
}

func (sys TerrainStreamingSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	// 1. Find the anchor chunk.
	var anchorChunk components.ChunkCoord
	found := false
	q := sys.anchorFilter.Query()
	for q.Next() {
		_, pos := q.Get()
		anchorChunk = pos.Chunk
		found = true
		q.Close()
		break
	}
	if !found {
		return
	}

	idx := sys.indexRes.Get()
	if idx == nil {
		return
	}

	// 2. Sweep existing chunk entities, classify each into:
	//    - keep (still in range; possibly tier-change)
	//    - evict (out of range, will be destroyed)
	// We can't mutate archetypes mid-query, so collect the work first.
	type tierChange struct {
		id      ecs.Entity
		oldTier core.LODTier
		newTier core.LODTier
	}
	var changes []tierChange
	type evictRec struct {
		id ecs.Entity
		cc components.ChunkCoord
	}
	var evictions []evictRec

	q2 := sys.chunkFilter.Query()
	for q2.Next() {
		cc, _ := q2.Get()
		id := q2.Entity()
		dist := chebyshev(*cc, anchorChunk)

		// Determine current LOD tier from marker components. A chunk that's
		// briefly between markers (shouldn't happen in practice) is treated
		// as Dormant to force re-evaluation.
		var current core.LODTier = core.LODTierDormant
		if sys.lodActiveMap.Has(id) {
			current = core.LODTierActive
		} else if sys.lodRelevantMap.Has(id) {
			current = core.LODTierRelevant
		}

		if dist > int32(terrainRelevantRadius+terrainHysteresis) {
			evictions = append(evictions, evictRec{id, *cc})
			continue
		}
		next := tierForDistance(dist, current)
		if next == core.LODTierDormant {
			// No Dormant tier for terrain — out-of-band counts as eviction.
			evictions = append(evictions, evictRec{id, *cc})
			continue
		}
		if next != current {
			changes = append(changes, tierChange{id, current, next})
		}
	}

	// 3. Compute the set of (cc) that *should* be loaded; create any missing.
	type spawnRec struct {
		cc   components.ChunkCoord
		tier core.LODTier
	}
	var spawns []spawnRec
	r := int32(terrainRelevantRadius)
	for dz := -r; dz <= r; dz++ {
		for dx := -r; dx <= r; dx++ {
			cc := components.ChunkCoord{X: anchorChunk.X + dx, Z: anchorChunk.Z + dz}
			if _, ok := idx.Loaded[cc]; ok {
				continue
			}
			dist := chebyshev(cc, anchorChunk)
			tier := initialTier(dist)
			if tier == core.LODTierDormant {
				continue
			}
			spawns = append(spawns, spawnRec{cc, tier})
		}
	}

	// 4. Apply evictions. CRITICAL: unload GPU mesh BEFORE removing the
	// entity, otherwise the VAO/VBO is leaked. We use rl.UnloadMesh (not
	// UnloadModel) so the Go-allocated mesh pointers don't get C.free'd —
	// see the comment on components.ChunkMesh.
	for _, ev := range evictions {
		if mesh := sys.chunkMeshMap.Get(ev.id); mesh != nil && mesh.Uploaded {
			rl.UnloadMesh(&mesh.Mesh)
		}
		delete(idx.Loaded, ev.cc)
		ctx.World.RemoveEntity(ev.id)
	}

	// 5. Apply tier changes. A tier transition needs a remesh (the new tier
	// uses a different vertex resolution) but the heightmap is still valid,
	// so we only set MeshDirty.
	for _, ch := range changes {
		switch ch.oldTier {
		case core.LODTierActive:
			sys.lodActiveMap.Remove(ch.id)
		case core.LODTierRelevant:
			sys.lodRelevantMap.Remove(ch.id)
		}
		switch ch.newTier {
		case core.LODTierActive:
			sys.lodActiveMap.Add(ch.id, &components.LODActive{})
		case core.LODTierRelevant:
			sys.lodRelevantMap.Add(ch.id, &components.LODRelevant{})
		}
		if !sys.meshDirtyMap.Has(ch.id) {
			sys.meshDirtyMap.Add(ch.id, &components.MeshDirty{})
		}
	}

	// 6. Spawn new chunk entities. WorldPos is set so chunk-(0,0,0) is the
	// chunk's origin corner — render code adds renderPos = pos.ToRenderSpace
	// and the mesh's local vertices already span [0, ChunkSize].
	for _, sp := range spawns {
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: sp.cc})
		ccCopy := sp.cc
		sys.chunkCoordMap.Add(e, &ccCopy)
		sys.chunkMarkerMap.Add(e, &components.TerrainChunk{})
		sys.heightDirtyMap.Add(e, &components.HeightmapDirty{})
		sys.meshDirtyMap.Add(e, &components.MeshDirty{})
		switch sp.tier {
		case core.LODTierActive:
			sys.lodActiveMap.Add(e, &components.LODActive{})
		case core.LODTierRelevant:
			sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		}
		idx.Loaded[sp.cc] = e
	}
}
