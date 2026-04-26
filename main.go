package main

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/ecs"
	"rts-go/systems"
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

	camera := rl.Camera3D{
		Position:   rl.Vector3{X: 0.0, Y: 15.0, Z: 20.0},
		Target:     rl.Vector3{X: 0.0, Y: 0.0, Z: 0.0},
		Up:         rl.Vector3{X: 0.0, Y: 1.0, Z: 0.0},
		Fovy:       45.0,
		Projection: rl.CameraPerspective,
	}

	world := ecs.NewWorld()
	world.AddSystem(systems.LODSystem{
		ActiveRadius:   10,
		RelevantRadius: 20,
		Hysteresis:     2,
	})
	world.AddSystem(systems.MovementSystem{})
	world.AddSystem(systems.SpatialAudioSystem{
		Manager:     audioManager,
		MaxPerChunk: 2,
	})

	anchor := world.Current().NewEntity()
	ecs.Set(world.Current(), anchor, components.Position3D{X: 0, Y: 0, Z: 0})
	ecs.Set(world.Current(), anchor, ecs.LOD{Level: ecs.LODActive})
	ecs.Set(world.Current(), anchor, components.LODAnchor{})
	ecs.Set(world.Current(), anchor, components.AlwaysActive{})

	for i := 0; i < 30; i++ {
		entity := world.Current().NewEntity()
		ecs.Set(world.Current(), entity, ecs.LOD{Level: ecs.LODRelevant})
		ecs.Set(world.Current(), entity, components.Position3D{
			X: float32(rl.GetRandomValue(-30, 30)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-30, 30)),
		})
		ecs.Set(world.Current(), entity, components.Velocity3D{
			X: float32(rl.GetRandomValue(-5, 5)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-5, 5)),
		})
	}

	for !rl.WindowShouldClose() {
		dt := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))

		anchorPos, _ := ecs.Get[components.Position3D](world.Current(), anchor)
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

		ecs.Set(world.Current(), anchor, anchorPos)

		camera.Target = rl.Vector3{X: anchorPos.X, Y: anchorPos.Y, Z: anchorPos.Z}
		camera.Position = rl.Vector3{X: anchorPos.X, Y: anchorPos.Y + 15.0, Z: anchorPos.Z + 20.0}

		world.Tick(dt)

		state := world.Current()

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)

		rl.BeginMode3D(camera)
		rl.DrawGrid(60, 1.0)

		rl.DrawCube(rl.Vector3{X: anchorPos.X, Y: anchorPos.Y, Z: anchorPos.Z}, 1, 1, 1, rl.Blue)

		// Use ForEach2 for rendering - more cache-friendly iteration
		ecs.ForEach2[components.Position3D, ecs.LOD](state, func(id ecs.EntityID, pos *components.Position3D, lod *ecs.LOD) {
			if id == anchor {
				return
			}

			color := rl.Gray
			switch lod.Level {
			case ecs.LODActive:
				color = rl.Red
			case ecs.LODRelevant:
				color = rl.Green
			case ecs.LODDormant:
				return // Skip dormant entities
			}

			rl.DrawCube(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, color)
			rl.DrawCubeWires(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z}, 0.5, 0.5, 0.5, rl.Maroon)
		})

		rl.EndMode3D()

		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("Arrow keys to move Anchor (Blue)", 10, 30, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 50, 20, rl.DarkGray)

		rl.EndDrawing()
	}
}
