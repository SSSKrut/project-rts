package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// TimeDisplay is the read-only snapshot of TimeScale state the top bar
// renders. main.go builds this from core.App fields each frame.
type TimeDisplay struct {
	Scale   float32 // 0 = paused
	Elapsed float32 // game-time seconds since start (scaled)
}

var (
	topBarBG       = rl.Color{R: 18, G: 22, B: 28, A: 255}
	topBarPauseBG  = rl.Color{R: 70, G: 28, B: 28, A: 255}
	topBarBorder   = rl.Color{R: 30, G: 30, B: 36, A: 255}
	topBarText     = rl.Color{R: 230, G: 235, B: 240, A: 255}
	topBarTextDim  = rl.Color{R: 140, G: 150, B: 160, A: 255}
	topBarAccent   = rl.Color{R: 80, G: 180, B: 240, A: 255}
)

// TopBarHitKind labels the interactive zone the cursor is over (or LMB
// pressed on). PanelManager dispatches LMB clicks via TopBarHitTest so
// the top bar feels like a real toolbar without growing dedicated input
// state.
type TopBarHitKind uint8

const (
	TopBarHitNone TopBarHitKind = iota
	TopBarHitPlayPause
	TopBarHitSpeedDown
	TopBarHitSpeedUp
)

// DrawTopBar renders the chromeless top toolbar: T+mm:ss clock on the left,
// play/pause + speed buttons centred, hotkey hint on the right. Background
// flips red when paused. Returns the four hit-rects so main.go can route
// LMB clicks back to TimeScale mutations without re-deriving geometry.
func DrawTopBar(panel Panel, font rl.Font, d TimeDisplay) (playPause, speedDown, speedUp rl.Rectangle) {
	r := panel.Bounds
	bg := topBarBG
	if d.Scale <= 0 {
		bg = topBarPauseBG
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawLine(int32(r.X), int32(r.Y+r.Height-1),
		int32(r.X+r.Width), int32(r.Y+r.Height-1), topBarBorder)

	cy := r.Y + r.Height*0.5
	const clockSize int32 = 16
	mins := int(d.Elapsed) / 60
	secs := int(d.Elapsed) % 60
	clock := fmt.Sprintf("T+%02d:%02d", mins, secs)
	rl.DrawTextEx(font, clock,
		rl.Vector2{X: r.X + 10, Y: cy - float32(clockSize)*0.5},
		float32(clockSize), 1.0, topBarText)

	// Centre cluster: [<<] [▶/⏸ label] [>>]. Buttons share a 26 px square,
	// label between them.
	const btnW float32 = 26
	const gap float32 = 7
	label := fmt.Sprintf("%.0fx", d.Scale)
	if d.Scale <= 0 {
		label = "PAUSED"
	}
	const labelSize int32 = 16
	labelW := rl.MeasureTextEx(font, label, float32(labelSize), 1.0).X
	totalW := btnW*3 + gap*4 + labelW
	startX := r.X + (r.Width-totalW)*0.5
	btnY := r.Y + (r.Height-btnW)*0.5

	speedDown = rl.Rectangle{X: startX, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(speedDown, "<<", font)

	playPause = rl.Rectangle{X: startX + btnW + gap, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(playPause, "", font)
	if d.Scale <= 0 {
		drawPlayGlyph(playPause, topBarAccent)
	} else {
		drawPauseGlyph(playPause, topBarAccent)
	}

	labelX := playPause.X + btnW + gap
	rl.DrawTextEx(font, label,
		rl.Vector2{X: labelX, Y: cy - float32(labelSize)*0.5},
		float32(labelSize), 1.0, topBarText)

	speedUp = rl.Rectangle{X: labelX + labelW + gap, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(speedUp, ">>", font)

	// Right: hotkey hint.
	const hintSize int32 = 13
	hint := "Space pause   +/- speed   click buttons"
	hintW := rl.MeasureTextEx(font, hint, float32(hintSize), 1.0).X
	rl.DrawTextEx(font, hint,
		rl.Vector2{X: r.X + r.Width - hintW - 10, Y: cy - float32(hintSize)*0.5},
		float32(hintSize), 1.0, topBarTextDim)
	return
}

// TopBarHitTest returns which button (if any) sits under the cursor.
// Caller passes the rects returned from DrawTopBar last frame.
func TopBarHitTest(cursor rl.Vector2, playPause, speedDown, speedUp rl.Rectangle) TopBarHitKind {
	if pointInRect(cursor, playPause) {
		return TopBarHitPlayPause
	}
	if pointInRect(cursor, speedDown) {
		return TopBarHitSpeedDown
	}
	if pointInRect(cursor, speedUp) {
		return TopBarHitSpeedUp
	}
	return TopBarHitNone
}

func drawTopBarButton(r rl.Rectangle, glyph string, font rl.Font) {
	rl.DrawRectangleRec(r, rl.Color{R: 30, G: 36, B: 44, A: 255})
	rl.DrawRectangleLinesEx(r, 1, topBarBorder)
	if glyph != "" {
		const sz int32 = 14
		m := rl.MeasureTextEx(font, glyph, float32(sz), 1.0)
		rl.DrawTextEx(font, glyph,
			rl.Vector2{X: r.X + (r.Width-m.X)*0.5, Y: r.Y + (r.Height-m.Y)*0.5},
			float32(sz), 1.0, topBarText)
	}
}

func drawPlayGlyph(r rl.Rectangle, c rl.Color) {
	pad := r.Width * 0.28
	v1 := rl.Vector2{X: r.X + pad, Y: r.Y + pad}
	v2 := rl.Vector2{X: r.X + pad, Y: r.Y + r.Height - pad}
	v3 := rl.Vector2{X: r.X + r.Width - pad, Y: r.Y + r.Height*0.5}
	rl.DrawTriangle(v1, v2, v3, c)
}

func drawPauseGlyph(r rl.Rectangle, c rl.Color) {
	pad := r.Width * 0.28
	barW := (r.Width - 2*pad) / 3
	rl.DrawRectangle(int32(r.X+pad), int32(r.Y+pad),
		int32(barW), int32(r.Height-2*pad), c)
	rl.DrawRectangle(int32(r.X+r.Width-pad-barW), int32(r.Y+pad),
		int32(barW), int32(r.Height-2*pad), c)
}
