package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// DebugToggle: On points at the bool the widget mutates on click.
type DebugToggle struct {
	Label string
	On    *bool
}

// DebugButton — immediate-mode button; Armed draws the accent background
// (spawn palette selection).
type DebugButton struct {
	Label string
	Armed bool
}

// DebugPanelCtx bundles everything DrawDebugPanel renders. Clicked button
// indices come back via the return values (-1 = none).
type DebugPanelCtx struct {
	Cursor   rl.Vector2
	LMBPress bool

	Toggles []DebugToggle

	SimLabel   string
	SimButtons []DebugButton

	SpawnLabel   string
	SpawnButtons []DebugButton

	DumpLines []string

	Footer string
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
	debugBtnBG      = rl.Color{R: 45, G: 54, B: 68, A: 255}
	debugBtnArmed   = rl.Color{R: 70, G: 140, B: 100, A: 255}
	debugDumpText   = rl.Color{R: 185, G: 195, B: 205, A: 255}
)

const (
	debugRowHeight  float32 = 24
	debugCheckSize  float32 = 14
	debugPad        float32 = 8
	debugTitleSize  int32   = 15
	debugRowSize    int32   = 13
	debugFooterSize int32   = 11
	debugBtnHeight  float32 = 22
	debugDumpSize   int32   = 11
)

// DrawDebugPanel mutates toggle bools directly; returns the clicked indices
// of SimButtons and SpawnButtons (-1 = none this frame).
func DrawDebugPanel(panel Panel, font rl.Font, ctx DebugPanelCtx) (simClicked, spawnClicked int) {
	simClicked, spawnClicked = -1, -1
	content := ContentRect(panel)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}
	rl.DrawRectangleRec(content, debugPanelBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y),
		int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	x := content.X + debugPad
	y := content.Y + debugPad
	rowW := content.Width - 2*debugPad

	section := func(title string) {
		rl.DrawTextEx(font, title, rl.Vector2{X: x, Y: y},
			float32(debugTitleSize), 1.0, debugTextOn)
		y += float32(debugTitleSize) + 4
	}

	// Sim controls.
	if len(ctx.SimButtons) > 0 || ctx.SimLabel != "" {
		section("Sim")
		if ctx.SimLabel != "" {
			rl.DrawTextEx(font, ctx.SimLabel, rl.Vector2{X: x, Y: y},
				float32(debugFooterSize), 1.0, debugSubtitle)
			y += float32(debugFooterSize) + 4
		}
		simClicked = drawDebugButtonRow(font, ctx.SimButtons, x, &y, rowW,
			ctx.Cursor, ctx.LMBPress)
		y += debugPad
	}

	// Spawn palette.
	if len(ctx.SpawnButtons) > 0 {
		section("Spawn")
		if ctx.SpawnLabel != "" {
			rl.DrawTextEx(font, ctx.SpawnLabel, rl.Vector2{X: x, Y: y},
				float32(debugFooterSize), 1.0, debugSubtitle)
			y += float32(debugFooterSize) + 4
		}
		spawnClicked = drawDebugButtonRow(font, ctx.SpawnButtons, x, &y, rowW,
			ctx.Cursor, ctx.LMBPress)
		y += debugPad
	}

	// Overlay toggles.
	section("Overlays")
	for i := range ctx.Toggles {
		row := rl.Rectangle{X: x, Y: y, Width: rowW, Height: debugRowHeight}
		hovered := ctx.Cursor.X >= row.X && ctx.Cursor.X <= row.X+row.Width &&
			ctx.Cursor.Y >= row.Y && ctx.Cursor.Y <= row.Y+row.Height
		if hovered {
			rl.DrawRectangleRec(row, debugRowHover)
			if ctx.LMBPress && ctx.Toggles[i].On != nil {
				*ctx.Toggles[i].On = !*ctx.Toggles[i].On
			}
		}

		boxY := row.Y + (debugRowHeight-debugCheckSize)*0.5
		box := rl.Rectangle{X: row.X + 2, Y: boxY, Width: debugCheckSize, Height: debugCheckSize}
		on := ctx.Toggles[i].On != nil && *ctx.Toggles[i].On
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
		rl.DrawTextEx(font, ctx.Toggles[i].Label,
			rl.Vector2{X: box.X + box.Width + debugPad, Y: row.Y + (debugRowHeight-float32(debugRowSize))*0.5},
			float32(debugRowSize), 1.0, textCol)

		y += debugRowHeight + 2
	}

	// Raw component dump of the selected entity.
	if len(ctx.DumpLines) > 0 {
		y += debugPad
		section("Entity")
		for _, ln := range ctx.DumpLines {
			if y > content.Y+content.Height {
				break
			}
			rl.DrawTextEx(font, ln, rl.Vector2{X: x, Y: y},
				float32(debugDumpSize), 1.0, debugDumpText)
			y += float32(debugDumpSize) + 3
		}
	}

	if ctx.Footer != "" {
		y += debugPad
		rl.DrawTextEx(font, ctx.Footer, rl.Vector2{X: x, Y: y},
			float32(debugFooterSize), 1.0, debugRadiusInfo)
	}
	return
}

// drawDebugButtonRow lays buttons horizontally with wrapping; returns the
// clicked index (-1 = none).
func drawDebugButtonRow(font rl.Font, buttons []DebugButton, x float32, y *float32,
	rowW float32, cursor rl.Vector2, lmbPress bool) int {
	clicked := -1
	bx := x
	for i := range buttons {
		w := rl.MeasureTextEx(font, buttons[i].Label, float32(debugRowSize), 1.0).X + 2*debugPad
		if bx+w > x+rowW && bx > x {
			bx = x
			*y += debugBtnHeight + 4
		}
		btn := rl.Rectangle{X: bx, Y: *y, Width: w, Height: debugBtnHeight}
		hovered := cursor.X >= btn.X && cursor.X <= btn.X+btn.Width &&
			cursor.Y >= btn.Y && cursor.Y <= btn.Y+btn.Height
		bg := debugBtnBG
		if buttons[i].Armed {
			bg = debugBtnArmed
		} else if hovered {
			bg = debugRowHover
		}
		rl.DrawRectangleRec(btn, bg)
		rl.DrawRectangleLinesEx(btn, 1, debugCheckEdge)
		rl.DrawTextEx(font, buttons[i].Label,
			rl.Vector2{X: btn.X + debugPad, Y: btn.Y + (debugBtnHeight-float32(debugRowSize))*0.5},
			float32(debugRowSize), 1.0, debugTextOn)
		if hovered && lmbPress {
			clicked = i
		}
		bx += w + 6
	}
	*y += debugBtnHeight + 4
	return clicked
}
