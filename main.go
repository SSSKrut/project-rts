package main

import (
	"fmt"
	"math"
	"runtime"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

const (
	screenWidth  int32 = 800 * 2
	screenHeight int32 = 450 * 2
)

func main() {
	rl.InitWindow(screenWidth, screenHeight, "RTS/FPS 3D ECS Prototype")
	defer rl.CloseWindow()

	// HUD font (Phase 7.5). Try common monospace paths; fall back to raylib's
	// default bitmap font if nothing loads. The atlas is generated once at
	// hudFontAtlasSize so DrawTextEx at slightly smaller sizes (16/18) stays
	// crisp via subpixel downscale.
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

	// Trace file (M7.5.5). No-op on normal builds; on `-tags trace` parses
	// the -trace=path.jsonl flag and opens the JSONL writer.
	initTrace(app)
	defer func() { _ = app.Trace.Close() }()

	// Singleton resources MUST be registered before any system InitUI runs —
	// systems grab a Resource[T] handle there and panic if missing.
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

	// Defers run LIFO; this fires before window/audio teardown — while
	// app.World is still alive — flushing any in-memory chunk modifications.
	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	// Pipeline order matters:
	//   1. terrain_streaming  — spawn/evict chunk entities
	//   2. terrain_load       — fill HeightmapDirty from disk if a save exists
	//   3. terrain_gen        — fill remaining HeightmapDirty via procgen
	//   4. river              — cut + water-props (after gen, before mesh & props)
	//   5. road               — flatten + road/bridge/junction props
	//   6. building           — bunker RectCut + walls/floors/stairs spawn
	//   7. trench             — earthworks polyline cut
	//   8. prop_spawn         — vegetation/rocks (sees all clearance)
	//   9. spatial_bake       — NavGrid / CoverMap / cover slots
	//  10. terrain_mesh       — build & upload GPU mesh
	//  11. ground_stick       — clamp anchor Y to surface
	//  12. lod                — units-only LOD
	//  13. movement
	//  14. spatial_audio
	//  15. streaming          — node graph (smart-spaces; not terrain)
	//  16. orbit              — camera input
	//  17. camera             — sync ECS camera to systems.CurrentCamera
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

	unitMovementSys := &systems.UnitMovementSystem{}
	unitMovementSys.InitUI(app.World)

	visionSys := &systems.VisionSystem{}
	visionSys.InitUI(app.World)

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

	stamper := systems.NewStamper(app.World)
	navService := systems.NewNavService(app.World)

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
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(audioSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	posMap := ecs.NewMap[components.WorldPos](app.World)
	lodActiveMap := ecs.NewMap[components.LODActive](app.World)
	lodRelevantMap := ecs.NewMap[components.LODRelevant](app.World)
	lodAnchorMap := ecs.NewMap[components.LODAnchor](app.World)
	alwaysActiveMap := ecs.NewMap[components.AlwaysActive](app.World)

	// Anchor at world origin. GroundStickSystem clamps Local.Y on every tick.
	anchor := app.World.NewEntity()
	posMap.Add(anchor, &components.WorldPos{})
	lodActiveMap.Add(anchor, &components.LODActive{})
	lodAnchorMap.Add(anchor, &components.LODAnchor{})
	alwaysActiveMap.Add(anchor, &components.AlwaysActive{})

	camCompMap := ecs.NewMap[components.Camera](app.World)
	orbitMap := ecs.NewMap[components.OrbitController](app.World)
	activeCamMap := ecs.NewMap[components.ActiveCamera](app.World)

	camEnt := app.World.NewEntity()
	// Camera world position: same chunk as anchor. OrbitSystem overwrites it
	// from spherical coords on the first tick.
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

	// Building roots — one entity per BuildingPlan. Root carries Building +
	// WorldPos + AlwaysActive so it survives chunk eviction; child entities
	// (walls/floors/...) are spawned by BuildingSystem on chunk presence and
	// torn down on eviction via BuildingChildIndex.
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

	// Phase 7 test scene: 12 hardcoded soldiers in three clusters around the
	// existing buildings/road/bunker. Each unit gets a Weapon entity tied via
	// Equipment.Primary + OwnedBy. Cubes from Phase 0/1 are gone — they were
	// placeholders for "mobile things" before GroundStick existed.
	unitPositions := []rl.Vector3{
		// Cluster 1 — around house 1 (-25, -40).
		{X: -22, Z: -38},
		{X: -28, Z: -38},
		{X: -22, Z: -42},
		{X: -28, Z: -42},
		// Cluster 2 — between house 2 (40, 30) and the road.
		{X: 38, Z: 28},
		{X: 42, Z: 28},
		{X: 38, Z: 32},
		{X: 42, Z: 32},
		// Cluster 3 — near the bunker (-30, 55).
		{X: -28, Z: 52},
		{X: -32, Z: 52},
		{X: -28, Z: 58},
		{X: -32, Z: 58},
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
	for _, p := range unitPositions {
		ent := app.World.NewEntity()
		wp := components.WorldPos{}.Add(p)
		posMap.Add(ent, &wp)
		unitMap.Add(ent, &components.Unit{})
		stanceMap.Add(ent, &components.Stance{Code: components.StanceStand})
		motionMap.Add(ent, &components.Motion{})
		colliderMap.Add(ent, &components.Collider{Radius: 0.35})
		visionMap.Add(ent, &components.Vision{RangeM: 40, AngleDot: 0.5}) // ~120° cone
		suppressionMap.Add(ent, &components.Suppression{})
		awarenessMap.Add(ent, &components.Awareness{})
		blackboardMap.Add(ent, &components.LocalBlackboard{})
		actionQueueMap.Add(ent, &components.ActionQueue{})
		lodRelevantMap.Add(ent, &components.LODRelevant{})

		weapon := app.World.NewEntity()
		weaponMap.Add(weapon, &components.Weapon{
			Kind: components.WeaponAK47, Ammo: 30, RangeM: 200, RoF: 10, Damage: 30,
		})
		ownedByMap.Add(weapon, &components.OwnedBy{Owner: ent})
		wpW := wp
		posMap.Add(weapon, &wpW)
		equipmentMap.Add(ent, &components.Equipment{Primary: weapon, Active: weapon})
	}

	// Render filters. Units are drawn through their own (Unit + WorldPos +
	// Stance) filter — Weapon entities also carry WorldPos but skip
	// the unit renderer (they're invisible placeholders in Phase 7).
	unitRenderFilter := ecs.NewFilter3[components.WorldPos, components.Unit, components.Stance](app.World)
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	// Single naive iteration over all props regardless of LOD tier (in
	// practice every prop is LODRelevant). Move to instanced rendering when
	// 5k+ props in frustum cause spikes.
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)
	wallRenderFilter := ecs.NewFilter2[components.WorldPos, components.WallSegment](app.World)
	floorRenderFilter := ecs.NewFilter2[components.WorldPos, components.Floor](app.World)
	stairsRenderFilter := ecs.NewFilter2[components.WorldPos, components.Stairs](app.World)
	// NavGrid debug overlay (key N). Restricted to active-tier chunks via the
	// LODActive marker — drawing 4096 cells × 50 chunks every frame is too
	// many DrawCubeV calls; the active ring (~49 chunks) is already heavy and
	// is what the player can usefully inspect.
	navOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverOverlayFilter := ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap](app.World).
		With(ecs.C[components.LODActive]())
	coverSlotFilter := ecs.NewFilter2[components.WorldPos, components.CoverSlot](app.World)
	navGridChunkFilter := ecs.NewFilter1[components.NavGrid](app.World)
	floorNavFilter := ecs.NewFilter3[components.WorldPos, components.Floor, components.FloorNavGrid](app.World)
	visionAwareFilter := ecs.NewFilter2[components.WorldPos, components.Awareness](app.World).
		With(ecs.C[components.Unit]())

	// Phase 7.5 HUD census filters. Cheap — these archetypes are tiny and
	// we only iterate while hold-P expanded HUD is up. Heap / entity-count
	// from world.Stats() are rate-limited to 1 Hz inside the main loop.
	chunkAllFilter := ecs.NewFilter1[components.TerrainChunk](app.World)
	weaponFilter := ecs.NewFilter1[components.Weapon](app.World)
	unitFilter := ecs.NewFilter1[components.Unit](app.World)
	stairsCountFilter := ecs.NewFilter1[components.Stairs](app.World)

	// Default material shared by every chunk DrawMesh. Loaded once after the
	// GL context exists; per-vertex colour does the LOD-tier debug shading.
	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

	// Active path for the anchor's RMB navigation when nothing is selected.
	var navPath []components.WorldPos

	// Selection state. `selected` is an ad-hoc group of Unit entities; it has
	// no ECS representation and dies with the next click. Phase 9's Squad
	// component is the persistent equivalent. The marquee is drawn in 2D after
	// EndMode3D when `marqueeActive` is true.
	var selected []ecs.Entity
	var marqueeStart rl.Vector2
	var marqueeActive bool
	const marqueeClickThreshold float32 = 5

	// Profiler expanded HUD toggle (M7.5.3). Flipped on plain P press; Ctrl+P
	// is the separate snapshot-to-stdout action.
	var expandedHUDVisible bool

	// Helpers for the selection state — keep them local to the main loop so
	// they close over the world's filters / maps without polluting global ns.
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

	for !rl.WindowShouldClose() {
		dt := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))

		// Anchor movement (WASD), driven by orbit yaw so W is always "into
		// the screen" no matter how the camera is rotated. Forward (camera →
		// anchor in XZ) is (-sin yaw, 0, -cos yaw); Right = Forward × Up =
		// (cos yaw, 0, -sin yaw). Y is overwritten by GroundStickSystem.
		anchorPos := posMap.Get(anchor)
		anchorSpeed := float32(20.0)
		if rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift) {
			anchorSpeed *= 4.0
		}
		orbit := orbitMap.Get(camEnt)
		sy := float32(math.Sin(float64(orbit.Yaw)))
		cy := float32(math.Cos(float64(orbit.Yaw)))
		var inFwd, inRight float32
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
		wasdActive := inFwd != 0 || inRight != 0
		if wasdActive {
			if mag := float32(math.Sqrt(float64(inFwd*inFwd + inRight*inRight))); mag > 1 {
				inFwd /= mag
				inRight /= mag
			}
			step := anchorSpeed * float32(dt.Seconds())
			move := rl.Vector3{
				X: step * (inFwd*(-sy) + inRight*cy),
				Z: step * (inFwd*(-cy) + inRight*(-sy)),
			}
			*anchorPos = anchorPos.Add(move)
			// WASD overrides any in-flight nav. The previous click is lost on
			// purpose — direct control is the player's veto.
			navPath = nil
		}

		shiftHeld := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)

		// ── LMB → selection ──
		// Press → start marquee. Release → either treat as a click (small
		// movement) or commit the marquee rectangle. Shift modifier toggles
		// individual units / unions a marquee with the current selection.
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			marqueeStart = rl.GetMousePosition()
			marqueeActive = true
		}
		if rl.IsMouseButtonReleased(rl.MouseButtonLeft) && marqueeActive {
			end := rl.GetMousePosition()
			dx := end.X - marqueeStart.X
			dy := end.Y - marqueeStart.Y
			dragDist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			if dragDist < marqueeClickThreshold {
				// Click — raycast to nearest unit.
				if hit, ok := pickUnitFromMouse(unitRenderFilter, *anchorPos); ok {
					if shiftHeld {
						toggleSelected(hit)
					} else {
						selected = []ecs.Entity{hit}
					}
				} else if !shiftHeld {
					selected = nil
				}
			} else {
				// Marquee — collect units whose screen-projected position
				// lies inside the rectangle.
				minX, maxX := marqueeStart.X, end.X
				if maxX < minX {
					minX, maxX = maxX, minX
				}
				minY, maxY := marqueeStart.Y, end.Y
				if maxY < minY {
					minY, maxY = maxY, minY
				}
				hits := collectUnitsInRect(unitRenderFilter, minX, maxX, minY, maxY)
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
			marqueeActive = false
		}

		// ── RMB → MoveTo order (selected units) OR anchor pathing fallback ──
		if rl.IsMouseButtonPressed(rl.MouseButtonRight) {
			if target, ok := mouseTargetWorldPos(systems.CurrentCamera,
				anchorPos.ToRenderSpace(systems.CurrentOriginChunk)); ok {
				if len(selected) > 0 {
					for _, e := range selected {
						aq := actionQueueMap.Get(e)
						pos := posMap.Get(e)
						if aq == nil || pos == nil {
							continue
						}
						// Per-unit MoveTo: plan from the unit's current pos
						// so each one gets its own path. We just push the
						// final waypoint as the action target — separation
						// steering handles the close-quarters spread.
						if !shiftHeld {
							systems.ClearActions(aq)
						}
						path := navService.FindPath(*pos, target, systems.NavOpts{
							Locomotion: components.LocomotionFoot,
						})
						if len(path) == 0 {
							// Direct MoveTo even if A* gave up — better than
							// stalling silently.
							systems.PushAction(aq, components.Action{
								Kind: components.ActionMoveTo, Target: target,
							})
						} else {
							for _, wp := range path {
								systems.PushAction(aq, components.Action{
									Kind: components.ActionMoveTo, Target: wp,
								})
							}
						}
					}
				}
			}
		}

		// ── H → Stop order for the selected group ──
		// (S is taken by WASD anchor movement; H = halt, conflict-free.)
		if rl.IsKeyPressed(rl.KeyH) && len(selected) > 0 {
			for _, e := range selected {
				if aq := actionQueueMap.Get(e); aq != nil {
					systems.ClearActions(aq)
					systems.PushAction(aq, components.Action{Kind: components.ActionStop})
				}
			}
		}

		// Anchor path follow. WASD wins; selection-driven orders go to units.
		if !wasdActive && len(navPath) > 0 {
			navPath = stepAlongPath(anchorPos, navPath, anchorSpeed*float32(dt.Seconds()))
		}

		if rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
		}

		// P (no Ctrl) → toggle expanded profiler HUD.
		// Ctrl+P → stdout snapshot dump (M7.5.4). Both are press-edge, not
		// hold, so the two branches are mutually exclusive on the Ctrl gate.
		if rl.IsKeyPressed(rl.KeyP) {
			if rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl) {
				app.Prof.PrintSnapshot()
				app.Trace.Mark("snapshot")
			} else {
				expandedHUDVisible = !expandedHUDVisible
			}
		}

		app.Tick(dt)

		// Re-fetch in case archetype mutations during Tick invalidated the
		// previous pointer (defensive).
		anchorPos = posMap.Get(anchor)
		anchorRender := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)

		rl.BeginMode3D(systems.CurrentCamera)

		// Terrain. The chunk's WorldPos is at its (0,0,0) corner; the mesh's
		// local vertices already span [0, ChunkSize]. Per-draw matrix +
		// DrawMesh (NOT DrawModel — see ChunkMesh comment).
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

		// Phase 7 placeholder soldier: olive cube whose height follows Stance.
		// Highlight selected units with a cyan circle + wires.
		unitsLive := 0
		qu := unitRenderFilter.Query()
		for qu.Next() {
			pos, _, st := qu.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			drawUnitCube(renderPos, *st)
			if isSelected(qu.Entity()) >= 0 {
				height := unitStanceHeight(st.Code)
				rl.DrawCircle3D(renderPos, 1.0, rl.Vector3{X: 1, Y: 0, Z: 0}, 90,
					rl.Color{R: 0, G: 220, B: 220, A: 255})
				c := rl.Vector3{X: renderPos.X, Y: renderPos.Y + height*0.5, Z: renderPos.Z}
				rl.DrawCubeWiresV(c, rl.Vector3{X: 0.7, Y: height + 0.1, Z: 0.7},
					rl.Color{R: 0, G: 220, B: 220, A: 255})
			}
			unitsLive++
		}

		// Props. Bridge tally is computed live (rather than as a static
		// count) so it actually drops to zero when the host chunk evicts —
		// visual proof for "bridges follow chunk lifecycle".
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

		// Floors first so walls / stairs draw above without z-fighting.
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

		// Road-graph debug overlay. Hold G to draw the whole graph (lines +
		// node markers) on top of the scene — useful for sanity-checking
		// preprocessing and bridge detection without walking to every chunk.
		if rl.IsKeyDown(rl.KeyG) {
			drawRoadGraphDebug(&roadGraph)
		}

		// NavGrid debug overlay. Hold N — for every active-tier chunk, draw
		// each cell as a flat coloured plate at surfaceY+0.05. Heavy (~50
		// chunks × 4096 cells), so only on demand.
		if rl.IsKeyDown(rl.KeyN) {
			qNav := navOverlayFilter.Query()
			for qNav.Next() {
				pos, cc, grid, hm := qNav.Get()
				drawNavGridOverlay(*pos, *cc, grid, hm)
			}
		}

		// CoverMap debug overlay. Hold C — translucent blue plate per cell,
		// alpha proportional to BaseCover (popcount(DirMask) × 32).
		if rl.IsKeyDown(rl.KeyC) {
			qCov := coverOverlayFilter.Query()
			for qCov.Next() {
				pos, cc, cov, hm := qCov.Get()
				drawCoverMapOverlay(*pos, *cc, cov, hm)
			}
		}

		// Vision debug overlay. Hold Y — thin green line between every pair
		// of units (a, b) where b ∈ a.Awareness.LastSeen.
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
			// Cheap census for HUD even when overlay is off.
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

		// FloorNavGrid debug overlay. Hold F — for every Floor entity, draw
		// the baked FloorNavGrid cells as small plates lifted just above the
		// floor surface.
		if rl.IsKeyDown(rl.KeyF) {
			qFloor := floorNavFilter.Query()
			for qFloor.Next() {
				pos, _, grid := qFloor.Get()
				drawFloorNavOverlay(*pos, grid)
			}
		}

		// Cover-slot debug overlay. Hold V — small yellow cube at every slot
		// position with a short magenta arrow along OriginDir.
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
			// Cheap census even when overlay is off — used in HUD.
			qSlot := coverSlotFilter.Query()
			for qSlot.Next() {
				qSlot.Get()
				coverSlotLive++
			}
		}

		// Active nav path: magenta line through every remaining waypoint, plus
		// a marker at the next target.
		drawNavPath(navPath, *anchorPos)

		rl.EndMode3D()

		// Marquee rectangle (2D, after EndMode3D).
		if marqueeActive {
			end := rl.GetMousePosition()
			minX, maxX := marqueeStart.X, end.X
			if maxX < minX {
				minX, maxX = maxX, minX
			}
			minY, maxY := marqueeStart.Y, end.Y
			if maxY < minY {
				minY, maxY = maxY, minY
			}
			rl.DrawRectangleLines(int32(minX), int32(minY),
				int32(maxX-minX), int32(maxY-minY),
				rl.Color{R: 0, G: 220, B: 220, A: 255})
			rl.DrawRectangle(int32(minX), int32(minY),
				int32(maxX-minX), int32(maxY-minY),
				rl.Color{R: 0, G: 220, B: 220, A: 40})
		}

		// Left HUD — control hints only. Per-frame entity census moved to the
		// right-side profiler HUD (M7.5.3). Vertical step = 24 px so 18-pt
		// NotoSansMono ascenders/descenders don't crash into the next line;
		// the default raylib bitmap font was forgiving on this, TTF isn't.
		drawHUDText(hudFont, "RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		drawHUDText(hudFont, "WASD = anchor; Shift = sprint; RMB-drag = orbit; wheel = zoom", 10, 40, 18, rl.DarkGray)
		drawHUDText(hudFont, "LMB-click/drag = select units; Shift+LMB = add/toggle; RMB = MoveTo; H = halt", 10, 64, 18, rl.DarkGray)
		drawHUDText(hudFont, "X = crater; G/N/C/V/F (hold) = road / nav / cover / cover-slot / floor-nav overlays", 10, 88, 18, rl.DarkGray)
		drawHUDText(hudFont, "P = toggle profiler details; Ctrl+P = snapshot to stdout", 10, 112, 18, rl.DarkGray)

		// Profiler HUD. Heap + entity count refresh at 1 Hz — ReadMemStats /
		// World.Stats() are stop-the-world-ish and we only need them on the
		// human-readable scale.
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

		// Census shared by HUD and trace. The Filter1 iterations are over
		// small archetypes (units / weapons / stairs / nav chunks) so this is
		// sub-µs in normal use; only when the player loads thousands of units
		// would we want to gate it behind hold-P.
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
			units:        countFilter1(unitFilter),
			weapons:      countFilter1(weaponFilter),
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

		drawCollapsedProfHUD(&app.Prof, screenWidth, hudFont)
		if expandedHUDVisible {
			drawExpandedProfHUD(&app.Prof, screenWidth, cen, hudFont)
		}

		rl.EndDrawing()

		// Build trace record after the frame is on screen — gives us the
		// final FPS / frame_ms for this frame and matches the on-disk
		// "this is what frame N looked like" semantics. No-op on builds
		// without -tags trace.
		recordTraceFrame(app, rl.GetFrameTime()*1000, rl.GetFPS(), cen)
		handleTraceHotkeys(app)
	}
}
