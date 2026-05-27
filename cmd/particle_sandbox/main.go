// Particle sandbox - standalone visual harness for ParticleSystem. Boots in
// <1 s with a flat ground plane + the same ParticleSystem +
// SpawnParticleHandles the real game uses, so tracer fades / debris arcs can
// be eyeballed without spinning up terrain / units / squads.
//
// Controls: 1..6 spawn one of each kind, Space = full burst, A = auto-emit,
// R = reset, RMB = orbit, wheel = zoom, Esc = quit.

package main

import (
	"fmt"
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
	"rts-go/systems"
)

const (
	screenW = 1280
	screenH = 720
)

func main() {
	rl.InitWindow(screenW, screenH, "RTS - Particle Sandbox")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	app := core.NewApp()

	handles := systems.NewSpawnHandles(app.World)
	particleSys := systems.NewParticleSystem()
	particleSys.InitUI(app.World)
	app.AddSystem(particleSys)

	// Render lives here in the sandbox so it's self-contained — ParticleSystem
	// owns only lifecycle.
	particleFilter := ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](app.World)
	endMap := ecs.NewMap[components.ParticleEnd](app.World)

	camera := rl.Camera3D{
		Position:   rl.Vector3{X: 8, Y: 6, Z: 8},
		Target:     rl.Vector3{X: 0, Y: 1, Z: 0},
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       60,
		Projection: rl.CameraPerspective,
	}
	yaw := float32(math.Pi / 4)
	pitch := float32(math.Pi / 6)
	dist := float32(12)

	autoEmit := false
	var autoTimer float32

	lastTick := time.Now()
	frame := 0

	for !rl.WindowShouldClose() {
		now := time.Now()
		dt := float32(now.Sub(lastTick).Seconds())
		if dt > 0.25 {
			dt = 0.25
		}
		lastTick = now
		frame++

		if rl.IsMouseButtonDown(rl.MouseButtonRight) {
			delta := rl.GetMouseDelta()
			yaw -= delta.X * 0.005
			pitch -= delta.Y * 0.005
			if pitch < 0.1 {
				pitch = 0.1
			}
			if pitch > 1.4 {
				pitch = 1.4
			}
		}
		dist -= rl.GetMouseWheelMove() * 0.8
		if dist < 3 {
			dist = 3
		}
		if dist > 40 {
			dist = 40
		}
		camera.Position = rl.Vector3{
			X: dist * float32(math.Cos(float64(pitch))*math.Sin(float64(yaw))),
			Y: dist * float32(math.Sin(float64(pitch))),
			Z: dist * float32(math.Cos(float64(pitch))*math.Cos(float64(yaw))),
		}

		spawnTime := float32(rl.GetTime())
		if rl.IsKeyPressed(rl.KeyOne) {
			emitTracers(handles, spawnTime, 1)
		}
		if rl.IsKeyPressed(rl.KeyTwo) {
			emitImpacts(handles, spawnTime, 1)
		}
		if rl.IsKeyPressed(rl.KeyThree) {
			emitMuzzleFlashes(handles, spawnTime, 1)
		}
		if rl.IsKeyPressed(rl.KeyFour) {
			emitSmoke(handles, spawnTime, 1)
		}
		if rl.IsKeyPressed(rl.KeyFive) {
			emitDust(handles, spawnTime, 6)
		}
		if rl.IsKeyPressed(rl.KeySix) {
			emitDebris(handles, spawnTime, 10)
		}
		if rl.IsKeyPressed(rl.KeySpace) {
			emitFullBurst(handles, spawnTime)
		}
		if rl.IsKeyPressed(rl.KeyA) {
			autoEmit = !autoEmit
		}
		if rl.IsKeyPressed(rl.KeyR) {
			resetAll(app.World, particleFilter)
		}

		if autoEmit {
			autoTimer += dt
			if autoTimer >= 0.015 {
				autoTimer = 0
				emitFullBurst(handles, spawnTime)
			}
		}

		app.Tick(time.Duration(dt * float32(time.Second)))

		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 18, G: 22, B: 30, A: 255})

		rl.BeginMode3D(camera)
		drawGround()
		drawAxes()
		drawParticlesSandbox(particleFilter, endMap, spawnTime)
		rl.EndMode3D()

		drawHUD(particleFilter, autoEmit)
		rl.EndDrawing()
	}
}

func emitTracers(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		angle := float32(i) * 0.5
		from := rl.Vector3{X: 0, Y: 1.6, Z: 0}
		to := rl.Vector3{
			X: from.X + 6*float32(math.Cos(float64(angle))),
			Y: 1.0,
			Z: from.Z + 6*float32(math.Sin(float64(angle))),
		}
		h.SpawnTracer(from, to, rl.Color{R: 255, G: 230, B: 120, A: 255}, now, 0.25)
	}
}

func emitImpacts(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		angle := float64(i) * 0.7
		pos := rl.Vector3{
			X: 2.5 * float32(math.Cos(angle)),
			Y: 1.0,
			Z: 2.5 * float32(math.Sin(angle)),
		}
		h.SpawnImpact(pos, rl.Color{R: 230, G: 200, B: 80, A: 255}, now, 0.4)
	}
}

func emitMuzzleFlashes(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		pos := rl.Vector3{X: 0, Y: 1.6, Z: 0}
		h.SpawnMuzzleFlash(pos, rl.Color{R: 255, G: 240, B: 160, A: 255}, now)
	}
}

func emitSmoke(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		pos := rl.Vector3{X: 0, Y: 0.6, Z: 0}
		h.SpawnSmoke(pos, rl.Color{R: 110, G: 110, B: 110, A: 200}, now)
	}
}

func emitDust(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		seed := uint64(now*10000.0) + uint64(i)*2654435761
		rx := pseudoRand(&seed) - 0.5
		rz := pseudoRand(&seed) - 0.5
		pos := rl.Vector3{X: 0, Y: 0.1, Z: 0}
		vel := rl.Vector3{X: rx * 2, Y: 0.6, Z: rz * 2}
		h.SpawnDust(pos, rl.Color{R: 180, G: 160, B: 130, A: 200}, vel, now)
	}
}

func emitDebris(h *systems.SpawnParticleHandles, now float32, count int) {
	for i := 0; i < count; i++ {
		seed := uint64(now*10000.0) + uint64(i)*48271
		rx := pseudoRand(&seed) - 0.5
		rz := pseudoRand(&seed) - 0.5
		ry := pseudoRand(&seed)
		pos := rl.Vector3{X: 0, Y: 0.3, Z: 0}
		vel := rl.Vector3{X: rx * 6, Y: 2 + ry*4, Z: rz * 6}
		h.SpawnDebris(pos, rl.Color{R: 120, G: 110, B: 100, A: 235}, vel, now)
	}
}

func emitFullBurst(h *systems.SpawnParticleHandles, now float32) {
	emitTracers(h, now, 1)
	emitImpacts(h, now, 1)
	emitMuzzleFlashes(h, now, 1)
	emitSmoke(h, now, 1)
	emitDust(h, now, 6)
	emitDebris(h, now, 8)
}

func resetAll(world *ecs.World, f *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual]) {
	var doomed []ecs.Entity
	q := f.Query()
	for q.Next() {
		doomed = append(doomed, q.Entity())
	}
	for _, e := range doomed {
		if world.Alive(e) {
			world.RemoveEntity(e)
		}
	}
}

// drawParticlesSandbox mirrors the main game's drawParticles. Inlined here so
// the sandbox stays decoupled from render_world.go (which pulls in the global
// render-origin chunk, units, ghosts, etc.).
func drawParticlesSandbox(
	f *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual],
	endMap *ecs.Map[components.ParticleEnd],
	now float32,
) {
	q := f.Query()
	for q.Next() {
		_, pos, vis := q.Get()
		age := now - vis.SpawnTime
		if age < 0 || age > vis.TTL {
			continue
		}
		fade := 1 - age/vis.TTL
		col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
			A: uint8(float32(vis.Color.A) * fade)}
		from := rl.Vector3{X: pos.Local.X, Y: pos.Local.Y, Z: pos.Local.Z}
		switch vis.Kind {
		case components.ParticleTracer:
			if end := endMap.Get(q.Entity()); end != nil {
				rl.DrawLine3D(from, end.To, col)
			}
		case components.ParticleImpact, components.ParticleMuzzleFlash,
			components.ParticleSmoke, components.ParticleDust:
			r := vis.Size
			if r <= 0 {
				r = 0.1
			}
			rl.DrawSphere(from, r, col)
		case components.ParticleDebris:
			s := vis.Size
			if s <= 0 {
				s = 0.15
			}
			rl.DrawCube(from, s, s, s, col)
		}
	}
}

func drawGround() {
	rl.DrawPlane(rl.Vector3{X: 0, Y: 0, Z: 0}, rl.Vector2{X: 40, Y: 40}, rl.Color{R: 60, G: 70, B: 60, A: 255})
	rl.DrawGrid(40, 1)
}

func drawAxes() {
	rl.DrawLine3D(rl.Vector3{X: 0, Y: 0, Z: 0}, rl.Vector3{X: 2, Y: 0, Z: 0}, rl.Red)
	rl.DrawLine3D(rl.Vector3{X: 0, Y: 0, Z: 0}, rl.Vector3{X: 0, Y: 2, Z: 0}, rl.Green)
	rl.DrawLine3D(rl.Vector3{X: 0, Y: 0, Z: 0}, rl.Vector3{X: 0, Y: 0, Z: 2}, rl.Blue)
}

func drawHUD(f *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual], autoEmit bool) {
	var live int
	q := f.Query()
	for q.Next() {
		live++
	}
	rl.DrawRectangle(8, 8, 360, 178, rl.Color{R: 0, G: 0, B: 0, A: 180})
	rl.DrawText(fmt.Sprintf("Particle Sandbox - live: %d / cap %d",
		live, components.ParticleSoftCap), 16, 16, 18, rl.White)
	rl.DrawText("1 Tracer  2 Impact  3 Muzzle  4 Smoke  5 Dust  6 Debris",
		16, 44, 14, rl.LightGray)
	rl.DrawText("Space - full burst   A - auto (every 0.15s)   R - reset",
		16, 64, 14, rl.LightGray)
	autoText := "auto: OFF"
	autoCol := rl.Gray
	if autoEmit {
		autoText = "auto: ON"
		autoCol = rl.Color{R: 100, G: 220, B: 100, A: 255}
	}
	rl.DrawText(autoText, 16, 90, 16, autoCol)
	rl.DrawText("RMB - orbit   Wheel - zoom   Esc - quit", 16, 114, 14, rl.LightGray)
	rl.DrawText("Y-axis = green ; Z = blue ; X = red", 16, 134, 12, rl.LightGray)
	rl.DrawText("Smoke rises ; debris falls ; dust settles slow ; tracer/impact/flash static.",
		16, 154, 12, rl.LightGray)
}

// pseudoRand: deterministic uniform [0, 1) from a seed, advances seed in
// place. Avoids math/rand for zero-alloc semantics.
func pseudoRand(seed *uint64) float32 {
	*seed += 0x9e3779b97f4a7c15
	z := *seed
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float32(uint32(z>>33)&0x7fffffff) / float32(0x7fffffff)
}
