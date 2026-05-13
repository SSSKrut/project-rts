package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Phase 10 chrome — the visible frame around every panel. One pixel of border
// + a translucent title bar with the panel's Title. Content is drawn by the
// caller, inside scissor if needed.

const (
	titleBarHeight int32 = 20
)

var (
	panelBorderColor = rl.Color{R: 30, G: 30, B: 36, A: 255}
	titleBarColor    = rl.Color{R: 22, G: 26, B: 34, A: 220}
	titleTextColor   = rl.Color{R: 210, G: 220, B: 230, A: 255}
)

// DrawChrome renders the border + title bar of the panel. Returns the inner
// content rectangle (panel minus border + title bar).
func DrawChrome(p Panel, font rl.Font, fontSize int32) rl.Rectangle {
	b := p.Bounds
	rl.DrawRectangleLinesEx(b, 1, panelBorderColor)

	titleRect := rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1,
		Width:  b.Width - 2,
		Height: float32(titleBarHeight),
	}
	rl.DrawRectangleRec(titleRect, titleBarColor)
	rl.DrawTextEx(font, p.Title,
		rl.Vector2{X: titleRect.X + 6, Y: titleRect.Y + 2},
		float32(fontSize), 1.0, titleTextColor)

	return rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1 + float32(titleBarHeight),
		Width:  b.Width - 2,
		Height: b.Height - 2 - float32(titleBarHeight),
	}
}

// ContentRect returns where panel content draws without re-stamping chrome.
// Used by code that draws content multiple times per frame (e.g. before
// chrome to paint background, after chrome to paint scissored overlays).
func ContentRect(p Panel) rl.Rectangle {
	b := p.Bounds
	return rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1 + float32(titleBarHeight),
		Width:  b.Width - 2,
		Height: b.Height - 2 - float32(titleBarHeight),
	}
}

// PanelBackground draws a flat colour fill in the panel's content area.
// Useful as a debug background in M10.1 before each panel has real content.
func PanelBackground(p Panel, fill rl.Color) {
	rl.DrawRectangleRec(ContentRect(p), fill)
}
