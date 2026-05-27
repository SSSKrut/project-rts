package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// DebugToggle: On points at the bool the widget mutates on click.
type DebugToggle struct {
	Label string
	On    *bool
}

var (
	debugPanelBG    = rl.Color{R: 20, G: 24, B: 30, A: 250}
	debugRowHover   = rl.Color{R: 40, G: 50, B: 65, A: 200}
	debugTextOn     = rl.Color{R: 230, G: 240, B: 250, A: 255}
	debugTextOff    = rl.Color{R: 150, G: 158, B: 168, A: 255}
	debugCheckOn    = rl.Color{R: 80, G: 200, B: 120, A: 255}
	debugCheckOff   = rl.Color{R: 60, G: 68, B: 80, A: 255}
	debugCheckEdge  = rl.Color{R: 110, G: 120, B: 135, A: 255}
	debugSubtitle   = rl.Color{R: 130, G: 140, B: 152, A: 255}
	debugRadiusInfo = rl.Color{R: 170, G: 180, B: 195, A: 255}
)

const (
	debugRowHeight  float32 = 24
	debugCheckSize  float32 = 14
	debugPad        float32 = 8
	debugTitleSize  int32   = 15
	debugRowSize    int32   = 13
	debugFooterSize int32   = 11
)

// DrawDebugPanel mutates `toggles[i].On` directly on click. The `footer`
// string renders below as a single-line info hint.
func DrawDebugPanel(panel Panel, font rl.Font, toggles []DebugToggle,
	cursor rl.Vector2, lmbPress bool, footer string) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, debugPanelBG)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}

	x := content.X + debugPad
	y := content.Y + debugPad

	title := "Overlays"
	rl.DrawTextEx(font, title, rl.Vector2{X: x, Y: y},
		float32(debugTitleSize), 1.0, debugTextOn)
	y += float32(debugTitleSize) + debugPad

	sub := "Click a row to toggle"
	rl.DrawTextEx(font, sub, rl.Vector2{X: x, Y: y},
		float32(debugFooterSize), 1.0, debugSubtitle)
	y += float32(debugFooterSize) + debugPad

	rowW := content.Width - 2*debugPad
	for i := range toggles {
		row := rl.Rectangle{X: x, Y: y, Width: rowW, Height: debugRowHeight}
		hovered := cursor.X >= row.X && cursor.X <= row.X+row.Width &&
			cursor.Y >= row.Y && cursor.Y <= row.Y+row.Height
		if hovered {
			rl.DrawRectangleRec(row, debugRowHover)
			if lmbPress && toggles[i].On != nil {
				*toggles[i].On = !*toggles[i].On
			}
		}

		boxY := row.Y + (debugRowHeight-debugCheckSize)*0.5
		box := rl.Rectangle{X: row.X + 2, Y: boxY, Width: debugCheckSize, Height: debugCheckSize}
		on := toggles[i].On != nil && *toggles[i].On
		fill := debugCheckOff
		if on {
			fill = debugCheckOn
		}
		rl.DrawRectangleRec(box, fill)
		rl.DrawRectangleLinesEx(box, 1, debugCheckEdge)

		textCol := debugTextOff
		if on {
			textCol = debugTextOn
		}
		textX := box.X + box.Width + debugPad
		textY := row.Y + (debugRowHeight-float32(debugRowSize))*0.5
		rl.DrawTextEx(font, toggles[i].Label, rl.Vector2{X: textX, Y: textY},
			float32(debugRowSize), 1.0, textCol)

		y += debugRowHeight + 2
	}

	if footer != "" {
		y += debugPad
		rl.DrawTextEx(font, footer, rl.Vector2{X: x, Y: y},
			float32(debugFooterSize), 1.0, debugRadiusInfo)
	}
}
