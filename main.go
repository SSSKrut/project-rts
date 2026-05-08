package main

import (
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

	// Camera will be provided by ECS camera system; systems.CurrentCamera is used for rendering.

	app := core.NewApp()

	// Initialize singleton resources BEFORE any system InitUI runs — systems
	// grab a Resource[T] handle in InitUI and panic if the resource hasn't
	// been registered yet.
	streamingMap := components.NewStreamingMap()
	ecs.AddResource(app.World, &streamingMap)
	terrainIndex := systems.NewTerrainChunkIndex()
	ecs.AddResource(app.World, &terrainIndex)

	// Flush any in-memory chunk modifications to disk on a clean shutdown.
	// Defers run LIFO, and this one is set up before rl.CloseWindow /
	// rl.CloseAudioDevice, so it executes first — while app.World is still
	// alive (P6).
	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	// Create systems and initialize their Filters/Maps. Registration order
	// (set below) matters for the per-tick pipeline:
	//   1. terrain_streaming  — spawn/evict chunk entities, mark Heightmap/MeshDirty
	//   2. terrain_load       — fill HeightmapDirty chunks from disk if a save exists
	//   3. terrain_gen        — fill remaining HeightmapDirty chunks via procgen
	//   4. terrain_mesh       — build & upload GPU mesh for MeshDirty chunks
	//   5. ground_stick       — clamp anchor Y to surface (uses GroundHeight)
	//   6. lod                — units-only LOD (excludes TerrainChunk via filter)
	//   7. movement           — pos += vel*dt
	//   8. spatial_audio
	//   9. streaming          — graph-based streaming (smart-spaces; not terrain)
	//  10. orbit              — camera input -> spherical->cartesian
	//  11. camera             — sync ECS camera to systems.CurrentCamera + OriginChunk
	terrainStreamingSys := &systems.TerrainStreamingSystem{}
	terrainStreamingSys.InitUI(app.World)

	terrainLoadSys := &systems.TerrainLoadSystem{}
	terrainLoadSys.InitUI(app.World)

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

	// Stamper: service object for terrain edits. Pre-built handles, called
	// from the input layer below (debug crater key today; ballistics later).
	stamper := systems.NewStamper(app.World)

	app.AddSystem(terrainStreamingSys)
	app.AddSystem(terrainLoadSys)
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

		// Update anchor position (WASD), driven by the orbit camera's yaw so
		// W is always "into the screen" and D is "to the right" no matter
		// how the camera is rotated. Forward (camera → anchor in XZ) is
		// (-sin yaw, 0, -cos yaw) given OrbitSystem's spherical→cartesian
		// formula; Right = Forward × Up = (cos yaw, 0, -sin yaw).
		// Y is overwritten by GroundStickSystem inside Tick, so we only set
		// X/Z here. Diagonal input is normalised so W+D isn't faster than W.
		anchorPos := posMap.Get(anchor)
		const anchorSpeed float32 = 10.0
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

		// Debug: drop a crater at the anchor on X. 4 m radius, 2 m deep —
		// large enough to see at distance, small enough to fit cleanly inside
		// one chunk most of the time (and to verify cross-chunk seams when it
		// straddles a boundary).
		if rl.IsKeyPressed(rl.KeyX) {
			stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
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

		rl.DrawCircle3D(anchorRender, 1, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Blue)
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
		rl.DrawText("X to drop a crater at the anchor", 10, 50, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 70, 20, rl.DarkGray)

		rl.EndDrawing()
	}
}
