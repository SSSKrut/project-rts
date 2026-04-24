package main

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/ecs"
)

const (
	screenWidth  int32 = 800
	screenHeight int32 = 450
)



// LODAnchor marks an entity as the focus point for LOD decisions.
type LODAnchor struct{}

// AlwaysActive pins an entity in the active LOD bucket.
type AlwaysActive struct{}

// LODSystem assigns LOD levels based on distance to the anchor.
type LODSystem struct {
	ActiveRadius   float32
	RelevantRadius float32
	Hysteresis     float32
}

func (LODSystem) Name() string { return "lod" }

func (LODSystem) Phase() ecs.Phase { return ecs.PhaseLogic }

func (LODSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: ecs.LODDisabled,
		DormantEvery:  ecs.LODDisabled,
	}
}

func (LODSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{
		ecs.TypeOf[ecs.Position3D](),
		ecs.TypeOf[ecs.LOD](),
		ecs.TypeOf[ecs.LODAnchor](),
		ecs.TypeOf[ecs.AlwaysActive](),
	}
}

func (LODSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[ecs.LOD]()}
}

func (system LODSystem) Update(ctx ecs.UpdateContext) {
	var (
		anchorID ecs.EntityID
		anchor   ecs.Position3D
		found    bool
	)
	for _, id := range ctx.Current.Query(ecs.TypeOf[ecs.LODAnchor](), ecs.TypeOf[ecs.Position3D]()) {
		position, ok := ecs.Get[ecs.Position3D](ctx.Current, id)
		if !ok {
			continue
		}
		anchor = position
		anchorID = id
		found = true
		break
	}
	if !found {
		return
	}

	activeIn := system.ActiveRadius - system.Hysteresis
	activeOut := system.ActiveRadius + system.Hysteresis
	relevantIn := system.RelevantRadius - system.Hysteresis
	relevantOut := system.RelevantRadius + system.Hysteresis

	if activeIn < 0 {
		activeIn = 0
	}
	if relevantIn < activeOut {
		relevantIn = activeOut
	}

	activeIn2 := activeIn * activeIn
	activeOut2 := activeOut * activeOut
	relevantIn2 := relevantIn * relevantIn
	relevantOut2 := relevantOut * relevantOut

	for _, id := range ctx.Current.Query(ecs.TypeOf[ecs.Position3D]()) {
		if id == anchorID {
			continue
		}
		if _, ok := ctx.Current.GetComponent(id, ecs.TypeOf[ecs.AlwaysActive]()); ok {
			if ecs.LODLevelForEntity(ctx.Current, id) != ecs.LODActive {
				ecs.BufferSet(ctx.Commands, id, ecs.LOD{Level: ecs.LODActive})
			}
			continue
		}

		position, ok := ecs.Get[ecs.Position3D](ctx.Current, id)
		if !ok {
			continue
		}
		dx := position.X - anchor.X
		dy := position.Y - anchor.Y
		dz := position.Z - anchor.Z
		dist2 := dx*dx + dy*dy + dz*dz

		currentLOD := ecs.LODLevelForEntity(ctx.Current, id)
		nextLOD := currentLOD
		switch currentLOD {
		case ecs.LODActive:
			if dist2 > activeOut2 {
				nextLOD = ecs.LODRelevant
			}
		case ecs.LODRelevant:
			if dist2 <= activeIn2 {
				nextLOD = ecs.LODActive
			} else if dist2 > relevantOut2 {
				nextLOD = ecs.LODDormant
			}
		case ecs.LODDormant:
			if dist2 <= relevantIn2 {
				nextLOD = ecs.LODRelevant
			}
		}

		if nextLOD != currentLOD {
			ecs.BufferSet(ctx.Commands, id, ecs.LOD{Level: nextLOD})
		}
	}
}

// MovementSystem updates positions based on velocity.
type MovementSystem struct{}

func (MovementSystem) Name() string { return "movement" }

func (MovementSystem) Phase() ecs.Phase { return ecs.PhaseLogic }

func (MovementSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  time.Second,
	}
}

func (MovementSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[ecs.Position3D](), ecs.TypeOf[ecs.Velocity3D]()}
}

func (MovementSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[ecs.Position3D]()}
}

func (MovementSystem) Update(ctx ecs.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	bounds := float32(300.0)
	for _, id := range ecs.QueryLOD(ctx.Current, ctx.LOD, ecs.TypeOf[ecs.Position3D](), ecs.TypeOf[ecs.Velocity3D]()) {
		position, ok := ecs.Get[ecs.Position3D](ctx.Current, id)
		if !ok {
			continue
		}
		velocity, ok := ecs.Get[ecs.Velocity3D](ctx.Current, id)
		if !ok {
			continue
		}

		position.X += velocity.X * dt
		position.Y += velocity.Y * dt
		position.Z += velocity.Z * dt

		// Simple wrap around grid bounds
		if position.X < -bounds {
			position.X = bounds
		}
		if position.X > bounds {
			position.X = -bounds
		}
		if position.Z < -bounds {
			position.Z = bounds
		}
		if position.Z > bounds {
			position.Z = -bounds
		}

		ecs.BufferSet(ctx.Commands, id, position)
	}
}

func main() {
	rl.InitWindow(screenWidth, screenHeight, "RTS/FPS 3D ECS Prototype")
	defer rl.CloseWindow()

	rl.InitAudioDevice()
	defer rl.CloseAudioDevice()

	rl.SetTargetFPS(60)

	// Generate a 1-second pulsing 440Hz "engine" synthesized beep
	samples := make([]byte, 44100*2) // 44100 Hz, 16-bit mono
	for i := 0; i < len(samples); i += 2 {
		val := int16(4000) // gentle volume
		if (i/200)%2 == 0 {
			val = -4000
		}
		samples[i] = byte(val & 0xFF)
		samples[i+1] = byte(val >> 8)
	}
	wave := rl.NewWave(44100, 44100, 16, 1, samples)
	defer rl.UnloadWave(wave)

	audioManager := ecs.NewAudioManager(8) // Max 8 hardware voices total for this sound
	audioManager.RegisterWave("engine", wave)
	defer audioManager.Unload()

	// Setup 3D camera
	camera := rl.Camera3D{
		Position:   rl.Vector3{X: 0.0, Y: 15.0, Z: 20.0},
		Target:     rl.Vector3{X: 0.0, Y: 0.0, Z: 0.0},
		Up:         rl.Vector3{X: 0.0, Y: 1.0, Z: 0.0},
		Fovy:       45.0,
		Projection: rl.CameraPerspective,
	}

	world := ecs.NewWorld()
	world.AddSystem(LODSystem{
		ActiveRadius:   10,
		RelevantRadius: 20,
		Hysteresis:     2,
	})
	world.AddSystem(MovementSystem{})
	world.AddSystem(ecs.SpatialAudioSystem{
		Manager:     audioManager,
		MaxPerChunk: 2,
	})

	anchor := world.Current().NewEntity()
	ecs.Set(world.Current(), anchor, ecs.Position3D{X: 0, Y: 0, Z: 0})
	ecs.Set(world.Current(), anchor, ecs.LOD{Level: ecs.LODActive})
	ecs.Set(world.Current(), anchor, ecs.LODAnchor{})
	ecs.Set(world.Current(), anchor, ecs.AlwaysActive{})

	// Add multiple 3D entities
	for i := 0; i < 50; i++ {
		entity := world.Current().NewEntity()
		ecs.Set(world.Current(), entity, ecs.LOD{Level: ecs.LODRelevant})
		ecs.Set(world.Current(), entity, ecs.Position3D{
			X: float32(rl.GetRandomValue(-30, 30)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-30, 30)),
		})
		ecs.Set(world.Current(), entity, ecs.Velocity3D{
			X: float32(rl.GetRandomValue(-5, 5)),
			Y: 0,
			Z: float32(rl.GetRandomValue(-5, 5)),
		})
	}

	for !rl.WindowShouldClose() {
		dt := time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))
		
		// Move anchor (camera target) with arrow keys
		anchorPos, _ := ecs.Get[ecs.Position3D](world.Current(), anchor)
		moveSpeed := float32(10.0) * float32(dt.Seconds())
		if rl.IsKeyDown(rl.KeyRight) { anchorPos.X += moveSpeed }
		if rl.IsKeyDown(rl.KeyLeft) { anchorPos.X -= moveSpeed }
		if rl.IsKeyDown(rl.KeyUp) { anchorPos.Z -= moveSpeed }
		if rl.IsKeyDown(rl.KeyDown) { anchorPos.Z += moveSpeed }
		
		// apply immediately for input responsiveness (ignoring command buffer here for simplicity)
		ecs.Set(world.Current(), anchor, anchorPos)
		
		camera.Target = rl.Vector3{X: anchorPos.X, Y: anchorPos.Y, Z: anchorPos.Z}
		camera.Position = rl.Vector3{X: anchorPos.X, Y: anchorPos.Y + 15.0, Z: anchorPos.Z + 20.0}

		world.Tick(dt)

		state := world.Current()

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)
		
		rl.BeginMode3D(camera)
		rl.DrawGrid(60, 1.0)
		
		// Draw anchor
		rl.DrawCube(rl.Vector3{X: anchorPos.X, Y: anchorPos.Y, Z: anchorPos.Z}, 1, 1, 1, rl.Blue)
		
		for _, id := range state.Query(ecs.TypeOf[ecs.Position3D]()) {
			if id == anchor {
				continue
			}
			
			position, ok := ecs.Get[ecs.Position3D](state, id)
			if !ok {
				continue
			}
			
			lod := ecs.LODLevelForEntity(state, id)
			color := rl.Gray
			switch lod {
			case ecs.LODActive:
				color = rl.Red
			case ecs.LODRelevant:
				color = rl.Green
			case ecs.LODDormant:
				color = rl.LightGray
				continue // Skip rendering dormant for visual effect
			}
			
			rl.DrawCube(rl.Vector3{X: position.X, Y: position.Y, Z: position.Z}, 0.5, 0.5, 0.5, color)
			rl.DrawCubeWires(rl.Vector3{X: position.X, Y: position.Y, Z: position.Z}, 0.5, 0.5, 0.5, rl.Maroon)
		}
		
		rl.EndMode3D()
		
		rl.DrawText("RTS/FPS 3D ECS Prototype", 10, 10, 20, rl.Black)
		rl.DrawText("Arrow keys to move Anchor (Blue)", 10, 30, 20, rl.DarkGray)
		rl.DrawText("Red = Active LOD, Green = Relevant LOD (Hidden = Dormant)", 10, 50, 20, rl.DarkGray)
		
		rl.EndDrawing()
	}
}
