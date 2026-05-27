package systems

import (
	"fmt"
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Streaming radii in chunks. Chebyshev distance gives a square ring (matches
// the grid streaming pattern and keeps neighbour lookup trivially correct).
// 7×7 active + 11×11 relevant; at 64 m chunks the far-LOD ring reaches 320 m.
const (
	terrainActiveRadius   = 3
	terrainRelevantRadius = 6
	terrainHysteresis     = 1
)

// TerrainChunkIndex is a singleton: ChunkCoord → entity. O(1) load-check and
// eviction set without a full scan.
type TerrainChunkIndex struct {
	Loaded map[components.ChunkCoord]ecs.Entity
}

func NewTerrainChunkIndex() TerrainChunkIndex {
	return TerrainChunkIndex{
		Loaded: make(map[components.ChunkCoord]ecs.Entity),
	}
}

// TerrainStreamingSystem owns the lifecycle of terrain chunk entities:
// spawns / evicts / toggles LOD markers. Does NOT generate heights or build
// meshes — sets HeightmapDirty / MeshDirty for downstream systems. Runs at
// 4 Hz; the anchor can't outrun a chunk in less.
type TerrainStreamingSystem struct {
	anchorFilter      *ecs.Filter2[components.LODAnchor, components.WorldPos]
	chunkFilter       *ecs.Filter2[components.ChunkCoord, components.TerrainChunk]
	buildingFilter    *ecs.Filter2[components.Building, components.WorldPos]
	indexRes          ecs.Resource[TerrainChunkIndex]
	propIndexRes      ecs.Resource[PropChunkIndex]
	buildingIndexRes  ecs.Resource[BuildingChildIndex]
	coverSlotIndexRes ecs.Resource[CoverSlotIndex]
	transitionRes     ecs.Resource[components.TransitionRegistry]
	posMap            *ecs.Map[components.WorldPos]
	chunkCoordMap     *ecs.Map[components.ChunkCoord]
	chunkMarkerMap    *ecs.Map[components.TerrainChunk]
	heightDirtyMap    *ecs.Map[components.HeightmapDirty]
	meshDirtyMap      *ecs.Map[components.MeshDirty]
	propsDirtyMap     *ecs.Map[components.PropsDirty]
	chunkMeshMap      *ecs.Map[components.ChunkMesh]
	lodActiveMap      *ecs.Map[components.LODActive]
	lodRelevantMap    *ecs.Map[components.LODRelevant]
	heightmapMap      *ecs.Map[components.Heightmap]
	modifiedMap       *ecs.Map[components.Modified]
}

func (sys *TerrainStreamingSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	sys.chunkFilter = ecs.NewFilter2[components.ChunkCoord, components.TerrainChunk](w)
	sys.buildingFilter = ecs.NewFilter2[components.Building, components.WorldPos](w)
	sys.indexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.buildingIndexRes = ecs.NewResource[BuildingChildIndex](w)
	sys.coverSlotIndexRes = ecs.NewResource[CoverSlotIndex](w)
	sys.transitionRes = ecs.NewResource[components.TransitionRegistry](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.chunkCoordMap = ecs.NewMap[components.ChunkCoord](w)
	sys.chunkMarkerMap = ecs.NewMap[components.TerrainChunk](w)
	sys.heightDirtyMap = ecs.NewMap[components.HeightmapDirty](w)
	sys.meshDirtyMap = ecs.NewMap[components.MeshDirty](w)
	sys.propsDirtyMap = ecs.NewMap[components.PropsDirty](w)
	sys.chunkMeshMap = ecs.NewMap[components.ChunkMesh](w)
	sys.lodActiveMap = ecs.NewMap[components.LODActive](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.modifiedMap = ecs.NewMap[components.Modified](w)
}

func (TerrainStreamingSystem) Name() string { return "terrain_streaming" }

func (TerrainStreamingSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

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

// tierForDistance applies inward/outward hysteresis bands so a chunk
// doesn't oscillate when the anchor sits on a boundary.
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
	default:
		if dist <= activeIn {
			return core.LODTierActive
		}
		if dist <= relevantIn {
			return core.LODTierRelevant
		}
		return core.LODTierDormant
	}
}

// initialTier returns the tier a brand-new chunk is born into.
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

	// Archetype mutations buffered for after the query.
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

		// A chunk briefly between markers is treated as Dormant.
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
			// No Dormant tier for terrain — out-of-band = eviction.
			evictions = append(evictions, evictRec{id, *cc})
			continue
		}
		if next != current {
			changes = append(changes, tierChange{id, current, next})
		}
	}

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

	// Evictions: unload GPU mesh BEFORE RemoveEntity (else VAO/VBO leak).
	// UnloadMesh, not UnloadModel — Go-allocated pointers must not C.free.
	// Modified chunks flush heights first; pristine chunks skip the write.
	propIdx := sys.propIndexRes.Get()
	bIdx := sys.buildingIndexRes.Get()
	coverIdx := sys.coverSlotIndexRes.Get()
	transitionReg := sys.transitionRes.Get()

	// Bucket each Building by every chunk its footprint overlaps so eviction
	// despawns only children whose Pos.Chunk matches the evicting chunk.
	var buildingsByChunk map[components.ChunkCoord][]ecs.Entity
	if bIdx != nil && len(evictions) > 0 {
		qb := sys.buildingFilter.Query()
		for qb.Next() {
			b, _ := qb.Get()
			root := qb.Entity()
			fp := b.Footprint
			minCX := int32(math.Floor(float64(fp.MinX) / float64(components.ChunkSize)))
			maxCX := int32(math.Floor(float64(fp.MaxX) / float64(components.ChunkSize)))
			minCZ := int32(math.Floor(float64(fp.MinZ) / float64(components.ChunkSize)))
			maxCZ := int32(math.Floor(float64(fp.MaxZ) / float64(components.ChunkSize)))
			if buildingsByChunk == nil {
				buildingsByChunk = make(map[components.ChunkCoord][]ecs.Entity)
			}
			for cx := minCX; cx <= maxCX; cx++ {
				for cz := minCZ; cz <= maxCZ; cz++ {
					cc := components.ChunkCoord{X: cx, Z: cz}
					buildingsByChunk[cc] = append(buildingsByChunk[cc], root)
				}
			}
		}
	}

	for _, ev := range evictions {
		if sys.modifiedMap.Has(ev.id) {
			if hm := sys.heightmapMap.Get(ev.id); hm != nil {
				if err := WriteChunk(SaveDir, ev.cc, &hm.Heights); err != nil {
					fmt.Printf("terrain_streaming: WriteChunk %v: %v\n", ev.cc, err)
				}
			}
		}
		// Chunk owns prop lifecycle; respawn deterministically on return.
		if propIdx != nil {
			if props, ok := propIdx.Loaded[ev.cc]; ok {
				for _, p := range props {
					if coverIdx != nil {
						delete(coverIdx.ByHost, p)
					}
					ctx.World.RemoveEntity(p)
				}
				delete(propIdx.Loaded, ev.cc)
			}
		}
		// Tear down children whose Pos.Chunk == ev.cc; other children of
		// the same building keep living. Root + Level are AlwaysActive.
		if bIdx != nil {
			for _, root := range buildingsByChunk[ev.cc] {
				children, ok := bIdx.Loaded[root]
				if !ok {
					continue
				}
				evictedChildren := make(map[ecs.Entity]bool)
				remaining := children[:0]
				for _, c := range children {
					cpos := sys.posMap.Get(c)
					if cpos == nil {
						continue
					}
					if cpos.Chunk == ev.cc {
						if coverIdx != nil {
							delete(coverIdx.ByHost, c)
						}
						evictedChildren[c] = true
						ctx.World.RemoveEntity(c)
					} else {
						remaining = append(remaining, c)
					}
				}
				// Drop every TransitionEdge whose owner just despawned —
				// otherwise A* would route through a stale doorway / stair.
				if transitionReg != nil && len(evictedChildren) > 0 {
					for k, edges := range transitionReg.Out {
						kept := edges[:0]
						for _, e := range edges {
							if !evictedChildren[e.Owner] && !evictedChildren[e.From.Level] && !evictedChildren[e.To.Level] {
								kept = append(kept, e)
							}
						}
						if len(kept) == 0 {
							delete(transitionReg.Out, k)
						} else {
							transitionReg.Out[k] = kept
						}
					}
				}
				if len(remaining) == 0 {
					delete(bIdx.Loaded, root)
				} else {
					bIdx.Loaded[root] = remaining
				}
			}
		}
		if mesh := sys.chunkMeshMap.Get(ev.id); mesh != nil && mesh.Uploaded {
			rl.UnloadMesh(&mesh.Mesh)
		}
		delete(idx.Loaded, ev.cc)
		ctx.World.RemoveEntity(ev.id)
	}

	// Tier transitions need a remesh (different resolution); heightmap is
	// still valid, so only MeshDirty.
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

	// WorldPos sits at the chunk's origin corner; mesh local vertices span
	// [0, ChunkSize] and render adds renderPos = pos.ToRenderSpace.
	for _, sp := range spawns {
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: sp.cc})
		ccCopy := sp.cc
		sys.chunkCoordMap.Add(e, &ccCopy)
		sys.chunkMarkerMap.Add(e, &components.TerrainChunk{})
		sys.heightDirtyMap.Add(e, &components.HeightmapDirty{})
		sys.meshDirtyMap.Add(e, &components.MeshDirty{})
		sys.propsDirtyMap.Add(e, &components.PropsDirty{})
		switch sp.tier {
		case core.LODTierActive:
			sys.lodActiveMap.Add(e, &components.LODActive{})
		case core.LODTierRelevant:
			sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		}
		idx.Loaded[sp.cc] = e
	}
}
