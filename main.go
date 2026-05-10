package main

import (
	"fmt"
	"math"
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
	//   9. terrain_mesh       — build & upload GPU mesh
	//  10. ground_stick       — clamp anchor Y to surface
	//  11. lod                — units-only LOD
	//  12. movement
	//  13. spatial_audio
	//  14. streaming          — node graph (smart-spaces; not terrain)
	//  15. orbit              — camera input
	//  16. camera             — sync ECS camera to systems.CurrentCamera
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

	terrainMeshSys := &systems.TerrainMeshSystem{}
	terrainMeshSys.InitUI(app.World)

	groundStickSys := &systems.GroundStickSystem{}
	groundStickSys.InitUI(app.World)

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

	app.AddSystem(terrainStreamingSys)
	app.AddSystem(terrainLoadSys)
	app.AddSystem(terrainGenSys)
	app.AddSystem(riverSys)
	app.AddSystem(roadSys)
	app.AddSystem(buildingSys)
	app.AddSystem(trenchSys)
	app.AddSystem(propSpawnSys)
	app.AddSystem(terrainMeshSys)
	app.AddSystem(groundStickSys)
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(audioSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	posMap := ecs.NewMap[components.WorldPos](app.World)
	velMap := ecs.NewMap[components.Velocity3D](app.World)
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

	// Mobile entities at Relevant LOD. WorldPos via Add from origin so any
	// negative offsets fold into the neighbouring chunk.
	for i := 0; i < 300; i++ {
		entity := app.World.NewEntity()
		lodRelevantMap.Add(entity, &components.LODRelevant{})
		wp := components.WorldPos{}.Add(rl.Vector3{
			X: float32(rl.GetRandomValue(-30, 30)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-30, 30)),
		})
		posMap.Add(entity, &wp)
		velMap.Add(entity, &components.Velocity3D{
			X: float32(rl.GetRandomValue(-5, 5)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-5, 5)),
		})
	}

	// Render filters. Unit cubes exclude terrain chunks, props, and building
	// children (each has its own renderer).
	activeRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODActive](app.World).
		Without(
			ecs.C[components.TerrainChunk](),
			ecs.C[components.Prop](),
			ecs.C[components.BuildingMember](),
			ecs.C[components.Building](),
		)
	relevantRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODRelevant](app.World).
		Without(
			ecs.C[components.TerrainChunk](),
			ecs.C[components.Prop](),
			ecs.C[components.BuildingMember](),
			ecs.C[components.Building](),
		)
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	// Single naive iteration over all props regardless of LOD tier (in
	// practice every prop is LODRelevant). Move to instanced rendering when
	// 5k+ props in frustum cause spikes.
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)
	wallRenderFilter := ecs.NewFilter2[components.WorldPos, components.WallSegment](app.World)
	floorRenderFilter := ecs.NewFilter2[components.WorldPos, components.Floor](app.World)
	stairsRenderFilter := ecs.NewFilter2[components.WorldPos, components.Stairs](app.World)

	// Default material shared by every chunk DrawMesh. Loaded once after the
	// GL context exists; per-vertex colour does the LOD-tier debug shading.
	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

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
		if inFwd != 0 || inRight != 0 {
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
		}

		if rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
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
		qcA := chunkActiveFilter.Query()
		for qcA.Next() {
			pos, mesh, _ := qcA.Get()
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
			if !mesh.Uploaded {
				continue
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			xform := rl.MatrixTranslate(renderPos.X, renderPos.Y, renderPos.Z)
			rl.DrawMesh(mesh.Mesh, terrainMaterial, xform)
		}

		rl.DrawCircle3D(anchorRender, 1, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Blue)
		// Active unit entities (red). Cubes do NOT ground-stick — they will
		// appear floating or buried; expected for now.
		q := activeRenderFilter.Query()
		for q.Next() {
			pos, _ := q.Get()
			if q.Entity() == anchor || q.Entity() == camEnt {
				continue
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCube(renderPos, 0.5, 0.5, 0.5, rl.Red)
			rl.DrawCubeWires(renderPos, 0.5, 0.5, 0.5, rl.Maroon)
		}

		// Relevant unit entities (green).
		q2 := relevantRenderFilter.Query()
		for q2.Next() {
			pos, _ := q2.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCube(renderPos, 0.5, 0.5, 0.5, rl.Green)
			rl.DrawCubeWires(renderPos, 0.5, 0.5, 0.5, rl.Maroon)
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

		rl.EndMode3D()

		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("WASD to move Anchor (Blue); Shift to sprint; right-drag to orbit; wheel to zoom", 10, 30, 20, rl.DarkGray)
		rl.DrawText("X = crater at anchor; G (hold) = road graph overlay", 10, 50, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 70, 20, rl.DarkGray)
		hud := fmt.Sprintf("Props live: %d  |  Rivers: %d  |  Bridges live: %d",
			propLive, len(rivers.Polylines), bridgeLive)
		rl.DrawText(hud, 10, 90, 20, rl.DarkGray)
		hud2 := fmt.Sprintf("Roads: nodes=%d edges=%d bridge-edges=%d  |  Buildings: plans=%d walls=%d floors=%d  |  Trenches: %d",
			len(roadGraph.Nodes), len(roadGraph.Edges), bridgeEdges,
			len(buildingPlans.Plans), wallLive, floorLive, len(trenches.Lines))
		rl.DrawText(hud2, 10, 110, 20, rl.DarkGray)

		rl.EndDrawing()
	}
}

// drawRoadGraphDebug overlays the road graph in 3D — one coloured line per
// edge, plus a small marker cube at each node. Colour by RoadKind: white
// Highway, blue Local, brown DirtTrack, yellow Bridge.
func drawRoadGraphDebug(g *components.RoadGraph) {
	for i := range g.Edges {
		e := &g.Edges[i]
		from := g.Nodes[e.From].Pos
		to := g.Nodes[e.To].Pos
		fr := from.ToRenderSpace(systems.CurrentOriginChunk)
		tr := to.ToRenderSpace(systems.CurrentOriginChunk)
		// Lift slightly so the line isn't buried in the road surface.
		fr.Y += 0.5
		tr.Y += 0.5
		var col rl.Color
		switch e.Kind {
		case components.RoadHighway:
			col = rl.White
		case components.RoadLocal:
			col = rl.Blue
		case components.RoadDirtTrack:
			col = rl.Brown
		case components.RoadBridge:
			col = rl.Yellow
		default:
			col = rl.Magenta
		}
		rl.DrawLine3D(fr, tr, col)
	}
	for i := range g.Nodes {
		p := g.Nodes[i].Pos.ToRenderSpace(systems.CurrentOriginChunk)
		p.Y += 0.5
		rl.DrawCube(p, 0.6, 0.6, 0.6, rl.Black)
	}
}

// drawProp renders one placeholder prop primitive. Yaw radians around +Y;
// scale uniform. Position is the prop's *foot* (ground contact), so primitives
// lift themselves to sit on top.
func drawProp(meta components.PropMeta, pos rl.Vector3, yaw, scale float32) {
	switch meta.Primitive {
	case components.PrimitiveCube:
		sx := meta.Size.X * scale
		sy := meta.Size.Y * scale
		sz := meta.Size.Z * scale
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y+sy*0.5, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawCubeV(rl.Vector3{}, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
			rl.PopMatrix()
		} else {
			c := rl.Vector3{X: pos.X, Y: pos.Y + sy*0.5, Z: pos.Z}
			rl.DrawCubeV(c, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
		}

	case components.PrimitiveSphere:
		r := meta.Size.X * scale
		c := rl.Vector3{X: pos.X, Y: pos.Y + r*0.5, Z: pos.Z}
		rl.DrawSphere(c, r, meta.Color)

	case components.PrimitiveCylinder:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, r, 8, meta.Color)

	case components.PrimitiveCone:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, 0, 8, meta.Color)

	case components.PrimitivePlane:
		size := rl.Vector2{X: meta.Size.X * scale, Y: meta.Size.Z * scale}
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawPlane(rl.Vector3{}, size, meta.Color)
			rl.PopMatrix()
		} else {
			rl.DrawPlane(pos, size, meta.Color)
		}

	case components.PrimitiveTree:
		// Trunk + canopy composite. 6-sided is enough at silhouette resolution.
		trunkR := meta.Size.X * scale
		trunkH := meta.Size.Y * scale
		canopyR := meta.Size.Z * scale
		canopyH := trunkH * 1.5
		trunkBase := pos
		trunkTop := rl.Vector3{X: pos.X, Y: pos.Y + trunkH, Z: pos.Z}
		canopyTip := rl.Vector3{X: pos.X, Y: pos.Y + trunkH + canopyH, Z: pos.Z}
		rl.DrawCylinderEx(trunkBase, trunkTop, trunkR, trunkR, 6, meta.TrunkColor)
		rl.DrawCylinderEx(trunkTop, canopyTip, canopyR, 0, 6, meta.Color)
	}
}

// makeStartingRoadGraph builds the Phase-4 test graph: a four-node chain
// (n0 → n1 → n2 → n3) where n1→n2 is intentionally aimed across the first
// river so the preprocessor can split it and tag the middle sub-edge
// RoadBridge. n0→n1 is highway, n2→n3 is dirt track — visual proof that
// kinds survive preprocessing.
func makeStartingRoadGraph() components.RoadGraph {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return components.RoadGraph{
		Nodes: []components.RoadNode{
			{Pos: wp(0, -50)},  // n0 — start, south of river 1
			{Pos: wp(15, -15)}, // n1 — south bank
			{Pos: wp(15, 50)},  // n2 — north bank (n1→n2 crosses river 1)
			{Pos: wp(-50, 80)}, // n3 — far NW
		},
		Edges: []components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4.0},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4.0},
			{From: 2, To: 3, Kind: components.RoadDirtTrack, Width: 2.5},
		},
	}
}

// makeStartingBuildings — Phase 5 hardcoded test scene: a single-storey house,
// a two-storey house (verifies stairs), and a sunken bunker (verifies
// RectCut). Footprints are sized to fit cleanly inside their host chunks (P5).
func makeStartingBuildings() []components.BuildingPlan {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return []components.BuildingPlan{
		{
			Pos:     wp(-25, -40),
			Kind:    components.BuildingHouse,
			Stories: 1,
			Size:    rl.Vector2{X: 8, Y: 8},
			Yaw:     0,
			Seed:    0xA1,
		},
		{
			Pos:     wp(40, 30),
			Kind:    components.BuildingHouse,
			Stories: 2,
			Size:    rl.Vector2{X: 12, Y: 10},
			Yaw:     0,
			Seed:    0xB2,
		},
		{
			Pos:     wp(-30, 55),
			Kind:    components.BuildingBunker,
			Stories: 1,
			Size:    rl.Vector2{X: 10, Y: 10},
			Yaw:     0,
			Seed:    0xC3,
		},
	}
}

// makeStartingTrenches — one ~30 m defensive earthwork running between the
// bunker and the road, so persistence + clearance are exercised in one place.
func makeStartingTrenches() []components.Trench {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return []components.Trench{
		{
			Points: []components.WorldPos{
				wp(-50, 40),
				wp(-35, 50),
				wp(-15, 55),
			},
			Width: 1.5,
			Depth: 1.5,
		},
	}
}

// drawBuildingFloor draws a horizontal grey plate at the floor's WorldPos.
// Floor.Y is the slab top — drop a thin slab below it.
func drawBuildingFloor(pos rl.Vector3, f components.Floor) {
	const slabThickness float32 = 0.15
	// Centre cube vertically below the floor surface.
	c := rl.Vector3{X: pos.X, Y: pos.Y - slabThickness*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: f.SizeX, Y: slabThickness, Z: f.SizeZ},
		rl.Color{R: 110, G: 110, B: 120, A: 255})
}

// drawBuildingWall renders a wall segment with optional opening (door / window).
// WallSegment local axes after Rotatef(Yaw): +Z = along wall, +X = thickness,
// +Y = up. Walls without an opening are one cube; walls with one are split
// into left + right solids, lintel above and (for windows) sill below, with
// a coloured panel filling the opening.
func drawBuildingWall(pos rl.Vector3, w components.WallSegment) {
	wallCol := rl.Color{R: 175, G: 170, B: 165, A: 255}
	doorCol := rl.Color{R: 90, G: 60, B: 35, A: 255}
	winCol := rl.Color{R: 160, G: 200, B: 230, A: 200}
	lintelCol := rl.Color{R: 150, G: 145, B: 140, A: 255}

	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(w.Yaw*(180.0/math.Pi), 0, 1, 0)

	if w.OpeningKind == components.OpeningNone || w.OpeningWidth <= 0 {
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: w.Length * 0.5}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: w.Length}, wallCol)
		rl.PopMatrix()
		return
	}

	openingCenter := w.OpeningCenterT * w.Length
	openStart := openingCenter - w.OpeningWidth*0.5
	openEnd := openingCenter + w.OpeningWidth*0.5
	if openStart < 0 {
		openStart = 0
	}
	if openEnd > w.Length {
		openEnd = w.Length
	}

	if openStart > 0 {
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: openStart * 0.5}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: openStart}, wallCol)
	}
	if openEnd < w.Length {
		rightLen := w.Length - openEnd
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: openEnd + rightLen*0.5}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: rightLen}, wallCol)
	}

	// Sill (only when bottom > 0, i.e. windows).
	if w.OpeningBottom > 0 {
		c := rl.Vector3{X: 0, Y: w.OpeningBottom * 0.5, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.OpeningBottom, Z: openEnd - openStart}, lintelCol)
	}
	// Lintel above opening.
	lintelBottom := w.OpeningBottom + w.OpeningHeight
	if lintelBottom < w.Height {
		lintelH := w.Height - lintelBottom
		c := rl.Vector3{X: 0, Y: lintelBottom + lintelH*0.5, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: lintelH, Z: openEnd - openStart}, lintelCol)
	}

	// Panel inside the opening.
	panelY := w.OpeningBottom + w.OpeningHeight*0.5
	switch w.OpeningKind {
	case components.OpeningDoor:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness * 0.6, Y: w.OpeningHeight, Z: openEnd - openStart}, doorCol)
	case components.OpeningWindow:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness * 0.3, Y: w.OpeningHeight, Z: openEnd - openStart}, winCol)
	}

	rl.PopMatrix()
}

// drawBuildingStairs draws a tilted slab approximating a stairwell. WorldPos
// is the bottom-of-stairs anchor; slab tilts up along Yaw (+Z by default).
func drawBuildingStairs(pos rl.Vector3, s components.Stairs) {
	col := rl.Color{R: 130, G: 110, B: 95, A: 255}
	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(s.Yaw*(180.0/math.Pi), 0, 1, 0)
	// Pitch by angle = atan(Rise/Length) around local X axis. raylib's Rotatef
	// expects degrees; do everything in degrees from here.
	pitchDeg := float32(math.Atan2(float64(s.Rise), float64(s.Length))) * 180.0 / math.Pi
	rl.Rotatef(-pitchDeg, 1, 0, 0)
	// Slab centred along forward (Z) so its midpoint lies above the diagonal.
	hyp := float32(math.Sqrt(float64(s.Length*s.Length + s.Rise*s.Rise)))
	c := rl.Vector3{X: 0, Y: 0.1, Z: hyp * 0.5}
	rl.DrawCubeV(c, rl.Vector3{X: s.Width, Y: 0.2, Z: hyp}, col)
	rl.PopMatrix()
}

// makeStartingRivers returns the hand-authored river polylines. World-unit
// coords. Two rivers cross the spawn area so the player sees water + cut +
// water-prop visuals without wandering far.
func makeStartingRivers() []components.RiverPolyline {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return []components.RiverPolyline{
		// Diagonal river NW → SE through chunk (0,0).
		{
			Points: []components.WorldPos{
				wp(-100, -80),
				wp(-30, -20),
				wp(20, 30),
				wp(80, 90),
				wp(160, 150),
			},
			Width: 5.0,
			Depth: 1.5,
		},
		// Smaller stream branching westward.
		{
			Points: []components.WorldPos{
				wp(-90, 40),
				wp(-30, 20),
				wp(20, 30),
			},
			Width: 3.5,
			Depth: 1.0,
		},
	}
}
