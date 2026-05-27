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
// Phase 11.5 M11.5.1; pool feeds the parallel hot-path systems below.
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

	rl.SetTargetFPS(60)

	// Phase 14.5 cleanup: audio scaffolding (placeholder square-wave +
	// SpatialAudioSystem) removed from main; the engine-tone test sound
	// and voice limiter served as a Phase 7 placeholder for vehicle/unit
	// sound. Real audio (footsteps, gunfire, voices) lands in Phase 25
	// polish - reintroduce wiring here when the audio asset pipeline
	// exists.

	app := core.NewApp()

	initTrace(app)
	defer func() { _ = app.Trace.Close() }()

	// Worker pool feeds the parallel hot-path systems (UnitMovement / Vision /
	// Formation / SquadMacroPath). Stop on shutdown so the worker goroutines
	// don't outlive main.
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
	// Phase 18 formation presets - shared across squads via the formation
	// editor "Save current" / "Apply preset" buttons.
	formationPresets := components.FormationPresets{}
	ecs.AddResource(app.World, &formationPresets)
	// Phase 14.5 M14.5.4 - VisualEvents resource replaced by ECS-entity
	// particles. Spawn handles + ParticleSystem registered below.
	// Phase 14.5 M14.5.2: SpatialHash for Unit XZ positions. Rebuilt every
	// tick (serial pass) before UnitMovement so this frame's separation
	// steering sees fresh positions. Consumed by UnitMovement.separation,
	// WeaponSystem.resolveShot (unit-vs-ray), WeaponSystem.propagateSuppression,
	// and VisionSystem.processVisionSeer (M14.5.3).
	unitSpatialHash := core.NewSpatialHash(32.0)
	ecs.AddResource(app.World, unitSpatialHash)

	// Phase 15 M15.C.2 - global event log. Push targets are SurvivalInstinct
	// (SuppressionStart), DamageService (KIA), OrderResolverSystem
	// (OrderCompleted / OrderFailed). Readers: Inspector squad view.
	eventLog := components.NewEventLog()
	ecs.AddResource(app.World, eventLog)

	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	stamper := systems.NewStamper(app.World)
	navService := systems.NewNavService(app.World)
	squadService := systems.NewSquadService(app.World)
	// Phase 14 M14.1/M14.2: damage service handles HP decrement and the
	// death-despawn path; WeaponSystem hands every applied hit through it.
	// Constructed before WeaponSystem.InitUI so the handle is live by then.
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

	// Phase 14.5 M14.5.2: SpatialHash rebuild runs before UnitMovement so
	// this tick's separation steering reads fresh positions.
	spatialHashRebuildSys := systems.NewSpatialHashRebuildSystem()
	spatialHashRebuildSys.InitUI(app.World)

	unitMovementSys := systems.NewUnitMovementSystem(workerPool)
	unitMovementSys.InitUI(app.World)

	visionSys := systems.NewVisionSystem(workerPool)
	visionSys.InitUI(app.World)

	// Phase 14.5 M14.5.4: particle spawn handles + ParticleSystem. Handles
	// built before WeaponSystem so its constructor can take a non-nil ref.
	particleHandles := systems.NewSpawnHandles(app.World)
	particleSys := systems.NewParticleSystem()
	particleSys.InitUI(app.World)

	// Phase 14 M14.2: WeaponSystem runs after Vision so it sees the freshest
	// Awareness FIFO entries each tick.
	weaponSys := systems.NewWeaponSystem(workerPool, damageService, particleHandles)
	weaponSys.InitUI(app.World)

	// Phase 17 M17.0.1 - per-unit Threat aggregator. Runs after WeaponSystem
	// (which mutates Threat.Suppression / ThreatDir) so SurvivalInstinct /
	// future StanceController read a recomputed Total + State.
	threatSys := systems.NewThreatSystem()
	threatSys.InitUI(app.World)

	// Phase 17 M17.C - autonomous stance controller. Reads Threat.State after
	// threatSys recomputes it, maps to Prone / Crouch / standing default, with
	// animation lock + player-override gate.
	stanceSys := systems.NewStanceControllerSystem()
	stanceSys.InitUI(app.World)

	// Phase 17.8 M17.8.2 — Utility AI evaluator. Picks per-unit ActionMode
	// (Following / Engaging / TakingCover / Repositioning / Reloading /
	// Suppressed) every ~0.5s with hysteresis. Reads Threat / Awareness /
	// Equipment / OrderQueue, writes LocalBlackboard.CurrentMode + Reason.
	// Other executor systems (M17.8.3) will gate behavior on CurrentMode.
	utilityEvalSys := systems.NewUtilityEvaluatorSystem()
	utilityEvalSys.InitUI(app.World)

	// Cleanup of expired ThreatSource entities. SurvivalInstinct reads
	// Threat.Suppression (a faster signal); ThreatSource entities will become
	// the primary input once M15.A.1 ScatterProtocol consumes the cluster.
	threatDecaySys := systems.NewThreatDecaySystem()
	threatDecaySys.InitUI(app.World)

	// Phase 15 M15.C.3 - despawn expired MapPing entities (KIA rings, etc.).
	mapPingDecaySys := systems.NewMapPingDecaySystem()
	mapPingDecaySys.InitUI(app.World)

	// Phase 16.C.2 - per-Level fog-of-war tracking. Marks LevelVisibility
	// when an anchor / unit enters the level bbox; renderer fades unseen
	// levels.
	levelVisSys := systems.NewLevelVisibilitySystem()
	levelVisSys.InitUI(app.World)

	// Phase 15 M15.A.0 - reactive cover seek. Runs after WeaponSystem (fresh
	// Threat.Suppression) and before FormationSystem (so override-driven
	// ActionQueue writes survive the formation pass).
	survivalSys := systems.NewSurvivalInstinctSystem()
	survivalSys.InitUI(app.World)

	orderResolverSys := systems.NewOrderResolverSystem(squadService)
	orderResolverSys.InitUI(app.World)

	squadMacroPathSys := systems.NewSquadMacroPathSystem(navService, workerPool)
	squadMacroPathSys.InitUI(app.World)

	formationSys := systems.NewFormationSystem(squadService, workerPool)
	formationSys.InitUI(app.World)

	// Phase 17 M17.A - per-unit waypoint planner. Runs after FormationSystem
	// (the writer of ActionQueue.Head.Target + MicroPath.Dirty) so the next
	// tick's UnitMovement reads a fresh waypoint stream. Serial - NavService
	// holds Filter handles and is not concurrent-safe.
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

		// Phase 16.B.0: Level entities live for the building's whole life,
		// independent of chunk lifecycle. They are AlwaysActive so a child
		// (Furniture / Marker / LevelTransition) spawned in any chunk can
		// reference a Level by stable entity handle.
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

		// Phase 16.C.0: BuildingViewMode is the cutaway state for this
		// building. Defaults: closed, ground-level selected (spec sorts
		// levels by storey ascending, so levels[0] is the lowest).
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

	// Phase 11: one TrenchRoot entity per polyline so the hit-test resolver
	// can return an ecs.Entity in OrderTarget.Entity for OccupyTrench. The
	// Trench polyline data stays in the TrenchNetwork resource - TrenchRoot
	// is a thin reverse-index. Pos is the polyline midpoint, used for any
	// "where is this trench" preview before order resolution.
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

	// Phase 17.5 refactor: unit spawn boilerplate hidden behind
	// entities.UnitFactory. Read handles for components the Inspector / input
	// layer touches are still reachable through factory fields (StanceMap,
	// MotionMap, ThreatMap, ActionQueueMap ...).
	unitFactoryRef := entities.NewUnitFactory(app.World, posMap)
	actionQueueMap := unitFactoryRef.ActionQueueMap

	// Inspector reads ~30 component maps. NewInspectorMaps pre-builds them
	// in one call so the per-frame InspectorCtx literal stays small.
	inspectorMaps := ui.NewInspectorMaps(app.World)
	// Phase 12: RoleService owns weapon / radio / medkit / spade lifecycle so
	// the Unit-side spawn block stays narrow. main.go just reads the maps
	// (Inspector + render).
	weaponMap := ecs.NewMap[components.Weapon](app.World)
	_ = weaponMap // referenced by render-time stats display; keep handle live.
	// roleMap is read by 3D render (cap colour + label) and the Inspector
	// (role-tinted roster rows) + map commander icon. Phase 12 only reads;
	// RoleService is the canonical writer.
	roleMap := ecs.NewMap[components.UnitRole](app.World)
	squadMemberMap := ecs.NewMap[components.SquadMember](app.World)
	rosterMap := ecs.NewMap[components.CommandRoster](app.World)
	formationDataMap := ecs.NewMap[components.FormationData](app.World)
	formationOrientMap := ecs.NewMap[components.FormationOrientation](app.World)
	formationCustomSlotsMap := ecs.NewMap[components.FormationCustomSlots](app.World)
	// Order / quick-bar maps that input handlers + ghost preview reuse outside
	// the Inspector layer. Maps used ONLY by the Inspector are reachable via
	// inspectorMaps.X without a separate declaration here.
	orderQueueMap := ecs.NewMap[components.OrderQueueHead](app.World)
	orderKindMap := ecs.NewMap[components.OrderKind](app.World)
	orderTargetMap := ecs.NewMap[components.OrderTarget](app.World)
	orderChainMap := ecs.NewMap[components.OrderChain](app.World)
	// Phase 18 timeline panel reads these directly from main.go - inspectorMaps
	// only exposes the head + 2-queued used by the inline order section.
	orderStateMap := ecs.NewMap[components.OrderState](app.World)
	orderProgressMap := ecs.NewMap[components.OrderProgress](app.World)
	orderIssuedAtMap := ecs.NewMap[components.OrderIssuedAt](app.World)
	movementProfileMap := ecs.NewMap[components.MovementProfile](app.World)
	staminaMap := ecs.NewMap[components.Stamina](app.World)
	hpMap := ecs.NewMap[components.HP](app.World)
	factionMap := ecs.NewMap[components.Faction](app.World)
	individualPosMap := ecs.NewMap[components.IndividualPosition](app.World)

	// Phase 14 M14.6: faction-aware squad colour. Lookups the entity's
	// Faction and picks the player or enemy palette accordingly. Missing
	// Faction (legacy spawns) falls through to FactionPlayer.
	squadColor := func(ent ecs.Entity) rl.Color {
		faction := components.FactionPlayer
		if ent != (ecs.Entity{}) && app.World.Alive(ent) {
			if f := factionMap.Get(ent); f != nil {
				faction = f.ID
			}
		}
		return squadColorFor(ent, faction)
	}

	// Phase 12 role service. Owns UnitRole + per-role Equipment sub-entities
	// (Primary weapon, Secondary gear: Radio / Medkit / Spade / sidearm).
	roleService := systems.NewRoleService(app.World)

	// unitFactory delegates to entities.UnitFactory.Spawn - kept as a closure
	// so existing SquadService.CreateFromTemplate / pie-menu spawn callsites
	// keep their func(WorldPos) ecs.Entity signature.
	unitFactory := unitFactoryRef.Spawn

	// Phase 12 starter scene: three 4-soldier squads with distinct templates
	// so the role differentiation (cap colours, ShortLabel, map icon) is
	// visible immediately at startup. Phase 14 M14.1: stamped FactionPlayer.
	playerFaction := components.Faction{ID: components.FactionPlayer}
	var doorScene *doorSceneState
	var aiTest *aiTestState
	if isAIScene() {
		// Phase 17.8 — automated AI test scene. Spawns 1 squad + selects
		// target building, returns a state struct that auto-issues an
		// OccupyBuilding order at t=2s and prints a PASS/FAIL verdict at
		// t=30s. main.go's default test squads + enemies are skipped so
		// only the scene under test is in the world.
		aiTest = aiSceneSpawn(app.World, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap, buildingMap)
	} else if isDoorScene() {
		// Minimal test scene: one 5-unit Recon squad 12 m south of the
		// single test house, no enemy. Building / Level entities were
		// spawned above; capture the first ones into doorScene for the
		// auto-verifier + O / I / K / U hotkeys.
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
		// Phase 16.B.1.b: squads spawn OUTSIDE buildings so formation slots
		// don't land on wall-rasterised surface cells (which would block path
		// planning). Player can RMB inside a building to test enter-through-door
		// nav.
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

		// Phase 14 M14.1: hostile MotorRifle squad ~60 m from the player base on
		// the opposite side. DefendPosition order parks them in place (Phase 14
		// simple: enemies don't patrol - Phase 15 reactive movement). Once
		// WeaponSystem lands in M14.2 the player can engage by hand.
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

	// Render filters.
	unitRenderFilter := ecs.NewFilter3[components.WorldPos, components.Unit, components.Stance](app.World)
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)
	wallRenderFilter := ecs.NewFilter2[components.WorldPos, components.WallSegment](app.World)
	floorRenderFilter := ecs.NewFilter2[components.WorldPos, components.Floor](app.World)
	stairsRenderFilter := ecs.NewFilter2[components.WorldPos, components.Stairs](app.World)
	// Phase 16.C.0 cutaway state. levelMap and buildingViewModeMap were
	// already constructed for spawn; levelMemberMap is the new read handle.
	levelCutawayFilter := ecs.NewFilter2[components.Level, components.BuildingMember](app.World)
	levelMemberMap := ecs.NewMap[components.LevelMember](app.World)
	coverDirReadMap := ecs.NewMap[components.CoverDirection](app.World)
	levelVisReadMap := ecs.NewMap[components.LevelVisibility](app.World)
	navOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverSlotFilter := ecs.NewFilter2[components.WorldPos, components.CoverSlot](app.World)
	// Phase 15 M15.C.3 - MapPing filter for the pulsing-ring render pass.
	mapPingFilter := ecs.NewFilter2[components.WorldPos, components.MapPing](app.World)
	navGridChunkFilter := ecs.NewFilter1[components.NavGrid](app.World)
	floorNavFilter := ecs.NewFilter3[components.WorldPos, components.Level, components.LevelNavGrid](app.World)
	visionAwareFilter := ecs.NewFilter2[components.WorldPos, components.Awareness](app.World).
		With(ecs.C[components.Unit]())
	// Phase 17.9 — debug "Unit paths" overlay. Two filters: members of a
	// squad (with SquadMember), and solo units (without SquadMember). The
	// solo filter still includes SquadMember-having entities; drawUnitPaths
	// dedupes via the squadMemberMap.Has check.
	unitPathSquadFilter := ecs.NewFilter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember](app.World)
	unitPathSoloFilter := ecs.NewFilter3[components.Unit, components.WorldPos, components.MicroPath](app.World)

	chunkAllFilter := ecs.NewFilter1[components.TerrainChunk](app.World)
	weaponFilter := ecs.NewFilter1[components.Weapon](app.World)
	unitFilter := ecs.NewFilter1[components.Unit](app.World)
	stairsCountFilter := ecs.NewFilter1[components.Stairs](app.World)
	squadFilter := ecs.NewFilter2[components.Squad, components.CommandRoster](app.World)

	// Phase 11 hit-test filters. The Building filter is the same archetype as
	// buildingMap; the Trench-root filter walks the small startup-spawned set.
	buildingFilter := ecs.NewFilter1[components.Building](app.World)
	trenchRootFilter := ecs.NewFilter1[components.TrenchRoot](app.World)
	// Phase 14 M14.4: unit hit-test wired with a Filter2[Unit, WorldPos] and
	// the global Faction map. 1.5 m snap radius matches the standing-unit
	// collider (0.35 m) plus a ~1 m forgiveness margin so the player doesn't
	// have to click pixel-perfect on a unit's torso.
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

	// Phase 13.6 ghost preview context - bundles the maps drawSelectionGhost
	// needs (squad roster + formation + stance + movement profile). One
	// allocation up-front so the render loop just passes &ghostCtx.
	//
	// M13.6.3 fields (hitTester / buildingIndex / wallMap / windowMap /
	// trenches / trenchRootMap) drive per-kind placement: cursor over a
	// building -> ghosts in N first windows; over a trench -> ghosts equal-
	// spaced along the polyline.
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

	// Phase 17.6 M17.6.7: 3D order-marker context — handles drawOrderMarkers3D
	// needs to walk OrderQueueHead + OrderChain for every selected squad.
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

	// Phase 14.5 M14.5.4: ParticleRenderCtx - handles for drawParticles.
	// Built once; reused every frame in the 3D pass.
	particleRenderCtx := ParticleRenderCtx{
		Filter: ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](app.World),
		EndMap: ecs.NewMap[components.ParticleEnd](app.World),
	}

	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

	// Phase 10 UI scaffold.
	screenW, screenH := initialScreenWidth, initialScreenHeight
	panelMgr := ui.NewPanelManager()
	// Phase 13.5 M13.5.5: restore split ratios from disk before the first
	// Recompute so panels start at the user's last layout. Missing/corrupt
	// file silently falls back to defaults.
	loadLayout(panelMgr)
	panelMgr.Recompute(screenW, screenH)
	// Fail-safe: persist on shutdown in case the user resized but didn't end
	// the drag (or the EndDrag persistence missed an edge case).
	defer saveLayout(panelMgr)
	scene3DRT := ui.NewScene3DRT(panelMgr.Get(ui.Panel3D))
	defer scene3DRT.Unload()

	// Pre-bake the map underlay. 2 km x 2 km centred at origin, 4 m / pixel
	// (500x500 = 250 KB upload). Blocking; runs once at startup before the
	// main loop kicks off.
	underlay := ui.BakeUnderlay(0, 0, 2000, 4, func(wx, wz float32) float32 {
		return systems.GroundHeight(wx, wz)
	})
	defer underlay.Unload()
	mapCam := ui.NewMapCamera()
	var mapPanning bool
	var mapPanCursor rl.Vector2
	// Phase 17.6 M17.6.4: ContextMenu replaces PieMenu for object-specific
	// popups. PieMenu type still exists in ui/pie_menu.go for future radial
	// revivals but is no longer driven from this flow.
	var ctxMenu ui.ContextMenu
	// rmbState bundles the per-frame state of an RMB-hold session. Replaces
	// pieMenu fields. Active is set on press, cleared on release.
	// PressOrigin / PressTarget capture press-time anchors so release
	// commits don't drift with the cursor.
	var rmbState struct {
		Active       bool
		SourcePanel  ui.PanelID
		PressOrigin  rl.Vector2
		PressTarget  components.WorldPos
		PressTimeSec float32 // session-time at press
		HoveredBldg  ecs.Entity
		HasSelection bool
		FacingActive bool // > 8 px drag committed to facing-drag
		Ctrl, Alt    bool
		Double       bool
	}
	var lastRMBPressAt float32 // session-time of the previous press
	// Window for treating consecutive RMB presses as a double-click. PHASE-13.md
	// заметка про Double-RMB: 300 ms is empirical - wide enough for relaxed
	// chains, narrow enough that two deliberate sequential clicks don't fuse.
	const rmbDoubleWindow float32 = 0.30
	// Phase 13.5 M13.5.4: scrollbar thumb drag state. scrollDragging = true
	// while LMB is held on a scrollbar thumb; scrollDragStartCursorY +
	// scrollDragStartOffset capture the press-time anchor so cursor delta
	// translates linearly to scroll offset.
	var (
		scrollDragging         bool
		scrollDragStartCursorY float32
		scrollDragStartOffset  float32
	)
	// One wheel-tick of `rl.GetMouseWheelMove()` scrolls Inspector by this
	// many pixels (~3 rows at fontSize=14). Tunable in M13.5.6 if it feels
	// off in playtest.
	const wheelScrollSpeed float32 = 30
	// Inter-frame smoothing for map squad markers (ISSUES #1 polish).
	smoothedSquadPos := make(map[ecs.Entity]components.WorldPos, 8)

	// Selection / hover state. Hover refreshes each frame from cursor + focused
	// panel; `hovered` is consumed by the inspector and the map renderer.
	var (
		navPath          []components.WorldPos
		selected         []ecs.Entity
		hovered          ecs.Entity
		hoveredBuilding  ecs.Entity // Phase 16.C.1
		hoveredLevel     ecs.Entity // Phase 17.6 M17.6.8 — level under ray
		selectedBuilding ecs.Entity // Phase 16.C.1 - sticky
		buildingWidget   *ui.BuildingWidgetLayout    // Phase 16.C.1 (per frame)
		marqueeStart     rl.Vector2                  // screen coords
		marqueeActive    bool
		marqueeOrigin    ui.PanelID
		expandedHUDOn    bool
		showMapDebugLy   bool // toggled per-frame by hold-G
		binds            [5]bindEntry
		// Phase 18 timeline panel state.
		timelineView      = ui.NewTimelineView()
		timelineData      ui.TimelineData
		timelineHoverHit  ui.TimelineHit
		timelineHoverOK   bool
		timelineHoverBlk  ui.TimelineOrderBlock
		topBarPlayPause   rl.Rectangle
		topBarSpeedDown   rl.Rectangle
		topBarSpeedUp     rl.Rectangle
		// Phase 18.C tree-of-splits popup menu (per-leaf widget switch / close).
		chevronMenu ui.ChevronMenu
		// Phase 18 floating panels (formation editor, future dialogs).
		floating = ui.NewFloatingState()
	)
	const marqueeClickThreshold float32 = 5
	// chromeBusy = "UI chrome currently owns mouse/keyboard". When true the
	// content layers (3D selection / marquee / map click / inspector chips /
	// top-bar buttons / timeline blocks) skip their LMB handlers so a chrome
	// interaction doesn't double-fire into the world below.
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

	// Shared FormationEditorCtx + singleton editor instance. Both the
	// floating-panel form (E hotkey) and the workspace-panel form
	// (PanelFormation in the leaf tree) read state through the same
	// pointer, so changes (zoom, kind, custom slots) survive a re-dock
	// and the editor always tracks the currently selected squad through
	// SelectionFn.
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

	// renderFloatingWidget paints any workspace widget into a floating
	// panel's content rect. ContentToPanel synthesises a Panel whose
	// ContentRect recovers `content`, so each widget's existing chrome-
	// aware draw code (which subtracts a title bar + border) lands in the
	// right place. Panel3D is intentionally a no-op: the scene RT is
	// sized to the workspace 3D leaf and detaching would need a second
	// render texture - skipped for the first iteration of Float pane.
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
			// Not floatable yet - see comment above. The chevron menu
			// disables Float pane for the 3D leaf so this branch is
			// unreachable in practice.
		}
		return false
	}

	// makeFloatingRender packs renderFloatingWidget into a closure for a
	// specific PanelID. Used both by floatSpawn (chevron Float pane) and
	// the E-hotkey, so a floater opened either way is uniformly
	// switchable via the title-bar chevron button.
	makeFloatingRender := func(id ui.PanelID) ui.FloatingRenderFn {
		return func(c rl.Rectangle, cu rl.Vector2, f rl.Font, l bool) bool {
			return renderFloatingWidget(id, c, cu, f, l)
		}
	}

	floatSpawn := func(id ui.PanelID, title string, bounds rl.Rectangle) {
		// Use the leaf's previous bounds as the initial floater rect so
		// the panel materialises in place rather than snapping to the
		// top-left default. Clamp to a sensible minimum so a tiny pane
		// doesn't yield an unusable floater.
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
		// Window resize -> re-layout + re-alloc the 3D RT to the new bounds.
		if rl.IsWindowResized() {
			screenW = int32(rl.GetScreenWidth())
			screenH = int32(rl.GetScreenHeight())
			panelMgr.Recompute(screenW, screenH)
			scene3DRT.EnsureSize(panelMgr.Get(ui.Panel3D))
		}

		// Real-time dt for input / camera-orbit. The simulation tick gets this
		// scaled by app.TimeScale inside App.Tick (Phase 10 P7).
		dtReal := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))
		// Session clock for OrderIssuedAt / progress timing. Scaled to match
		// simulation time so pause freezes the clock with the rest of the sim.
		squadService.SetClock(float32(app.Elapsed().Seconds()))
		utilityEvalSys.SetClock(float32(app.Elapsed().Seconds()))

		cursor := rl.GetMousePosition()
		focused := panelMgr.FocusedAt(cursor)
		shiftHeld := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
		ctrlHeld := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)
		altHeld := rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt)

		panel3D := panelMgr.Get(ui.Panel3D)
		panelMap := panelMgr.Get(ui.PanelMap)

		// -- Tab -> toggle layout preset --
		if rl.IsKeyPressed(rl.KeyTab) {
			// Phase 13.5: Tab during a splitter drag aborts the drag (revert
			// to pre-drag ratio) before flipping the preset - avoids weird
			// half-applied resize state on preset swap.
			if panelMgr.IsDragging() {
				panelMgr.AbortDrag()
			}
			panelMgr.TogglePreset()
			panelMgr.Recompute(screenW, screenH)
			scene3DRT.EnsureSize(panelMgr.Get(ui.Panel3D))
			panel3D = panelMgr.Get(ui.Panel3D)
			panelMap = panelMgr.Get(ui.PanelMap)
		}

		// -- Phase 18.C - splitter / corner / chevron hover + drag --
		// Priority chain: open menu wins LMB; then splitter (drag existing
		// divider); then chevron (open menu); then corner-grab (start a
		// pending split). The chrome cursor reflects the topmost target.
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

		// Phase 18 floating panels eat LMB first (drag header, X close,
		// content focus). Returns true when this frame's LMB was consumed.
		floatingConsumed := floating.HandleInput(cursor,
			rl.IsMouseButtonPressed(rl.MouseButtonLeft),
			rl.IsMouseButtonDown(rl.MouseButtonLeft),
			rl.IsKeyPressed(rl.KeyEscape),
			screenW, screenH)

		// LMB press dispatch. Order matters: menu-open takes priority so the
		// rest of the UI doesn't react under it. lmbDown is the raw "press
		// this frame" signal (menu still needs to receive it even when
		// chromeBusy gates everything else); lmbPress is the gated form
		// used by splitter / chevron / corner start. A press inside any
		// floating panel is forwarded to the panel only - workspace chrome
		// and content stay silent.
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

		// Active splitter drag - apply cursor pos to ratio, persist on
		// release.
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
		// Active corner drag - release commits a new split.
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

		// -- Door test scene auto-verifier + hotkeys (O / I / K / U) --
		if doorScene != nil {
			doorScene.EnsureInit()
			doorScene.Update(float32(app.Elapsed().Seconds()))
			doorScene.HandleHotkeys(focused == ui.Panel3D)
		}

		// -- AI test scene auto-verifier (Phase 17.8) --
		if aiTest != nil {
			aiTest.Update(float32(app.Elapsed().Seconds()))
		}

		// -- Space -> toggle pause; +/- -> cycle speed 1->2->4->8->1 --
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

		// -- WASD anchor (Panel3D-or-none focus) --
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

		// Map's content rect - the actual drawing surface, minus chrome. Cursor
		// conversions go through this rather than panelMap.Bounds so clicks /
		// zoom pivots align with what the player sees.
		panelMapContent := ui.ContentRect(panelMap)

		// -- Map pan / zoom (only when map focused, no chrome busy) --
		// chromeBusy includes "cursor over a floating panel", so wheel /
		// pan / click stay locked to the floater chrome above and don't
		// bleed through to the underlying map.
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

		// -- Phase 13.5 M13.5.4 - Inspector wheel scroll + thumb drag --
		// Wheel only fires when the cursor is over the Inspector panel and
		// no splitter drag is active. MapCamera's wheel block above is gated
		// on focused == ui.PanelMap, so the two paths are mutually exclusive.
		if focused == ui.PanelInspect && !chromeBusy() {
			if wheel := rl.GetMouseWheelMove(); wheel != 0 {
				if scroll := panelMgr.ScrollByID(ui.PanelInspect); scroll != nil {
					scroll.OffsetY -= wheel * wheelScrollSpeed
					ui.ClampScrollOffset(panelMgr.Get(ui.PanelInspect), scroll)
				}
			}
		}
		// Thumb drag - LMB-press on thumb rect starts the drag, regardless of
		// focused panel (the thumb itself is always inside Inspector bounds).
		// Splitter drag has priority - it uses LMB too, so guard against both.
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

		// -- Phase 18 top bar: LMB on play/pause/speed buttons --
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

		// -- Phase 18 timeline: hover/click on order blocks, wheel pan/zoom --
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
			// Double-click on the panel background -> re-enable Follow.
			if rl.IsMouseButtonPressed(rl.MouseButtonRight) && timelineHoverOK && !timelineHoverHit.HitOrder {
				timelineView.Follow = true
			}
		}

		// -- 3D panel cursor (content-rect-local) --
		// Cursor coords used for raycast / marquee / picking are relative to
		// the 3D content rect (panel minus chrome), and viewW/H match the
		// content rect - same as the RT - so GetScreenToWorldRayEx /
		// GetWorldToScreenEx project consistently with what the player sees.
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

		// -- LMB press --
		// Phase 13.5 guard: a splitter drag claims LMB exclusively. Skip
		// selection / marquee / map-pick on the press that started the drag
		// AND every frame the drag is active.
		//
		// Phase 16.C.1: building widget chips claim the press too. If the
		// cursor sits over a chip when LMB goes down, apply the mutation
		// (CurrentLevel / WallMode / InteriorOpen) and skip marquee start;
		// otherwise the click would also start a stray selection rectangle.
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
					// Clicking a chip implicitly pins the widget on this
					// building so it doesn't disappear when the cursor moves
					// off the chip during a follow-up click.
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
				// Click on map: pick squad marker, else clear selection.
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

		// -- LMB release -> commit marquee or treat as a 3D click --
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
						// Clicking a unit removes any prior building pin.
						selectedBuilding = ecs.Entity{}
					} else if hoveredBuilding != (ecs.Entity{}) {
						// Phase 16.C.1: empty 3D click that landed on a
						// building footprint pins the widget on that building.
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

		// -- RMB orders (Phase 17.6 rewrite). Flow:
		//   - Press: snapshot target / modifiers / hovered building. Start
		//     an RMB session (rmbState.Active = true).
		//   - While held + no popup + no facing-drag:
		//       drag > 8 px → facing-drag mode (yaw from cursor delta).
		//       hold >= 200 ms over a building → open ContextMenu popup.
		//   - Release:
		//       popup active → commit hovered item (or cancel).
		//       facing-drag active → commit order with arrived-facing yaw.
		//       neither → tap commit (default hit-test action).
		//   - ESC closes the popup at any time.
		// Camera (MMB) is independent — no orbit-suppression needed.
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
				// Phase 16.B.1.b / 17.6 UX: cursor visually over a building
				// but raycast lands just outside the footprint → snap target
				// to the ground-floor centre so hit-test classifies as
				// HitBuilding (Footprint.Contains succeeds) → OccupyBuilding
				// resolves through the nearest door.
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

		// While RMB session is active, decide between facing-drag and
		// popup-open. Popup, once opened, persists until release / ESC.
		if rmbState.Active {
			rmbDown := rl.IsMouseButtonDown(rl.MouseButtonRight)
			rmbReleased := rl.IsMouseButtonReleased(rl.MouseButtonRight)

			// While held, no popup yet, no facing yet — check both triggers.
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

			// Popup-active path: read LMB / ESC. RMB release also commits
			// the hovered item in single-gesture style.
			if ctxMenu.IsActive() {
				escPressed := rl.IsKeyPressed(rl.KeyEscape)
				lmbPressedForMenu := rl.IsMouseButtonPressed(rl.MouseButtonLeft)
				res := ctxMenu.Tick(cursor, lmbPressedForMenu, escPressed)
				switch {
				case rmbReleased && ctxMenu.IsActive():
					// Single-gesture release: commit hovered item if any.
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
				// res.Cancelled or still-active — nothing else to do.
			}

			// Release handling for paths that don't involve popup.
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
					// Tap commit. Shift+RMB on subset → IndividualPosition path.
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

			// Close session on release regardless of outcome.
			if rmbReleased {
				rmbState.Active = false
				rmbState.FacingActive = false
			}
		}

		// -- H -> Stop order (global hotkey) --
		// Phase 11: iterates SquadsToOrder for distributed cancel, plus the
		// per-unit Stop for soloists. Mirrors resolveRMBOrder's split.
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

		// -- T -> form Squad. Phase 18 auto-formation rule: infantry-only
		// merges land in FormationLoose (free); mixed infantry+vehicle
		// merges snapshot current world positions into FormationCustomSlots
		// + lock OrientNorth so each member stays where it stood relative
		// to the new squad centre. Phase 19 will populate the vehicle
		// branch; until then the mixed check is always false.
		if rl.IsKeyPressed(rl.KeyT) && len(selected) >= 2 {
			mixed := containsVehicle(selected, app.World)
			kind := components.FormationLoose
			if mixed {
				kind = components.FormationLine
			}
			// Snapshot positions BEFORE create (CreateFromUnits may despawn
			// old squads but doesn't move WorldPos).
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

		// -- U -> ungroup --
		if rl.IsKeyPressed(rl.KeyU) && len(selected) > 0 {
			for _, e := range selected {
				squadService.Leave(e)
			}
		}

		// -- F1-F4 -> change formation --
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

		// -- E -> toggle formation editor floating panel. Pinned to the
		//        squad active when the panel is opened; afterwards
		//        SelectionFn keeps it in sync as selection changes.
		//        (F is taken by FloorNavGrid debug hold-overlay.)
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

		// -- Phase 13 M13.7 - MovementProfile hotkeys --
		// `[` / `]` cycle MovementProfile presets prev/next. `'` toggles
		// Posture Standard <-> Quiet. Stance hotkeys (Z/X/C) are deferred to
		// Phase 21 because Z/X are already taken (crater, cover overlay) and
		// the Inspector quick-bar covers the case meanwhile.
		//
		// Hotkeys apply when a homogeneous squad is selected - same gate as
		// formation hotkeys above.
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

		// -- Ctrl+1..5 bind / 1..5 recall --
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

		// -- X -> crater (Panel3D only) --
		if focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
		}

		// -- B -> toggle BuildingViewMode.InteriorOpen on every building.
		// Phase 16.C.0 smoke; replaced by per-building widget (M16.C.1).
		// PgUp / PgDn cycle CurrentLevel up / down across the building's
		// level list (sorted ascending by storey by HouseTemplate).
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

		// -- Hover update --
		// Hover in Panel3D: closest unit under cursor (silent pick).
		// Hover in PanelMap: closest squad marker within 12 px.
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

		// Phase 13.6 M13.6.2: cursor target in WorldPos for the ghost preview.
		// Recomputed every frame (continuous hover tracking); the raycast is one
		// per-frame, cheap. When the cursor leaves Panel3D or the ray misses
		// the ground plane (sky / parallel), the ghost pass skips itself.
		var (
			ghostTarget   components.WorldPos
			ghostTargetOK bool
		)
		if focused == ui.Panel3D {
			ghostTarget, ghostTargetOK = mouseTargetWorldPos(systems.CurrentCamera,
				anchorPos.ToRenderSpace(systems.CurrentOriginChunk),
				panel3DLocal, panel3DW, panel3DH)
		}

		// Phase 16.C.1: building hover - cursor's ground target inside any
		// Building.Footprint surfaces that building as hoverable. Used both
		// to gate the chip widget and to claim LMB clicks on it.
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
			// Phase 17.6 M17.6.8 — narrow the outline to the storey the
			// cursor's ray actually pierces. Falls back to whole-building
			// outline if no Level box catches the ray (ray near floor
			// plane, multi-floor building viewed from above, etc.).
			if hoveredBuilding != (ecs.Entity{}) {
				ray := rl.GetScreenToWorldRayEx(panel3DLocal, systems.CurrentCamera, panel3DW, panel3DH)
				if lvl, ok := pickLevelUnderRay(ray, hoveredBuilding, &buildingPlanIndex, levelMap); ok {
					hoveredLevel = lvl
				}
			}
		}

		// Widget target priority: a sticky selectedBuilding wins so the
		// player can move the cursor onto the chips without the panel
		// vanishing. Otherwise show a hover preview for whatever footprint
		// the cursor sits inside.
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

		// Gate the 3D camera's orbit / wheel zoom by panel focus. Wheel events
		// when the map panel is focused belong to the map's own zoom; MMB held
		// inside the map panel should pan the map, not spin the field camera.
		// Also suppress while a floating panel owns the cursor (so wheel-zoom
		// of the formation editor doesn't also dolly the field camera
		// underneath). Phase 17.6: orbit moved to MMB so RMB is free for order
		// popups - pieMenu suppressors removed since pie no longer captures
		// RMB anyway (it'll be torn out of the RMB flow in M17.6.4).
		systems.OrbitInputEnabled = (focused == ui.Panel3D || focused == ui.PanelNone) &&
			!floating.IsBusy(cursor)

		app.Tick(dtReal)

		anchorPos = posMap.Get(anchor)
		anchorRender := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)

		// Phase 14.5 M14.5.4: particles are ECS entities now - lifecycle
		// (age + despawn) lives in ParticleSystem.Update. No per-frame
		// decay needed here.

		// -- Render 3D scene into RT --
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
			// Phase 12: role drives cap colour + ShortLabel. Fall back to
			// Rifleman if a unit somehow lacks UnitRole - keeps render
			// resilient if a future spawn path forgets AssignRole.
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

		// Phase 16.C.0: build a per-frame "hidden level" set. A Level is
		// hidden when its owning building has BuildingViewMode.InteriorOpen
		// AND the level's avgY sits above CurrentLevel's avgY + epsilon.
		// Skip-render gating below reads this map.
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

		// Phase 16.C.2: a level is "fogged" when it's never been discovered
		// OR was last seen more than FogVisibleDuration ago. Static layout
		// (walls / floors) renders with a grey tint; dynamic contents would
		// be culled outright (no renderer yet for furniture / hostiles).
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
				// Phase 16.C.4: wall mode only applies to walls of the
				// currently-viewed level inside an open cutaway. Lower
				// levels render normally even when InteriorOpen.
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

		// Phase 16.C.1 polish: outline boxes around the hovered (preview) and
		// selected (pinned) buildings. Drawn after walls/floors so the lines
		// sit on top of the geometry from most angles. Box is padded 0.15 m
		// outwards from the footprint to keep the wireframe legible against
		// the wall surfaces.
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
			// Phase 17.6 M17.6.8 — outline the storey under the ray if we
			// have one; otherwise fall back to the whole-building wireframe.
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

		// Debug overlays - all gated by hold-key. The hold-G road overlay also
		// toggles the map's road / river / building debug layer for consistency.
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

		// Phase 16.B.1.b debug: transition edges in the registry as coloured
		// 3D lines. Surface<->Level edges = green, Level<->Level = yellow.
		// Missing lines through a door/stair = the bake failed to resolve
		// LevelMember / StairLevels for that opening. Toggle via Debug widget.
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
					col := rl.Color{R: 50, G: 220, B: 80, A: 255} // surf<->level
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

		// Phase 17.9 — Unit paths overlay. Toggle from Debug widget; when
		// a squad is selected, only its members' MicroPath stripes draw.
		if debugOverlay.UnitPaths {
			drawUnitPaths(unitPathRenderCtx{
				filter:         unitPathSquadFilter,
				soloFilter:     unitPathSoloFilter,
				squadMemberMap: squadMemberMap,
				selectedSquad:  unitPathsSelectedSquad(selected, squadMemberMap),
			})
		}

		// Phase 13.6 M13.6.2 / M13.6.4: ghost-preview formation. Continuous
		// render of where the selected squad would arrive if the player issued
		// a Move order at the current cursor target. No-op when cursor isn't
		// in Panel3D, no squad in selection, or raycast missed the ground.
		// While the player is facing-dragging, ghosts rotate live to match the
		// drag-derived yaw so the orientation preview is honest about what
		// release will commit to.
		var ghostDragFacing *float32
		if rmbState.Active && rmbState.FacingActive {
			// Recompute the current yaw — release commits the same screen-space
			// formula. Pin the ghost to press-time target so the formation
			// orientation rotates around a stable anchor while dragging.
			dx := cursor.X - rmbState.PressOrigin.X
			dy := cursor.Y - rmbState.PressOrigin.Y
			yaw := float32(math.Atan2(float64(dx), float64(-dy)))
			ghostDragFacing = &yaw
			ghostTarget = rmbState.PressTarget
			ghostTargetOK = true
		}
		// Phase 17.6 M17.6.4: when ContextMenu is open and hovering an item,
		// expose the kind so the ghost render swaps preview placement (Garrison
		// = at-windows, OccupyBuilding = by-floors, etc — full per-kind swap is
		// M17.6.9).
		var ghostPopupKind *components.OrderKindCode
		var ghostPopupLevel ecs.Entity
		if ctxMenu.IsActive() {
			if item, ok := ctxMenu.HoveredItemDetails(); ok {
				k := item.Kind
				ghostPopupKind = &k
				ghostPopupLevel = item.LevelEntity
			}
			// Anchor at press-time target while the popup is open — player
			// is choosing for that point, not for whatever's under the cursor
			// (which is on a menu item).
			ghostTarget = rmbState.PressTarget
			ghostTargetOK = true
		}
		drawSelectionGhost(ghostCtx, selected, focused == ui.Panel3D, ghostTarget, ghostTargetOK,
			ghostDragFacing, ghostPopupKind, ghostPopupLevel, levelMap)

		// Phase 17.6 M17.6.7: 3D order markers for every selected squad —
		// cube + connector lines per Order. Depth-test disabled so markers
		// stay visible through walls. Drawn after ghost so the active head
		// marker sits on top of any overlapping ghost dot.
		drawOrderMarkers3D(orderMarkerRenderCtx, selected)

		// Phase 14.5 M14.5.4: ECS-particle render walks the Particle filter.
		// Per-kind dispatch (tracer line / impact sphere / smoke / dust /
		// debris cube / muzzle flash) lives in drawParticles.
		drawParticles(particleRenderCtx, float32(app.Elapsed().Seconds()))

		rl.EndMode3D()
		rl.EndTextureMode()

		// -- 2D pass - clear bg, paint each panel --
		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 8, G: 10, B: 14, A: 255})

		// Panel content (background + body) drawn before chrome so the title
		// bar overlays the content cleanly.
		// Map panel.
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

		// Inspector panel.
		// Phase 13 M13.6: feed click state + standing-rule handles for the
		// quick-bar sections. LMBPressed mirrors the single press edge so
		// chips fire once per click. PanelFocused gates clicks so a drag
		// originating elsewhere doesn't accidentally trigger toggles.
		// Phase 13.5 M13.5.3: pass the per-panel scroll handle so DrawInspector
		// can subtract OffsetY and write back ContentHeight.
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
		// Phase 13.5 M13.5.3: scrollbar overlay drawn AFTER DrawInspector so
		// the EndScissorMode released its clip first. ClampScrollOffset keeps
		// the offset valid if a content shrink (selection switch) made the
		// previous offset out-of-range.
		if inspectorScroll != nil {
			ui.ClampScrollOffset(inspectorPanel, inspectorScroll)
			ui.DrawScrollbar(inspectorPanel, inspectorScroll)
		}

		// Phase 18 top bar (chromeless toolbar).
		topBarPlayPause, topBarSpeedDown, topBarSpeedUp = ui.DrawTopBar(
			panelMgr.Get(ui.PanelTopBar), hudFont, ui.TimeDisplay{
				Scale:   app.TimeScale,
				Elapsed: float32(app.Elapsed().Seconds()),
			})

		// Phase 18 timeline panel.
		timelineData = buildTimelineData(app.World, squadFilter, posMap, factionMap,
			orderQueueMap, orderChainMap, orderKindMap, orderTargetMap, orderStateMap,
			orderProgressMap, orderIssuedAtMap, squadColor,
			float32(app.Elapsed().Seconds()))
		ui.DrawTimelinePanel(panelMgr.Get(ui.PanelTimeline), hudFont, timelineData, &timelineView)
		if timelineHoverOK && timelineHoverHit.HitOrder {
			ui.DrawTimelineTooltip(hudFont, cursor, timelineHoverBlk)
		}

		// Formation editor as a workspace panel - the chevron menu can place
		// it in any leaf. lmbPress is gated by chromeBusy so a press inside
		// the editor's chrome (kind dropdown, dot drag) doesn't double-fire
		// into the world below.
		if leaf := panelMgr.LeafFor(ui.PanelFormation); leaf != nil {
			formationLMB := !chromeBusy() && !scrollDragging &&
				panelMgr.FocusedAt(cursor) == ui.PanelFormation &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft)
			formationEditor.DrawPanel(panelMgr.Get(ui.PanelFormation),
				hudFont, cursor, formationLMB)
		}

		// Phase 17.9 — Debug overlays widget. Toggle state lives in
		// `debugOverlay`; the actual overlay rendering happens later in the
		// 3D pass, gated by the flags this widget mutates.
		if leaf := panelMgr.LeafFor(ui.PanelDebug); leaf != nil {
			debugLMB := !chromeBusy() && !scrollDragging &&
				panelMgr.FocusedAt(cursor) == ui.PanelDebug &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft)
			ui.DrawDebugPanel(panelMgr.Get(ui.PanelDebug),
				hudFont, debugOverlayToggles(&debugOverlay), cursor, debugLMB,
				"Radius: 2 chunks around camera")
		}

		// 3D RT composite into Panel3D bounds.
		scene3DRT.Composite(panel3D)

		// Phase 12 role labels - 2D screen-projected ShortLabel pills above
		// every unit. Done after RT composite so the labels overlay the
		// scene; scissored to Panel3D content rect so they don't bleed onto
		// neighbouring panels.
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
			// Phase 13 M13.7: thin Stamina bar above the cap when < 80%.
			if stam := staminaMap.Get(ent); stam != nil {
				drawUnitStaminaBar(renderPos, *st, role, stam.Current, stam.MaxLevel, panel3DContent)
			}
			// Phase 14 M14.6: HP bar above Stamina when damaged.
			if hp := hpMap.Get(ent); hp != nil {
				drawUnitHPBar(renderPos, *st, role, hp.Current, hp.Max, panel3DContent)
			}
		}
		rl.EndScissorMode()

		// Phase 16.C.1: cutaway chip widget for the hovered building.
		// Layout was built in the input phase against the same cursor and
		// camera as the click hit-test, so the visual matches the click.
		if buildingWidget != nil {
			rl.BeginScissorMode(int32(panel3DContent.X), int32(panel3DContent.Y),
				int32(panel3DContent.Width), int32(panel3DContent.Height))
			ui.DrawBuildingWidget(buildingWidget, hudFont)
			rl.EndScissorMode()
		}

		// Marquee (panel-local clipped). Drawn after composite so it sits over
		// the 3D scene; scissored to Panel3D so dragging outside the panel
		// doesn't leak. Only fired when the marquee originated in Panel3D.
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

		// Chrome (border + title) on every panel, drawn last so it overlays
		// content (including marquee strokes that bleed onto the title bar).
		// PanelTopBar drawn by DrawTopBar itself (no chrome). All workspace
		// leaves get normal chrome via tree walk - Phase 18.C tree layout
		// means we no longer iterate a fixed list of PanelIDs.
		panelMgr.Workspace.WalkLeaves(func(l *ui.LayoutNode) {
			ui.DrawChrome(ui.Panel{ID: l.Panel, Bounds: l.Bounds, Title: l.Title}, hudFont, 16)
		})

		// Phase 18.C corner-grab handles + active split preview line.
		ui.DrawCornerHandles(panelMgr, cursor)
		if panelMgr.IsCornerDragging() {
			ui.DrawCornerDragPreview(panelMgr, cursor)
		}

		// Chevron popup menu (Phase 18.C). Drawn after chrome so the menu
		// sits over title bars.
		chevronMenu.Draw(hudFont, cursor)

		// Phase 18 floating panels (formation editor + future dialogs).
		// Drawn after chevron menu so floaters sit above it; rendered
		// before pie menu so RMB pie still wins as top overlay.
		floating.DrawAll(hudFont, cursor, rl.IsMouseButtonPressed(rl.MouseButtonLeft))
		// Switch-content popup is drawn last so it sits over every
		// floater's chrome.
		floating.DrawSwitchMenu(hudFont, cursor)

		// Phase 17.6 M17.6.4: context menu (RMB-hold-on-building popup).
		// Drawn after chrome + switch menu so it sits above every panel.
		// Pie menu (legacy) is no longer in the RMB flow but kept in
		// ui/pie_menu.go for future radial revivals.
		ctxMenu.Draw(hudFont, cursor)

		// HUD hotkey hints moved into expanded profiler HUD (toggle with P).
		// Phase 10: drawing them over the panel chrome on every frame conflicts
		// with each panel's title bar; they're discoverable on demand instead.

		// Profiler HUD - collapsed always, expanded behind P toggle.
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

// containsVehicle reports whether any unit in `units` is non-infantry.
// Placeholder until Phase 19 lands the Vehicle component — currently
// always returns false (every unit on the field is infantry). When
// vehicles ship, swap the body for a real Vehicle-map.Has loop.
func containsVehicle(units []ecs.Entity, world *ecs.World) bool {
	_ = world
	_ = units
	return false
}

// applyPreservedSlots snapshots each rostered member's pre-merge world
// position into FormationCustomSlots, expressed in north-relative local
// frame (X = world +X, Y = world +Z), then sets OrientNorth so the layout
// doesn't rotate with motion. Slot 0 (commander) is the anchor: all other
// slots are offsets from the commander's snapshot position.
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
		// WorldPos.Sub returns a world-space rl.Vector3 from `other` to `p`
		// (chunk-aware), so this is already the per-member offset from
		// the commander in metres.
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

// chevronLeafAt returns the workspace leaf whose chevron button sits under
// the cursor, or nil. Walks the tree and tests each leaf's chevron rect.
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

// handleMenuItem dispatches a chevron-menu selection: switch the leaf to
// a different widget (swapping with the existing host if it's already in
// the tree), merge the leaf into its sibling (close pane), or detach the
// widget into a floating panel via the supplied floatSpawn callback (the
// callback owns the FloatingState + per-widget render closures since
// those live in main's scope).
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
		// If the target widget is already shown elsewhere, swap contents
		// so each PanelID appears at most once.
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
		// Snapshot leaf state before we tear it out of the tree.
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

// nextTimeScale advances the speed multiplier through 1 -> 2 -> 4 -> 8 -> 1
// (step=+1) or backwards (step=-1). When currently paused, advancing forward
// jumps to 1x; advancing back jumps to 8x. Used by the +/- hotkey.
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

// buildTimelineData snapshots all squads + their order queues into a flat
// structure for ui.DrawTimelinePanel. Phase 18 MVP: live orders only (head +
// chain). Queued blocks stack right after the head's estimated end so the
// timeline reads left-to-right even before resolver actually starts them.
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
// MoveTo / Garrison / OccupyTrench mix travel time (dist / 5 m/s) with a
// fixed action timer; pure timers (Defend / Patrol / Attack / Suppress) use
// a flat block so the player still sees something on the timeline.
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
