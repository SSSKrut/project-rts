package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// DebugToggle is one checkbox row in the Debug widget. Label is the player-
// facing text, On points at the bool the widget mutates on click. The widget
// has no knowledge of which subsystem the bool drives — main.go owns the
// state struct and builds the toggle list each frame.
type DebugToggle struct {
	Label string
	On    *bool
}

// Visual constants for the Debug panel — kept private to this file so the
// rest of the UI palette isn't polluted.
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

// DrawDebugPanel paints the Debug toggles widget and returns nothing — any
// click hit-test mutates `toggles[i].On` directly. Click input is gated by
// `lmbPress` (the same edge-trigger the workspace passes around so we don't
// double-toggle on a single mouse-down event).
//
// The `footer` string is rendered below the toggle list as a single-line
// info hint; main.go uses it to surface the active overlay-radius (e.g.
// "Radius: 2 chunks around camera").
func DrawDebugPanel(panel Panel, font rl.Font, toggles []DebugToggle,
	cursor rl.Vector2, lmbPress bool, footer string) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, debugPanelBG)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}

	x := content.X + debugPad
	y := content.Y + debugPad

	// Title.
	title := "Overlays"
	rl.DrawTextEx(font, title, rl.Vector2{X: x, Y: y},
		float32(debugTitleSize), 1.0, debugTextOn)
	y += float32(debugTitleSize) + debugPad

	// Subtitle line.
	sub := "Click a row to toggle"
	rl.DrawTextEx(font, sub, rl.Vector2{X: x, Y: y},
		float32(debugFooterSize), 1.0, debugSubtitle)
	y += float32(debugFooterSize) + debugPad

	// Rows.
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

		// Checkbox box.
		boxY := row.Y + (debugRowHeight-debugCheckSize)*0.5
		box := rl.Rectangle{X: row.X + 2, Y: boxY, Width: debugCheckSize, Height: debugCheckSize}
		on := toggles[i].On != nil && *toggles[i].On
		fill := debugCheckOff
		if on {
			fill = debugCheckOn
		}
		rl.DrawRectangleRec(box, fill)
		rl.DrawRectangleLinesEx(box, 1, debugCheckEdge)

		// Label.
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

	// Footer info.
	if footer != "" {
		y += debugPad
		rl.DrawTextEx(font, footer, rl.Vector2{X: x, Y: y},
			float32(debugFooterSize), 1.0, debugRadiusInfo)
	}
}
