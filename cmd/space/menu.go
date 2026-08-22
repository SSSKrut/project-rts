package main

import (
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
)

type menuItem struct {
	label string
	on    bool
}

type menu struct {
	items []menuItem
	sel   int
	glow  []float32
	rects []rl.Rectangle
	quit  bool
	note  string
	noteT float32
}

func newMenu() *menu {
	m := &menu{items: []menuItem{
		{"NEW OPERATION", true},
		{"CONTINUE", false},
		{"SKIRMISH", true},
		{"SETTINGS", true},
		{"QUIT", true},
	}}
	m.glow = make([]float32, len(m.items))
	m.rects = make([]rl.Rectangle, len(m.items))
	return m
}

func (m *menu) update(dt float32) {
	if rl.IsKeyPressed(rl.KeyDown) || rl.IsKeyPressed(rl.KeyS) {
		m.step(1)
	}
	if rl.IsKeyPressed(rl.KeyUp) || rl.IsKeyPressed(rl.KeyW) {
		m.step(-1)
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		m.quit = true
	}

	mp := rl.GetMousePosition()
	for i, rec := range m.rects {
		if rec.Width > 0 && rl.CheckCollisionPointRec(mp, rec) && m.items[i].on {
			m.sel = i
			if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
				m.activate()
			}
		}
	}
	if rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeySpace) {
		m.activate()
	}

	for i := range m.glow {
		want := float32(0)
		if i == m.sel {
			want = 1
		}
		m.glow[i] += (want - m.glow[i]) * min32(1, dt*11)
	}
	if m.noteT > 0 {
		m.noteT -= dt
	}
}

func (m *menu) step(d int) {
	for k := 0; k < len(m.items); k++ {
		m.sel = (m.sel + d + len(m.items)) % len(m.items)
		if m.items[m.sel].on {
			return
		}
	}
}

func (m *menu) activate() {
	it := m.items[m.sel]
	if !it.on {
		return
	}
	if it.label == "QUIT" {
		m.quit = true
		return
	}
	m.note = it.label + " - not wired up yet"
	m.noteT = 2.5
}

// draw lays the menu out against the current window size so the column keeps
// its proportions on any resolution; rects are recorded for next frame's hover
// test.
func (m *menu) draw(font rl.Font, w, h, fade float32) {
	x := w * 0.085
	titleSize := h * 0.050
	subSize := h * 0.0165
	itemSize := h * 0.027
	step := h * 0.058

	dim := func(c rl.Color, a float32) rl.Color {
		c.A = uint8(float32(c.A) * a * fade)
		return c
	}

	titleY := h*0.5 - step*float32(len(m.items))*0.5 - h*0.17
	rl.DrawTextEx(font, "PROJECT RTS", rl.Vector2{X: x, Y: titleY}, titleSize, titleSize*0.22,
		dim(rl.NewColor(226, 233, 244, 255), 1))
	lineY := titleY + titleSize*1.35
	rl.DrawRectangleGradientH(int32(x), int32(lineY), int32(w*0.20), 1,
		dim(rl.NewColor(120, 160, 220, 190), 1), dim(rl.NewColor(120, 160, 220, 0), 1))
	rl.DrawTextEx(font, "COLD WAR TACTICAL OPERATIONS",
		rl.Vector2{X: x, Y: lineY + h*0.018}, subSize, subSize*0.28,
		dim(rl.NewColor(126, 146, 176, 255), 1))

	y := h*0.5 - step*float32(len(m.items))*0.5 + h*0.06
	for i, it := range m.items {
		g := m.glow[i]
		col := rl.NewColor(132, 146, 168, 255)
		if !it.on {
			col = rl.NewColor(78, 86, 100, 255)
		} else if g > 0 {
			col = lerpColor(col, rl.NewColor(238, 244, 252, 255), g)
		}

		ind := x + g*h*0.022 // selected items step to the right
		size := rl.MeasureTextEx(font, it.label, itemSize, itemSize*0.18)
		m.rects[i] = rl.NewRectangle(x-h*0.02, y-h*0.012, size.X+h*0.09, size.Y+h*0.024)

		if g > 0.01 {
			rl.DrawRectangleGradientH(int32(x-h*0.02), int32(y-h*0.011),
				int32(size.X+h*0.10), int32(size.Y+h*0.022),
				dim(rl.NewColor(90, 130, 205, 46), g), dim(rl.NewColor(90, 130, 205, 0), g))
			rl.DrawRectangle(int32(x-h*0.020), int32(y-h*0.011), 2, int32(size.Y+h*0.022),
				dim(rl.NewColor(150, 190, 255, 235), g))
		}
		rl.DrawTextEx(font, it.label, rl.Vector2{X: ind, Y: y}, itemSize, itemSize*0.18, dim(col, 1))
		y += step
	}

	hint := "UP/DOWN select    ENTER confirm    ESC exit"
	hs := h * 0.0155
	rl.DrawTextEx(font, hint, rl.Vector2{X: x, Y: h - h*0.085}, hs, hs*0.28,
		dim(rl.NewColor(92, 104, 124, 255), 1))

	ver := "prototype build 0.1"
	vs := rl.MeasureTextEx(font, ver, hs, hs*0.28)
	rl.DrawTextEx(font, ver, rl.Vector2{X: w - vs.X - w*0.03, Y: h - h*0.085}, hs, hs*0.28,
		dim(rl.NewColor(72, 82, 100, 255), 1))

	if m.noteT > 0 {
		a := min32(1, m.noteT/0.4)
		ns := h * 0.017
		rl.DrawTextEx(font, m.note, rl.Vector2{X: x, Y: h - h*0.125}, ns, ns*0.28,
			dim(rl.NewColor(190, 170, 120, 255), a))
	}
}

func lerpColor(a, b rl.Color, t float32) rl.Color {
	l := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return rl.NewColor(l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), l(a.A, b.A))
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// Same probe list the in-game HUD uses: first monospace TTF that loads wins,
// otherwise raylib's engine-owned default (which must not be unloaded).
var fontPaths = []string{
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf",
	"/usr/share/fonts/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
	"/usr/share/fonts/TTF/DejaVuSansMono-Bold.ttf",
	"/System/Library/Fonts/Menlo.ttc",
	"C:/Windows/Fonts/consola.ttf",
}

func loadMenuFont() (rl.Font, bool) {
	for _, p := range fontPaths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		f := rl.LoadFontEx(p, 96, nil)
		if f.BaseSize > 0 {
			rl.SetTextureFilter(f.Texture, rl.FilterBilinear)
			return f, true
		}
	}
	return rl.GetFontDefault(), false
}
