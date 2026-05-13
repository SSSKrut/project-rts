package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// TimeDisplay is the read-only snapshot of TimeScale state the time panel
// renders. main.go builds this from core.App fields each frame.
type TimeDisplay struct {
	Scale   float32 // 0 = paused
	Elapsed float32 // game-time seconds since start (scaled)
}

var (
	timeBG         = rl.Color{R: 20, G: 24, B: 30, A: 255}
	timePauseBG    = rl.Color{R: 80, G: 30, B: 30, A: 255}
	timeText       = rl.Color{R: 230, G: 235, B: 240, A: 255}
	timeTextDim    = rl.Color{R: 140, G: 150, B: 160, A: 255}
	timeAccent     = rl.Color{R: 80, G: 180, B: 240, A: 255}
)

// DrawTimePanel renders the bottom time strip: a centred play/pause icon, a
// large multiplier readout, and a small hotkey hint. Background flips red
// when the game is paused so the player notices.
func DrawTimePanel(panel Panel, font rl.Font, d TimeDisplay) {
	content := ContentRect(panel)
	bg := timeBG
	if d.Scale <= 0 {
		bg = timePauseBG
	}
	rl.DrawRectangleRec(content, bg)

	cx := int32(content.X + content.Width*0.5)
	cy := int32(content.Y + content.Height*0.5)

	// Icon + label centred horizontally.
	label := fmt.Sprintf("%.0fx", d.Scale)
	if d.Scale == 1 {
		label = "1x"
	}
	if d.Scale <= 0 {
		label = "PAUSED"
	}
	const labelSize int32 = 22
	labelW := int32(rl.MeasureTextEx(font, label, float32(labelSize), 1.0).X)
	iconSize := int32(content.Height) - 16
	if iconSize < 12 {
		iconSize = 12
	}
	totalW := labelW + iconSize + 12

	x := cx - totalW/2
	iconY := cy - iconSize/2
	if d.Scale <= 0 {
		drawPlayIcon(int32(x), iconY, iconSize, timeAccent)
	} else {
		drawPauseIcon(int32(x), iconY, iconSize, timeAccent)
	}
	rl.DrawTextEx(font, label,
		rl.Vector2{X: float32(x + iconSize + 12), Y: float32(cy) - float32(labelSize)*0.5},
		float32(labelSize), 1.0, timeText)

	// Hotkey hint on the right side.
	const hintSize int32 = 13
	hint := "Space pause   +/- speed"
	hintW := int32(rl.MeasureTextEx(font, hint, float32(hintSize), 1.0).X)
	rl.DrawTextEx(font, hint,
		rl.Vector2{X: content.X + content.Width - float32(hintW) - 12, Y: float32(cy) - float32(hintSize)*0.5},
		float32(hintSize), 1.0, timeTextDim)

	// Game-time clock on the left.
	const clockSize int32 = 14
	mins := int(d.Elapsed) / 60
	secs := int(d.Elapsed) % 60
	clock := fmt.Sprintf("T+%02d:%02d", mins, secs)
	rl.DrawTextEx(font, clock,
		rl.Vector2{X: content.X + 12, Y: float32(cy) - float32(clockSize)*0.5},
		float32(clockSize), 1.0, timeTextDim)
}

// drawPauseIcon renders a 2-bar vertical pause glyph.
func drawPauseIcon(x, y, size int32, c rl.Color) {
	barW := size / 4
	gap := size / 6
	rl.DrawRectangle(x, y, barW, size, c)
	rl.DrawRectangle(x+barW+gap, y, barW, size, c)
}

// drawPlayIcon renders a right-pointing triangle.
func drawPlayIcon(x, y, size int32, c rl.Color) {
	v1 := rl.Vector2{X: float32(x), Y: float32(y)}
	v2 := rl.Vector2{X: float32(x), Y: float32(y + size)}
	v3 := rl.Vector2{X: float32(x + size), Y: float32(y) + float32(size)*0.5}
	rl.DrawTriangle(v1, v2, v3, c)
}
