// Model sandbox - standalone harness for baked assets. Boots in <1 s with
// nothing but a ground grid and the same assets.Registry the game uses, so a
// freshly baked .glb can be checked without terrain, units or squads.
//
// It exists to make one distinction cheap: when the first in-game frame looks
// wrong, is the model bad, the loader bad, the shader bad, or the axes bad?
// Here each is visible on its own — mounts as coloured pins, pivots as cubes,
// LOD switching forced by hand, and a forward arrow that says which way +Z is.
//
// Controls: Tab next asset, 0/1/2 force LOD, L auto-LOD by distance,
// M mounts, P pivots, W wireframe, T turret sweep, R roll (wheels + steer),
// G grid, RMB/MMB orbit, wheel zoom, Esc quit.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/assets"
)

const (
	screenW = 1280
	screenH = 720
	fovY    = 55
)

var mountColors = map[string]rl.Color{
	"exhaust": {R: 230, G: 40, B: 15, A: 255},
	"muzzle":  {R: 255, G: 216, B: 25, A: 255},
	"smoke":   {R: 25, G: 205, B: 230, A: 255},
	"seat":    {R: 40, G: 216, B: 65, A: 255},
	"door":    {R: 50, G: 90, B: 255, A: 255},
	"light":   {R: 245, G: 245, B: 245, A: 255},
	"decal":   {R: 255, G: 50, B: 230, A: 255},
	"hatch":   {R: 255, G: 128, B: 0, A: 255},
}

func mountColor(id string) rl.Color {
	for i := 0; i < len(id); i++ {
		if id[i] == '.' {
			if c, ok := mountColors[id[:i]]; ok {
				return c
			}
			break
		}
	}
	return rl.Color{R: 160, G: 160, B: 160, A: 255}
}

func main() {
	dir := flag.String("dir", "assets/models", "baked asset directory")
	pick := flag.String("asset", "", "asset name to open first")
	shot := flag.String("shot", "", "write one PNG and exit")
	shotAt := flag.Int("shot-at", 6, "frame to capture")
	shotLOD := flag.Int("shot-lod", 0, "LOD to capture")
	shotCam := flag.String("shot-cam", "", "dist,pitchDeg,yawDeg for the capture")
	shotYaw := flag.Float64("shot-yaw", 0, "hull yaw in degrees for the capture")
	shotRotor := flag.Float64("shot-rotor", 0, "rotor angle in degrees for the capture")
	flag.Parse()

	// raylib's TakeScreenshot always prepends the working directory, so an
	// absolute -shot path lands somewhere absurd. Move the process to where the
	// shot belongs and make -dir absolute first, so both flags keep working.
	if *shot != "" {
		if abs, err := filepath.Abs(*dir); err == nil {
			*dir = abs
		}
		if d := filepath.Dir(*shot); d != "." {
			if err := os.Chdir(d); err == nil {
				*shot = filepath.Base(*shot)
			}
		}
	}

	reg, err := assets.LoadRegistry(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model_sandbox: %v\n", err)
		fmt.Fprintf(os.Stderr, "  (looked in %s — bake writes there via BAKE_OUT)\n",
			filepath.Clean(*dir))
		os.Exit(1)
	}
	names := reg.Names()
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "model_sandbox: manifest lists no assets")
		os.Exit(1)
	}
	cur := 0
	if *pick != "" {
		if id, ok := reg.ID(*pick); ok {
			cur = int(id)
		}
	}

	rl.InitWindow(screenW, screenH, "RTS - Model Sandbox")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)
	defer reg.Unload()

	cam := rl.Camera3D{Up: rl.Vector3{Y: 1}, Fovy: fovY, Projection: rl.CameraPerspective}
	yaw, pitch, dist := float32(2.4), float32(0.42), float32(14)

	forceLOD, autoLOD := 0, false
	showMounts, showPivots, wire, grid := true, false, false, true
	sweep, roll := false, false
	var turret, gun, spin, steer, hull, rotor float32

	light := assets.DefaultLight()

	if *shot != "" {
		hull = float32(*shotYaw) * math.Pi / 180
		rotor = float32(*shotRotor) * math.Pi / 180
		forceLOD = *shotLOD
		if n, _ := fmt.Sscanf(*shotCam, "%f,%f,%f", &dist, &pitch, &yaw); n == 3 {
			pitch *= math.Pi / 180
			yaw *= math.Pi / 180
		}
	}

	for frame := 0; !rl.WindowShouldClose(); frame++ {
		dt := rl.GetFrameTime()

		if rl.IsKeyPressed(rl.KeyTab) {
			cur = (cur + 1) % len(names)
		}
		for i, k := range []int32{rl.KeyZero, rl.KeyOne, rl.KeyTwo} {
			if rl.IsKeyPressed(k) {
				forceLOD, autoLOD = i, false
			}
		}
		if rl.IsKeyPressed(rl.KeyL) {
			autoLOD = !autoLOD
		}
		if rl.IsKeyPressed(rl.KeyM) {
			showMounts = !showMounts
		}
		if rl.IsKeyPressed(rl.KeyP) {
			showPivots = !showPivots
		}
		if rl.IsKeyPressed(rl.KeyW) {
			wire = !wire
		}
		if rl.IsKeyPressed(rl.KeyG) {
			grid = !grid
		}
		if rl.IsKeyPressed(rl.KeyT) {
			sweep = !sweep
		}
		if rl.IsKeyPressed(rl.KeyR) {
			roll = !roll
		}

		if rl.IsMouseButtonDown(rl.MouseRightButton) || rl.IsMouseButtonDown(rl.MouseMiddleButton) {
			d := rl.GetMouseDelta()
			yaw -= d.X * 0.006
			pitch = clamp(pitch+d.Y*0.006, -1.3, 1.4)
		}
		dist = clamp(dist-rl.GetMouseWheelMove()*1.4, 2.2, 90)

		if sweep {
			turret += dt * 0.6
			gun = float32(math.Sin(float64(turret)*0.7)) * 0.25
		}
		if roll {
			spin += dt * 4
			steer = float32(math.Sin(float64(spin)*0.25)) * 0.45
			// Rotors ride the same toggle: an airframe has no wheels to roll,
			// so R means "turn whatever this thing turns".
			rotor += dt * 6
		}
		// Hull yaw by hand: the one convention the engine and the bake have to
		// agree on is that forward at yaw 0 is +Z and yaw 90 deg is +X.
		if rl.IsKeyDown(rl.KeyQ) {
			hull -= dt * 1.2
		}
		if rl.IsKeyDown(rl.KeyE) {
			hull += dt * 1.2
		}

		asset := reg.Get(assets.ModelID(cur))
		centre := rl.Vector3{Y: asset.Size[1] * 0.45}
		cam.Target = centre
		cam.Position = rl.Vector3{
			X: centre.X + dist*float32(math.Cos(float64(pitch))*math.Sin(float64(yaw))),
			Y: centre.Y + dist*float32(math.Sin(float64(pitch))),
			Z: centre.Z + dist*float32(math.Cos(float64(pitch))*math.Cos(float64(yaw))),
		}

		lod := forceLOD
		if autoLOD {
			lod = reg.PickLOD(assets.ModelID(cur), dist, fovY, screenH)
		}

		pose := assets.Pose{
			Yaw:    hull,
			Turret: turret, Gun: gun, WheelSpin: spin, Steer: steer, Rotor: rotor,
			Tint: rl.White,
		}

		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 92, G: 100, B: 110, A: 255})
		rl.BeginMode3D(cam)

		if grid {
			rl.DrawGrid(40, 1)
			// +Z is forward at yaw 0 — the one convention the whole pipeline
			// leans on, so the sandbox states it out loud.
			rl.DrawLine3D(rl.Vector3{}, rl.Vector3{Z: 6}, rl.Color{R: 90, G: 200, B: 255, A: 255})
			rl.DrawCube(rl.Vector3{Z: 6.2}, 0.3, 0.3, 0.5, rl.Color{R: 90, G: 200, B: 255, A: 255})
			// +X = yaw 90 deg. A hull that reads backwards at 90 and correct at
			// 0 is a yaw sign error, not a reverse gear.
			rl.DrawLine3D(rl.Vector3{}, rl.Vector3{X: 6}, rl.Color{R: 255, G: 110, B: 90, A: 255})
			rl.DrawCube(rl.Vector3{X: 6.2}, 0.5, 0.3, 0.3, rl.Color{R: 255, G: 110, B: 90, A: 255})
		}

		reg.SetLight(light, cam.Position)
		if wire {
			rl.EnableWireMode()
		}
		reg.Draw(assets.ModelID(cur), lod, pose)
		if wire {
			rl.DisableWireMode()
		}

		if showPivots {
			for i := range asset.Parts {
				p := &asset.Parts[i]
				if p.Axis == "" {
					continue
				}
				rl.DrawCubeV(rl.Vector3{X: p.Pivot[0], Y: p.Pivot[1], Z: p.Pivot[2]},
					rl.Vector3{X: 0.12, Y: 0.12, Z: 0.12}, rl.Red)
			}
		}
		if showMounts {
			for i := range asset.Mounts {
				m := &asset.Mounts[i]
				pos, dir, ok := reg.MountWorld(assets.ModelID(cur), m.ID, pose)
				if !ok {
					continue
				}
				c := mountColor(m.ID)
				rl.DrawSphere(pos, 0.07, c)
				rl.DrawLine3D(pos, rl.Vector3Add(pos, rl.Vector3Scale(dir, 0.5)), c)
			}
		}
		rl.EndMode3D()

		drawHUD(asset, names[cur], lod, hull*180/math.Pi, autoLOD, showMounts, showPivots)
		if showMounts {
			drawMountList(asset)
		}
		if *shot != "" && frame >= *shotAt {
			// raylib batches draw calls and TakeScreenshot reads the
			// framebuffer, so the last thing drawn is missing without a flush.
			// A scissor pair is the cheapest one raylib-go exposes — same trap
			// as the game's -shot (ISSUES #27 sweep).
			rl.BeginScissorMode(0, 0, int32(rl.GetScreenWidth()), int32(rl.GetScreenHeight()))
			rl.EndScissorMode()
			rl.TakeScreenshot(*shot)
			rl.EndDrawing()
			return
		}
		rl.EndDrawing()
	}
}

func drawHUD(a *assets.Asset, name string, lod int, yawDeg float32, autoLOD, mounts, pivots bool) {
	rl.DrawRectangle(0, 0, 430, 132, rl.Color{R: 0, G: 0, B: 0, A: 150})
	y := int32(8)
	line := func(s string, c rl.Color) {
		rl.DrawText(s, 10, y, 16, c)
		y += 19
	}
	line(fmt.Sprintf("%s   %s / %s", name, a.Class, a.Side), rl.RayWhite)
	line(fmt.Sprintf("size %.2f x %.2f x %.2f m   forward %s   %.2f MB",
		a.Size[0], a.Size[1], a.Size[2], a.Forward, float32(a.Bytes)/1e6),
		rl.Color{R: 190, G: 200, B: 210, A: 255})
	mode := fmt.Sprintf("LOD %d", lod)
	if autoLOD {
		mode += " (auto)"
	}
	line(fmt.Sprintf("%s   parts %d   mounts %d   yaw %.0f deg", mode, len(a.Parts), len(a.Mounts), yawDeg),
		rl.Color{R: 255, G: 216, B: 25, A: 255})
	flags := ""
	if mounts {
		flags += "mounts "
	}
	if pivots {
		flags += "pivots "
	}
	line(flags, rl.Color{R: 150, G: 200, B: 150, A: 255})
	line("Tab asset  0/1/2 LOD  L auto  M/P/W/G  T turret  R roll  Q/E yaw",
		rl.Color{R: 150, G: 160, B: 170, A: 255})
}

func drawMountList(a *assets.Asset) {
	h := int32(len(a.Mounts)*15 + 16)
	rl.DrawRectangle(screenW-300, 0, 300, h, rl.Color{R: 0, G: 0, B: 0, A: 140})
	for i := range a.Mounts {
		m := &a.Mounts[i]
		rl.DrawText(fmt.Sprintf("%-20s %s", m.ID, m.Part),
			screenW-292, int32(8+i*15), 13, mountColor(m.ID))
	}
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
