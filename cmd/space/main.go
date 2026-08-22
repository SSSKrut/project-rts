// Space menu - standalone harness for the game's start screen. Deep space,
// faint traces of distant suns, and a lit planet in the middle, all drawn by
// one fullscreen shader (see shader.go); the menu column is ordinary 2D chrome
// on top.
//
// Controls: Up/Down or W/S move, Enter/Space activate, mouse hovers and clicks,
// Esc quits.
//
//	go build -o bin/space ./cmd/space
//	./bin/space -shot=/tmp/space.png -shot-at=90
package main

import (
	"flag"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

var (
	widthFlag  = flag.Int("w", 1600, "window width")
	heightFlag = flag.Int("h", 900, "window height")
	shotFlag   = flag.String("shot", "", "dev: write a PNG of frame -shot-at, then exit")
	shotAtFlag = flag.Int("shot-at", 90, "dev: frame index for -shot")
)

// Planet placement in uv units (origin at screen centre, y up, one unit = the
// window height). Off-centre to the right leaves the left third to the menu
// without the column ever sitting on the lit limb.
const (
	planetX   = 0.155
	planetY   = 0.015
	planetRad = 0.305

	parallaxAmp = 0.020
	fadeInSecs  = 1.6
)

var sunDir = normalize3(0.62, 0.44, 0.65)

type uniforms struct {
	res, time, par, sun, ctr, rad, fade int32
}

func main() {
	flag.Parse()

	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagVsyncHint)
	rl.InitWindow(int32(*widthFlag), int32(*heightFlag), "Project RTS")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)
	rl.SetExitKey(0) // Esc is the menu's own, not raylib's kill switch

	shader := rl.LoadShaderFromMemory(spaceVS, spaceFS)
	defer rl.UnloadShader(shader)
	u := uniforms{
		res:  rl.GetShaderLocation(shader, "uRes"),
		time: rl.GetShaderLocation(shader, "uTime"),
		par:  rl.GetShaderLocation(shader, "uPar"),
		sun:  rl.GetShaderLocation(shader, "uSunDir"),
		ctr:  rl.GetShaderLocation(shader, "uCtr"),
		rad:  rl.GetShaderLocation(shader, "uRad"),
		fade: rl.GetShaderLocation(shader, "uFade"),
	}

	// A 1x1 white texture is the cheapest way to get a fullscreen quad through
	// raylib's batch; the shader ignores it and reads gl_FragCoord.
	img := rl.GenImageColor(1, 1, rl.White)
	quad := rl.LoadTextureFromImage(img)
	rl.UnloadImage(img)
	defer rl.UnloadTexture(quad)

	font, owned := loadMenuFont()
	if owned {
		defer rl.UnloadFont(font)
	}

	menu := newMenu()
	var t, parX, parY float32
	frame := 0

	for !rl.WindowShouldClose() && !menu.quit {
		dt := rl.GetFrameTime()
		if dt > 0.25 {
			dt = 0.25
		}
		t += dt
		frame++

		w, h := float32(rl.GetScreenWidth()), float32(rl.GetScreenHeight())

		// Parallax follows the cursor with a lag, so the starfield drifts
		// behind the planet instead of snapping to the mouse.
		mp := rl.GetMousePosition()
		tx := (mp.X/w - 0.5) * -2 * parallaxAmp
		ty := (mp.Y/h - 0.5) * 2 * parallaxAmp
		k := 1 - float32(math.Exp(float64(-dt*2.5)))
		parX += (tx - parX) * k
		parY += (ty - parY) * k

		fade := t / fadeInSecs
		if fade > 1 {
			fade = 1
		}
		fade = fade * fade * (3 - 2*fade)

		menu.update(dt)

		rl.SetShaderValue(shader, u.res, []float32{w, h}, rl.ShaderUniformVec2)
		rl.SetShaderValue(shader, u.time, []float32{t}, rl.ShaderUniformFloat)
		rl.SetShaderValue(shader, u.par, []float32{parX, parY}, rl.ShaderUniformVec2)
		rl.SetShaderValue(shader, u.sun, sunDir[:], rl.ShaderUniformVec3)
		rl.SetShaderValue(shader, u.ctr, []float32{planetX, planetY}, rl.ShaderUniformVec2)
		rl.SetShaderValue(shader, u.rad, []float32{planetRad}, rl.ShaderUniformFloat)
		rl.SetShaderValue(shader, u.fade, []float32{fade}, rl.ShaderUniformFloat)

		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)

		rl.BeginShaderMode(shader)
		rl.DrawTexturePro(quad,
			rl.NewRectangle(0, 0, 1, 1), rl.NewRectangle(0, 0, w, h),
			rl.Vector2{}, 0, rl.White)
		rl.EndShaderMode()

		menu.draw(font, w, h, fade)

		if *shotFlag != "" && frame >= *shotAtFlag {
			// raylib batches draw calls; a scissor pair is the cheapest flush
			// it exposes, and without it the last thing drawn is missing from
			// the capture.
			rl.BeginScissorMode(0, 0, int32(w), int32(h))
			rl.EndScissorMode()
			rl.TakeScreenshot(*shotFlag)
			rl.EndDrawing()
			return
		}
		rl.EndDrawing()
	}
}

func normalize3(x, y, z float32) [3]float32 {
	l := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	return [3]float32{x / l, y / l, z / l}
}
