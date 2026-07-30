// Generator sandbox — standalone viewer for the synthetic object generators
// (PHASE-17.5-SYNTH). Boots in milliseconds: no world, no ECS, no ticks. It
// draws BuildingPlan values straight from gen/buildings through the SAME
// render/ helpers the game uses, so what shows here is what shows in game.
//
// Controls: MMB/RMB drag = orbit, wheel = zoom, F = frame all, R = reroll
// seed, M = matrix mode, Esc = quit. Click a validation issue to fly to it.
package main

import (
	"flag"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/ui"
)

var (
	shotFlag     = flag.String("shot", "", "render N frames, write a PNG here and exit")
	shotAtFlag   = flag.Int("shot-at", 2, "frame index to capture")
	templateFlag = flag.Int("template", tplHouse, "0 house, 1 office, 2 compound, 3 compound+")
	matrixFlag   = flag.Bool("matrix", false, "start in matrix mode")
	seedFlag     = flag.Uint64("seed", 0xA1, "base seed")
	storiesFlag  = flag.Int("stories", 2, "storeys (house template)")
)

const (
	screenW = 1500
	screenH = 900

	leftW    = 250.0
	rightW   = 330.0
	statusH  = 66.0
	panelPad = 8.0

	fovY = 55.0
)

type sandbox struct {
	params  genParams
	cells   []cell
	focused int

	// View
	wallMode      int // index into wallModeNames
	levelFilter   int // -1 = all levels, else level index
	showGrid      bool
	showFootprint bool
	showRoof      bool

	// Camera
	yaw, pitch, dist float32
	target           rl.Vector3
	orbiting         bool

	style    ui.Style
	scroll   ui.ScrollState
	lastStep int

	// Scene RT — sized to the viewport so BeginMode3D's aspect matches the
	// box we composite into, instead of the whole window.
	rt       rl.RenderTexture2D
	rtW, rtH int32
}

func (sb *sandbox) ensureRT(vp rl.Rectangle) {
	w, h := int32(vp.Width), int32(vp.Height)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w == sb.rtW && h == sb.rtH {
		return
	}
	if sb.rtW != 0 {
		rl.UnloadRenderTexture(sb.rt)
	}
	sb.rt = rl.LoadRenderTexture(w, h)
	sb.rtW, sb.rtH = w, h
}

func main() {
	flag.Parse()
	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagMsaa4xHint)
	rl.InitWindow(screenW, screenH, "RTS - Generator Sandbox")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	st := ui.DebugStyle(rl.GetFontDefault())
	st.FontSize = 14
	st.RowH = 19
	st.Disabled = rl.Color{R: 30, G: 34, B: 40, A: 255}

	params := defaultParams()
	params.template = *templateFlag
	params.matrix = *matrixFlag
	params.seed = *seedFlag
	params.stories = *storiesFlag

	sb := &sandbox{
		params:        params,
		levelFilter:   -1,
		showGrid:      true,
		showFootprint: true,
		showRoof:      true,
		yaw:           float32(math.Pi * 0.25),
		pitch:         float32(math.Pi * 0.17),
		dist:          38,
		style:         st,
	}
	sb.rebuild()
	sb.frameAll()
	defer func() {
		if sb.rtW != 0 {
			rl.UnloadRenderTexture(sb.rt)
		}
	}()

	frame := 0
	for !rl.WindowShouldClose() {
		sb.handleInput()

		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 18, G: 20, B: 24, A: 255})

		sb.drawScene()
		sb.drawPanels()

		rl.EndDrawing()

		frame++
		if *shotFlag != "" && frame >= *shotAtFlag {
			rl.TakeScreenshot(*shotFlag)
			return
		}
	}
}

func (sb *sandbox) viewport() rl.Rectangle {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	return rl.Rectangle{
		X:      leftW,
		Y:      0,
		Width:  w - leftW - rightW,
		Height: h - statusH,
	}
}

func (sb *sandbox) camera() rl.Camera3D {
	cp := rl.Vector3{
		X: sb.target.X + sb.dist*float32(math.Cos(float64(sb.pitch))*math.Sin(float64(sb.yaw))),
		Y: sb.target.Y + sb.dist*float32(math.Sin(float64(sb.pitch))),
		Z: sb.target.Z + sb.dist*float32(math.Cos(float64(sb.pitch))*math.Cos(float64(sb.yaw))),
	}
	return rl.Camera3D{
		Position:   cp,
		Target:     sb.target,
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       fovY,
		Projection: rl.CameraPerspective,
	}
}

func (sb *sandbox) handleInput() {
	cursor := rl.GetMousePosition()
	inView := pointIn(cursor, sb.viewport())

	orbitHeld := rl.IsMouseButtonDown(rl.MouseMiddleButton) || rl.IsMouseButtonDown(rl.MouseRightButton)
	if orbitHeld && (inView || sb.orbiting) {
		sb.orbiting = true
		d := rl.GetMouseDelta()
		sb.yaw -= d.X * 0.006
		sb.pitch += d.Y * 0.006
		const lim = float32(math.Pi*0.5) - 0.05
		if sb.pitch > lim {
			sb.pitch = lim
		}
		if sb.pitch < -0.2 {
			sb.pitch = -0.2
		}
	}
	if !orbitHeld {
		sb.orbiting = false
	}

	if inView {
		if w := rl.GetMouseWheelMove(); w != 0 {
			sb.dist *= float32(math.Pow(0.88, float64(w)))
			if sb.dist < 3 {
				sb.dist = 3
			}
			if sb.dist > 900 {
				sb.dist = 900
			}
		}
	}

	if rl.IsKeyPressed(rl.KeyF) {
		sb.frameAll()
	}
	if rl.IsKeyPressed(rl.KeyR) {
		sb.params.seed = splitMix64(sb.params.seed)
		sb.rebuild()
	}
	if rl.IsKeyPressed(rl.KeyM) {
		sb.params.matrix = !sb.params.matrix
		sb.rebuild()
		sb.frameAll()
	}
}

// frameAll fits the camera around every generated plan.
func (sb *sandbox) frameAll() {
	if len(sb.cells) == 0 {
		return
	}
	minX, maxX := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	minZ, maxZ := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	maxY := float32(0)
	for i := range sb.cells {
		b := sb.cells[i].bounds
		minX = minF(minX, b.MinX)
		maxX = maxF(maxX, b.MaxX)
		minZ = minF(minZ, b.MinZ)
		maxZ = maxF(maxZ, b.MaxZ)
		maxY = maxF(maxY, sb.cells[i].topY)
	}
	sb.target = rl.Vector3{X: 0.5 * (minX + maxX), Y: maxY * 0.4, Z: 0.5 * (minZ + maxZ)}
	// Fit the larger of footprint span and building height into the vertical
	// FOV. A single sample wants extra margin — at this pitch its near wall is
	// much closer than its centre; a wide matrix is flat and needs almost none.
	span := maxF(maxF(maxX-minX, maxZ-minZ), maxY)
	margin := float32(1.9)
	if span > 25 {
		margin = 1.25
	}
	sb.dist = maxF(span/(2*float32(math.Tan(float64(fovY*0.5*math.Pi/180))))*margin, 16)
}

func (sb *sandbox) flyTo(p rl.Vector3) {
	sb.target = p
	sb.dist = 16
}

func pointIn(p rl.Vector2, r rl.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// splitMix64 mirrors the generator's hash so rerolled seeds walk the same
// sequence the map data would.
func splitMix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
