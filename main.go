package main

import (
	"flag"
	"fmt"
	"math"
	"runtime"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
	"rts-go/entities"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

const (
	initialScreenWidth  int32 = 800 * 2
	initialScreenHeight int32 = 450 * 2
)

// workersFlag picks the worker-pool size. 0 (default) -> runtime.NumCPU().
var workersFlag = flag.Int("workers", 0, "worker pool size (default = NumCPU)")

func main() {
	flag.Parse()

	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(initialScreenWidth, initialScreenHeight, "RTS/FPS 3D ECS Prototype")
	defer rl.CloseWindow()

	hudFont, hudFontIsCustom := loadHUDFont()
	if hudFontIsCustom {
		defer rl.UnloadFont(hudFont)
	}

	headless := isAIScene()
	if headless {
		rl.SetTargetFPS(0)
	} else {
		rl.SetTargetFPS(60)
	}

	app := core.NewApp()

	initTrace(app)
	defer func() { _ = app.Trace.Close() }()

	// Worker pool feeds the parallel hot-path systems (UnitMovement / Vision /
	// Formation / SquadMacroPath).
	workerCount := *workersFlag
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	workerPool := core.NewWorkerPool(workerCount)
	defer workerPool.Stop()
	fmt.Printf("worker pool: %d workers\n", workerPool.Workers())

	streamingMap := components.NewStreamingMap()
	ecs.AddResource(app.World, &streamingMap)
	terrainIndex := systems.NewTerrainChunkIndex()
	ecs.AddResource(app.World, &terrainIndex)
	propRegistry := systems.NewPropTypeRegistry()
	ecs.AddResource(app.World, propRegistry)
	propIndex := systems.NewPropChunkIndex()
	ecs.AddResource(app.World, &propIndex)
	rivers := components.Rivers{Polylines: makeStartingRivers()}
	ecs.AddResource(app.World, &rivers)
	roadGraph := makeStartingRoadGraph()
	systems.PreprocessRoadGraph(&roadGraph, &rivers)
	ecs.AddResource(app.World, &roadGraph)
	bridgeEdges := 0
	for _, e := range roadGraph.Edges {
		if e.Kind == components.RoadBridge {
			bridgeEdges++
		}
	}
	fmt.Printf("road graph: nodes=%d edges=%d bridges=%d\n",
		len(roadGraph.Nodes), len(roadGraph.Edges), bridgeEdges)

	buildingPlans := components.BuildingPlanList{Plans: makeStartingBuildings()}
	ecs.AddResource(app.World, &buildingPlans)
	buildingIndex := systems.NewBuildingChildIndex()
	ecs.AddResource(app.World, &buildingIndex)
	buildingPlanIndex := systems.NewBuildingPlanIndex()
	ecs.AddResource(app.World, &buildingPlanIndex)
	trenches := components.TrenchNetwork{Lines: makeStartingTrenches()}
	ecs.AddResource(app.World, &trenches)
	coverSlotIndex := systems.NewCoverSlotIndex()
	ecs.AddResource(app.World, &coverSlotIndex)
	transitionRegistry := components.NewTransitionRegistry()
	ecs.AddResource(app.World, &transitionRegistry)
	mapMarkerCache := components.NewMapMarkerCache()
	ecs.AddResource(app.World, &mapMarkerCache)
	formationPresets := components.FormationPresets{}
	ecs.AddResource(app.World, &formationPresets)
	// SpatialHash for Unit XZ positions; rebuilt serially before UnitMovement
	// so this tick's separation steering reads fresh positions. Consumers:
	// UnitMovement.separation, WeaponSystem.resolveShot/propagateSuppression,
	// VisionSystem.processVisionSeer.
	unitSpatialHash := core.NewSpatialHash(32.0)
	ecs.AddResource(app.World, unitSpatialHash)

	eventLog := components.NewEventLog()
	ecs.AddResource(app.World, eventLog)

	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	stamper := systems.NewStamper(app.World)
	navService := systems.NewNavService(app.World)
	squadService := systems.NewSquadService(app.World)
	// DamageService constructed before WeaponSystem.InitUI so the handle is live.
	damageService := systems.NewDamageService(app.World, squadService)
	damageService.SetClock(func() float32 { return squadService.Clock() })
	mapPingService := systems.NewMapPingService(app.World, func() float32 { return squadService.Clock() })
	damageService.SetMapPings(mapPingService)

	terrainStreamingSys := &systems.TerrainStreamingSystem{}
	terrainStreamingSys.InitUI(app.World)

	terrainLoadSys := &systems.TerrainLoadSystem{}
	terrainLoadSys.InitUI(app.World)

	terrainGenSys := &systems.TerrainGenSystem{}
	terrainGenSys.InitUI(app.World)

	riverSys := &systems.RiverSystem{}
	riverSys.InitUI(app.World)

	roadSys := &systems.RoadSystem{}
	roadSys.InitUI(app.World)

	buildingSys := &systems.BuildingSystem{}
	buildingSys.InitUI(app.World)

	trenchSys := &systems.TrenchSystem{}
	trenchSys.InitUI(app.World)

	propSpawnSys := &systems.PropSpawnSystem{}
	propSpawnSys.InitUI(app.World)

	spatialBakeSys := &systems.SpatialBakeSystem{}
	spatialBakeSys.InitUI(app.World)

	terrainMeshSys := &systems.TerrainMeshSystem{}
	terrainMeshSys.InitUI(app.World)

	groundStickSys := &systems.GroundStickSystem{}
	groundStickSys.InitUI(app.World)

	spatialHashRebuildSys := systems.NewSpatialHashRebuildSystem()
	spatialHashRebuildSys.InitUI(app.World)

	unitMovementSys := systems.NewUnitMovementSystem(workerPool)
	unitMovementSys.InitUI(app.World)

	visionSys := systems.NewVisionSystem(workerPool)
	visionSys.InitUI(app.World)

	// Particle handles built before WeaponSystem so its constructor takes a non-nil ref.
	particleHandles := systems.NewSpawnHandles(app.World)
	particleSys := systems.NewParticleSystem()
	particleSys.InitUI(app.World)

	// WeaponSystem runs after Vision so it reads the freshest Awareness FIFO.
	weaponSys := systems.NewWeaponSystem(workerPool, damageService, particleHandles)
	weaponSys.InitUI(app.World)

	// ThreatSystem runs after WeaponSystem (which mutates Threat.Suppression /
	// ThreatDir) so SurvivalInstinct / StanceController read recomputed State.
	threatSys := systems.NewThreatSystem()
	threatSys.InitUI(app.World)

	stanceSys := systems.NewStanceControllerSystem()
	stanceSys.InitUI(app.World)

	utilityEvalSys := systems.NewUtilityEvaluatorSystem()
	utilityEvalSys.InitUI(app.World)

	threatDecaySys := systems.NewThreatDecaySystem()
	threatDecaySys.InitUI(app.World)

	mapPingDecaySys := systems.NewMapPingDecaySystem()
	mapPingDecaySys.InitUI(app.World)

	levelVisSys := systems.NewLevelVisibilitySystem()
	levelVisSys.InitUI(app.World)

	// SurvivalInstinct runs after WeaponSystem (fresh Threat.Suppression) and
	// before FormationSystem so override-driven ActionQueue writes survive.
	survivalSys := systems.NewSurvivalInstinctSystem()
	survivalSys.InitUI(app.World)

	orderResolverSys := systems.NewOrderResolverSystem(squadService)
	orderResolverSys.InitUI(app.World)

	squadMacroPathSys := systems.NewSquadMacroPathSystem(navService, workerPool)
	squadMacroPathSys.InitUI(app.World)

	formationSys := systems.NewFormationSystem(squadService, workerPool)
	formationSys.InitUI(app.World)

	// MicroPath runs after FormationSystem (writer of ActionQueue.Head.Target
	// + MicroPath.Dirty). Serial — NavService holds Filter handles not
	// concurrent-safe.
	microPathSys := systems.NewMicroPathSystem(navService, workerPool)
	microPathSys.InitUI(app.World)

	mapMarkerCacheSys := &systems.MapMarkerCacheSystem{}
	mapMarkerCacheSys.InitUI(app.World)

	lodSys := &systems.LODSystem{
		ActiveRadius:   60,
		RelevantRadius: 120,
		Hysteresis:     2,
	}
	lodSys.InitUI(app.World)

	movementSys := &systems.MovementSystem{}
	movementSys.InitUI(app.World)

	streamingSys := &systems.StreamingSystem{}
	streamingSys.InitUI(app.World)

	orbitSys := &systems.OrbitSystem{}
	orbitSys.InitUI(app.World)

	cameraSys := &systems.CameraSystem{}
	cameraSys.InitUI(app.World)

	app.AddSystem(terrainStreamingSys)
	app.AddSystem(terrainLoadSys)
	app.AddSystem(terrainGenSys)
	app.AddSystem(riverSys)
	app.AddSystem(roadSys)
	app.AddSystem(buildingSys)
	app.AddSystem(trenchSys)
	app.AddSystem(propSpawnSys)
	app.AddSystem(spatialBakeSys)
	app.AddSystem(terrainMeshSys)
	app.AddSystem(groundStickSys)
	app.AddSystem(spatialHashRebuildSys)
	app.AddSystem(unitMovementSys)
	app.AddSystem(visionSys)
	app.AddSystem(weaponSys)
	app.AddSystem(particleSys)
	app.AddSystem(threatSys)
	app.AddSystem(stanceSys)
	app.AddSystem(utilityEvalSys)
	app.AddSystem(threatDecaySys)
	app.AddSystem(levelVisSys)
	app.AddSystem(mapPingDecaySys)
	app.AddSystem(orderResolverSys)
	app.AddSystem(survivalSys)
	app.AddSystem(squadMacroPathSys)
	app.AddSystem(formationSys)
	app.AddSystem(microPathSys)
	app.AddSystem(mapMarkerCacheSys)
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	posMap := ecs.NewMap[components.WorldPos](app.World)
	lodActiveMap := ecs.NewMap[components.LODActive](app.World)
	lodAnchorMap := ecs.NewMap[components.LODAnchor](app.World)
	alwaysActiveMap := ecs.NewMap[components.AlwaysActive](app.World)

	anchor := app.World.NewEntity()
	anchorPos := components.WorldPos{}
	if isDoorScene() {
		anchorPos = doorSceneAnchorPos()
	} else if isAIScene() {
		anchorPos = aiSceneAnchorPos()
	}
	posMap.Add(anchor, &anchorPos)
	lodActiveMap.Add(anchor, &components.LODActive{})
	lodAnchorMap.Add(anchor, &components.LODAnchor{})
	alwaysActiveMap.Add(anchor, &components.AlwaysActive{})

	camCompMap := ecs.NewMap[components.Camera](app.World)
	orbitMap := ecs.NewMap[components.OrbitController](app.World)
	activeCamMap := ecs.NewMap[components.ActiveCamera](app.World)

	camEnt := app.World.NewEntity()
	posMap.Add(camEnt, &components.WorldPos{Local: rl.Vector3{X: 0, Y: 15.0, Z: 20.0}})
	camCompMap.Add(camEnt, &components.Camera{Fovy: 75.0, Perspective: true})
	orbitMap.Add(camEnt, &components.OrbitController{
		Target:           anchor,
		Yaw:              0,
		Pitch:            0.6,
		Radius:           25.0,
		MinRadius:        5.0,
		MaxRadius:        100.0,
		SensitivityYaw:   0.01,
		SensitivityPitch: 0.01,
		SensitivityZoom:  4.0,
		Smooth:           0,
	})
	activeCamMap.Add(camEnt, &components.ActiveCamera{})

	buildingMap := ecs.NewMap[components.Building](app.World)
	buildingMemberMap := ecs.NewMap[components.BuildingMember](app.World)
	levelMap := ecs.NewMap[components.Level](app.World)
	buildingViewModeMap := ecs.NewMap[components.BuildingViewMode](app.World)
	levelVisibilityMap := ecs.NewMap[components.LevelVisibility](app.World)
	for i := range buildingPlans.Plans {
		p := &buildingPlans.Plans[i]
		root := app.World.NewEntity()
		fp := components.AABB2D{
			MinX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize - p.Size.X*0.5,
			MinZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize - p.Size.Y*0.5,
			MaxX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize + p.Size.X*0.5,
			MaxZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize + p.Size.Y*0.5,
		}
		posMap.Add(root, &p.Pos)
		buildingMap.Add(root, &components.Building{
			Kind:      p.Kind,
			Stories:   p.Stories,
			Yaw:       p.Yaw,
			Footprint: fp,
			Seed:      p.Seed,
		})
		alwaysActiveMap.Add(root, &components.AlwaysActive{})
		buildingPlanIndex.Plans[root] = p

		// Level entities live for the building's whole life independent of
		// chunk lifecycle (AlwaysActive); chunk-spawned children reference
		// them by stable entity handle.
		levels := make([]ecs.Entity, len(p.Levels))
		for li := range p.Levels {
			ls := &p.Levels[li]
			lev := app.World.NewEntity()
			lwx := ls.AABB.CenterX()
			lwz := ls.AABB.CenterZ()
			lcx := int32(math.Floor(float64(lwx) / float64(components.ChunkSize)))
			lcz := int32(math.Floor(float64(lwz) / float64(components.ChunkSize)))
			posMap.Add(lev, &components.WorldPos{
				Chunk: components.ChunkCoord{X: lcx, Z: lcz},
				Local: rl.Vector3{
					X: lwx - float32(lcx)*components.ChunkSize,
					Y: ls.AABB.CenterY(),
					Z: lwz - float32(lcz)*components.ChunkSize,
				},
			})
			buildingMemberMap.Add(lev, &components.BuildingMember{Building: root})
			alwaysActiveMap.Add(lev, &components.AlwaysActive{})
			levelMap.Add(lev, &components.Level{
				AABB:         ls.AABB,
				Name:         ls.Name,
				DisplayOrder: ls.DisplayOrder,
			})
			levelVisibilityMap.Add(lev, &components.LevelVisibility{})
			levels[li] = lev
		}
		buildingPlanIndex.Levels[root] = levels
		fmt.Printf("[startup] building %d kind=%d stories=%d levels=%d footprint=(%.0f..%.0f, %.0f..%.0f)\n",
			i, p.Kind, p.Stories, len(levels), fp.MinX, fp.MaxX, fp.MinZ, fp.MaxZ)

		var currentLevel ecs.Entity
		if len(levels) > 0 {
			currentLevel = levels[0]
		}
		buildingViewModeMap.Add(root, &components.BuildingViewMode{
			InteriorOpen: false,
			CurrentLevel: currentLevel,
			WallMode:     components.WallRenderAll,
		})
	}

	// One TrenchRoot entity per polyline so the hit-test resolver can return
	// an ecs.Entity in OrderTarget.Entity for OccupyTrench.
	trenchRootMap := ecs.NewMap[components.TrenchRoot](app.World)
	for i := range trenches.Lines {
		pts := trenches.Lines[i].Points
		if len(pts) == 0 {
			continue
		}
		ent := app.World.NewEntity()
		mid := pts[len(pts)/2]
		posMap.Add(ent, &mid)
		trenchRootMap.Add(ent, &components.TrenchRoot{Index: i})
		alwaysActiveMap.Add(ent, &components.AlwaysActive{})
	}

	unitFactoryRef := entities.NewUnitFactory(app.World, posMap)
	actionQueueMap := unitFactoryRef.ActionQueueMap

	inspectorMaps := ui.NewInspectorMaps(app.World)
	weaponMap := ecs.NewMap[components.Weapon](app.World)
	_ = weaponMap
	roleMap := ecs.NewMap[components.UnitRole](app.World)
	squadMemberMap := ecs.NewMap[components.SquadMember](app.World)
	rosterMap := ecs.NewMap[components.CommandRoster](app.World)
	formationDataMap := ecs.NewMap[components.FormationData](app.World)
	formationOrientMap := ecs.NewMap[components.FormationOrientation](app.World)
	formationCustomSlotsMap := ecs.NewMap[components.FormationCustomSlots](app.World)
	orderQueueMap := ecs.NewMap[components.OrderQueueHead](app.World)
	orderKindMap := ecs.NewMap[components.OrderKind](app.World)
	orderTargetMap := ecs.NewMap[components.OrderTarget](app.World)
	orderChainMap := ecs.NewMap[components.OrderChain](app.World)
	orderStateMap := ecs.NewMap[components.OrderState](app.World)
	orderProgressMap := ecs.NewMap[components.OrderProgress](app.World)
	orderIssuedAtMap := ecs.NewMap[components.OrderIssuedAt](app.World)
	movementProfileMap := ecs.NewMap[components.MovementProfile](app.World)
	staminaMap := ecs.NewMap[components.Stamina](app.World)
	hpMap := ecs.NewMap[components.HP](app.World)
	factionMap := ecs.NewMap[components.Faction](app.World)
	individualPosMap := ecs.NewMap[components.IndividualPosition](app.World)

	// Missing Faction (legacy spawns) falls through to FactionPlayer.
	squadColor := func(ent ecs.Entity) rl.Color {
		faction := components.FactionPlayer
		if ent != (ecs.Entity{}) && app.World.Alive(ent) {
			if f := factionMap.Get(ent); f != nil {
				faction = f.ID
			}
		}
		return squadColorFor(ent, faction)
	}

	// RoleService owns UnitRole + per-role Equipment sub-entities.
	roleService := systems.NewRoleService(app.World)

	// Closure so existing call sites keep their func(WorldPos) ecs.Entity sig.
	unitFactory := unitFactoryRef.Spawn

	playerFaction := components.Faction{ID: components.FactionPlayer}
	var doorScene *doorSceneState
	var aiTest *aiTestState
	if isAIScene() {
		aiTest = aiSceneSpawn(app.World, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap, buildingMap)
	} else if isDoorScene() {
		testSquad := squadService.CreateFromTemplate(
			systems.TmplMotorRifle, doorSceneSquadSpawn(),
			components.FormationLine, playerFaction, roleService, unitFactory)
		var firstBuilding, firstLevel ecs.Entity
		var firstLevelAABB components.AABB3D
		var firstFootprint components.AABB2D
		buildingScan := ecs.NewFilter1[components.Building](app.World)
		bq := buildingScan.Query()
		for bq.Next() {
			b := bq.Get()
			firstBuilding = bq.Entity()
			firstFootprint = b.Footprint
			break
		}
		bq.Close()
		if levels, ok := buildingPlanIndex.Levels[firstBuilding]; ok && len(levels) > 0 {
			firstLevel = levels[0]
			if lv := levelMap.Get(firstLevel); lv != nil {
				firstLevelAABB = lv.AABB
			}
		}
		doorScene = &doorSceneState{
			Squad:        testSquad,
			Building:     firstBuilding,
			Level:        firstLevel,
			LevelAABB:    firstLevelAABB,
			Footprint:    firstFootprint,
			World:        app.World,
			SquadService: squadService,
			PosMap:       posMap,
			RosterMap:    rosterMap,
		}
	} else {
		// Squads spawn OUTSIDE buildings so formation slots don't land on
		// wall-rasterised surface cells (which would block path planning).
		squadService.CreateFromTemplate(
			systems.TmplLightInfantry,
			components.WorldPos{}.Add(rl.Vector3{X: -25, Z: -55}),
			components.FormationLine, playerFaction, roleService, unitFactory)
		squadService.CreateFromTemplate(
			systems.TmplMGTeam,
			components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 15}),
			components.FormationWedge, playerFaction, roleService, unitFactory)
		squadService.CreateFromTemplate(
			systems.TmplATTeam,
			components.WorldPos{}.Add(rl.Vector3{X: -30, Z: 40}),
			components.FormationColumn, playerFaction, roleService, unitFactory)
		squadService.CreateFromTemplate(
			systems.TmplMotorRifle,
			components.WorldPos{}.Add(rl.Vector3{X: -20, Z: 0}),
			components.FormationLoose, playerFaction, roleService, unitFactory)

		// Hostile MotorRifle squad parked via DefendPosition.
		enemySpawn := components.WorldPos{}.Add(rl.Vector3{X: 5, Z: -90})
		enemySquad := squadService.CreateFromTemplate(
			systems.TmplMotorRifle, enemySpawn,
			components.FormationLine, components.Faction{ID: components.FactionEnemyRed},
			roleService, unitFactory)
		if enemySquad != (ecs.Entity{}) {
			squadService.IssueOrder(enemySquad,
				components.OrderKindDefendPosition, enemySpawn, ecs.Entity{},
				false, systems.OrderParams{})
		}
	}

	unitRenderFilter := ecs.NewFilter3[components.WorldPos, components.Unit, components.Stance](app.World)
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)
	wallRenderFilter := ecs.NewFilter2[components.WorldPos, components.WallSegment](app.World)
	floorRenderFilter := ecs.NewFilter2[components.WorldPos, components.Floor](app.World)
	stairsRenderFilter := ecs.NewFilter2[components.WorldPos, components.Stairs](app.World)
	levelCutawayFilter := ecs.NewFilter2[components.Level, components.BuildingMember](app.World)
	levelMemberMap := ecs.NewMap[components.LevelMember](app.World)
	coverDirReadMap := ecs.NewMap[components.CoverDirection](app.World)
	levelVisReadMap := ecs.NewMap[components.LevelVisibility](app.World)
	navOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverSlotFilter := ecs.NewFilter2[components.WorldPos, components.CoverSlot](app.World)
	mapPingFilter := ecs.NewFilter2[components.WorldPos, components.MapPing](app.World)
	navGridChunkFilter := ecs.NewFilter1[components.NavGrid](app.World)
	floorNavFilter := ecs.NewFilter3[components.WorldPos, components.Level, components.LevelNavGrid](app.World)
	visionAwareFilter := ecs.NewFilter2[components.WorldPos, components.Awareness](app.World).
		With(ecs.C[components.Unit]())
	// drawUnitPaths dedupes squad members from the solo filter via squadMemberMap.Has.
	unitPathSquadFilter := ecs.NewFilter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember](app.World)
	unitPathSoloFilter := ecs.NewFilter3[components.Unit, components.WorldPos, components.MicroPath](app.World)

	chunkAllFilter := ecs.NewFilter1[components.TerrainChunk](app.World)
	weaponFilter := ecs.NewFilter1[components.Weapon](app.World)
	unitFilter := ecs.NewFilter1[components.Unit](app.World)
	stairsCountFilter := ecs.NewFilter1[components.Stairs](app.World)
	squadFilter := ecs.NewFilter2[components.Squad, components.CommandRoster](app.World)

	buildingFilter := ecs.NewFilter1[components.Building](app.World)
	trenchRootFilter := ecs.NewFilter1[components.TrenchRoot](app.World)
	// 1.5 m snap radius = standing collider (0.35 m) + ~1 m forgiveness margin.
	unitHitFilter := ecs.NewFilter2[components.Unit, components.WorldPos](app.World)
	hitTester := &HitTester{
		BuildingFilter:  buildingFilter,
		BuildingMap:     buildingMap,
		TrenchRootMap:   trenchRootMap,
		TrenchRoots:     trenchRootFilter,
		Trenches:        &trenches,
		TrenchHitRadius: 2.5,
		UnitFilter:      unitHitFilter,
		FactionMap:      factionMap,
		UnitHitRadius:   1.5,
	}

	ghostWallMap := ecs.NewMap[components.WallSegment](app.World)
	ghostWindowMap := ecs.NewMap[components.Window](app.World)
	ghostFloorMap := ecs.NewMap[components.Floor](app.World)
	ghostCtx := &ghostContext{
		world:            app.World,
		posMap:           posMap,
		rosterMap:        rosterMap,
		formationDataMap: formationDataMap,
		stanceMap:        inspectorMaps.StanceMap,
		movementMap:      movementProfileMap,
		squadMemberMap:   squadMemberMap,
		hitTester:        hitTester,
		buildingIndex:    &buildingIndex,
		wallMap:          ghostWallMap,
		windowMap:        ghostWindowMap,
		floorMap:         ghostFloorMap,
		trenches:         &trenches,
		trenchRootMap:    trenchRootMap,
		squadColor:       squadColor,
	}

	orderMarkerRenderCtx := orderMarkerCtx{
		world:          app.World,
		posMap:         posMap,
		rosterMap:      rosterMap,
		squadMemberMap: squadMemberMap,
		orderQueueMap:  orderQueueMap,
		orderKindMap:   orderKindMap,
		orderTargetMap: orderTargetMap,
		orderChainMap:  orderChainMap,
		orderFacingMap: ecs.NewMap[components.OrderParamFacing](app.World),
		squadColor:     squadColor,
	}

	particleRenderCtx := ParticleRenderCtx{
		Filter: ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](app.World),
		EndMap: ecs.NewMap[components.ParticleEnd](app.World),
	}

	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

	screenW, screenH := initialScreenWidth, initialScreenHeight
	panelMgr := ui.NewPanelManager()
	// Restore split ratios from disk before the first Recompute.
	loadLayout(panelMgr)
	panelMgr.Recompute(screenW, screenH)
	defer saveLayout(panelMgr)
	scene3DRT := ui.NewScene3DRT(panelMgr.Get(ui.Panel3D))
	defer scene3DRT.Unload()

	// Pre-bake the map underlay (2 km x 2 km, 4 m/pixel = 500x500 = 250 KB).
	underlay := ui.BakeUnderlay(0, 0, 2000, 4, func(wx, wz float32) float32 {
		return systems.GroundHeight(wx, wz)
	})
	defer underlay.Unload()
	mapCam := ui.NewMapCamera()
	var mapPanning bool
	var mapPanCursor rl.Vector2
	var ctxMenu ui.ContextMenu
	// rmbState bundles RMB-hold session state. Active set on press, cleared
	// on release. PressOrigin / PressTarget are captured at press time so
	// release commits don't drift with the cursor.
	var rmbState struct {
		Active       bool
		SourcePanel  ui.PanelID
		PressOrigin  rl.Vector2
		PressTarget  components.WorldPos
		PressTimeSec float32
		HoveredBldg  ecs.Entity
		HasSelection bool
		FacingActive bool // > 8 px drag committed to facing-drag
		Ctrl, Alt    bool
		Double       bool
	}
	var lastRMBPressAt float32
	// 300 ms window for double-RMB — wide enough for relaxed chains, narrow
	// enough that two deliberate sequential clicks don't fuse.
	const rmbDoubleWindow float32 = 0.30
	var (
		scrollDragging         bool
		scrollDragStartCursorY float32
		scrollDragStartOffset  float32
	)
	const wheelScrollSpeed float32 = 30
	smoothedSquadPos := make(map[ecs.Entity]components.WorldPos, 8)

	var (
		navPath          []components.WorldPos
		selected         []ecs.Entity
		hovered          ecs.Entity
		hoveredBuilding  ecs.Entity
		hoveredLevel     ecs.Entity
		selectedBuilding ecs.Entity
		buildingWidget   *ui.BuildingWidgetLayout
		marqueeStart     rl.Vector2
		marqueeActive    bool
		marqueeOrigin    ui.PanelID
		expandedHUDOn    bool
		showMapDebugLy   bool
		binds            [5]bindEntry
		timelineView     = ui.NewTimelineView()
		timelineData     ui.TimelineData
		timelineHoverHit ui.TimelineHit
		timelineHoverOK  bool
		timelineHoverBlk ui.TimelineOrderBlock
		topBarPlayPause  rl.Rectangle
		topBarSpeedDown  rl.Rectangle
		topBarSpeedUp    rl.Rectangle
		chevronMenu      ui.ChevronMenu
		floating         = ui.NewFloatingState()
	)
	const marqueeClickThreshold float32 = 5
	// chromeBusy = "UI chrome currently owns mouse/keyboard"; content layers
	// skip LMB handlers when true so chrome doesn't double-fire into the world.
	chromeBusy := func() bool {
		return panelMgr.IsDragging() || panelMgr.IsCornerDragging() ||
			chevronMenu.Open || floating.IsBusy(rl.GetMousePosition()) ||
			floating.SwitchMenuOpen()
	}

	isSelected := func(e ecs.Entity) int {
		for i := range selected {
			if selected[i] == e {
				return i
			}
		}
		return -1
	}
	toggleSelected := func(e ecs.Entity) {
		if i := isSelected(e); i >= 0 {
			selected = append(selected[:i], selected[i+1:]...)
		} else {
			selected = append(selected, e)
		}
	}

	squadCenter := func(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool) {
		return systems.SquadCenter(world, roster, posMap)
	}

	// Floating and workspace forms share this pointer so zoom / kind / custom
	// slots survive a re-dock; SelectionFn keeps it bound to the selected squad.
	formationEditorCtx := ui.FormationEditorCtx{
		World:          app.World,
		RosterMap:      rosterMap,
		FormationMap:   formationDataMap,
		OrientMap:      formationOrientMap,
		CustomSlotsMap: formationCustomSlotsMap,
		RoleMap:        roleMap,
		PosMap:         posMap,
		SquadColor:     squadColor,
		Presets:        &formationPresets,
		SelectionFn: func() ecs.Entity {
			if cs, homo := groupSelected(selected, squadMemberMap); homo {
				return cs
			}
			return ecs.Entity{}
		},
	}
	formationEditor := ui.NewFormationEditor(ecs.Entity{}, formationEditorCtx)

	// ContentToPanel synthesises a Panel whose ContentRect recovers `content`
	// so each widget's chrome-aware draw code lands in the right place.
	// Panel3D is intentionally a no-op: the scene RT is sized to the
	// workspace leaf; detaching would need a second render texture.
	renderFloatingWidget := func(id ui.PanelID, content rl.Rectangle,
		cursor rl.Vector2, font rl.Font, lmbPress bool) bool {
		syn := func(title string) ui.Panel { return ui.ContentToPanel(content, title, id) }
		switch id {
		case ui.PanelFormation:
			formationEditor.DrawPanel(syn("Formation"), font, cursor, lmbPress)
		case ui.PanelMap:
			ui.DrawMap(syn("Map"), ui.MapRenderCtx{
				World:            app.World,
				Cam:              mapCam,
				Underlay:         &underlay,
				AnchorPos:        *posMap.Get(anchor),
				Selected:         selected,
				Hovered:          hovered,
				PosMap:           posMap,
				RosterMap:        rosterMap,
				SquadMemberMap:   squadMemberMap,
				SquadFilter:      squadFilter,
				SquadCenter:      squadCenter,
				SquadColor:       squadColor,
				RoadGraph:        &roadGraph,
				Rivers:           &rivers,
				Buildings:        &buildingPlans,
				ShowDebugLayers:  showMapDebugLy,
				OrderQueueMap:    orderQueueMap,
				OrderKindMap:     orderKindMap,
				OrderTargetMap:   orderTargetMap,
				OrderChainMap:    orderChainMap,
				SmoothedSquadPos: smoothedSquadPos,
				MapMarkerCache:   &mapMarkerCache,
				RoleMap:          roleMap,
				Font:             font,
				MapPingFilter:    mapPingFilter,
				Clock:            squadService.Clock(),
			})
		case ui.PanelInspect:
			ui.DrawInspector(syn("Inspector"), ui.InspectorCtx{
				InspectorMaps: inspectorMaps,
				World:         app.World,
				Selected:      selected,
				Hovered:       hovered,
				Font:          font,
				EventLog:      eventLog,
				Cursor:        cursor,
				LMBPressed:    lmbPress,
				PanelFocused:  true,
				Scroll:        nil,
				SquadColor:    squadColor,
			})
		case ui.PanelTimeline:
			ui.DrawTimelinePanel(syn("Timeline"), font, timelineData, &timelineView)
		case ui.PanelDebug:
			ui.DrawDebugPanel(syn("Debug"), font,
				debugOverlayToggles(&debugOverlay), cursor, lmbPress,
				"Radius: 2 chunks around camera")
		case ui.Panel3D:
			// Not floatable; chevron menu disables Float pane for the 3D leaf.
		}
		return false
	}

	makeFloatingRender := func(id ui.PanelID) ui.FloatingRenderFn {
		return func(c rl.Rectangle, cu rl.Vector2, f rl.Font, l bool) bool {
			return renderFloatingWidget(id, c, cu, f, l)
		}
	}

	floatSpawn := func(id ui.PanelID, title string, bounds rl.Rectangle) {
		// Use the leaf's previous bounds so the floater materialises in place.
		if bounds.Width < 240 {
			bounds.Width = 360
		}
		if bounds.Height < 160 {
			bounds.Height = 280
		}
		floating.Open(&ui.FloatingPanel{
			ID:        "float:" + string(id),
			Title:     title,
			Bounds:    bounds,
			PanelID:   id,
			RenderFor: makeFloatingRender,
		})
	}

	for !rl.WindowShouldClose() {
		if rl.IsWindowResized() {
			screenW = int32(rl.GetScreenWidth())
			screenH = int32(rl.GetScreenHeight())
			panelMgr.Recompute(screenW, screenH)
			scene3DRT.EnsureSize(panelMgr.Get(ui.Panel3D))
		}

		// Real-time dt for input / camera-orbit; the simulation tick scales
		// this by app.TimeScale inside App.Tick.
		var dtReal time.Duration
		if headless {
			// Fixed sim dt so headless AI tests are deterministic and don't
			// depend on whatever the uncapped CPU frame time happens to be.
			dtReal = time.Second / 60
		} else {
			dtReal = time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))
		}
		squadService.SetClock(float32(app.Elapsed().Seconds()))
		utilityEvalSys.SetClock(float32(app.Elapsed().Seconds()))

		cursor := rl.GetMousePosition()
		focused := panelMgr.FocusedAt(cursor)
		shiftHeld := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
		ctrlHeld := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)
		altHeld := rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt)

		panel3D := panelMgr.Get(ui.Panel3D)
		panelMap := panelMgr.Get(ui.PanelMap)

		if rl.IsKeyPressed(rl.KeyTab) {
			// Tab during a splitter drag aborts the drag before flipping the
			// preset (avoids half-applied resize state).
			if panelMgr.IsDragging() {
				panelMgr.AbortDrag()
			}
			panelMgr.TogglePreset()
			panelMgr.Recompute(screenW, screenH)
			scene3DRT.EnsureSize(panelMgr.Get(ui.Panel3D))
			panel3D = panelMgr.Get(ui.Panel3D)
			panelMap = panelMgr.Get(ui.PanelMap)
		}

		// Splitter / corner / chevron hover + drag. Priority chain: open menu
		// wins LMB; splitter; chevron (open menu); corner-grab (start split).
		splitterHover := panelMgr.SplitterAt(cursor)
		cornerHover := panelMgr.CornerAt(cursor)
		chevronHover := chevronLeafAt(panelMgr, cursor)
		switch {
		case panelMgr.IsDragging():
			if sp := panelMgr.DraggingSplitter(); sp != nil {
				if sp.Orient == ui.SplitVertical {
					rl.SetMouseCursor(rl.MouseCursorResizeEW)
				} else {
					rl.SetMouseCursor(rl.MouseCursorResizeNS)
				}
			}
		case panelMgr.IsCornerDragging():
			rl.SetMouseCursor(rl.MouseCursorResizeAll)
		case floating.IsResizing() || floating.ResizeHover(cursor):
			if c, ok := floating.ResizeCursorAt(cursor); ok {
				rl.SetMouseCursor(c)
			} else {
				rl.SetMouseCursor(rl.MouseCursorResizeNWSE)
			}
		case splitterHover != nil:
			if splitterHover.Orient == ui.SplitVertical {
				rl.SetMouseCursor(rl.MouseCursorResizeEW)
			} else {
				rl.SetMouseCursor(rl.MouseCursorResizeNS)
			}
		case cornerHover != nil:
			rl.SetMouseCursor(rl.MouseCursorResizeNWSE)
		case chevronHover != nil:
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		default:
			rl.SetMouseCursor(rl.MouseCursorDefault)
		}

		// Floating panels eat LMB first; returns true when consumed.
		floatingConsumed := floating.HandleInput(cursor,
			rl.IsMouseButtonPressed(rl.MouseButtonLeft),
			rl.IsMouseButtonDown(rl.MouseButtonLeft),
			rl.IsKeyPressed(rl.KeyEscape),
			screenW, screenH)

		// LMB press dispatch. lmbDown is the raw press; lmbPress is the gated
		// form. Menu-open and floating-panel presses bypass workspace handlers.
		overFloating := floating.HitTest(cursor) != nil
		lmbDown := rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !floatingConsumed && !overFloating
		lmbPress := lmbDown && !panelMgr.IsDragging() && !panelMgr.IsCornerDragging()
		if chevronMenu.Open && lmbDown {
			if idx := chevronMenu.HitItem(hudFont, cursor); idx >= 0 {
				it := chevronMenu.Items[idx]
				if !it.Disabled {
					handleMenuItem(panelMgr, &chevronMenu, it, floatSpawn)
					saveLayout(panelMgr)
				}
				chevronMenu.Close()
			} else {
				chevronMenu.Close()
			}
			panel3D = panelMgr.Get(ui.Panel3D)
			panelMap = panelMgr.Get(ui.PanelMap)
			scene3DRT.EnsureSize(panel3D)
		} else if lmbPress && splitterHover != nil {
			panelMgr.BeginDrag(splitterHover)
		} else if lmbPress && chevronHover != nil {
			isRoot := chevronHover == panelMgr.Workspace
			ch := ui.ChevronRect(ui.Panel{Bounds: chevronHover.Bounds})
			chevronMenu.OpenAt(chevronHover, ch, isRoot)
		} else if lmbPress && cornerHover != nil {
			panelMgr.BeginCornerDrag(cornerHover, cursor)
		}

		if panelMgr.IsDragging() {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				panelMgr.UpdateDrag(cursor)
				panel3D = panelMgr.Get(ui.Panel3D)
				panelMap = panelMgr.Get(ui.PanelMap)
				scene3DRT.EnsureSize(panel3D)
			} else {
				if panelMgr.EndDrag() {
					saveLayout(panelMgr)
				}
				panel3D = panelMgr.Get(ui.Panel3D)
				panelMap = panelMgr.Get(ui.PanelMap)
				scene3DRT.EnsureSize(panel3D)
			}
		}
		if panelMgr.IsCornerDragging() {
			if !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				if panelMgr.CommitCornerDrag(cursor) {
					saveLayout(panelMgr)
				}
				panel3D = panelMgr.Get(ui.Panel3D)
				panelMap = panelMgr.Get(ui.PanelMap)
				scene3DRT.EnsureSize(panel3D)
			} else if rl.IsKeyPressed(rl.KeyEscape) {
				panelMgr.CancelCornerDrag()
			}
		}

		if doorScene != nil {
			doorScene.EnsureInit()
			doorScene.Update(float32(app.Elapsed().Seconds()))
			doorScene.HandleHotkeys(focused == ui.Panel3D)
		}

		if aiTest != nil {
			aiTest.Update(float32(app.Elapsed().Seconds()))
		}

		if rl.IsKeyPressed(rl.KeySpace) {
			if app.TimeScale > 0 {
				app.LastNonZeroScale = app.TimeScale
				app.TimeScale = 0
			} else {
				if app.LastNonZeroScale <= 0 {
					app.LastNonZeroScale = 1
				}
				app.TimeScale = app.LastNonZeroScale
			}
		}
		if rl.IsKeyPressed(rl.KeyEqual) || rl.IsKeyPressed(rl.KeyKpAdd) {
			app.TimeScale = nextTimeScale(app.TimeScale, +1)
			app.LastNonZeroScale = app.TimeScale
		}
		if rl.IsKeyPressed(rl.KeyMinus) || rl.IsKeyPressed(rl.KeyKpSubtract) {
			app.TimeScale = nextTimeScale(app.TimeScale, -1)
			app.LastNonZeroScale = app.TimeScale
		}

		anchorPos := posMap.Get(anchor)
		anchorSpeed := float32(20.0)
		if shiftHeld {
			anchorSpeed *= 4.0
		}
		orbit := orbitMap.Get(camEnt)
		sy := float32(math.Sin(float64(orbit.Yaw)))
		cy := float32(math.Cos(float64(orbit.Yaw)))
		var inFwd, inRight float32
		wasdAllowed := focused == ui.Panel3D || focused == ui.PanelNone
		if wasdAllowed {
			if rl.IsKeyDown(rl.KeyW) {
				inFwd += 1
			}
			if rl.IsKeyDown(rl.KeyS) {
				inFwd -= 1
			}
			if rl.IsKeyDown(rl.KeyD) {
				inRight += 1
			}
			if rl.IsKeyDown(rl.KeyA) {
				inRight -= 1
			}
		}
		wasdActive := inFwd != 0 || inRight != 0
		if wasdActive {
			if mag := float32(math.Sqrt(float64(inFwd*inFwd + inRight*inRight))); mag > 1 {
				inFwd /= mag
				inRight /= mag
			}
			step := anchorSpeed * float32(dtReal.Seconds())
			move := rl.Vector3{
				X: step * (inFwd*(-sy) + inRight*cy),
				Z: step * (inFwd*(-cy) + inRight*(-sy)),
			}
			*anchorPos = anchorPos.Add(move)
			navPath = nil
		}

		// Map's content rect (drawing surface minus chrome). Cursor conversions
		// go through this so clicks / zoom pivots align with what's drawn.
		panelMapContent := ui.ContentRect(panelMap)

		if focused == ui.PanelMap && !chromeBusy() {
			if rl.IsMouseButtonPressed(rl.MouseButtonMiddle) {
				mapPanning = true
				mapPanCursor = cursor
			}
			if mapPanning && rl.IsMouseButtonDown(rl.MouseButtonMiddle) {
				dx := cursor.X - mapPanCursor.X
				dy := cursor.Y - mapPanCursor.Y
				mapCam.Pan(dx, dy)
				mapPanCursor = cursor
			}
			if rl.IsMouseButtonReleased(rl.MouseButtonMiddle) {
				mapPanning = false
			}
			if wheel := rl.GetMouseWheelMove(); wheel != 0 {
				factor := float32(math.Pow(1.15, float64(wheel)))
				mapCam.ZoomAt(cursor, panelMapContent, factor)
			}
		} else {
			mapPanning = false
		}

		if focused == ui.PanelInspect && !chromeBusy() {
			if wheel := rl.GetMouseWheelMove(); wheel != 0 {
				if scroll := panelMgr.ScrollByID(ui.PanelInspect); scroll != nil {
					scroll.OffsetY -= wheel * wheelScrollSpeed
					ui.ClampScrollOffset(panelMgr.Get(ui.PanelInspect), scroll)
				}
			}
		}
		inspScrollPanel := panelMgr.Get(ui.PanelInspect)
		inspScroll := panelMgr.ScrollByID(ui.PanelInspect)
		if !chromeBusy() && inspScroll != nil {
			thumb := ui.ScrollbarThumbRect(inspScrollPanel, inspScroll)
			if !scrollDragging && thumb.Width > 0 && thumb.Height > 0 &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft) &&
				cursor.X >= thumb.X && cursor.X < thumb.X+thumb.Width &&
				cursor.Y >= thumb.Y && cursor.Y < thumb.Y+thumb.Height {
				scrollDragging = true
				scrollDragStartCursorY = cursor.Y
				scrollDragStartOffset = inspScroll.OffsetY
			}
		}
		if scrollDragging {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) && inspScroll != nil {
				track := ui.ScrollbarRect(inspScrollPanel)
				maxOffset := inspScroll.ContentHeight - track.Height
				thumbH := ui.ScrollbarThumbRect(inspScrollPanel, inspScroll).Height
				scrollableTrack := track.Height - thumbH
				if scrollableTrack > 0 && maxOffset > 0 {
					dy := cursor.Y - scrollDragStartCursorY
					inspScroll.OffsetY = scrollDragStartOffset + dy*(maxOffset/scrollableTrack)
					ui.ClampScrollOffset(inspScrollPanel, inspScroll)
				}
			} else {
				scrollDragging = false
			}
		}

		if focused == ui.PanelTopBar && !chromeBusy() && !scrollDragging &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			switch ui.TopBarHitTest(cursor, topBarPlayPause, topBarSpeedDown, topBarSpeedUp) {
			case ui.TopBarHitPlayPause:
				if app.TimeScale > 0 {
					app.LastNonZeroScale = app.TimeScale
					app.TimeScale = 0
				} else {
					if app.LastNonZeroScale <= 0 {
						app.LastNonZeroScale = 1
					}
					app.TimeScale = app.LastNonZeroScale
				}
			case ui.TopBarHitSpeedDown:
				app.TimeScale = nextTimeScale(app.TimeScale, -1)
				app.LastNonZeroScale = app.TimeScale
			case ui.TopBarHitSpeedUp:
				app.TimeScale = nextTimeScale(app.TimeScale, +1)
				app.LastNonZeroScale = app.TimeScale
			}
		}

		timelineHoverOK = false
		if focused == ui.PanelTimeline && !chromeBusy() {
			panelTL := panelMgr.Get(ui.PanelTimeline)
			timelineHoverHit, timelineHoverOK = ui.TimelineHitTest(panelTL, timelineData, timelineView, cursor)
			if timelineHoverHit.HitOrder {
				for _, r := range timelineData.Rows {
					if r.Squad != timelineHoverHit.Squad {
						continue
					}
					for _, ob := range r.Orders {
						if ob.Order == timelineHoverHit.Order {
							timelineHoverBlk = ob
							break
						}
					}
				}
			}
			if wheel := rl.GetMouseWheelMove(); wheel != 0 {
				if shiftHeld {
					factor := float32(math.Pow(1.15, float64(wheel)))
					next := timelineView.PixelsPerSec * factor
					if next < ui.TimelineMinPxPerSec {
						next = ui.TimelineMinPxPerSec
					}
					if next > ui.TimelineMaxPxPerSec {
						next = ui.TimelineMaxPxPerSec
					}
					timelineView.PixelsPerSec = next
					timelineView.Follow = false
				} else {
					timelineView.OffsetT -= wheel * 30 / timelineView.PixelsPerSec
					timelineView.Follow = false
				}
			}
			if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !scrollDragging {
				if timelineHoverOK && timelineHoverHit.Squad != (ecs.Entity{}) &&
					app.World.Alive(timelineHoverHit.Squad) {
					if r := rosterMap.Get(timelineHoverHit.Squad); r != nil {
						selected = selected[:0]
						for i := uint8(0); i < r.Count; i++ {
							if m := r.Members[i]; m != (ecs.Entity{}) && app.World.Alive(m) {
								selected = append(selected, m)
							}
						}
						navPath = nil
					}
					if timelineHoverHit.HitOrder && app.World.Alive(timelineHoverHit.Order) {
						if t := orderTargetMap.Get(timelineHoverHit.Order); t != nil {
							*posMap.Get(anchor) = t.Pos
						}
					}
				}
			}
			// RMB on background -> re-enable Follow.
			if rl.IsMouseButtonPressed(rl.MouseButtonRight) && timelineHoverOK && !timelineHoverHit.HitOrder {
				timelineView.Follow = true
			}
		}

		// 3D panel cursor (content-rect-local). viewW/H match the content
		// rect (= RT size) so screen<->world projections match what's drawn.
		panel3DContent := ui.ContentRect(panel3D)
		panel3DLocal := rl.Vector2{
			X: cursor.X - panel3DContent.X,
			Y: cursor.Y - panel3DContent.Y,
		}
		panel3DW := int32(panel3DContent.Width)
		panel3DH := int32(panel3DContent.Height)
		if panel3DW < 1 {
			panel3DW = 1
		}
		if panel3DH < 1 {
			panel3DH = 1
		}

		// LMB press. Splitter drag claims LMB exclusively. Building widget
		// chips also claim the press — skip marquee start when over a chip.
		widgetClickConsumed := false
		if !chromeBusy() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && buildingWidget != nil {
			if hit := ui.HitTestBuildingWidget(buildingWidget, cursor); hit != nil {
				target := buildingWidget.Root
				if bvm := buildingViewModeMap.Get(target); bvm != nil {
					levels := buildingPlanIndex.Levels[target]
					switch hit.Kind {
					case ui.ChipKindLevel:
						if hit.Index >= 0 && hit.Index < len(levels) {
							bvm.CurrentLevel = levels[hit.Index]
						}
					case ui.ChipKindWallMode:
						bvm.WallMode = components.WallRenderMode(hit.Index)
					case ui.ChipKindInside:
						bvm.InteriorOpen = !bvm.InteriorOpen
					}
					// Pin the widget so it doesn't vanish on a follow-up click.
					selectedBuilding = target
					widgetClickConsumed = true
				}
			}
		}
		if !chromeBusy() && !widgetClickConsumed && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			switch focused {
			case ui.Panel3D:
				marqueeStart = cursor
				marqueeActive = true
				marqueeOrigin = ui.Panel3D
			case ui.PanelMap:
				mapCtx := ui.MapRenderCtx{
					World: app.World, Cam: mapCam, SquadFilter: squadFilter,
					SquadCenter:    squadCenter,
					MapMarkerCache: &mapMarkerCache,
				}
				hit := ui.PickSquadAt(cursor, mapCtx, panelMap, 12)
				if hit != (ecs.Entity{}) && app.World.Alive(hit) {
					if r := rosterMap.Get(hit); r != nil {
						if shiftHeld {
							for i := uint8(0); i < r.Count; i++ {
								if isSelected(r.Members[i]) < 0 {
									selected = append(selected, r.Members[i])
								}
							}
						} else {
							selected = append(selected[:0], r.Members[:r.Count]...)
						}
					}
				} else if !shiftHeld {
					selected = nil
				}
			}
		}

		if rl.IsMouseButtonReleased(rl.MouseButtonLeft) && marqueeActive {
			end := cursor
			dx := end.X - marqueeStart.X
			dy := end.Y - marqueeStart.Y
			dragDist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			if marqueeOrigin == ui.Panel3D {
				if dragDist < marqueeClickThreshold {
					localEnd := rl.Vector2{X: end.X - panel3DContent.X, Y: end.Y - panel3DContent.Y}
					if hit, ok := pickUnitFromMouse(unitRenderFilter, *anchorPos, localEnd, panel3DW, panel3DH); ok {
						if shiftHeld {
							toggleSelected(hit)
						} else {
							selected = []ecs.Entity{hit}
						}
						selectedBuilding = ecs.Entity{}
					} else if hoveredBuilding != (ecs.Entity{}) {
						// Empty 3D click on a footprint pins the widget.
						selectedBuilding = hoveredBuilding
						if !shiftHeld {
							selected = nil
						}
					} else if !shiftHeld {
						selected = nil
						selectedBuilding = ecs.Entity{}
					}
				} else {
					localStart := rl.Vector2{X: marqueeStart.X - panel3DContent.X, Y: marqueeStart.Y - panel3DContent.Y}
					localEnd := rl.Vector2{X: end.X - panel3DContent.X, Y: end.Y - panel3DContent.Y}
					minX, maxX := localStart.X, localEnd.X
					if maxX < minX {
						minX, maxX = maxX, minX
					}
					minY, maxY := localStart.Y, localEnd.Y
					if maxY < minY {
						minY, maxY = maxY, minY
					}
					hits := collectUnitsInRect(unitRenderFilter, minX, maxX, minY, maxY, panel3DW, panel3DH)
					if shiftHeld {
						for _, h := range hits {
							if isSelected(h) < 0 {
								selected = append(selected, h)
							}
						}
					} else {
						selected = hits
					}
				}
			}
			marqueeActive = false
		}

		// RMB orders. Flow: press snapshots target+modifiers+hovered-building;
		// while held, drag>8px enters facing-drag mode and hold≥200ms over a
		// building opens the ContextMenu popup; release commits hovered popup
		// item / facing-drag yaw / tap; ESC closes the popup.
		if rl.IsMouseButtonPressed(rl.MouseButtonRight) && !floating.IsBusy(cursor) {
			var (
				pressTarget components.WorldPos
				targetOK    bool
			)
			switch focused {
			case ui.Panel3D:
				pressTarget, targetOK = mouseTargetWorldPos(systems.CurrentCamera,
					anchorPos.ToRenderSpace(systems.CurrentOriginChunk),
					panel3DLocal, panel3DW, panel3DH)
				// Snap to ground-floor centre when cursor visually over a
				// building but raycast lands just outside the footprint.
				if targetOK && hoveredBuilding != (ecs.Entity{}) {
					if levels := buildingPlanIndex.Levels[hoveredBuilding]; len(levels) > 0 {
						if lvl := levelMap.Get(levels[0]); lvl != nil {
							wx := lvl.AABB.CenterX()
							wz := lvl.AABB.CenterZ()
							cx := int32(math.Floor(float64(wx) / float64(components.ChunkSize)))
							cz := int32(math.Floor(float64(wz) / float64(components.ChunkSize)))
							pressTarget = components.WorldPos{
								Chunk: components.ChunkCoord{X: cx, Z: cz},
								Local: rl.Vector3{
									X: wx - float32(cx)*components.ChunkSize,
									Y: lvl.AABB.MinY,
									Z: wz - float32(cz)*components.ChunkSize,
								},
							}
						}
					}
				}
			case ui.PanelMap:
				pressTarget = ui.MapPanelToWorld(cursor, mapCam, panelMapContent)
				targetOK = true
			}
			if targetOK && len(selected) > 0 && (focused == ui.Panel3D || focused == ui.PanelMap) {
				now := float32(app.Elapsed().Seconds())
				rmbState.Active = true
				rmbState.SourcePanel = focused
				rmbState.PressOrigin = cursor
				rmbState.PressTarget = pressTarget
				rmbState.PressTimeSec = now
				rmbState.HoveredBldg = hoveredBuilding
				rmbState.HasSelection = true
				rmbState.FacingActive = false
				rmbState.Ctrl = ctrlHeld
				rmbState.Alt = altHeld
				rmbState.Double = (now - lastRMBPressAt) <= rmbDoubleWindow
				lastRMBPressAt = now
				fmt.Printf("[rmb] press hovered=%v target=(%.1f,%.1f) selected=%d focused=%s\n",
					hoveredBuilding != (ecs.Entity{}),
					pressTarget.Local.X+float32(pressTarget.Chunk.X)*components.ChunkSize,
					pressTarget.Local.Z+float32(pressTarget.Chunk.Z)*components.ChunkSize,
					len(selected), focused)
			}
		}

		if rmbState.Active {
			rmbDown := rl.IsMouseButtonDown(rl.MouseButtonRight)
			rmbReleased := rl.IsMouseButtonReleased(rl.MouseButtonRight)

			if rmbDown && !ctxMenu.IsActive() && !rmbState.FacingActive {
				dx := cursor.X - rmbState.PressOrigin.X
				dy := cursor.Y - rmbState.PressOrigin.Y
				if dx*dx+dy*dy > 8*8 {
					rmbState.FacingActive = true
				} else {
					now := float32(app.Elapsed().Seconds())
					if (now-rmbState.PressTimeSec) >= 0.200 &&
						rmbState.HoveredBldg != (ecs.Entity{}) &&
						rmbState.SourcePanel == ui.Panel3D {
						sections := buildBuildingPopupSections(rmbState.HoveredBldg, &buildingPlanIndex, levelMap)
						ctxMenu.Begin(cursor, sections, rmbState.SourcePanel, panel3DContent)
					}
				}
			}

			// Popup active: LMB / ESC / RMB-release (single-gesture commit).
			if ctxMenu.IsActive() {
				escPressed := rl.IsKeyPressed(rl.KeyEscape)
				lmbPressedForMenu := rl.IsMouseButtonPressed(rl.MouseButtonLeft)
				res := ctxMenu.Tick(cursor, lmbPressedForMenu, escPressed)
				switch {
				case rmbReleased && ctxMenu.IsActive():
					if item, ok := ctxMenu.HoveredItemDetails(); ok && item.Enabled {
						ctxMenu.Reset()
						issueBuildingPopupOrder(selected, item, rmbState.HoveredBldg,
							rmbState.PressTarget, shiftHeld,
							squadService, navService, squadMemberMap, posMap, actionQueueMap, levelMap)
					} else {
						ctxMenu.Reset()
					}
				case res.Committed:
					issueBuildingPopupOrder(selected, res.Item, rmbState.HoveredBldg,
						rmbState.PressTarget, shiftHeld,
						squadService, navService, squadMemberMap, posMap, actionQueueMap, levelMap)
				}
			}

			if rmbReleased && !ctxMenu.IsActive() {
				switch {
				case rmbState.FacingActive:
					dx := cursor.X - rmbState.PressOrigin.X
					dy := cursor.Y - rmbState.PressOrigin.Y
					yaw := float32(math.Atan2(float64(dx), float64(-dy)))
					mods := rmbModifiersFromPress(rmbState.Ctrl, rmbState.Alt, rmbState.Double)
					params := applyModifiersToParams(systems.OrderParams{
						HasFacing:    true,
						FacingYawRad: yaw,
					}, mods)
					resolveRMBOrderWithParams(selected, rmbState.PressTarget, shiftHeld, nil, params, hitTester,
						squadService, navService, squadMemberMap, posMap, actionQueueMap)
				default:
					// Shift+RMB on subset → IndividualPosition path.
					placed := false
					if shiftHeld {
						if _, ok := detectSubsetOfSquad(selected, squadMemberMap, rosterMap); ok {
							placeIndividualPositions(app.World, selected, rmbState.PressTarget,
								individualPosMap, float32(app.Elapsed().Seconds()))
							placed = true
						}
					}
					if !placed {
						mods := rmbModifiersFromPress(rmbState.Ctrl, rmbState.Alt, rmbState.Double)
						fmt.Printf("[rmb] tap commit target=(%.1f,%.1f) selected=%d\n",
							rmbState.PressTarget.Local.X+float32(rmbState.PressTarget.Chunk.X)*components.ChunkSize,
							rmbState.PressTarget.Local.Z+float32(rmbState.PressTarget.Chunk.Z)*components.ChunkSize,
							len(selected))
						resolveRMBOrder(selected, rmbState.PressTarget, shiftHeld, nil, mods, hitTester,
							squadService, navService, squadMemberMap, posMap, actionQueueMap)
					}
				}
			}

			if rmbReleased {
				rmbState.Active = false
				rmbState.FacingActive = false
			}
		}

		// H -> Stop order. Mirrors resolveRMBOrder's squad-vs-soloist split.
		if rl.IsKeyPressed(rl.KeyH) && len(selected) > 0 {
			groups := groupSelectionByOwner(selected, squadMemberMap)
			for _, s := range groups.SquadsToOrder {
				squadService.CancelAllOrders(s)
			}
			for _, e := range groups.Soloists {
				if aq := actionQueueMap.Get(e); aq != nil {
					systems.ClearActions(aq)
					systems.PushAction(aq, components.Action{Kind: components.ActionStop})
				}
			}
		}

		// T -> form Squad. Infantry-only merges land in FormationLoose;
		// mixed infantry+vehicle (future) snapshot positions into
		// FormationCustomSlots + lock OrientNorth.
		if rl.IsKeyPressed(rl.KeyT) && len(selected) >= 2 {
			mixed := containsVehicle(selected, app.World)
			kind := components.FormationLoose
			if mixed {
				kind = components.FormationLine
			}
			// Snapshot BEFORE create (CreateFromUnits may despawn old squads).
			snapshots := make(map[ecs.Entity]components.WorldPos, len(selected))
			for _, e := range selected {
				if p := posMap.Get(e); p != nil {
					snapshots[e] = *p
				}
			}
			newSquad := squadService.CreateFromUnits(selected, kind)
			if newSquad != (ecs.Entity{}) && app.World.Alive(newSquad) {
				if r := rosterMap.Get(newSquad); r != nil {
					if mixed {
						applyPreservedSlots(newSquad, r, snapshots,
							formationCustomSlotsMap, formationOrientMap)
					}
					selected = append(selected[:0], r.Members[:r.Count]...)
				}
			}
		}

		if rl.IsKeyPressed(rl.KeyU) && len(selected) > 0 {
			for _, e := range selected {
				squadService.Leave(e)
			}
		}

		// F1-F4 -> change formation.
		if len(selected) > 0 {
			if commonSquad, homo := groupSelected(selected, squadMemberMap); homo && commonSquad != (ecs.Entity{}) {
				var newKind components.FormationKind
				keyHit := false
				switch {
				case rl.IsKeyPressed(rl.KeyF1):
					newKind = components.FormationLine
					keyHit = true
				case rl.IsKeyPressed(rl.KeyF2):
					newKind = components.FormationColumn
					keyHit = true
				case rl.IsKeyPressed(rl.KeyF3):
					newKind = components.FormationWedge
					keyHit = true
				case rl.IsKeyPressed(rl.KeyF4):
					newKind = components.FormationLoose
					keyHit = true
				}
				if keyHit && app.World.Alive(commonSquad) {
					if fd := formationDataMap.Get(commonSquad); fd != nil {
						fd.Type = newKind
						fd.Spacing = systems.FormationSpacing(newKind)
					}
				}
			}
		}

		// E -> toggle formation editor floating panel.
		if rl.IsKeyPressed(rl.KeyE) {
			if floating.IsOpen("formation-editor") {
				floating.Close("formation-editor")
			} else {
				floating.Open(&ui.FloatingPanel{
					ID:        "formation-editor",
					Title:     ui.WidgetTitle(ui.PanelFormation),
					Bounds:    rl.Rectangle{X: 220, Y: 80, Width: 360, Height: 400},
					PanelID:   ui.PanelFormation,
					RenderFor: makeFloatingRender,
				})
			}
		}

		// [ / ] cycle MovementProfile presets; ' toggles Posture.
		if len(selected) > 0 {
			if commonSquad, homo := groupSelected(selected, squadMemberMap); homo && commonSquad != (ecs.Entity{}) {
				if app.World.Alive(commonSquad) {
					profile := movementProfileMap.Get(commonSquad)
					if profile != nil {
						switch {
						case rl.IsKeyPressed(rl.KeyLeftBracket):
							*profile = components.ApplyPreset(cyclePreset(detectPreset(*profile), -1))
						case rl.IsKeyPressed(rl.KeyRightBracket):
							*profile = components.ApplyPreset(cyclePreset(detectPreset(*profile), +1))
						case rl.IsKeyPressed(rl.KeyApostrophe):
							if profile.Posture == components.PostureStandard {
								profile.Posture = components.PostureQuiet
							} else {
								profile.Posture = components.PostureStandard
							}
						}
					}
				}
			}
		}

		digitKeys := [5]int32{rl.KeyOne, rl.KeyTwo, rl.KeyThree, rl.KeyFour, rl.KeyFive}
		for i, k := range digitKeys {
			if !rl.IsKeyPressed(k) {
				continue
			}
			if ctrlHeld {
				if commonSquad, homo := groupSelected(selected, squadMemberMap); homo && commonSquad != (ecs.Entity{}) {
					binds[i] = bindEntry{Squad: commonSquad}
				} else {
					cp := make([]ecs.Entity, len(selected))
					copy(cp, selected)
					binds[i] = bindEntry{Units: cp}
				}
			} else {
				b := binds[i]
				switch {
				case b.Squad != (ecs.Entity{}) && app.World.Alive(b.Squad):
					if r := rosterMap.Get(b.Squad); r != nil {
						selected = append(selected[:0], r.Members[:r.Count]...)
					} else {
						selected = nil
					}
				case b.Squad != (ecs.Entity{}):
					binds[i] = bindEntry{}
					selected = nil
				default:
					selected = append(selected[:0], b.Units...)
				}
			}
		}

		if !wasdActive && len(navPath) > 0 {
			navPath = stepAlongPath(anchorPos, navPath, anchorSpeed*float32(dtReal.Seconds()))
		}

		if focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
		}

		// B toggles InteriorOpen on every building; PgUp/PgDn cycles CurrentLevel.
		if focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyB) {
			qbf := buildingFilter.Query()
			for qbf.Next() {
				root := qbf.Entity()
				bvm := buildingViewModeMap.Get(root)
				if bvm == nil {
					continue
				}
				bvm.InteriorOpen = !bvm.InteriorOpen
			}
		}
		if focused == ui.Panel3D && (rl.IsKeyPressed(rl.KeyPageDown) || rl.IsKeyPressed(rl.KeyPageUp)) {
			step := 1
			if rl.IsKeyPressed(rl.KeyPageUp) {
				step = -1
			}
			qbf := buildingFilter.Query()
			for qbf.Next() {
				root := qbf.Entity()
				bvm := buildingViewModeMap.Get(root)
				if bvm == nil {
					continue
				}
				levels := buildingPlanIndex.Levels[root]
				if len(levels) <= 1 {
					continue
				}
				idx := 0
				for i, l := range levels {
					if l == bvm.CurrentLevel {
						idx = i
						break
					}
				}
				idx += step
				if idx < 0 {
					idx = 0
				}
				if idx >= len(levels) {
					idx = len(levels) - 1
				}
				bvm.CurrentLevel = levels[idx]
			}
		}

		if rl.IsKeyPressed(rl.KeyP) {
			if ctrlHeld {
				app.Prof.PrintSnapshot()
				app.Trace.Mark("snapshot")
			} else {
				expandedHUDOn = !expandedHUDOn
			}
		}

		// Hover: closest unit (Panel3D) / closest squad marker (PanelMap).
		hovered = ecs.Entity{}
		switch focused {
		case ui.Panel3D:
			if hit, ok := hoverUnitFromMouse(unitRenderFilter, *anchorPos, panel3DLocal, panel3DW, panel3DH); ok {
				hovered = hit
			}
		case ui.PanelMap:
			mapCtx := ui.MapRenderCtx{
				World: app.World, Cam: mapCam, SquadFilter: squadFilter,
				SquadCenter:    squadCenter,
				MapMarkerCache: &mapMarkerCache,
			}
			hovered = ui.PickSquadAt(cursor, mapCtx, panelMap, 12)
		}

		var (
			ghostTarget   components.WorldPos
			ghostTargetOK bool
		)
		if focused == ui.Panel3D {
			ghostTarget, ghostTargetOK = mouseTargetWorldPos(systems.CurrentCamera,
				anchorPos.ToRenderSpace(systems.CurrentOriginChunk),
				panel3DLocal, panel3DW, panel3DH)
		}

		// Building hover: cursor's ground target inside any Building.Footprint.
		hoveredBuilding = ecs.Entity{}
		hoveredLevel = ecs.Entity{}
		if focused == ui.Panel3D && ghostTargetOK {
			gtX := ghostTarget.Local.X + float32(ghostTarget.Chunk.X)*components.ChunkSize
			gtZ := ghostTarget.Local.Z + float32(ghostTarget.Chunk.Z)*components.ChunkSize
			qbf := buildingFilter.Query()
			for qbf.Next() {
				b := qbf.Get()
				if b.Footprint.Contains(gtX, gtZ) {
					hoveredBuilding = qbf.Entity()
					qbf.Close()
					break
				}
			}
			// Narrow outline to the storey the ray hits; falls back to
			// whole-building if no Level box catches the ray.
			if hoveredBuilding != (ecs.Entity{}) {
				ray := rl.GetScreenToWorldRayEx(panel3DLocal, systems.CurrentCamera, panel3DW, panel3DH)
				if lvl, ok := pickLevelUnderRay(ray, hoveredBuilding, &buildingPlanIndex, levelMap); ok {
					hoveredLevel = lvl
				}
			}
		}

		// Sticky selectedBuilding wins over hovered so cursor can move onto
		// chips without the panel vanishing.
		effectiveBuilding := selectedBuilding
		if effectiveBuilding != (ecs.Entity{}) && !app.World.Alive(effectiveBuilding) {
			selectedBuilding = ecs.Entity{}
			effectiveBuilding = ecs.Entity{}
		}
		if effectiveBuilding == (ecs.Entity{}) {
			effectiveBuilding = hoveredBuilding
		}
		buildingWidget = nil
		if effectiveBuilding != (ecs.Entity{}) {
			if bvm := buildingViewModeMap.Get(effectiveBuilding); bvm != nil {
				if rootPos := posMap.Get(effectiveBuilding); rootPos != nil {
					elev := float32(3)
					if bldg := buildingMap.Get(effectiveBuilding); bldg != nil {
						elev = float32(bldg.Stories)*components.FloorHeight + 1
					}
					above := *rootPos
					above.Local.Y += elev
					rp := above.ToRenderSpace(systems.CurrentOriginChunk)
					w := int32(panel3DContent.Width)
					h := int32(panel3DContent.Height)
					if w > 0 && h > 0 {
						sp := rl.GetWorldToScreenEx(rp, systems.CurrentCamera, w, h)
						if sp.X >= 0 && sp.X <= panel3DContent.Width && sp.Y >= 0 && sp.Y <= panel3DContent.Height {
							screen := rl.Vector2{
								X: panel3DContent.X + sp.X,
								Y: panel3DContent.Y + sp.Y,
							}
							levels := buildingPlanIndex.Levels[effectiveBuilding]
							buildingWidget = ui.ComputeBuildingWidget(
								effectiveBuilding, bvm, levels, screen, levelMap,
							)
						}
					}
				}
			}
		}

		// Gate orbit/wheel by focus; suppress while a floater owns the cursor.
		systems.OrbitInputEnabled = (focused == ui.Panel3D || focused == ui.PanelNone) &&
			!floating.IsBusy(cursor)

		app.Tick(dtReal)

		if headless {
			if aiTest != nil && aiTest.verdictDone {
				break
			}
			continue
		}

		anchorPos = posMap.Get(anchor)
		anchorRender := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)

		rl.BeginTextureMode(scene3DRT.RT)
		rl.ClearBackground(rl.RayWhite)
		rl.BeginMode3D(systems.CurrentCamera)

		chunksActiveLive := 0
		chunksRelLive := 0
		qcA := chunkActiveFilter.Query()
		for qcA.Next() {
			pos, mesh, _ := qcA.Get()
			chunksActiveLive++
			if !mesh.Uploaded {
				continue
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			xform := rl.MatrixTranslate(renderPos.X, renderPos.Y, renderPos.Z)
			rl.DrawMesh(mesh.Mesh, terrainMaterial, xform)
		}
		qcR := chunkRelevantFilter.Query()
		for qcR.Next() {
			pos, mesh, _ := qcR.Get()
			chunksRelLive++
			if !mesh.Uploaded {
				continue
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			xform := rl.MatrixTranslate(renderPos.X, renderPos.Y, renderPos.Z)
			rl.DrawMesh(mesh.Mesh, terrainMaterial, xform)
		}

		rl.DrawCircle3D(anchorRender, 1, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Blue)

		unitsLive := 0
		qu := unitRenderFilter.Query()
		for qu.Next() {
			pos, _, st := qu.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			ent := qu.Entity()
			// Default Rifleman keeps render resilient if a spawn path forgot AssignRole.
			role := components.RoleRifleman
			if r := roleMap.Get(ent); r != nil {
				role = r.Kind
			}
			drawUnitCube(renderPos, *st, role)
			if isSelected(ent) >= 0 {
				height := unitStanceHeight(st.Code)
				rl.DrawCircle3D(renderPos, 1.0, rl.Vector3{X: 1, Y: 0, Z: 0}, 90,
					rl.Color{R: 0, G: 220, B: 220, A: 255})
				c := rl.Vector3{X: renderPos.X, Y: renderPos.Y + height*0.5, Z: renderPos.Z}
				rl.DrawCubeWiresV(c, rl.Vector3{X: 0.7, Y: height + 0.1, Z: 0.7},
					rl.Color{R: 0, G: 220, B: 220, A: 255})
			}
			if hovered == ent {
				height := unitStanceHeight(st.Code)
				c := rl.Vector3{X: renderPos.X, Y: renderPos.Y + height*0.5, Z: renderPos.Z}
				rl.DrawCubeWiresV(c, rl.Vector3{X: 0.8, Y: height + 0.2, Z: 0.8},
					rl.Color{R: 240, G: 240, B: 120, A: 255})
			}
			unitsLive++
		}

		propLive := 0
		bridgeLive := 0
		qp := propFilter.Query()
		for qp.Next() {
			pos, prop := qp.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawProp(propRegistry.Metas[prop.Type], renderPos, prop.Yaw, prop.Scale)
			propLive++
			if prop.Type == components.PropBridge {
				bridgeLive++
			}
		}

		// A Level is hidden when its building has InteriorOpen AND its avgY
		// sits above CurrentLevel's avgY + epsilon.
		hiddenLevels := map[ecs.Entity]bool{}
		qLev := levelCutawayFilter.Query()
		for qLev.Next() {
			lvl, member := qLev.Get()
			bvm := buildingViewModeMap.Get(member.Building)
			if bvm == nil || !bvm.InteriorOpen {
				continue
			}
			if bvm.CurrentLevel == (ecs.Entity{}) {
				continue
			}
			curLev := levelMap.Get(bvm.CurrentLevel)
			if curLev == nil {
				continue
			}
			if lvl.AABB.CenterY() > curLev.AABB.CenterY()+0.1 {
				hiddenLevels[qLev.Entity()] = true
			}
		}

		// A level is fogged when never discovered OR last seen >FogVisibleDuration ago.
		now := levelVisSys.Clock()
		levelFogged := func(level ecs.Entity) bool {
			if level == (ecs.Entity{}) {
				return false
			}
			vis := levelVisReadMap.Get(level)
			if vis == nil {
				return false
			}
			if !vis.Discovered {
				return true
			}
			return now-vis.LastSeenAt > components.FogVisibleDuration
		}

		floorLive := 0
		qf := floorRenderFilter.Query()
		for qf.Next() {
			pos, fl := qf.Get()
			fogged := false
			if lm := levelMemberMap.Get(qf.Entity()); lm != nil {
				if hiddenLevels[lm.Level] {
					continue
				}
				fogged = levelFogged(lm.Level)
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingFloor(renderPos, *fl, fogged)
			floorLive++
		}
		wallLive := 0
		qw := wallRenderFilter.Query()
		for qw.Next() {
			pos, ws := qw.Get()
			e := qw.Entity()
			mode := components.WallRenderAll
			var outward rl.Vector3
			fogged := false
			if lm := levelMemberMap.Get(e); lm != nil {
				if hiddenLevels[lm.Level] {
					continue
				}
				fogged = levelFogged(lm.Level)
				// WallMode only applies to the currently-viewed level inside an open cutaway.
				if member := buildingMemberMap.Get(e); member != nil {
					if bvm := buildingViewModeMap.Get(member.Building); bvm != nil &&
						bvm.InteriorOpen && lm.Level == bvm.CurrentLevel {
						mode = bvm.WallMode
					}
				}
				if mode == components.WallRenderCameraFacing {
					if cd := coverDirReadMap.Get(e); cd != nil {
						outward = cd.Dir
					}
				}
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingWall(renderPos, *ws, mode, outward, fogged)
			wallLive++
		}
		qst := stairsRenderFilter.Query()
		for qst.Next() {
			pos, st := qst.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingStairs(renderPos, *st)
		}

		// Outline boxes around hovered + selected buildings. 0.15 m pad keeps
		// the wireframe legible against wall surfaces.
		drawBuildingOutline := func(root ecs.Entity, color rl.Color) {
			if root == (ecs.Entity{}) || !app.World.Alive(root) {
				return
			}
			bldg := buildingMap.Get(root)
			rootPos := posMap.Get(root)
			if bldg == nil || rootPos == nil {
				return
			}
			const pad float32 = 0.15
			height := float32(bldg.Stories) * components.FloorHeight
			if height < 1 {
				height = components.FloorHeight
			}
			sizeX := bldg.Footprint.SizeX() + 2*pad
			sizeZ := bldg.Footprint.SizeZ() + 2*pad
			center := *rootPos
			center.Local.Y += height * 0.5
			rp := center.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCubeWires(rp, sizeX, height, sizeZ, color)
		}
		if hoveredBuilding != (ecs.Entity{}) && hoveredBuilding != selectedBuilding {
			yellow := rl.Color{R: 255, G: 220, B: 60, A: 200}
			if hoveredLevel != (ecs.Entity{}) {
				if lvl := levelMap.Get(hoveredLevel); lvl != nil {
					drawLevelOutline(lvl, yellow)
				} else {
					drawBuildingOutline(hoveredBuilding, yellow)
				}
			} else {
				drawBuildingOutline(hoveredBuilding, yellow)
			}
		}
		if selectedBuilding != (ecs.Entity{}) {
			drawBuildingOutline(selectedBuilding, rl.Color{R: 90, G: 200, B: 240, A: 230})
		}

		// Hold-G also toggles the map's road / river / building debug layer.
		if rl.IsKeyDown(rl.KeyG) {
			drawRoadGraphDebug(&roadGraph)
			showMapDebugLy = true
		} else {
			showMapDebugLy = false
		}
		if debugOverlay.NavGrid {
			qNav := navOverlayFilter.Query()
			for qNav.Next() {
				pos, cc, grid, hm := qNav.Get()
				if !debugChunkInRadius(*cc, systems.CurrentOriginChunk) {
					continue
				}
				drawNavGridOverlay(*pos, *cc, grid, hm)
			}
		}
		if debugOverlay.CoverMap {
			qCov := coverOverlayFilter.Query()
			for qCov.Next() {
				pos, cc, cov, hm := qCov.Get()
				if !debugChunkInRadius(*cc, systems.CurrentOriginChunk) {
					continue
				}
				drawCoverMapOverlay(*pos, *cc, cov, hm)
			}
		}

		visionPairs := 0
		if debugOverlay.Vision {
			qV := visionAwareFilter.Query()
			for qV.Next() {
				pos, aware := qV.Get()
				if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
					continue
				}
				from := pos.ToRenderSpace(systems.CurrentOriginChunk)
				from.Y += 1.0
				for i := range aware.LastSeen {
					if aware.LastSeen[i].Time == 0 {
						continue
					}
					to := aware.LastSeen[i].Pos.ToRenderSpace(systems.CurrentOriginChunk)
					to.Y += 1.0
					rl.DrawLine3D(from, to, rl.Green)
					visionPairs++
				}
			}
		} else {
			qV := visionAwareFilter.Query()
			for qV.Next() {
				_, aware := qV.Get()
				for i := range aware.LastSeen {
					if aware.LastSeen[i].Time != 0 {
						visionPairs++
					}
				}
			}
		}

		squadsLive := 0
		squadMembersLive := 0
		drawAllSquads := rl.IsKeyDown(rl.KeyK)
		selectedSquad, selectedHomo := groupSelected(selected, squadMemberMap)
		qSq := squadFilter.Query()
		for qSq.Next() {
			_, roster := qSq.Get()
			squadsLive++
			squadMembersLive += int(roster.Count)
			squadEnt := qSq.Entity()
			isSelectedSquad := selectedHomo && selectedSquad != (ecs.Entity{}) && selectedSquad == squadEnt
			if !drawAllSquads && !isSelectedSquad {
				continue
			}
			centerWP, ok := systems.SquadCenter(app.World, roster, posMap)
			if !ok {
				continue
			}
			centerRender := centerWP.ToRenderSpace(systems.CurrentOriginChunk)
			centerRender.Y += 0.2
			memberPos := make([]rl.Vector3, 0, roster.Count)
			for i := uint8(0); i < roster.Count; i++ {
				mem := roster.Members[i]
				if mem == (ecs.Entity{}) || !app.World.Alive(mem) {
					continue
				}
				if p := posMap.Get(mem); p != nil {
					r := p.ToRenderSpace(systems.CurrentOriginChunk)
					r.Y += 0.2
					memberPos = append(memberPos, r)
				}
			}
			drawSquadConnections(centerRender, memberPos, squadColor(squadEnt))
		}

		if debugOverlay.LevelNavGrid {
			qFloor := floorNavFilter.Query()
			for qFloor.Next() {
				pos, _, grid := qFloor.Get()
				if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
					continue
				}
				drawFloorNavOverlay(*pos, grid)
			}
		}

		// Surface<->Level edges = green, Level<->Level = yellow. Missing
		// lines through a door/stair ⇒ bake failed to resolve LevelMember.
		if debugOverlay.Transitions {
			levelGridReadMap := ecs.NewMap[components.LevelNavGrid](app.World)
			nodeWorld := func(n components.NavNode) (rl.Vector3, bool) {
				switch n.Kind {
				case components.NodeSurface:
					wx := float32(n.Chunk.X)*components.ChunkSize + float32(n.I) + 0.5
					wz := float32(n.Chunk.Z)*components.ChunkSize + float32(n.J) + 0.5
					wp := components.WorldPos{}.Add(rl.Vector3{
						X: wx, Y: systems.GroundHeight(wx, wz) + 0.5, Z: wz,
					})
					return wp.ToRenderSpace(systems.CurrentOriginChunk), true
				case components.NodeLevel:
					rootPos := posMap.Get(n.Level)
					ng := levelGridReadMap.Get(n.Level)
					if rootPos == nil || ng == nil {
						return rl.Vector3{}, false
					}
					rChunkBaseX := float32(rootPos.Chunk.X) * components.ChunkSize
					rChunkBaseZ := float32(rootPos.Chunk.Z) * components.ChunkSize
					cx := rChunkBaseX + ng.Origin.X + float32(n.I) + 0.5
					cz := rChunkBaseZ + ng.Origin.Z + float32(n.J) + 0.5
					wp := components.WorldPos{}.Add(rl.Vector3{X: cx, Y: ng.Origin.Y + 0.5, Z: cz})
					return wp.ToRenderSpace(systems.CurrentOriginChunk), true
				}
				return rl.Vector3{}, false
			}
			for _, edges := range transitionRegistry.Out {
				for _, e := range edges {
					a, ok1 := nodeWorld(e.From)
					b, ok2 := nodeWorld(e.To)
					if !ok1 || !ok2 {
						continue
					}
					col := rl.Color{R: 50, G: 220, B: 80, A: 255}
					if e.From.Kind == components.NodeLevel && e.To.Kind == components.NodeLevel {
						col = rl.Color{R: 240, G: 220, B: 60, A: 255}
					}
					rl.DrawLine3D(a, b, col)
				}
			}
		}

		coverSlotLive := 0
		if debugOverlay.CoverSlots {
			qSlot := coverSlotFilter.Query()
			for qSlot.Next() {
				pos, slot := qSlot.Get()
				if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
					continue
				}
				render := pos.ToRenderSpace(systems.CurrentOriginChunk)
				rl.DrawCubeV(render, rl.Vector3{X: 0.25, Y: 0.25, Z: 0.25}, rl.Yellow)
				tip := rl.Vector3{
					X: render.X + slot.OriginDir.X*1.0,
					Y: render.Y,
					Z: render.Z + slot.OriginDir.Z*1.0,
				}
				rl.DrawLine3D(render, tip, rl.Magenta)
				coverSlotLive++
			}
		} else {
			qSlot := coverSlotFilter.Query()
			for qSlot.Next() {
				qSlot.Get()
				coverSlotLive++
			}
		}

		drawNavPath(navPath, *anchorPos)

		if debugOverlay.UnitPaths {
			drawUnitPaths(unitPathRenderCtx{
				filter:         unitPathSquadFilter,
				soloFilter:     unitPathSoloFilter,
				squadMemberMap: squadMemberMap,
				selectedSquad:  unitPathsSelectedSquad(selected, squadMemberMap),
			})
		}

		// Ghost preview rotates live during facing-drag so the orientation
		// matches what release will commit to.
		var ghostDragFacing *float32
		if rmbState.Active && rmbState.FacingActive {
			dx := cursor.X - rmbState.PressOrigin.X
			dy := cursor.Y - rmbState.PressOrigin.Y
			yaw := float32(math.Atan2(float64(dx), float64(-dy)))
			ghostDragFacing = &yaw
			ghostTarget = rmbState.PressTarget
			ghostTargetOK = true
		}
		// Popup-hover swaps ghost placement per kind; anchor at press-time target.
		var ghostPopupKind *components.OrderKindCode
		var ghostPopupLevel ecs.Entity
		if ctxMenu.IsActive() {
			if item, ok := ctxMenu.HoveredItemDetails(); ok {
				k := item.Kind
				ghostPopupKind = &k
				ghostPopupLevel = item.LevelEntity
			}
			ghostTarget = rmbState.PressTarget
			ghostTargetOK = true
		}
		drawSelectionGhost(ghostCtx, selected, focused == ui.Panel3D, ghostTarget, ghostTargetOK,
			ghostDragFacing, ghostPopupKind, ghostPopupLevel, levelMap)

		drawOrderMarkers3D(orderMarkerRenderCtx, selected)

		drawParticles(particleRenderCtx, float32(app.Elapsed().Seconds()))

		rl.EndMode3D()
		rl.EndTextureMode()

		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 8, G: 10, B: 14, A: 255})

		// Panel content drawn before chrome so title bars overlay cleanly.
		mapCtx := ui.MapRenderCtx{
			World:            app.World,
			Cam:              mapCam,
			Underlay:         &underlay,
			AnchorPos:        *anchorPos,
			Selected:         selected,
			Hovered:          hovered,
			PosMap:           posMap,
			RosterMap:        rosterMap,
			SquadMemberMap:   squadMemberMap,
			SquadFilter:      squadFilter,
			SquadCenter:      squadCenter,
			SquadColor:       squadColor,
			RoadGraph:        &roadGraph,
			Rivers:           &rivers,
			Buildings:        &buildingPlans,
			ShowDebugLayers:  showMapDebugLy,
			OrderQueueMap:    orderQueueMap,
			OrderKindMap:     orderKindMap,
			OrderTargetMap:   orderTargetMap,
			OrderChainMap:    orderChainMap,
			SmoothedSquadPos: smoothedSquadPos,
			MapMarkerCache:   &mapMarkerCache,
			RoleMap:          roleMap,
			Font:             hudFont,
			MapPingFilter:    mapPingFilter,
			Clock:            squadService.Clock(),
		}
		ui.DrawMap(panelMap, mapCtx)

		inspectorFocused := panelMgr.FocusedAt(cursor) == ui.PanelInspect
		inspectorPanel := panelMgr.Get(ui.PanelInspect)
		inspectorScroll := panelMgr.ScrollByID(ui.PanelInspect)
		ui.DrawInspector(inspectorPanel, ui.InspectorCtx{
			InspectorMaps: inspectorMaps,
			World:         app.World,
			Selected:      selected,
			Hovered:       hovered,
			Font:          hudFont,
			EventLog:      eventLog,
			Cursor:        cursor,
			LMBPressed:    !chromeBusy() && !scrollDragging && rl.IsMouseButtonPressed(rl.MouseButtonLeft),
			PanelFocused:  inspectorFocused,
			Scroll:        inspectorScroll,
			SquadColor:    squadColor,
		})
		// Scrollbar overlay drawn AFTER DrawInspector so EndScissorMode has released its clip.
		if inspectorScroll != nil {
			ui.ClampScrollOffset(inspectorPanel, inspectorScroll)
			ui.DrawScrollbar(inspectorPanel, inspectorScroll)
		}

		topBarPlayPause, topBarSpeedDown, topBarSpeedUp = ui.DrawTopBar(
			panelMgr.Get(ui.PanelTopBar), hudFont, ui.TimeDisplay{
				Scale:   app.TimeScale,
				Elapsed: float32(app.Elapsed().Seconds()),
			})

		timelineData = buildTimelineData(app.World, squadFilter, posMap, factionMap,
			orderQueueMap, orderChainMap, orderKindMap, orderTargetMap, orderStateMap,
			orderProgressMap, orderIssuedAtMap, squadColor,
			float32(app.Elapsed().Seconds()))
		ui.DrawTimelinePanel(panelMgr.Get(ui.PanelTimeline), hudFont, timelineData, &timelineView)
		if timelineHoverOK && timelineHoverHit.HitOrder {
			ui.DrawTimelineTooltip(hudFont, cursor, timelineHoverBlk)
		}

		if leaf := panelMgr.LeafFor(ui.PanelFormation); leaf != nil {
			formationLMB := !chromeBusy() && !scrollDragging &&
				panelMgr.FocusedAt(cursor) == ui.PanelFormation &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft)
			formationEditor.DrawPanel(panelMgr.Get(ui.PanelFormation),
				hudFont, cursor, formationLMB)
		}

		if leaf := panelMgr.LeafFor(ui.PanelDebug); leaf != nil {
			debugLMB := !chromeBusy() && !scrollDragging &&
				panelMgr.FocusedAt(cursor) == ui.PanelDebug &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft)
			ui.DrawDebugPanel(panelMgr.Get(ui.PanelDebug),
				hudFont, debugOverlayToggles(&debugOverlay), cursor, debugLMB,
				"Radius: 2 chunks around camera")
		}

		scene3DRT.Composite(panel3D)

		// Role labels are 2D screen-projected after RT composite; scissored
		// to Panel3D so they don't bleed onto neighbouring panels.
		rl.BeginScissorMode(int32(panel3DContent.X), int32(panel3DContent.Y),
			int32(panel3DContent.Width), int32(panel3DContent.Height))
		quLabels := unitRenderFilter.Query()
		for quLabels.Next() {
			pos, _, st := quLabels.Get()
			ent := quLabels.Entity()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			role := components.RoleRifleman
			if r := roleMap.Get(ent); r != nil {
				role = r.Kind
			}
			drawUnitRoleLabel(renderPos, *st, role, hudFont, panel3DContent)
			if stam := staminaMap.Get(ent); stam != nil {
				drawUnitStaminaBar(renderPos, *st, role, stam.Current, stam.MaxLevel, panel3DContent)
			}
			if hp := hpMap.Get(ent); hp != nil {
				drawUnitHPBar(renderPos, *st, role, hp.Current, hp.Max, panel3DContent)
			}
		}
		rl.EndScissorMode()

		if buildingWidget != nil {
			rl.BeginScissorMode(int32(panel3DContent.X), int32(panel3DContent.Y),
				int32(panel3DContent.Width), int32(panel3DContent.Height))
			ui.DrawBuildingWidget(buildingWidget, hudFont)
			rl.EndScissorMode()
		}

		// Marquee drawn after composite, scissored to Panel3D.
		if marqueeActive && marqueeOrigin == ui.Panel3D {
			end := cursor
			minX, maxX := marqueeStart.X, end.X
			if maxX < minX {
				minX, maxX = maxX, minX
			}
			minY, maxY := marqueeStart.Y, end.Y
			if maxY < minY {
				minY, maxY = maxY, minY
			}
			rl.BeginScissorMode(int32(panel3D.Bounds.X), int32(panel3D.Bounds.Y),
				int32(panel3D.Bounds.Width), int32(panel3D.Bounds.Height))
			rl.DrawRectangleLines(int32(minX), int32(minY),
				int32(maxX-minX), int32(maxY-minY),
				rl.Color{R: 0, G: 220, B: 220, A: 255})
			rl.DrawRectangle(int32(minX), int32(minY),
				int32(maxX-minX), int32(maxY-minY),
				rl.Color{R: 0, G: 220, B: 220, A: 40})
			rl.EndScissorMode()
		}

		// Chrome drawn last so it overlays content (incl. marquee strokes
		// that bleed onto title bars). PanelTopBar draws its own chrome.
		panelMgr.Workspace.WalkLeaves(func(l *ui.LayoutNode) {
			ui.DrawChrome(ui.Panel{ID: l.Panel, Bounds: l.Bounds, Title: l.Title}, hudFont, 16)
		})

		ui.DrawCornerHandles(panelMgr, cursor)
		if panelMgr.IsCornerDragging() {
			ui.DrawCornerDragPreview(panelMgr, cursor)
		}

		chevronMenu.Draw(hudFont, cursor)

		floating.DrawAll(hudFont, cursor, rl.IsMouseButtonPressed(rl.MouseButtonLeft))
		floating.DrawSwitchMenu(hudFont, cursor)

		ctxMenu.Draw(hudFont, cursor)

		const heapInterval = time.Second
		if app.Prof.HeapStale(app.Elapsed(), heapInterval) {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			app.Prof.SetHeap(ms.HeapAlloc, app.Elapsed())
		}
		if app.Prof.EntityCountStale(app.Elapsed(), heapInterval) {
			st := app.World.Stats()
			app.Prof.SetEntityCount(st.Entities.Used, app.Elapsed())
		}

		totalUnits := countFilter1(unitFilter)
		soloists := totalUnits - squadMembersLive
		if soloists < 0 {
			soloists = 0
		}
		cen := census{
			chunksActive: chunksActiveLive,
			chunksRel:    chunksRelLive,
			chunksTotal:  countFilter1(chunkAllFilter),
			navChunks:    countFilter1(navGridChunkFilter),
			props:        propLive,
			bridgesLive:  bridgeLive,
			walls:        wallLive,
			floors:       floorLive,
			stairs:       countFilter1(stairsCountFilter),
			coverSlots:   coverSlotLive,
			units:        totalUnits,
			weapons:      countFilter1(weaponFilter),
			squads:       squadsLive,
			squadMembers: squadMembersLive,
			soloists:     soloists,
			transitions:  transitionEdgeCount(&transitionRegistry),
			visionPairs:  visionPairs,
			selection:    len(selected),
			pathWaypts:   len(navPath),
			roadNodes:    len(roadGraph.Nodes),
			roadEdges:    len(roadGraph.Edges),
			bridgeEdges:  bridgeEdges,
			rivers:       len(rivers.Polylines),
			bldgPlans:    len(buildingPlans.Plans),
			trenches:     len(trenches.Lines),
		}

		drawCollapsedProfHUD(&app.Prof, screenW, hudFont)
		if expandedHUDOn {
			drawExpandedProfHUD(&app.Prof, screenW, cen, hudFont)
		}

		rl.EndDrawing()

		recordTraceFrame(app, rl.GetFrameTime()*1000, rl.GetFPS(), cen)
		handleTraceHotkeys(app)
	}
}

// containsVehicle is a placeholder — always false until the Vehicle
// component ships. Swap the body for a real Vehicle-map.Has loop then.
func containsVehicle(units []ecs.Entity, world *ecs.World) bool {
	_ = world
	_ = units
	return false
}

// applyPreservedSlots snapshots each rostered member's pre-merge position
// into FormationCustomSlots (north-relative: X = world +X, Y = world +Z),
// then sets OrientNorth. Slot 0 (commander) is the anchor.
func applyPreservedSlots(
	squad ecs.Entity,
	roster *components.CommandRoster,
	snapshots map[ecs.Entity]components.WorldPos,
	customSlotsMap *ecs.Map[components.FormationCustomSlots],
	orientMap *ecs.Map[components.FormationOrientation],
) {
	if roster == nil || roster.Count == 0 {
		return
	}
	commander := roster.Members[0]
	center, ok := snapshots[commander]
	if !ok {
		return
	}
	var cs components.FormationCustomSlots
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		snap, ok := snapshots[mem]
		if !ok {
			continue
		}
		diff := snap.Sub(center)
		cs.Slots[i] = rl.Vector2{X: diff.X, Y: diff.Z}
	}
	if customSlotsMap.Has(squad) {
		*customSlotsMap.Get(squad) = cs
	} else {
		customSlotsMap.Add(squad, &cs)
	}
	if orientMap.Has(squad) {
		orientMap.Get(squad).Mode = components.OrientNorth
	} else {
		orientMap.Add(squad, &components.FormationOrientation{Mode: components.OrientNorth})
	}
}

// chevronLeafAt returns the workspace leaf whose chevron button is under the cursor.
func chevronLeafAt(panelMgr *ui.PanelManager, cursor rl.Vector2) *ui.LayoutNode {
	if panelMgr == nil || panelMgr.Workspace == nil {
		return nil
	}
	var hit *ui.LayoutNode
	panelMgr.Workspace.WalkLeaves(func(l *ui.LayoutNode) {
		if hit != nil {
			return
		}
		ch := ui.ChevronRect(ui.Panel{Bounds: l.Bounds})
		if cursor.X >= ch.X && cursor.X < ch.X+ch.Width &&
			cursor.Y >= ch.Y && cursor.Y < ch.Y+ch.Height {
			hit = l
		}
	})
	return hit
}

// handleMenuItem dispatches a chevron-menu selection: switch widget, close
// (merge with sibling), or detach into a floating panel via floatSpawn.
func handleMenuItem(panelMgr *ui.PanelManager, menu *ui.ChevronMenu, it ui.MenuItem,
	floatSpawn func(id ui.PanelID, title string, bounds rl.Rectangle)) {
	leaf := menu.Leaf
	if leaf == nil {
		return
	}
	switch it.Kind {
	case ui.MenuItemSwitch:
		if it.Target == leaf.Panel {
			return
		}
		// If the target widget is already shown, swap contents so each
		// PanelID appears at most once.
		if other := panelMgr.Workspace.FindLeaf(it.Target); other != nil {
			ui.SwapPanels(leaf, other)
		} else {
			leaf.Panel = it.Target
			leaf.Title = ui.WidgetTitle(it.Target)
		}
	case ui.MenuItemClose:
		if leaf.Parent == nil {
			return
		}
		wasRootChild := leaf.Parent == panelMgr.Workspace
		sib := ui.MergeIntoSibling(leaf)
		if wasRootChild && sib != nil {
			panelMgr.SetWorkspace(sib)
		}
	case ui.MenuItemFloat:
		if leaf.Parent == nil || floatSpawn == nil {
			return
		}
		// Snapshot leaf state before tearing it out of the tree.
		id := leaf.Panel
		title := leaf.Title
		bounds := leaf.Bounds
		wasRootChild := leaf.Parent == panelMgr.Workspace
		sib := ui.MergeIntoSibling(leaf)
		if wasRootChild && sib != nil {
			panelMgr.SetWorkspace(sib)
		}
		floatSpawn(id, title, bounds)
	}
	panelMgr.Recompute(int32(rl.GetScreenWidth()), int32(rl.GetScreenHeight()))
}

// nextTimeScale cycles 1 -> 2 -> 4 -> 8 -> 1 (step=+1) / reverse (step=-1).
// Paused → step=+1 jumps to 1x, step=-1 to 8x.
func nextTimeScale(cur float32, step int) float32 {
	stops := [...]float32{1, 2, 4, 8}
	if cur <= 0 {
		if step > 0 {
			return stops[0]
		}
		return stops[len(stops)-1]
	}
	idx := 0
	for i, v := range stops {
		if v == cur {
			idx = i
			break
		}
	}
	idx = (idx + step + len(stops)) % len(stops)
	return stops[idx]
}

// buildTimelineData flattens all squads + their order queues for
// ui.DrawTimelinePanel. Queued blocks stack right after the head's estimated
// end so the timeline reads left-to-right.
func buildTimelineData(
	world *ecs.World,
	squadFilter *ecs.Filter2[components.Squad, components.CommandRoster],
	posMap *ecs.Map[components.WorldPos],
	factionMap *ecs.Map[components.Faction],
	orderQueueMap *ecs.Map[components.OrderQueueHead],
	orderChainMap *ecs.Map[components.OrderChain],
	orderKindMap *ecs.Map[components.OrderKind],
	orderTargetMap *ecs.Map[components.OrderTarget],
	orderStateMap *ecs.Map[components.OrderState],
	orderProgressMap *ecs.Map[components.OrderProgress],
	orderIssuedAtMap *ecs.Map[components.OrderIssuedAt],
	squadColor func(ent ecs.Entity) rl.Color,
	nowT float32,
) ui.TimelineData {
	data := ui.TimelineData{NowT: nowT}
	q := squadFilter.Query()
	for q.Next() {
		squad := q.Entity()
		_, roster := q.Get()
		center, _ := systems.SquadCenter(world, roster, posMap)

		head := orderQueueMap.Get(squad)
		row := ui.TimelineSquadRow{
			Squad: squad,
			Color: squadColor(squad),
		}
		if head != nil && head.First != (ecs.Entity{}) {
			lastEnd := float32(0)
			cur := head.First
			isHead := true
			for cur != (ecs.Entity{}) && world.Alive(cur) {
				kind := orderKindMap.Get(cur)
				state := orderStateMap.Get(cur)
				target := orderTargetMap.Get(cur)
				if kind == nil || state == nil || target == nil {
					break
				}
				startT := nowT
				if iss := orderIssuedAtMap.Get(cur); iss != nil {
					startT = iss.Time
				}
				if !isHead && startT < lastEnd {
					startT = lastEnd
				}
				est := estimateOrderDuration(kind.Code, center, target.Pos)
				endT := startT + est
				prog := float32(0)
				if pr := orderProgressMap.Get(cur); pr != nil {
					prog = pr.Value
				}
				row.Orders = append(row.Orders, ui.TimelineOrderBlock{
					Order:     cur,
					KindCode:  kind.Code,
					StateCode: state.Code,
					StartT:    startT,
					EndT:      endT,
					Progress:  prog,
					IsHead:    isHead,
				})
				lastEnd = endT
				ch := orderChainMap.Get(cur)
				if ch == nil {
					break
				}
				cur = ch.Next
				isHead = false
			}
		}
		data.Rows = append(data.Rows, row)
	}
	q.Close()
	_ = factionMap
	return data
}

// estimateOrderDuration is a heuristic display-only duration per order kind.
func estimateOrderDuration(kind components.OrderKindCode, from, to components.WorldPos) float32 {
	var moveTime float32
	switch kind {
	case components.OrderKindMoveTo, components.OrderKindGarrison,
		components.OrderKindOccupyBuilding, components.OrderKindClearBuilding,
		components.OrderKindOccupyTrench:
		moveTime = components.Distance(from, to) / ui.TimelineMoveSpeedMps
	}
	var est float32
	switch kind {
	case components.OrderKindMoveTo:
		est = moveTime
	case components.OrderKindGarrison, components.OrderKindOccupyBuilding, components.OrderKindClearBuilding:
		est = moveTime + ui.TimelineGarrisonDurationSec
	case components.OrderKindOccupyTrench:
		est = moveTime + ui.TimelineDefendDurationSec
	case components.OrderKindDefendPosition:
		est = ui.TimelineDefendDurationSec
	case components.OrderKindPatrol:
		est = ui.TimelinePatrolDurationSec
	case components.OrderKindAttackTarget:
		est = ui.TimelineAttackDurationSec
	case components.OrderKindSuppressFire:
		est = ui.TimelineSuppressDurationSec
	default:
		est = ui.TimelineUnknownDurationSec
	}
	if est < ui.TimelineMinBlockDurationSec {
		est = ui.TimelineMinBlockDurationSec
	}
	return est
}
