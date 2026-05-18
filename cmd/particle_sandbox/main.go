// Particle sandbox — standalone visual harness for ParticleSystem.
//
// Build & run:
//
//	go build -o /tmp/particle_sandbox ./cmd/particle_sandbox && /tmp/particle_sandbox
//	# or:
//	go run ./cmd/particle_sandbox
//
// Controls:
//
//	1..6      — spawn N particles of kind {Tracer, Impact, MuzzleFlash, Smoke, Dust, Debris}
//	Space     — fire all six kinds at once (firefight-style burst)
//	A         — toggle auto-emit (continuous burst every 0.15 s)
//	R         — reset (despawn every live particle)
//	Mouse RMB — orbit camera; wheel — zoom
//	Esc       — quit
//
// Purpose. The full game scene needs terrain / units / squads / orders to
// even start; that's three minutes of compile-then-load every time you
// want to eyeball a tracer fade or check that debris cubes fall sensibly.
// This sandbox boots in <1 second with nothing but a flat ground plane +
// the same `ParticleSystem` + `SpawnParticleHandles` the real game uses.
//
// What it validates:
//   - Per-kind render dispatch in `render_world.drawParticles` (tracer
//     line, impact sphere, muzzle-flash sphere, smoke sphere, dust
//     sphere, debris cube). Sandbox re-implements draw inline so this
//     file owns zero render-side code from the game proper.
//   - `ParticleSystem.Update` lifecycle: aging, velocity integration,
//     per-kind gravity (smoke rises, debris falls, dust settles), TTL
//     expiry, soft-cap eviction.
//   - `SpawnParticleHandles` writers: tracer with end-point, impact /
//     muzzle / smoke / dust / debris with velocity hints.
//
// What it does NOT validate. SpatialHash readers (no units in the scene),
// WeaponSystem.serial post-pass plumbing (no shots), Damage / Suppression
// propagation. Those require the full pipeline and are tested elsewhere.

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
	rl.InitWindow(screenW, screenH, "RTS — Particle Sandbox")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	// Minimal ECS world: ParticleSystem + a single emitter at origin.
	app := core.NewApp()

	handles := systems.NewSpawnHandles(app.World)
	particleSys := systems.NewParticleSystem()
	particleSys.InitUI(app.World)
	app.AddSystem(particleSys)

	// Walk the Particle filter for render. ParticleSystem owns lifecycle;
	// rendering lives here in the sandbox so the sandbox is self-contained.
	particleFilter := ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](app.World)
	endMap := ecs.NewMap[components.ParticleEnd](app.World)

	camera := rl.Camera3D{
		Position:   rl.Vector3{X: 8, Y: 6, Z: 8},
		Target:     rl.Vector3{X: 0, Y: 1, Z: 0},
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       60,
		Projection: rl.CameraPerspective,
	}
	// Spherical orbit state — RMB drag + wheel zoom mimic the main game's
	// OrbitSystem so the sandbox feels like the real product.
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

		// Camera orbit input.
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

		// Spawn input.
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

		// Drive ParticleSystem.Update with the real Tick path. The
		// sandbox uses delta-second wall-clock; the real game uses
		// scaled-dt via App.Tick.
		app.Tick(time.Duration(dt * float32(time.Second)))

		// Render.
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

// emitTracers spawns N line-particles diverging from origin in a fan. Each
// tracer is short so it visually reads as a single shot.
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
		// Land impacts on a small ring so multiple presses cluster visibly.
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

// emitFullBurst fires one of each kind — visual sanity check for cohabiting
// kinds (do debris fall through smoke? do tracers occlude impacts?).
func emitFullBurst(h *systems.SpawnParticleHandles, now float32) {
	emitTracers(h, now, 1)
	emitImpacts(h, now, 1)
	emitMuzzleFlashes(h, now, 1)
	emitSmoke(h, now, 1)
	emitDust(h, now, 6)
	emitDebris(h, now, 8)
}

// resetAll despawns every Particle entity. Cleaner than waiting for TTL
// when the test starts to accumulate visual junk.
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

// drawParticlesSandbox is a self-contained mirror of the main game's
// drawParticles. Keeps the sandbox decoupled from `render_world.go` (which
// pulls in the global render-origin chunk, units, ghosts, etc.).
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
	// Live particle count.
	var live int
	q := f.Query()
	for q.Next() {
		live++
	}
	rl.DrawRectangle(8, 8, 360, 178, rl.Color{R: 0, G: 0, B: 0, A: 180})
	rl.DrawText(fmt.Sprintf("Particle Sandbox — live: %d / cap %d",
		live, components.ParticleSoftCap), 16, 16, 18, rl.White)
	rl.DrawText("1 Tracer  2 Impact  3 Muzzle  4 Smoke  5 Dust  6 Debris",
		16, 44, 14, rl.LightGray)
	rl.DrawText("Space — full burst   A — auto (every 0.15s)   R — reset",
		16, 64, 14, rl.LightGray)
	autoText := "auto: OFF"
	autoCol := rl.Gray
	if autoEmit {
		autoText = "auto: ON"
		autoCol = rl.Color{R: 100, G: 220, B: 100, A: 255}
	}
	rl.DrawText(autoText, 16, 90, 16, autoCol)
	rl.DrawText("RMB — orbit   Wheel — zoom   Esc — quit", 16, 114, 14, rl.LightGray)
	rl.DrawText("Y-axis = green ; Z = blue ; X = red", 16, 134, 12, rl.LightGray)
	rl.DrawText("Smoke rises ; debris falls ; dust settles slow ; tracer/impact/flash static.",
		16, 154, 12, rl.LightGray)
}

// pseudoRand returns a deterministic uniform [0, 1) value from a seed and
// advances the seed in place. Avoids math/rand for zero-alloc semantics.
func pseudoRand(seed *uint64) float32 {
	*seed += 0x9e3779b97f4a7c15
	z := *seed
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float32(uint32(z>>33)&0x7fffffff) / float32(0x7fffffff)
}
