package main

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

const (
	screenWidth  int32 = 800
	screenHeight int32 = 450
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

	// Camera will be provided by ECS camera system; systems.CurrentCamera is used for rendering.

	app := core.NewApp()

	// Initialize singleton resources BEFORE any system InitUI runs — systems
	// grab a Resource[T] handle in InitUI and panic if the resource hasn't
	// been registered yet.
	streamingMap := components.NewStreamingMap()
	ecs.AddResource(app.World, &streamingMap)
	terrainIndex := systems.NewTerrainChunkIndex()
	ecs.AddResource(app.World, &terrainIndex)

	// Create systems and initialize their Filters/Maps. Registration order
	// (set below) matters for the per-tick pipeline:
	//   1. terrain_streaming  — spawn/evict chunk entities, mark Heightmap/MeshDirty
	//   2. terrain_gen        — fill heights for HeightmapDirty chunks
	//   3. terrain_mesh       — build & upload GPU mesh for MeshDirty chunks
	//   4. ground_stick       — clamp anchor Y to surface (uses GroundHeight)
	//   5. lod                — units-only LOD (excludes TerrainChunk via filter)
	//   6. movement           — pos += vel*dt
	//   7. spatial_audio
	//   8. streaming          — graph-based streaming (smart-spaces; not terrain)
	//   9. orbit              — camera input -> spherical->cartesian
	//  10. camera             — sync ECS camera to systems.CurrentCamera + OriginChunk
	terrainStreamingSys := &systems.TerrainStreamingSystem{}
	terrainStreamingSys.InitUI(app.World)

	terrainGenSys := &systems.TerrainGenSystem{}
	terrainGenSys.InitUI(app.World)

	terrainMeshSys := &systems.TerrainMeshSystem{}
	terrainMeshSys.InitUI(app.World)

	groundStickSys := &systems.GroundStickSystem{}
	groundStickSys.InitUI(app.World)

	lodSys := &systems.LODSystem{
		ActiveRadius:   30,
		RelevantRadius: 60,
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
	app.AddSystem(terrainGenSys)
	app.AddSystem(terrainMeshSys)
	app.AddSystem(groundStickSys)
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(audioSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	// Maps for entity creation
	posMap := ecs.NewMap[components.WorldPos](app.World)
	velMap := ecs.NewMap[components.Velocity3D](app.World)
	lodActiveMap := ecs.NewMap[components.LODActive](app.World)
	lodRelevantMap := ecs.NewMap[components.LODRelevant](app.World)
	lodAnchorMap := ecs.NewMap[components.LODAnchor](app.World)
	alwaysActiveMap := ecs.NewMap[components.AlwaysActive](app.World)

	// Create anchor entity at world origin (chunk 0,0; local 0,0,0).
	// GroundStickSystem clamps Local.Y to GroundHeight + AnchorEyeHeight on
	// every tick.
	anchor := app.World.NewEntity()
	posMap.Add(anchor, &components.WorldPos{})
	lodActiveMap.Add(anchor, &components.LODActive{})
	lodAnchorMap.Add(anchor, &components.LODAnchor{})
	alwaysActiveMap.Add(anchor, &components.AlwaysActive{})

	// Create camera entity that orbits the anchor
	camCompMap := ecs.NewMap[components.Camera](app.World)
	orbitMap := ecs.NewMap[components.OrbitController](app.World)
	activeCamMap := ecs.NewMap[components.ActiveCamera](app.World)

	camEnt := app.World.NewEntity()
	// Camera world position: same chunk as anchor; OrbitSystem will overwrite
	// it on the first tick from spherical coords. Initial value just keeps the
	// invariant valid before that tick.
	posMap.Add(camEnt, &components.WorldPos{Local: rl.Vector3{X: 0, Y: 15.0, Z: 20.0}})
	camCompMap.Add(camEnt, &components.Camera{Fovy: 45.0, Perspective: true})
	orbitMap.Add(camEnt, &components.OrbitController{
		Target:           anchor,
		Yaw:              0,
		Pitch:            0.6,
		Radius:           25.0,
		MinRadius:        5.0,
		MaxRadius:        100.0,
		SensitivityYaw:   0.01,
		SensitivityPitch: 0.01,
		SensitivityZoom:  1.0,
		Smooth:           0,
	})
	activeCamMap.Add(camEnt, &components.ActiveCamera{})

	// Create mobile entities at Relevant LOD. Build their WorldPos by
	// translating from origin so that any negative offsets fold into the
	// neighbouring chunk (preserves the Local.X/Local.Z ∈ [0, ChunkSize) invariant).
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

	// Pre-built Filters for rendering. Unit cubes exclude terrain chunks
	// (those are rendered through dedicated chunk filters below).
	activeRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODActive](app.World).
		Without(ecs.C[components.TerrainChunk]())
	relevantRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODRelevant](app.World).
		Without(ecs.C[components.TerrainChunk]())
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)

	// Default material shared by every chunk DrawMesh call. Loaded once after
	// the GL context exists. Per-vertex colour on each mesh provides the
	// LOD-tier debug shading.
	terrainMaterial := rl.LoadMaterialDefault()
	defer rl.UnloadMaterial(terrainMaterial)

	for !rl.WindowShouldClose() {
		dt := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))

		// Update anchor position (WASD). Y is overwritten by GroundStickSystem
		// inside Tick — we only set X/Z here.
		anchorPos := posMap.Get(anchor)
		moveSpeed := float32(10.0) * float32(dt.Seconds())
		var move rl.Vector3
		if rl.IsKeyDown(rl.KeyD) {
			move.X += moveSpeed
		}
		if rl.IsKeyDown(rl.KeyA) {
			move.X -= moveSpeed
		}
		if rl.IsKeyDown(rl.KeyW) {
			move.Z -= moveSpeed
		}
		if rl.IsKeyDown(rl.KeyS) {
			move.Z += moveSpeed
		}
		if move.X != 0 || move.Z != 0 {
			*anchorPos = anchorPos.Add(move)
		}

		app.Tick(dt)

		// Re-fetch anchor pointer in case archetype mutations during Tick
		// invalidated the previous one (defensive — current systems don't
		// touch the anchor's archetype).
		anchorPos = posMap.Get(anchor)
		anchorRender := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)

		rl.BeginMode3D(systems.CurrentCamera)

		// Terrain — Active and Relevant chunks. The chunk's WorldPos is at
		// its (0,0,0) corner, so ToRenderSpace gives the corner position
		// and the mesh's local vertices already span [0, ChunkSize]. We
		// translate via a per-draw matrix and call DrawMesh (NOT DrawModel
		// — see comment on components.ChunkMesh).
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

		rl.DrawCube(anchorRender, 1, 1, 1, rl.Blue)

		// Render Active unit entities (red). Cubes do NOT ground-stick in
		// Phase 1 — they will appear floating or buried; that's expected.
		q := activeRenderFilter.Query()
		for q.Next() {
			pos, _ := q.Get()
			if q.Entity() == anchor {
				continue
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCube(renderPos, 0.5, 0.5, 0.5, rl.Red)
			rl.DrawCubeWires(renderPos, 0.5, 0.5, 0.5, rl.Maroon)
		}

		// Render Relevant unit entities (green)
		q2 := relevantRenderFilter.Query()
		for q2.Next() {
			pos, _ := q2.Get()
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCube(renderPos, 0.5, 0.5, 0.5, rl.Green)
			rl.DrawCubeWires(renderPos, 0.5, 0.5, 0.5, rl.Maroon)
		}

		rl.EndMode3D()

		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("WASD to move Anchor (Blue); right-drag to orbit; wheel to zoom", 10, 30, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 50, 20, rl.DarkGray)

		rl.EndDrawing()
	}
}
