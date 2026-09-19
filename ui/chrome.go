package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	titleBarHeight int32 = 24
)

// Chromeless drops the top bar, borders and title bars so a capture run's
// field fills the window. Set before the first Recompute.
var Chromeless bool

var (
	panelBorderColor = rl.Color{R: 30, G: 30, B: 36, A: 255}
	titleBarColor    = rl.Color{R: 22, G: 26, B: 34, A: 220}
	titleTextColor   = rl.Color{R: 210, G: 220, B: 230, A: 255}
)

// DrawChrome renders the border + title bar (including the chevron button)
// and returns the inner content rectangle.
func DrawChrome(p Panel, font rl.Font, fontSize int32) rl.Rectangle {
	b := p.Bounds
	rl.DrawRectangleLinesEx(b, 1, panelBorderColor)

	titleRect := PanelTitleRect(b)
	rl.DrawRectangleRec(titleRect, titleBarColor)
	rl.DrawTextEx(font, p.Title,
		rl.Vector2{X: titleRect.X + 8, Y: titleRect.Y + 4},
		float32(fontSize), 1.0, titleTextColor)

	drawChevron(ChevronRect(p), font)

	return rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1 + float32(titleBarHeight),
		Width:  b.Width - 2,
		Height: b.Height - 2 - float32(titleBarHeight),
	}
}

// PanelTitleRect is the title-bar strip inside the panel border — drawn by
// DrawChrome, hit-tested by the title-bar drag.
func PanelTitleRect(b rl.Rectangle) rl.Rectangle {
	return rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1,
		Width:  b.Width - 2,
		Height: float32(titleBarHeight),
	}
}

func ChevronRect(p Panel) rl.Rectangle {
	b := p.Bounds
	const sz float32 = 22
	return rl.Rectangle{
		X:      b.X + b.Width - sz - 2,
		Y:      b.Y + 1,
		Width:  sz,
		Height: float32(titleBarHeight),
	}
}

var chevronColor = rl.Color{R: 200, G: 215, B: 230, A: 220}

func drawChevron(r rl.Rectangle, font rl.Font) {
	cx := r.X + r.Width*0.5
	cy := r.Y + r.Height*0.5 + 1
	const w float32 = 4
	const h float32 = 3
	v1 := rl.Vector2{X: cx - w, Y: cy - h}
	v2 := rl.Vector2{X: cx + w, Y: cy - h}
	v3 := rl.Vector2{X: cx, Y: cy + h}
	rl.DrawTriangle(v1, v3, v2, chevronColor)
}

// ContentRect returns the panel's content area. Clamps width/height to
// non-negative because raylib treats negative-sized rects as "scissor
// disabled", which would leak the panel's draw calls across the whole
// screen when its Bounds collapse (e.g. mid corner-merge).
func ContentRect(p Panel) rl.Rectangle {
	b := p.Bounds
	if Chromeless {
		return rl.Rectangle{X: b.X, Y: b.Y, Width: max(b.Width, 0), Height: max(b.Height, 0)}
	}
	w := b.Width - 2
	h := b.Height - 2 - float32(titleBarHeight)
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return rl.Rectangle{
		X:      b.X + 1,
		Y:      b.Y + 1 + float32(titleBarHeight),
		Width:  w,
		Height: h,
	}
}

func PanelBackground(p Panel, fill rl.Color) {
	rl.DrawRectangleRec(ContentRect(p), fill)
}

// PanelChromePadding returns the (top, sides) chrome inset thicknesses.
// top = title bar + border; sides = border on each edge.
func PanelChromePadding() (top, sides float32) {
	if Chromeless {
		return 0, 0
	}
	return float32(titleBarHeight) + 1, 1
}

// ContentToPanel is the inverse of ContentRect: builds a synthetic Panel
// whose Bounds extend far enough that ContentRect(p) recovers the input
// area. Used to host workspace widgets inside floating panels (which draw
// their own chrome).
func ContentToPanel(content rl.Rectangle, title string, id PanelID) Panel {
	return Panel{
		ID:    id,
		Title: title,
		Bounds: rl.Rectangle{
			X:      content.X - 1,
			Y:      content.Y - 1 - float32(titleBarHeight),
			Width:  content.Width + 2,
			Height: content.Height + 2 + float32(titleBarHeight),
		},
	}
}

const (
	scrollbarTrackWidth float32 = 8
	scrollbarThumbMin   float32 = 30
)

var (
	scrollbarTrackColor = rl.Color{R: 28, G: 32, B: 38, A: 200}
	scrollbarThumbColor = rl.Color{R: 80, G: 90, B: 105, A: 220}
)

// ScrollbarRect sits inside the content rect along the right edge so the
// existing content scissor still clips it correctly.
func ScrollbarRect(p Panel) rl.Rectangle {
	c := ContentRect(p)
	return rl.Rectangle{
		X:      c.X + c.Width - scrollbarTrackWidth,
		Y:      c.Y,
		Width:  scrollbarTrackWidth,
		Height: c.Height,
	}
}

// ScrollbarThumbRect returns a zero rect when content fits (no scroll needed).
func ScrollbarThumbRect(p Panel, scroll *ScrollState) rl.Rectangle {
	if scroll == nil {
		return rl.Rectangle{}
	}
	track := ScrollbarRect(p)
	if scroll.ContentHeight <= track.Height || track.Height <= 0 {
		return rl.Rectangle{}
	}
	thumbH := track.Height * (track.Height / scroll.ContentHeight)
	if thumbH < scrollbarThumbMin {
		thumbH = scrollbarThumbMin
	}
	if thumbH > track.Height {
		thumbH = track.Height
	}
	maxOffset := scroll.ContentHeight - track.Height
	scrollableTrack := track.Height - thumbH
	thumbY := track.Y
	if maxOffset > 0 && scrollableTrack > 0 {
		thumbY += scrollableTrack * (scroll.OffsetY / maxOffset)
	}
	return rl.Rectangle{
		X:      track.X,
		Y:      thumbY,
		Width:  track.Width,
		Height: thumbH,
	}
}

// DrawScrollbar is a no-op when content fits, so callers don't need an
// "if needed" check.
func DrawScrollbar(p Panel, scroll *ScrollState) {
	if scroll == nil {
		return
	}
	track := ScrollbarRect(p)
	if scroll.ContentHeight <= track.Height || track.Height <= 0 {
		return
	}
	rl.DrawRectangleRec(track, scrollbarTrackColor)
	thumb := ScrollbarThumbRect(p, scroll)
	rl.DrawRectangleRec(thumb, scrollbarThumbColor)
}

// ClampScrollOffset must run after Recompute so a panel resize that
// suddenly fits the content doesn't leave a stale offset.
func ClampScrollOffset(p Panel, scroll *ScrollState) {
	if scroll == nil {
		return
	}
	track := ScrollbarRect(p)
	maxOffset := scroll.ContentHeight - track.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if scroll.OffsetY < 0 {
		scroll.OffsetY = 0
	}
	if scroll.OffsetY > maxOffset {
		scroll.OffsetY = maxOffset
	}
}
