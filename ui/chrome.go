package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Phase 10 chrome - the visible frame around every panel. One pixel of border
// + a translucent title bar with the panel's Title. Content is drawn by the
// caller, inside scissor if needed.

const (
	titleBarHeight int32 = 24
)

var (
	panelBorderColor = rl.Color{R: 30, G: 30, B: 36, A: 255}
	titleBarColor    = rl.Color{R: 22, G: 26, B: 34, A: 220}
	titleTextColor   = rl.Color{R: 210, G: 220, B: 230, A: 255}
)

// DrawChrome renders the border + title bar of the panel, including the
// Phase 18.C chevron button on the right of the title bar. Returns the
// inner content rectangle (panel minus border + title bar).
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
		rl.Vector2{X: titleRect.X + 8, Y: titleRect.Y + 4},
		float32(fontSize), 1.0, titleTextColor)

	drawChevron(ChevronRect(p), font)

	return rl.Rectangle{
		X: b.X + 1, Y: b.Y + 1 + float32(titleBarHeight),
		Width:  b.Width - 2,
		Height: b.Height - 2 - float32(titleBarHeight),
	}
}

// ChevronRect returns the click target for the panel-switch chevron at the
// right edge of the title bar.
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

// ContentRect returns where panel content draws without re-stamping chrome.
// Used by code that draws content multiple times per frame (e.g. before
// chrome to paint background, after chrome to paint scissored overlays).
// Phase 18.C: clamp width/height to non-negative so panels with zero
// Bounds (the result of a chevron Close or corner-merge that removed the
// leaf from the tree) don't pass negative-sized rects to BeginScissorMode
// — raylib treats those as "scissor disabled" and the panel's draw calls
// leak across the whole screen, ghosting onto the surviving leaves.
func ContentRect(p Panel) rl.Rectangle {
	b := p.Bounds
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

// PanelBackground draws a flat colour fill in the panel's content area.
// Useful as a debug background in M10.1 before each panel has real content.
func PanelBackground(p Panel, fill rl.Color) {
	rl.DrawRectangleRec(ContentRect(p), fill)
}

// PanelChromePadding returns the (top, sides) chrome inset thicknesses so
// callers outside the package can size around it without hardcoding values.
// top = title bar + border; sides = border on each edge.
func PanelChromePadding() (top, sides float32) {
	return float32(titleBarHeight) + 1, 1
}

// ContentToPanel is the inverse of ContentRect: given a content rectangle
// it returns a Panel whose ContentRect(panel) recovers that exact area.
// Used when a widget that normally draws into a workspace leaf has to draw
// into a floating panel's interior - the floating panel handles its own
// chrome, so we pass the widget a synthetic Panel whose Bounds extend just
// far enough to absorb the chrome offsets the widget will subtract.
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

// Scrollbar visual constants (Phase 13.5 M13.5.3).
const (
	scrollbarTrackWidth float32 = 8
	scrollbarThumbMin   float32 = 30
)

var (
	scrollbarTrackColor = rl.Color{R: 28, G: 32, B: 38, A: 200}
	scrollbarThumbColor = rl.Color{R: 80, G: 90, B: 105, A: 220}
)

// ScrollbarRect returns the on-screen rectangle of the vertical scrollbar
// track for a panel. Phase 13.5 places it inside the content rect along the
// right edge so existing content scissor still clips correctly.
func ScrollbarRect(p Panel) rl.Rectangle {
	c := ContentRect(p)
	return rl.Rectangle{
		X:      c.X + c.Width - scrollbarTrackWidth,
		Y:      c.Y,
		Width:  scrollbarTrackWidth,
		Height: c.Height,
	}
}

// ScrollbarThumbRect returns the position + size of the scrollbar thumb based
// on the panel's content height vs visible height. Returns zero rect when no
// scroll is needed (content fits).
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

// DrawScrollbar paints the vertical scrollbar track + thumb on the right
// edge of `p`'s content rect. Skipped silently when content fits (no
// overflow) - caller doesn't need an "if needed" check.
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

// ClampScrollOffset constrains scroll.OffsetY to a valid range given the
// current ContentHeight. Called from input handlers + after Recompute so a
// panel resize that suddenly fits the content doesn't leave a stale offset.
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
