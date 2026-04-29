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

	// Create systems and initialize their Filters/Maps
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

	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(audioSys)
	app.AddSystem(streamingSys)

	// Create camera entity and systems (ECS-integrated orbital camera)
	orbitSys := &systems.OrbitSystem{}
	orbitSys.InitUI(app.World)

	cameraSys := &systems.CameraSystem{}
	cameraSys.InitUI(app.World)

	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)

	// Maps for entity creation
	posMap := ecs.NewMap[components.Position3D](app.World)
	velMap := ecs.NewMap[components.Velocity3D](app.World)
	lodActiveMap := ecs.NewMap[components.LODActive](app.World)
	lodRelevantMap := ecs.NewMap[components.LODRelevant](app.World)
	lodAnchorMap := ecs.NewMap[components.LODAnchor](app.World)
	alwaysActiveMap := ecs.NewMap[components.AlwaysActive](app.World)
	posMap2 := ecs.NewMap[components.Position3D](app.World) // separate instance for anchor query

	// Create anchor entity
	anchor := app.World.NewEntity()
	posMap.Add(anchor, &components.Position3D{X: 0, Y: 0, Z: 0})
	lodActiveMap.Add(anchor, &components.LODActive{})
	lodAnchorMap.Add(anchor, &components.LODAnchor{})
	alwaysActiveMap.Add(anchor, &components.AlwaysActive{})

	// Create camera entity that orbits the anchor
	camPosMap := ecs.NewMap[components.Position3D](app.World)
	camCompMap := ecs.NewMap[components.Camera](app.World)
	orbitMap := ecs.NewMap[components.OrbitController](app.World)
	activeCamMap := ecs.NewMap[components.ActiveCamera](app.World)

	camEnt := app.World.NewEntity()
	// initial position relative to anchor
	camPosMap.Add(camEnt, &components.Position3D{X: 0, Y: 15.0, Z: 20.0})
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

	// Create mobile entities at Relevant LOD
	for i := 0; i < 300; i++ {
		entity := app.World.NewEntity()
		lodRelevantMap.Add(entity, &components.LODRelevant{})
		posMap.Add(entity, &components.Position3D{
			X: float32(rl.GetRandomValue(-30, 30)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-30, 30)),
		})
		velMap.Add(entity, &components.Velocity3D{
			X: float32(rl.GetRandomValue(-5, 5)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-5, 5)),
		})
	}

	// Pre-built Filters for rendering
	activeRenderFilter := ecs.NewFilter2[components.Position3D, components.LODActive](app.World)
	relevantRenderFilter := ecs.NewFilter2[components.Position3D, components.LODRelevant](app.World)

	for !rl.WindowShouldClose() {
		dt := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))

		// Update anchor position
		anchorPos := posMap2.Get(anchor)
		moveSpeed := float32(10.0) * float32(dt.Seconds())
		if rl.IsKeyDown(rl.KeyRight) {
			anchorPos.X += moveSpeed
		}
		if rl.IsKeyDown(rl.KeyLeft) {
			anchorPos.X -= moveSpeed
		}
		if rl.IsKeyDown(rl.KeyUp) {
			anchorPos.Z -= moveSpeed
		}
		if rl.IsKeyDown(rl.KeyDown) {
			anchorPos.Z += moveSpeed
		}

		app.Tick(dt)

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)

		rl.BeginMode3D(systems.CurrentCamera)
		rl.DrawGrid(60, 1.0)

		rl.DrawCube(rl.Vector3{X: anchorPos.X, Y: anchorPos.Y, Z: anchorPos.Z}, 1, 1, 1, rl.Blue)

		// Render Active entities (red)
		q := activeRenderFilter.Query()
		for q.Next() {
			pos, _ := q.Get()
			if q.Entity() == anchor {
				continue
			}
			rl.DrawCube(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, rl.Red)
			rl.DrawCubeWires(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, rl.Maroon)
		}

		// Render Relevant entities (green)
		q2 := relevantRenderFilter.Query()
		for q2.Next() {
			pos, _ := q2.Get()
			rl.DrawCube(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, rl.Green)
			rl.DrawCubeWires(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, rl.Maroon)
		}

		rl.EndMode3D()

		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("Arrow keys to move Anchor (Blue)", 10, 30, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 50, 20, rl.DarkGray)

		rl.EndDrawing()
	}
}
