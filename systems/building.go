package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// BuildingSystem runs two independent passes per tick over chunks needing
// building work:
//
//  1. Terrain pass — for chunks with Heightmap, no Modified, no
//     BuildingTerrainProcessed: apply Stamper.RectCut for every Bunker
//     building whose Pos.Chunk equals this chunk. Marks BuildingTerrainProcessed.
//
//  2. Child pass — for chunks with Heightmap, no BuildingsProcessed: spawn
//     wall / floor / stairs entities (plus Doors / Windows / Smart Object
//     attachments) for every Building in this chunk. Independent of Modified.
//
// Marker split mirrors RoadSystem (Phase 4 M4.5): heightmap edits freeze on
// player Modified, but child entities still respawn on every chunk return.
type BuildingSystem struct {
	terrainFilter        *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	childFilter          *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	buildingFilter       *ecs.Filter2[components.Building, components.WorldPos]
	indexRes             ecs.Resource[BuildingChildIndex]
	terrainProcessedMap  *ecs.Map[components.BuildingTerrainProcessed]
	childProcessedMap    *ecs.Map[components.BuildingsProcessed]
	posMap               *ecs.Map[components.WorldPos]
	wallMap              *ecs.Map[components.WallSegment]
	floorMap             *ecs.Map[components.Floor]
	stairsMap            *ecs.Map[components.Stairs]
	doorMap              *ecs.Map[components.Door]
	windowMap            *ecs.Map[components.Window]
	memberMap            *ecs.Map[components.BuildingMember]
	occupancyMap         *ecs.Map[components.Occupancy]
	coverDirectionMap    *ecs.Map[components.CoverDirection]
	shootingArcMap       *ecs.Map[components.ShootingArc]
	lodRelevantMap       *ecs.Map[components.LODRelevant]
	stamper              *Stamper
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
	sys.terrainProcessedMap = ecs.NewMap[components.BuildingTerrainProcessed](w)
	sys.childProcessedMap = ecs.NewMap[components.BuildingsProcessed](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.wallMap = ecs.NewMap[components.WallSegment](w)
	sys.floorMap = ecs.NewMap[components.Floor](w)
	sys.stairsMap = ecs.NewMap[components.Stairs](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.windowMap = ecs.NewMap[components.Window](w)
	sys.memberMap = ecs.NewMap[components.BuildingMember](w)
	sys.occupancyMap = ecs.NewMap[components.Occupancy](w)
	sys.coverDirectionMap = ecs.NewMap[components.CoverDirection](w)
	sys.shootingArcMap = ecs.NewMap[components.ShootingArc](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
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
	root ecs.Entity
	b    components.Building
}

func (sys BuildingSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	idx := sys.indexRes.Get()
	if idx == nil {
		return
	}

	// Bucket buildings by host chunk in one pass — used twice (terrain + child)
	// and several times per tick when chunks reload, so the bucketing pays off.
	byChunk := make(map[components.ChunkCoord][]buildingRec)
	qb := sys.buildingFilter.Query()
	for qb.Next() {
		b, pos := qb.Get()
		byChunk[pos.Chunk] = append(byChunk[pos.Chunk], buildingRec{root: qb.Entity(), b: *b})
	}

	// ── Pass 1: bunker RectCut ──
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
				sys.stamper.RectCut(ccVal, r.b.Footprint, bunkerDepth, bunkerFalloffWidth)
			}
		}
		terrainDone = append(terrainDone, processedEnt{qT.Entity()})
	}
	for _, p := range terrainDone {
		if !sys.terrainProcessedMap.Has(p.id) {
			sys.terrainProcessedMap.Add(p.id, &components.BuildingTerrainProcessed{})
		}
	}

	// ── Pass 2: child entity spawn ──
	type pendingChild struct {
		root ecs.Entity
		spec childSpec
		cc   components.ChunkCoord
	}
	var pending []pendingChild
	var childDone []processedEnt
	qC := sys.childFilter.Query()
	for qC.Next() {
		cc, _, _ := qC.Get()
		ccVal := *cc
		baseX := float32(ccVal.X) * components.ChunkSize
		baseZ := float32(ccVal.Z) * components.ChunkSize
		if recs, ok := byChunk[ccVal]; ok {
			for _, r := range recs {
				surfaceY := GroundHeight(r.b.Footprint.CenterX(), r.b.Footprint.CenterZ())
				specs := generateBuildingLayout(r.b, baseX, baseZ, surfaceY)
				for _, sp := range specs {
					pending = append(pending, pendingChild{root: r.root, spec: sp, cc: ccVal})
				}
			}
		}
		childDone = append(childDone, processedEnt{qC.Entity()})
	}

	for i := range pending {
		p := &pending[i]
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: p.cc, Local: p.spec.Local})
		sys.memberMap.Add(e, &components.BuildingMember{Building: p.root})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})

		switch p.spec.Kind {
		case childWall:
			w := p.spec.Wall
			sys.wallMap.Add(e, &w)
			sys.coverDirectionMap.Add(e, &components.CoverDirection{Dir: p.spec.OutwardNormal})
			switch w.OpeningKind {
			case components.OpeningDoor:
				// Phase 14.6 followup — default doors to Open so squads can
				// route through them via NavService TransitionEdges
				// (closed-door edges cost=0 / impassable). Player-driven
				// open/close interactions land in Phase 24 polish.
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
					Forward:      p.spec.OutwardNormal,
					HalfAngleRad: float32(math.Pi / 3),
				})
			}
		case childFloor:
			f := p.spec.Floor
			sys.floorMap.Add(e, &f)
		case childStairs:
			st := p.spec.Stairs
			sys.stairsMap.Add(e, &st)
		}

		idx.Loaded[p.root] = append(idx.Loaded[p.root], e)
	}

	for _, p := range childDone {
		if !sys.childProcessedMap.Has(p.id) {
			sys.childProcessedMap.Add(p.id, &components.BuildingsProcessed{})
		}
	}
}
