package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// BuildingSystem runs two independent passes per tick over chunks needing
// building work:
//
//  1. Terrain pass - for chunks with Heightmap, no Modified, no
//     BuildingTerrainProcessed: apply Stamper.RectCut for every Bunker
//     building whose Pos.Chunk equals this chunk. Marks BuildingTerrainProcessed.
//
//  2. Child pass - for chunks with Heightmap, no BuildingsProcessed: spawn
//     Level / Wall / Floor / Stairs / Furniture / Marker entities from the
//     full BuildingPlan stored in BuildingPlanIndex. Phase 16.5 emits the
//     plan from a generator; Phase 16.A loader will emit from a .glb file.
//
// Marker split mirrors RoadSystem (Phase 4 M4.5): heightmap edits freeze on
// player Modified, but child entities still respawn on every chunk return.
type BuildingSystem struct {
	terrainFilter       *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	childFilter         *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	buildingFilter      *ecs.Filter2[components.Building, components.WorldPos]
	indexRes            ecs.Resource[BuildingChildIndex]
	planIdxRes          ecs.Resource[BuildingPlanIndex]
	terrainProcessedMap *ecs.Map[components.BuildingTerrainProcessed]
	childProcessedMap   *ecs.Map[components.BuildingsProcessed]
	posMap              *ecs.Map[components.WorldPos]
	wallMap             *ecs.Map[components.WallSegment]
	floorMap            *ecs.Map[components.Floor]
	stairsMap           *ecs.Map[components.Stairs]
	doorMap             *ecs.Map[components.Door]
	windowMap           *ecs.Map[components.Window]
	memberMap           *ecs.Map[components.BuildingMember]
	levelMemberMap      *ecs.Map[components.LevelMember]
	stairLevelsMap      *ecs.Map[components.StairLevels]
	occupancyMap        *ecs.Map[components.Occupancy]
	coverDirectionMap   *ecs.Map[components.CoverDirection]
	shootingArcMap      *ecs.Map[components.ShootingArc]
	lodRelevantMap      *ecs.Map[components.LODRelevant]
	levelMap            *ecs.Map[components.Level]
	transitionMap       *ecs.Map[components.LevelTransition]
	furnitureMap        *ecs.Map[components.Furniture]
	markerMap           *ecs.Map[components.Marker]
	stamper             *Stamper
}

func (sys *BuildingSystem) InitUI(w *ecs.World) {
	sys.terrainFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.Modified](),
			ecs.C[components.BuildingTerrainProcessed](),
		)
	sys.childFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(ecs.C[components.BuildingsProcessed]())
	sys.buildingFilter = ecs.NewFilter2[components.Building, components.WorldPos](w)
	sys.indexRes = ecs.NewResource[BuildingChildIndex](w)
	sys.planIdxRes = ecs.NewResource[BuildingPlanIndex](w)
	sys.terrainProcessedMap = ecs.NewMap[components.BuildingTerrainProcessed](w)
	sys.childProcessedMap = ecs.NewMap[components.BuildingsProcessed](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.wallMap = ecs.NewMap[components.WallSegment](w)
	sys.floorMap = ecs.NewMap[components.Floor](w)
	sys.stairsMap = ecs.NewMap[components.Stairs](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.windowMap = ecs.NewMap[components.Window](w)
	sys.memberMap = ecs.NewMap[components.BuildingMember](w)
	sys.levelMemberMap = ecs.NewMap[components.LevelMember](w)
	sys.stairLevelsMap = ecs.NewMap[components.StairLevels](w)
	sys.occupancyMap = ecs.NewMap[components.Occupancy](w)
	sys.coverDirectionMap = ecs.NewMap[components.CoverDirection](w)
	sys.shootingArcMap = ecs.NewMap[components.ShootingArc](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.levelMap = ecs.NewMap[components.Level](w)
	sys.transitionMap = ecs.NewMap[components.LevelTransition](w)
	sys.furnitureMap = ecs.NewMap[components.Furniture](w)
	sys.markerMap = ecs.NewMap[components.Marker](w)
	sys.stamper = NewStamper(w)
}

func (BuildingSystem) Name() string { return "building" }

func (BuildingSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

type buildingRec struct {
	root      ecs.Entity
	b         components.Building
	rootChunk components.ChunkCoord
}

type pendingBuilding struct {
	root        ecs.Entity
	plan        *components.BuildingPlan
	rootChunk   components.ChunkCoord
	targetChunk components.ChunkCoord
}

func (sys *BuildingSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	idx := sys.indexRes.Get()
	if idx == nil {
		return
	}
	planIdx := sys.planIdxRes.Get()

	// Phase 16.B.0: bucket each Building by EVERY chunk its footprint
	// overlaps, not just the root chunk. Same Building shows up in 1..N
	// buckets - the terrain pass and child-spawn pass both rely on this
	// to do per-chunk work for cross-boundary buildings.
	byChunk := make(map[components.ChunkCoord][]buildingRec)
	qb := sys.buildingFilter.Query()
	for qb.Next() {
		b, pos := qb.Get()
		root := qb.Entity()
		fp := b.Footprint
		minCX := int32(math.Floor(float64(fp.MinX) / float64(components.ChunkSize)))
		maxCX := int32(math.Floor(float64(fp.MaxX) / float64(components.ChunkSize)))
		minCZ := int32(math.Floor(float64(fp.MinZ) / float64(components.ChunkSize)))
		maxCZ := int32(math.Floor(float64(fp.MaxZ) / float64(components.ChunkSize)))
		for cx := minCX; cx <= maxCX; cx++ {
			for cz := minCZ; cz <= maxCZ; cz++ {
				cc := components.ChunkCoord{X: cx, Z: cz}
				byChunk[cc] = append(byChunk[cc], buildingRec{
					root:      root,
					b:         *b,
					rootChunk: pos.Chunk,
				})
			}
		}
	}

	type processedEnt struct{ id ecs.Entity }
	var terrainDone []processedEnt
	qT := sys.terrainFilter.Query()
	for qT.Next() {
		cc, _, _ := qT.Get()
		ccVal := *cc
		if recs, ok := byChunk[ccVal]; ok {
			for _, r := range recs {
				if r.b.Kind != components.BuildingBunker {
					continue
				}
				sys.stamper.RectCut(ccVal, r.b.Footprint, components.BunkerDepth, components.BunkerFalloffWidth)
			}
		}
		terrainDone = append(terrainDone, processedEnt{qT.Entity()})
	}
	for _, p := range terrainDone {
		if !sys.terrainProcessedMap.Has(p.id) {
			sys.terrainProcessedMap.Add(p.id, &components.BuildingTerrainProcessed{})
		}
	}

	var pending []pendingBuilding
	var childDone []processedEnt
	qC := sys.childFilter.Query()
	for qC.Next() {
		cc, _, _ := qC.Get()
		ccVal := *cc
		if recs, ok := byChunk[ccVal]; ok {
			for _, r := range recs {
				if planIdx == nil {
					continue
				}
				plan := planIdx.Plans[r.root]
				if plan == nil || len(plan.Walls) == 0 {
					continue
				}
				pending = append(pending, pendingBuilding{
					root:        r.root,
					plan:        plan,
					rootChunk:   r.rootChunk,
					targetChunk: ccVal,
				})
			}
		}
		childDone = append(childDone, processedEnt{qC.Entity()})
	}

	for i := range pending {
		sys.spawnBuilding(ctx, idx, planIdx, pending[i].root, pending[i].plan, pending[i].rootChunk, pending[i].targetChunk)
	}

	for _, p := range childDone {
		if !sys.childProcessedMap.Has(p.id) {
			sys.childProcessedMap.Add(p.id, &components.BuildingsProcessed{})
		}
	}
}

// spawnBuilding emits child entities (Wall / Floor / Stairs / Furniture /
// Marker) for one Building INTO ONE TARGET CHUNK. Multi-chunk buildings
// process the same plan once per chunk their footprint overlaps; each pass
// spawns only the children whose world position falls inside `targetChunk`.
//
// Level entities are spawned in main.go (AlwaysActive) and resolved via
// BuildingPlanIndex; LevelTransition attaches to a wall entity if and only
// if the wall in question landed in this chunk (otherwise the next chunk's
// pass will attach it).
func (sys *BuildingSystem) spawnBuilding(ctx core.UpdateContext, idx *BuildingChildIndex, planIdx *BuildingPlanIndex, root ecs.Entity, plan *components.BuildingPlan, rootChunk, targetChunk components.ChunkCoord) {
	chunkBaseX := float32(rootChunk.X) * components.ChunkSize
	chunkBaseZ := float32(rootChunk.Z) * components.ChunkSize

	levelEnts := planIdx.Levels[root]
	resolveLevel := func(ref uint8) ecs.Entity {
		if ref == components.NoLevelRef || int(ref) >= len(levelEnts) {
			return ecs.Entity{}
		}
		return levelEnts[ref]
	}

	// localToTarget converts a chunk-local coord (relative to rootChunk) into
	// (target chunk, target-local). Returns (cc, local, true) iff the coord
	// lies in `targetChunk`. Used to filter children for this pass.
	localToTarget := func(local rl.Vector3) (components.ChunkCoord, rl.Vector3, bool) {
		worldX := local.X + chunkBaseX
		worldZ := local.Z + chunkBaseZ
		cx := int32(math.Floor(float64(worldX) / float64(components.ChunkSize)))
		cz := int32(math.Floor(float64(worldZ) / float64(components.ChunkSize)))
		cc := components.ChunkCoord{X: cx, Z: cz}
		if cc != targetChunk {
			return cc, rl.Vector3{}, false
		}
		return cc, rl.Vector3{
			X: worldX - float32(cx)*components.ChunkSize,
			Y: local.Y,
			Z: worldZ - float32(cz)*components.ChunkSize,
		}, true
	}

	// Walls: we still allocate a per-plan slice so LevelTransition can index
	// into it; slots whose wall didn't land in targetChunk stay zero.
	wallEnts := make([]ecs.Entity, len(plan.Walls))
	for i := range plan.Walls {
		ws := &plan.Walls[i]
		cc, local, hit := localToTarget(ws.Local)
		if !hit {
			continue
		}
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: cc, Local: local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		// Walls spanning multiple levels carry the lowest as their primary
		// LevelMember; the cutaway renderer uses wall.WorldPos.Y to derive
		// the visible level range independently.
		if len(ws.LevelRefs) > 0 {
			if lev := resolveLevel(ws.LevelRefs[0]); lev != (ecs.Entity{}) {
				sys.levelMemberMap.Add(e, &components.LevelMember{Level: lev})
			}
		}

		seg := ws.Segment
		sys.wallMap.Add(e, &seg)
		sys.coverDirectionMap.Add(e, &components.CoverDirection{Dir: ws.OutwardNormal})

		switch seg.OpeningKind {
		case components.OpeningDoor:
			sys.doorMap.Add(e, &components.Door{
				State:               components.DoorOpen,
				Material:            components.DoorWood,
				BlocksLOSWhenClosed: true,
			})
			sys.occupancyMap.Add(e, &components.Occupancy{Max: 1})
		case components.OpeningWindow:
			sys.windowMap.Add(e, &components.Window{Glass: true})
			sys.occupancyMap.Add(e, &components.Occupancy{Max: 1})
			sys.shootingArcMap.Add(e, &components.ShootingArc{
				Forward:      ws.OutwardNormal,
				HalfAngleRad: float32(math.Pi / 3),
			})
		}
		wallEnts[i] = e
		idx.Loaded[root] = append(idx.Loaded[root], e)
	}

	for i := range plan.LevelTransitions {
		t := &plan.LevelTransitions[i]
		if t.ViaWall < 0 || int(t.ViaWall) >= len(wallEnts) {
			continue
		}
		wallEnt := wallEnts[t.ViaWall]
		if wallEnt == (ecs.Entity{}) {
			continue
		}
		sys.transitionMap.Add(wallEnt, &components.LevelTransition{
			LevelA: resolveLevel(t.LevelA),
			LevelB: resolveLevel(t.LevelB),
		})
	}

	for i := range plan.Floors {
		fs := &plan.Floors[i]
		cc, local, hit := localToTarget(fs.Local)
		if !hit {
			continue
		}
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: cc, Local: local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		if lev := resolveLevel(fs.LevelRef); lev != (ecs.Entity{}) {
			sys.levelMemberMap.Add(e, &components.LevelMember{Level: lev})
		}
		f := fs.Floor
		sys.floorMap.Add(e, &f)
		idx.Loaded[root] = append(idx.Loaded[root], e)
	}

	for i := range plan.Stairs {
		ss := &plan.Stairs[i]
		cc, local, hit := localToTarget(ss.Local)
		if !hit {
			continue
		}
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: cc, Local: local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		s := ss.Stairs
		sys.stairsMap.Add(e, &s)
		// Phase 16.B.1.b: attach the two Level entities this stair joins,
		// resolved from the first and last anchor in the spec. Used by
		// SpatialBakeSystem to wire NavNode level endpoints.
		var fromLev, toLev ecs.Entity
		if len(ss.Anchors) > 0 {
			fromLev = resolveLevel(ss.Anchors[0].LevelRef)
			toLev = resolveLevel(ss.Anchors[len(ss.Anchors)-1].LevelRef)
		}
		sys.stairLevelsMap.Add(e, &components.StairLevels{From: fromLev, To: toLev})
		idx.Loaded[root] = append(idx.Loaded[root], e)
	}

	for i := range plan.Furniture {
		fs := &plan.Furniture[i]
		cc, local, hit := localToTarget(fs.Local)
		if !hit {
			continue
		}
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: cc, Local: local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		sys.furnitureMap.Add(e, &components.Furniture{
			Kind:  fs.Kind,
			Yaw:   fs.Yaw,
			Level: resolveLevel(fs.LevelRef),
		})
		if lev := resolveLevel(fs.LevelRef); lev != (ecs.Entity{}) {
			sys.levelMemberMap.Add(e, &components.LevelMember{Level: lev})
		}
		idx.Loaded[root] = append(idx.Loaded[root], e)
	}

	for i := range plan.Markers {
		ms := &plan.Markers[i]
		cc, local, hit := localToTarget(ms.Local)
		if !hit {
			continue
		}
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: cc, Local: local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		sys.markerMap.Add(e, &components.Marker{
			Kind:  ms.Kind,
			Level: resolveLevel(ms.LevelRef),
		})
		if lev := resolveLevel(ms.LevelRef); lev != (ecs.Entity{}) {
			sys.levelMemberMap.Add(e, &components.LevelMember{Level: lev})
		}
		idx.Loaded[root] = append(idx.Loaded[root], e)
	}
}
