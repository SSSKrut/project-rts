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

	// Defers run LIFO; this fires before window/audio teardown — while
	// app.World is still alive — flushing any in-memory chunk modifications.
	defer systems.FlushModifiedChunks(app.World, systems.SaveDir)

	// Pipeline order matters:
	//   1. terrain_streaming  — spawn/evict chunk entities
	//   2. terrain_load       — fill HeightmapDirty from disk if a save exists
	//   3. terrain_gen        — fill remaining HeightmapDirty via procgen
	//   4. river              — cut + water-props (after gen, before mesh & props)
	//   5. prop_spawn         — vegetation/rocks
	//   6. terrain_mesh       — build & upload GPU mesh
	//   7. ground_stick       — clamp anchor Y to surface
	//   8. lod                — units-only LOD
	//   9. movement
	//  10. spatial_audio
	//  11. streaming          — node graph (smart-spaces; not terrain)
	//  12. orbit              — camera input
	//  13. camera             — sync ECS camera to systems.CurrentCamera
	terrainStreamingSys := &systems.TerrainStreamingSystem{}
	terrainStreamingSys.InitUI(app.World)

	terrainLoadSys := &systems.TerrainLoadSystem{}
	terrainLoadSys.InitUI(app.World)

	terrainGenSys := &systems.TerrainGenSystem{}
	terrainGenSys.InitUI(app.World)

	riverSys := &systems.RiverSystem{}
	riverSys.InitUI(app.World)

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

	// Manual test bridge across the first river polyline. Registered in
	// propIndex so it inherits the host chunk's lifecycle: when the chunk
	// evicts the bridge despawns with the rest of the chunk's props and will
	// NOT come back when you walk back — bridges are not procedurally
	// re-derivable from the rivers data. Phase 4 will tie bridges to the road
	// graph (which is persistent).
	propMap := ecs.NewMap[components.Prop](app.World)
	const bridgeWX, bridgeWZ float32 = -5, 5
	bridgeWP := components.WorldPos{}.Add(rl.Vector3{X: bridgeWX, Z: bridgeWZ})
	bridgeWP.Local.Y = systems.GroundHeight(bridgeWX, bridgeWZ)
	bridgeEnt := app.World.NewEntity()
	posMap.Add(bridgeEnt, &bridgeWP)
	// Yaw = π/4 puts the cube's long axis perpendicular to the first river's
	// NW→SE flow at this segment. Adjust if the polyline changes.
	propMap.Add(bridgeEnt, &components.Prop{
		Type:  components.PropBridge,
		Yaw:   float32(math.Pi / 4),
		Scale: 1.0,
	})
	lodRelevantMap.Add(bridgeEnt, &components.LODRelevant{})
	propIndex.Loaded[bridgeWP.Chunk] = append(propIndex.Loaded[bridgeWP.Chunk], bridgeEnt)

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

	// Render filters. Unit cubes exclude terrain chunks (rendered via dedicated
	// chunk filters) and props (rendered via propFilter).
	activeRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODActive](app.World).
		Without(ecs.C[components.TerrainChunk](), ecs.C[components.Prop]())
	relevantRenderFilter := ecs.NewFilter2[components.WorldPos, components.LODRelevant](app.World).
		Without(ecs.C[components.TerrainChunk](), ecs.C[components.Prop]())
	chunkActiveFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](app.World)
	chunkRelevantFilter := ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](app.World)
	// Single naive iteration over all props regardless of LOD tier (in
	// practice every prop is LODRelevant). Move to instanced rendering when
	// 5k+ props in frustum cause spikes.
	propFilter := ecs.NewFilter2[components.WorldPos, components.Prop](app.World)

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

		rl.EndMode3D()

		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("WASD to move Anchor (Blue); Shift to sprint; right-drag to orbit; wheel to zoom", 10, 30, 20, rl.DarkGray)
		rl.DrawText("X to drop a crater at the anchor", 10, 50, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 70, 20, rl.DarkGray)
		hud := fmt.Sprintf("Props live: %d  |  Rivers: %d  |  Bridges: %d",
			propLive, len(rivers.Polylines), bridgeLive)
		rl.DrawText(hud, 10, 90, 20, rl.DarkGray)

		rl.EndDrawing()
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
