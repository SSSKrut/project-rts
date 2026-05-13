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
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

const (
	initialScreenWidth  int32 = 800 * 2
	initialScreenHeight int32 = 450 * 2
)

// workersFlag picks the worker-pool size. 0 (default) → runtime.NumCPU().
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

	rl.InitAudioDevice()
	defer rl.CloseAudioDevice()

	rl.SetTargetFPS(60)

	samples := make([]byte, 44100*2)
	for i := 0; i < len(samples); i += 2 {
		val := int16(4000)
		if (i/200)%2 == 0 {
			val = -4000
		}
		samples[i] = byte(val & 0xFF)
		samples[i+1] = byte(val >> 8)
	}
	wave := rl.NewWave(44100, 44100, 16, 1, samples)
	defer rl.UnloadWave(wave)

	audioManager := systems.NewAudioManager(8)
	audioManager.RegisterWave("engine", wave)
	defer audioManager.Unload()

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
	trenches := components.TrenchNetwork{Lines: makeStartingTrenches()}
	ecs.AddResource(app.World, &trenches)
	coverSlotIndex := systems.NewCoverSlotIndex()
	ecs.AddResource(app.World, &coverSlotIndex)
	transitionRegistry := components.NewTransitionRegistry()
	ecs.AddResource(app.World, &transitionRegistry)
	mapMarkerCache := components.NewMapMarkerCache()
	ecs.AddResource(app.World, &mapMarkerCache)

	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	stamper := systems.NewStamper(app.World)
	navService := systems.NewNavService(app.World)
	squadService := systems.NewSquadService(app.World)

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

	unitMovementSys := systems.NewUnitMovementSystem(workerPool)
	unitMovementSys.InitUI(app.World)

	visionSys := systems.NewVisionSystem(workerPool)
	visionSys.InitUI(app.World)

	orderResolverSys := systems.NewOrderResolverSystem(squadService)
	orderResolverSys.InitUI(app.World)

	squadMacroPathSys := systems.NewSquadMacroPathSystem(navService, workerPool)
	squadMacroPathSys.InitUI(app.World)

	formationSys := systems.NewFormationSystem(squadService, workerPool)
	formationSys.InitUI(app.World)

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

	audioSys := &systems.SpatialAudioSystem{
		Manager:     audioManager,
		MaxPerChunk: 2,
	}
	audioSys.InitUI(app.World)

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
	app.AddSystem(unitMovementSys)
	app.AddSystem(visionSys)
	app.AddSystem(orderResolverSys)
	app.AddSystem(squadMacroPathSys)
	app.AddSystem(formationSys)
	app.AddSystem(mapMarkerCacheSys)
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(audioSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	posMap := ecs.NewMap[components.WorldPos](app.World)
	lodActiveMap := ecs.NewMap[components.LODActive](app.World)
	lodAnchorMap := ecs.NewMap[components.LODAnchor](app.World)
	alwaysActiveMap := ecs.NewMap[components.AlwaysActive](app.World)

	anchor := app.World.NewEntity()
	posMap.Add(anchor, &components.WorldPos{})
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
	for i := range buildingPlans.Plans {
		p := &buildingPlans.Plans[i]
		root := app.World.NewEntity()
		fp := components.AABB2D{
			MinX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize - p.Size.X*0.5,
			MinZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize - p.Size.Y*0.5,
			MaxX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize + p.Size.X*0.5,
			MaxZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize + p.Size.Y*0.5,
		}
		bpos := p.Pos
		bpos.Local.Y = systems.GroundHeight(fp.CenterX(), fp.CenterZ())
		posMap.Add(root, &bpos)
		buildingMap.Add(root, &components.Building{
			Kind:      p.Kind,
			Stories:   p.Stories,
			Yaw:       p.Yaw,
			Footprint: fp,
			Seed:      p.Seed,
		})
		alwaysActiveMap.Add(root, &components.AlwaysActive{})
	}

	// Phase 11: one TrenchRoot entity per polyline so the hit-test resolver
	// can return an ecs.Entity in OrderTarget.Entity for OccupyTrench. The
	// Trench polyline data stays in the TrenchNetwork resource — TrenchRoot
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

	unitPositions := []rl.Vector3{
		{X: -22, Z: -38}, {X: -28, Z: -38}, {X: -22, Z: -42}, {X: -28, Z: -42},
		{X: 38, Z: 28}, {X: 42, Z: 28}, {X: 38, Z: 32}, {X: 42, Z: 32},
		{X: -28, Z: 52}, {X: -32, Z: 52}, {X: -28, Z: 58}, {X: -32, Z: 58},
	}
	unitMap := ecs.NewMap[components.Unit](app.World)
	stanceMap := ecs.NewMap[components.Stance](app.World)
	motionMap := ecs.NewMap[components.Motion](app.World)
	colliderMap := ecs.NewMap[components.Collider](app.World)
	visionMap := ecs.NewMap[components.Vision](app.World)
	suppressionMap := ecs.NewMap[components.Suppression](app.World)
	awarenessMap := ecs.NewMap[components.Awareness](app.World)
	blackboardMap := ecs.NewMap[components.LocalBlackboard](app.World)
	actionQueueMap := ecs.NewMap[components.ActionQueue](app.World)
	equipmentMap := ecs.NewMap[components.Equipment](app.World)
	weaponMap := ecs.NewMap[components.Weapon](app.World)
	ownedByMap := ecs.NewMap[components.OwnedBy](app.World)
	squadMemberMap := ecs.NewMap[components.SquadMember](app.World)
	rosterMap := ecs.NewMap[components.CommandRoster](app.World)
	formationDataMap := ecs.NewMap[components.FormationData](app.World)
	macroPathMap := ecs.NewMap[components.MacroPath](app.World)
	// Phase 11 order maps. main.go reads (no mutation) for UI render. All
	// writes flow through SquadService.IssueOrder / CancelAllOrders.
	orderQueueMap := ecs.NewMap[components.OrderQueueHead](app.World)
	orderKindMap := ecs.NewMap[components.OrderKind](app.World)
	orderStateMap := ecs.NewMap[components.OrderState](app.World)
	orderTargetMap := ecs.NewMap[components.OrderTarget](app.World)
	orderProgressMap := ecs.NewMap[components.OrderProgress](app.World)
	orderChainMap := ecs.NewMap[components.OrderChain](app.World)
	unitEnts := make([]ecs.Entity, 0, len(unitPositions))
	for _, p := range unitPositions {
		ent := app.World.NewEntity()
		wp := components.WorldPos{}.Add(p)
		posMap.Add(ent, &wp)
		unitMap.Add(ent, &components.Unit{})
		stanceMap.Add(ent, &components.Stance{Code: components.StanceStand})
		motionMap.Add(ent, &components.Motion{})
		colliderMap.Add(ent, &components.Collider{Radius: 0.35})
		visionMap.Add(ent, &components.Vision{RangeM: 40, AngleDot: 0.5})
		suppressionMap.Add(ent, &components.Suppression{})
		awarenessMap.Add(ent, &components.Awareness{})
		blackboardMap.Add(ent, &components.LocalBlackboard{})
		actionQueueMap.Add(ent, &components.ActionQueue{})
		// Phase 11.5 P6: units no longer carry LOD markers. Simulation systems
		// (UnitMovement / Vision / Formation / SquadMacroPath / OrderResolver)
		// iterate every unit each tick; LODSystem also excludes the Unit
		// archetype to avoid thrashing markers that nothing reads.

		weapon := app.World.NewEntity()
		weaponMap.Add(weapon, &components.Weapon{
			Kind: components.WeaponAK47, Ammo: 30, RangeM: 200, RoF: 10, Damage: 30,
		})
		ownedByMap.Add(weapon, &components.OwnedBy{Owner: ent})
		wpW := wp
		posMap.Add(weapon, &wpW)
		equipmentMap.Add(ent, &components.Equipment{Primary: weapon, Active: weapon})

		unitEnts = append(unitEnts, ent)
	}

	squadService.CreateFromUnits(unitEnts[0:4], components.FormationLine)
	squadService.CreateFromUnits(unitEnts[4:8], components.FormationWedge)
	squadService.CreateFromUnits(unitEnts[8:12], components.FormationColumn)

	// Render filters.
	unitRenderFilter := ecs.NewFilter3[components.WorldPos, components.Unit, components.Stance](app.World)
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)
	wallRenderFilter := ecs.NewFilter2[components.WorldPos, components.WallSegment](app.World)
	floorRenderFilter := ecs.NewFilter2[components.WorldPos, components.Floor](app.World)
	stairsRenderFilter := ecs.NewFilter2[components.WorldPos, components.Stairs](app.World)
	navOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverSlotFilter := ecs.NewFilter2[components.WorldPos, components.CoverSlot](app.World)
	navGridChunkFilter := ecs.NewFilter1[components.NavGrid](app.World)
	floorNavFilter := ecs.NewFilter3[components.WorldPos, components.Floor, components.FloorNavGrid](app.World)
	visionAwareFilter := ecs.NewFilter2[components.WorldPos, components.Awareness](app.World).
		With(ecs.C[components.Unit]())

	chunkAllFilter := ecs.NewFilter1[components.TerrainChunk](app.World)
	weaponFilter := ecs.NewFilter1[components.Weapon](app.World)
	unitFilter := ecs.NewFilter1[components.Unit](app.World)
	stairsCountFilter := ecs.NewFilter1[components.Stairs](app.World)
	squadFilter := ecs.NewFilter2[components.Squad, components.CommandRoster](app.World)

	// Phase 11 hit-test filters. The Building filter is the same archetype as
	// buildingMap; the Trench-root filter walks the small startup-spawned set.
	buildingFilter := ecs.NewFilter1[components.Building](app.World)
	trenchRootFilter := ecs.NewFilter1[components.TrenchRoot](app.World)
	hitTester := &HitTester{
		BuildingFilter:  buildingFilter,
		BuildingMap:     buildingMap,
		TrenchRootMap:   trenchRootMap,
		TrenchRoots:     trenchRootFilter,
		Trenches:        &trenches,
		TrenchHitRadius: 2.5,
	}

	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

	// Phase 10 UI scaffold.
	screenW, screenH := initialScreenWidth, initialScreenHeight
	panelMgr := ui.NewPanelManager()
	panelMgr.Recompute(screenW, screenH)
	scene3DRT := ui.NewScene3DRT(panelMgr.Get(ui.Panel3D))
	defer scene3DRT.Unload()

	// Pre-bake the map underlay. 2 km × 2 km centred at origin, 4 m / pixel
	// (500×500 = 250 KB upload). Blocking; runs once at startup before the
	// main loop kicks off.
	underlay := ui.BakeUnderlay(0, 0, 2000, 4, func(wx, wz float32) float32 {
		return systems.GroundHeight(wx, wz)
	})
	defer underlay.Unload()
	mapCam := ui.NewMapCamera()
	var mapPanning bool
	var mapPanCursor rl.Vector2
	var pieMenu ui.PieMenu
	// Inter-frame smoothing for map squad markers (ISSUES #1 polish).
	smoothedSquadPos := make(map[ecs.Entity]components.WorldPos, 8)

	// Selection / hover state. Hover refreshes each frame from cursor + focused
	// panel; `hovered` is consumed by the inspector and the map renderer.
	var (
		navPath        []components.WorldPos
		selected       []ecs.Entity
		hovered        ecs.Entity
		marqueeStart   rl.Vector2 // screen coords
		marqueeActive  bool
		marqueeOrigin  ui.PanelID
		expandedHUDOn  bool
		showMapDebugLy bool // toggled per-frame by hold-G
		binds          [5]bindEntry
	)
	const marqueeClickThreshold float32 = 5

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

	for !rl.WindowShouldClose() {
		// Window resize → re-layout + re-alloc the 3D RT to the new bounds.
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

		cursor := rl.GetMousePosition()
		focused := panelMgr.FocusedAt(cursor)
		shiftHeld := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
		ctrlHeld := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)

		panel3D := panelMgr.Get(ui.Panel3D)
		panelMap := panelMgr.Get(ui.PanelMap)

		// ── Tab → toggle layout preset ──
		if rl.IsKeyPressed(rl.KeyTab) {
			panelMgr.TogglePreset()
			panelMgr.Recompute(screenW, screenH)
			scene3DRT.EnsureSize(panelMgr.Get(ui.Panel3D))
			panel3D = panelMgr.Get(ui.Panel3D)
			panelMap = panelMgr.Get(ui.PanelMap)
		}

		// ── Space → toggle pause; +/− → cycle speed 1→2→4→8→1 ──
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

		// ── WASD anchor (Panel3D-or-none focus) ──
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

		// Map's content rect — the actual drawing surface, minus chrome. Cursor
		// conversions go through this rather than panelMap.Bounds so clicks /
		// zoom pivots align with what the player sees.
		panelMapContent := ui.ContentRect(panelMap)

		// ── Map pan / zoom (only when map focused) ──
		if focused == ui.PanelMap {
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

		// ── 3D panel cursor (content-rect-local) ──
		// Cursor coords used for raycast / marquee / picking are relative to
		// the 3D content rect (panel minus chrome), and viewW/H match the
		// content rect — same as the RT — so GetScreenToWorldRayEx /
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

		// ── LMB press ──
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
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

		// ── LMB release → commit marquee or treat as a 3D click ──
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
					} else if !shiftHeld {
						selected = nil
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

		// ── RMB orders (3D or map). Two paths:
		//   • Tap: PieMenu stays inactive, hit-test resolver runs at release.
		//   • Hold > 200 ms: PieMenu activates, sweep cursor for kind, commit
		//     on release. Kind override overrides hit-test mapping.
		// Note: PieMenu suppresses OrbitSystem's RMB-orbit because it captures
		// the press inside Panel3D too — fine, the camera doesn't spin during
		// the menu interaction. After menu release, RMB is no longer held and
		// the orbit doesn't catch the trailing frame either.
		if rl.IsMouseButtonPressed(rl.MouseButtonRight) {
			var (
				pressTarget components.WorldPos
				targetOK    bool
			)
			switch focused {
			case ui.Panel3D:
				pressTarget, targetOK = mouseTargetWorldPos(systems.CurrentCamera,
					anchorPos.ToRenderSpace(systems.CurrentOriginChunk),
					panel3DLocal, panel3DW, panel3DH)
			case ui.PanelMap:
				pressTarget = ui.MapPanelToWorld(cursor, mapCam, panelMapContent)
				targetOK = true
			}
			if targetOK && len(selected) > 0 && (focused == ui.Panel3D || focused == ui.PanelMap) {
				pieMenu.Begin(cursor, pressTarget, focused)
			}
		}

		// While RMB is held the menu may activate, get drag-cancelled, or
		// commit on release. Tick returns one of three outcomes; only commit
		// and tap issue orders.
		if pieMenu.SourcePanel != ui.PanelNone {
			rmbDown := rl.IsMouseButtonDown(rl.MouseButtonRight)
			rmbReleased := rl.IsMouseButtonReleased(rl.MouseButtonRight)
			res := pieMenu.Tick(cursor, rmbDown, rmbReleased)
			switch {
			case res.ReleasedAsCommit:
				k := res.Kind
				resolveRMBOrder(selected, pieMenu.Target, shiftHeld, &k, hitTester,
					squadService, navService, squadMemberMap, posMap, actionQueueMap)
				pieMenu.SourcePanel = ui.PanelNone
			case res.ReleasedAsTap:
				resolveRMBOrder(selected, pieMenu.Target, shiftHeld, nil, hitTester,
					squadService, navService, squadMemberMap, posMap, actionQueueMap)
				pieMenu.SourcePanel = ui.PanelNone
			case res.Cancelled, res.ReleasedAsDrag:
				pieMenu.SourcePanel = ui.PanelNone
			}
		}

		// ── H → Stop order (global hotkey) ──
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

		// ── T → form Squad ──
		if rl.IsKeyPressed(rl.KeyT) && len(selected) >= 2 {
			newSquad := squadService.CreateFromUnits(selected, components.FormationLine)
			if newSquad != (ecs.Entity{}) && app.World.Alive(newSquad) {
				if r := rosterMap.Get(newSquad); r != nil {
					selected = append(selected[:0], r.Members[:r.Count]...)
				}
			}
		}

		// ── U → ungroup ──
		if rl.IsKeyPressed(rl.KeyU) && len(selected) > 0 {
			for _, e := range selected {
				squadService.Leave(e)
			}
		}

		// ── F1-F4 → change formation ──
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

		// ── Ctrl+1..5 bind / 1..5 recall ──
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

		// ── X → crater (Panel3D only) ──
		if focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
		}

		if rl.IsKeyPressed(rl.KeyP) {
			if ctrlHeld {
				app.Prof.PrintSnapshot()
				app.Trace.Mark("snapshot")
			} else {
				expandedHUDOn = !expandedHUDOn
			}
		}

		// ── Hover update ──
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

		// Gate the 3D camera's orbit / wheel zoom by panel focus. Wheel events
		// when the map panel is focused belong to the map's own zoom; RMB held
		// while drawing a map marquee shouldn't spin the field camera. Also
		// suppress while pie menu is active so the camera doesn't drift as the
		// player sweeps cursor to pick a segment.
		systems.OrbitInputEnabled = (focused == ui.Panel3D || focused == ui.PanelNone) &&
			!pieMenu.IsActive() && pieMenu.SourcePanel == ui.PanelNone

		app.Tick(dtReal)

		anchorPos = posMap.Get(anchor)
		anchorRender := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)

		// ── Render 3D scene into RT ──
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
			drawUnitCube(renderPos, *st)
			ent := qu.Entity()
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

		floorLive := 0
		qf := floorRenderFilter.Query()
		for qf.Next() {
			pos, fl := qf.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingFloor(renderPos, *fl)
			floorLive++
		}
		wallLive := 0
		qw := wallRenderFilter.Query()
		for qw.Next() {
			pos, ws := qw.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingWall(renderPos, *ws)
			wallLive++
		}
		qst := stairsRenderFilter.Query()
		for qst.Next() {
			pos, st := qst.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawBuildingStairs(renderPos, *st)
		}

		// Debug overlays — all gated by hold-key. The hold-G road overlay also
		// toggles the map's road / river / building debug layer for consistency.
		if rl.IsKeyDown(rl.KeyG) {
			drawRoadGraphDebug(&roadGraph)
			showMapDebugLy = true
		} else {
			showMapDebugLy = false
		}
		if rl.IsKeyDown(rl.KeyN) {
			qNav := navOverlayFilter.Query()
			for qNav.Next() {
				pos, cc, grid, hm := qNav.Get()
				drawNavGridOverlay(*pos, *cc, grid, hm)
			}
		}
		if rl.IsKeyDown(rl.KeyC) {
			qCov := coverOverlayFilter.Query()
			for qCov.Next() {
				pos, cc, cov, hm := qCov.Get()
				drawCoverMapOverlay(*pos, *cc, cov, hm)
			}
		}

		visionPairs := 0
		if rl.IsKeyDown(rl.KeyY) {
			qV := visionAwareFilter.Query()
			for qV.Next() {
				pos, aware := qV.Get()
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
			drawSquadConnections(centerRender, memberPos, squadColor(squadEnt.ID()))
		}

		if rl.IsKeyDown(rl.KeyF) {
			qFloor := floorNavFilter.Query()
			for qFloor.Next() {
				pos, _, grid := qFloor.Get()
				drawFloorNavOverlay(*pos, grid)
			}
		}

		coverSlotLive := 0
		if rl.IsKeyDown(rl.KeyV) {
			qSlot := coverSlotFilter.Query()
			for qSlot.Next() {
				pos, slot := qSlot.Get()
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

		rl.EndMode3D()
		rl.EndTextureMode()

		// ── 2D pass — clear bg, paint each panel ──
		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 8, G: 10, B: 14, A: 255})

		// Panel content (background + body) drawn before chrome so the title
		// bar overlays the content cleanly.
		// Map panel.
		mapCtx := ui.MapRenderCtx{
			World:           app.World,
			Cam:             mapCam,
			Underlay:        &underlay,
			AnchorPos:       *anchorPos,
			Selected:        selected,
			Hovered:         hovered,
			PosMap:          posMap,
			RosterMap:       rosterMap,
			SquadMemberMap:  squadMemberMap,
			SquadFilter:     squadFilter,
			SquadCenter:     squadCenter,
			SquadColor:      squadColor,
			RoadGraph:       &roadGraph,
			Rivers:          &rivers,
			Buildings:       &buildingPlans,
			ShowDebugLayers: showMapDebugLy,
			OrderQueueMap:    orderQueueMap,
			OrderKindMap:     orderKindMap,
			OrderTargetMap:   orderTargetMap,
			OrderChainMap:    orderChainMap,
			SmoothedSquadPos: smoothedSquadPos,
			MapMarkerCache:   &mapMarkerCache,
		}
		ui.DrawMap(panelMap, mapCtx)

		// Inspector panel.
		ui.DrawInspector(panelMgr.Get(ui.PanelInspect), ui.InspectorCtx{
			World:            app.World,
			Selected:         selected,
			Hovered:          hovered,
			Font:             hudFont,
			PosMap:           posMap,
			StanceMap:        stanceMap,
			MotionMap:        motionMap,
			SuppressionMap:   suppressionMap,
			EquipmentMap:     equipmentMap,
			SquadMemberMap:   squadMemberMap,
			RosterMap:        rosterMap,
			FormationDataMap: formationDataMap,
			MacroPathMap:     macroPathMap,
			SquadFilter:      squadFilter,
			OrderQueueMap:    orderQueueMap,
			OrderKindMap:     orderKindMap,
			OrderStateMap:    orderStateMap,
			OrderTargetMap:   orderTargetMap,
			OrderProgressMap: orderProgressMap,
			OrderChainMap:    orderChainMap,
			BuildingMap:      buildingMap,
			TrenchRootMap:    trenchRootMap,
			SquadColor:       squadColor,
		})

		// Time panel.
		ui.DrawTimePanel(panelMgr.Get(ui.PanelTime), hudFont, ui.TimeDisplay{
			Scale:   app.TimeScale,
			Elapsed: float32(app.Elapsed().Seconds()),
		})

		// 3D RT composite into Panel3D bounds.
		scene3DRT.Composite(panel3D)

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
		for _, id := range []ui.PanelID{ui.Panel3D, ui.PanelMap, ui.PanelInspect, ui.PanelTime} {
			ui.DrawChrome(panelMgr.Get(id), hudFont, 14)
		}

		// Pie menu (RMB-hold overlay, M11.6). Drawn after chrome so it sits
		// above every panel.
		pieMenu.Draw(hudFont, cursor)

		// HUD hotkey hints moved into expanded profiler HUD (toggle with P).
		// Phase 10: drawing them over the panel chrome on every frame conflicts
		// with each panel's title bar; they're discoverable on demand instead.

		// Profiler HUD — collapsed always, expanded behind P toggle.
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

// nextTimeScale advances the speed multiplier through 1 → 2 → 4 → 8 → 1
// (step=+1) or backwards (step=-1). When currently paused, advancing forward
// jumps to 1×; advancing back jumps to 8×. Used by the +/- hotkey.
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
