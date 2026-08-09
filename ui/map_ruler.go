package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// The map ruler (Phase 19.7 M4). V-hold answers "what can I see from there";
// this answers the cheaper question the map kept refusing to answer at all —
// how far apart are those two things, and on what bearing.

// RulerRange tints the readout against the selection's reach: green inside,
// red outside, neutral when nothing selected carries a weapon.
type RulerRange uint8

const (
	RulerNoWeapon RulerRange = iota
	RulerInRange
	RulerOutOfRange
)

var (
	rulerLine    = rl.Color{R: 235, G: 235, B: 240, A: 220}
	rulerEnd     = rl.Color{R: 255, G: 255, B: 255, A: 240}
	rulerBoxBG   = rl.Color{R: 12, G: 16, B: 22, A: 225}
	rulerNeutral = rl.Color{R: 230, G: 234, B: 240, A: 255}
	rulerInCol   = rl.Color{R: 130, G: 220, B: 140, A: 255}
	rulerOutCol  = rl.Color{R: 235, G: 120, B: 100, A: 255}
)

// Bearing is degrees clockwise from north, and north is -Z: the map draws
// world +Z downward, so "up the screen" is negative Z.
func RulerBearing(from, to components.WorldPos) float32 {
	d := to.Sub(from)
	deg := float32(math.Atan2(float64(d.X), float64(-d.Z)) * 180 / math.Pi)
	if deg < 0 {
		deg += 360
	}
	return deg
}

// DrawMapRuler paints the measuring line and its readout inside the map panel.
func DrawMapRuler(panel Panel, cam MapCamera, from, to components.WorldPos,
	font rl.Font, rng RulerRange) {
	content := ContentRect(panel)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}
	a := MapWorldToPanel(from, cam, content)
	b := MapWorldToPanel(to, cam, content)

	rl.BeginScissorMode(int32(content.X), int32(content.Y),
		int32(content.Width), int32(content.Height))
	rl.DrawLineEx(a, b, 1.5, rulerLine)
	rl.DrawCircleV(a, 3, rulerEnd)
	rl.DrawCircleV(b, 3, rulerEnd)

	label := fmt.Sprintf("%.0f m   brg %03.0f",
		components.Distance(from, to), RulerBearing(from, to))
	const fontSize float32 = 14
	m := rl.MeasureTextEx(font, label, fontSize, 1.0)
	box := rl.Rectangle{X: b.X + 10, Y: b.Y - m.Y - 8, Width: m.X + 12, Height: m.Y + 8}
	// Keep the readout inside the panel: near the right or top edge it flips
	// to the other side of the cursor rather than being clipped away.
	if box.X+box.Width > content.X+content.Width {
		box.X = b.X - 10 - box.Width
	}
	if box.Y < content.Y {
		box.Y = b.Y + 10
	}
	rl.DrawRectangleRec(box, rulerBoxBG)
	rl.DrawRectangleLinesEx(box, 1, rulerLine)
	col := rulerNeutral
	switch rng {
	case RulerInRange:
		col = rulerInCol
	case RulerOutOfRange:
		col = rulerOutCol
	}
	rl.DrawTextEx(font, label,
		rl.Vector2{X: box.X + 6, Y: box.Y + 4}, fontSize, 1.0, col)
	rl.EndScissorMode()
}
