package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Phase 18 floating panels - in-game overlay windows (not OS-level). A
// reusable container: title bar (drag-to-move + X close), borders,
// content callback. Used for the formation editor and any future dialog
// that doesn't belong in the workspace tree (asset picker, doctrine
// editor, etc.).
//
// FloatingState owns the slice of live panels in Z order (last = top).
// Clicking a panel raises it. Drag of the title bar moves the panel,
// clamped to screen edges. ESC closes the topmost panel.

// FloatingRenderFn is called once per frame inside a panel's content
// rect. lmbPress is true on the frame the user pressed LMB inside this
// panel (so widget code doesn't have to re-derive focus). Returning true
// requests that the panel be closed at end of frame.
type FloatingRenderFn func(content rl.Rectangle, cursor rl.Vector2, font rl.Font, lmbPress bool) bool

// FloatingPanel - one floating window.
type FloatingPanel struct {
	ID     string
	Title  string
	Bounds rl.Rectangle
	// Min size for sanity / future resize. Defaults to 120x80 if zero.
	MinW, MinH float32
	Render     FloatingRenderFn
	// OnClose fires when the panel is removed (X click, ESC, or
	// programmatic Close). Useful for cleanup (e.g. clearing a selection).
	OnClose func()
}

// FloatingState - the manager.
type FloatingState struct {
	Panels []*FloatingPanel // last index = topmost Z

	// Active move drag. dragIdx = -1 when no drag in progress.
	dragIdx       int
	dragOffset    rl.Vector2
	dragStartRect rl.Rectangle

	// Active resize drag from BR corner.
	resizeIdx   int
	resizeStart rl.Vector2 // cursor at press
	resizeRect  rl.Rectangle
}

// NewFloatingState constructs an empty manager.
func NewFloatingState() *FloatingState {
	return &FloatingState{dragIdx: -1, resizeIdx: -1}
}

const (
	floatingTitleH    float32 = 24
	floatingBorderPx  float32 = 1
	floatingCloseSize float32 = 16
	floatingClosePad  float32 = 4
	floatingResizeSz  float32 = 14 // BR corner resize-grip half-size
)

var (
	floatingBG          = rl.Color{R: 26, G: 30, B: 38, A: 245}
	floatingTitleBG     = rl.Color{R: 38, G: 46, B: 60, A: 250}
	floatingTitleHotBG  = rl.Color{R: 54, G: 68, B: 86, A: 250}
	floatingBorder      = rl.Color{R: 70, G: 80, B: 95, A: 255}
	floatingTitleColor  = rl.Color{R: 220, G: 226, B: 232, A: 255}
	floatingCloseBG     = rl.Color{R: 80, G: 50, B: 50, A: 180}
	floatingCloseHotBG  = rl.Color{R: 200, G: 70, B: 70, A: 230}
	floatingCloseGlyph  = rl.Color{R: 230, G: 230, B: 230, A: 255}
	floatingResizeGrip  = rl.Color{R: 130, G: 145, B: 165, A: 220}
	floatingResizeHot   = rl.Color{R: 220, G: 230, B: 245, A: 240}
)

// Open adds (or replaces) a panel by ID and raises it to the top.
func (s *FloatingState) Open(p *FloatingPanel) {
	if p == nil || p.ID == "" {
		return
	}
	if p.MinW <= 0 {
		p.MinW = 120
	}
	if p.MinH <= 0 {
		p.MinH = 80
	}
	// Replace if same ID already open.
	for i, existing := range s.Panels {
		if existing.ID == p.ID {
			s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
			break
		}
	}
	s.Panels = append(s.Panels, p)
}

// Close removes a panel by ID. Fires OnClose if defined.
func (s *FloatingState) Close(id string) {
	for i, p := range s.Panels {
		if p.ID == id {
			if p.OnClose != nil {
				p.OnClose()
			}
			s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
			if s.dragIdx == i {
				s.dragIdx = -1
			} else if s.dragIdx > i {
				s.dragIdx--
			}
			return
		}
	}
}

// IsOpen reports whether `id` is currently shown.
func (s *FloatingState) IsOpen(id string) bool {
	for _, p := range s.Panels {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Get returns the panel by ID (or nil).
func (s *FloatingState) Get(id string) *FloatingPanel {
	for _, p := range s.Panels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Count returns the live panel count.
func (s *FloatingState) Count() int { return len(s.Panels) }

// HitTest returns the topmost panel under cursor (nil if none).
func (s *FloatingState) HitTest(cursor rl.Vector2) *FloatingPanel {
	for i := len(s.Panels) - 1; i >= 0; i-- {
		if pointInRect(cursor, s.Panels[i].Bounds) {
			return s.Panels[i]
		}
	}
	return nil
}

// IsBusy reports whether the user is currently interacting with a
// floating panel (cursor over one, or active drag/resize). main.go folds
// this into chromeBusy so content-layer LMB handlers skip while a floater
// owns the cursor.
func (s *FloatingState) IsBusy(cursor rl.Vector2) bool {
	if s.dragIdx >= 0 || s.resizeIdx >= 0 {
		return true
	}
	return s.HitTest(cursor) != nil
}

// IsDragging reports a title-bar drag in progress.
func (s *FloatingState) IsDragging() bool { return s.dragIdx >= 0 }

// titleRect returns the panel's title bar rect (full chrome strip).
func floatingTitleRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X,
		Y:      p.Bounds.Y,
		Width:  p.Bounds.Width,
		Height: floatingTitleH,
	}
}

// closeRect returns the X-button rect at the title bar's right edge.
func floatingCloseRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X + p.Bounds.Width - floatingCloseSize - floatingClosePad,
		Y:      p.Bounds.Y + (floatingTitleH-floatingCloseSize)*0.5,
		Width:  floatingCloseSize,
		Height: floatingCloseSize,
	}
}

// floatingResizeRect returns the BR corner resize grip rect.
func floatingResizeRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X + p.Bounds.Width - floatingResizeSz,
		Y:      p.Bounds.Y + p.Bounds.Height - floatingResizeSz,
		Width:  floatingResizeSz,
		Height: floatingResizeSz,
	}
}

// IsResizing reports an active BR-corner resize drag.
func (s *FloatingState) IsResizing() bool { return s.resizeIdx >= 0 }

// ResizeHover returns true when cursor is over the BR resize grip of any
// panel (used by main.go to swap the system cursor).
func (s *FloatingState) ResizeHover(cursor rl.Vector2) bool {
	for i := len(s.Panels) - 1; i >= 0; i-- {
		if pointInRect(cursor, floatingResizeRect(s.Panels[i])) {
			return true
		}
	}
	return false
}

// contentRect returns the inner draw area (excludes title + border).
func floatingContentRect(p *FloatingPanel) rl.Rectangle {
	w := p.Bounds.Width - 2*floatingBorderPx
	h := p.Bounds.Height - floatingTitleH - 2*floatingBorderPx
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return rl.Rectangle{
		X:      p.Bounds.X + floatingBorderPx,
		Y:      p.Bounds.Y + floatingTitleH,
		Width:  w,
		Height: h,
	}
}

// HandleInput processes title-bar press/drag/release and X-button click.
// Returns true if LMB this frame was consumed by floater chrome (raise,
// drag-start, X click). Caller wires this BEFORE other LMB-press handlers.
//
//   - lmbPress = pressed this frame
//   - lmbDown  = currently held
//   - escPress = ESC pressed this frame (closes topmost)
//
// Called once per frame.
func (s *FloatingState) HandleInput(cursor rl.Vector2, lmbPress, lmbDown, escPress bool, screenW, screenH int32) bool {
	if escPress && len(s.Panels) > 0 {
		s.Close(s.Panels[len(s.Panels)-1].ID)
		return true
	}

	// In-flight move drag.
	if s.dragIdx >= 0 {
		if !lmbDown {
			s.dragIdx = -1
			return true
		}
		if s.dragIdx >= len(s.Panels) {
			s.dragIdx = -1
		} else {
			p := s.Panels[s.dragIdx]
			p.Bounds.X = cursor.X - s.dragOffset.X
			p.Bounds.Y = cursor.Y - s.dragOffset.Y
			clampPanelToScreen(p, screenW, screenH)
			return true
		}
	}

	// In-flight resize drag.
	if s.resizeIdx >= 0 {
		if !lmbDown {
			s.resizeIdx = -1
			return true
		}
		if s.resizeIdx >= len(s.Panels) {
			s.resizeIdx = -1
		} else {
			p := s.Panels[s.resizeIdx]
			dx := cursor.X - s.resizeStart.X
			dy := cursor.Y - s.resizeStart.Y
			newW := s.resizeRect.Width + dx
			newH := s.resizeRect.Height + dy
			if newW < p.MinW {
				newW = p.MinW
			}
			if newH < p.MinH {
				newH = p.MinH
			}
			// Don't let the panel grow past the screen edge.
			if p.Bounds.X+newW > float32(screenW)-2 {
				newW = float32(screenW) - 2 - p.Bounds.X
			}
			if p.Bounds.Y+newH > float32(screenH)-2 {
				newH = float32(screenH) - 2 - p.Bounds.Y
			}
			p.Bounds.Width = newW
			p.Bounds.Height = newH
			return true
		}
	}

	consumed := false
	// Walk top-down so the topmost panel wins.
	for i := len(s.Panels) - 1; i >= 0; i-- {
		p := s.Panels[i]
		if !pointInRect(cursor, p.Bounds) {
			continue
		}
		if lmbPress {
			// X click closes.
			if pointInRect(cursor, floatingCloseRect(p)) {
				s.Close(p.ID)
				return true
			}
			// BR-corner grip starts a resize drag.
			if pointInRect(cursor, floatingResizeRect(p)) {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.resizeIdx = newIdx
				s.resizeStart = cursor
				s.resizeRect = s.Panels[newIdx].Bounds
				return true
			}
			// Title-bar press starts a move drag and raises panel.
			if pointInRect(cursor, floatingTitleRect(p)) {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.dragIdx = newIdx
				p2 := s.Panels[newIdx]
				s.dragOffset = rl.Vector2{
					X: cursor.X - p2.Bounds.X,
					Y: cursor.Y - p2.Bounds.Y,
				}
				s.dragStartRect = p2.Bounds
				return true
			}
			// Body press just raises (no drag).
			s.raise(i)
		}
		break
	}
	return consumed
}

// raise moves Panels[i] to the end (topmost Z).
func (s *FloatingState) raise(i int) {
	if i < 0 || i >= len(s.Panels)-1 {
		return
	}
	p := s.Panels[i]
	s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
	s.Panels = append(s.Panels, p)
}

// clampPanelToScreen keeps the panel's title bar at least partially on
// screen so the user can always grab and drag it back.
func clampPanelToScreen(p *FloatingPanel, screenW, screenH int32) {
	sw, sh := float32(screenW), float32(screenH)
	margin := float32(40)
	if p.Bounds.X+p.Bounds.Width < margin {
		p.Bounds.X = margin - p.Bounds.Width
	}
	if p.Bounds.X > sw-margin {
		p.Bounds.X = sw - margin
	}
	if p.Bounds.Y < 0 {
		p.Bounds.Y = 0
	}
	if p.Bounds.Y > sh-floatingTitleH {
		p.Bounds.Y = sh - floatingTitleH
	}
}

// DrawAll renders every panel bottom-up. content callbacks receive an
// lmbPress flag scoped to whether the press happened inside this panel.
func (s *FloatingState) DrawAll(font rl.Font, cursor rl.Vector2, lmbPress bool) {
	// Snapshot pointers so a content callback that calls Close mid-draw
	// doesn't shift the slice underneath us.
	live := make([]*FloatingPanel, len(s.Panels))
	copy(live, s.Panels)
	var pendingClose []string
	top := len(live) - 1
	for i, p := range live {
		isTop := i == top
		drawFloatingChrome(p, font, cursor, isTop)
		content := floatingContentRect(p)
		insidePanel := pointInRect(cursor, p.Bounds)
		callbackLMB := lmbPress && insidePanel && isTop &&
			!pointInRect(cursor, floatingTitleRect(p))
		if p.Render != nil {
			rl.BeginScissorMode(int32(content.X), int32(content.Y),
				int32(content.Width), int32(content.Height))
			closed := p.Render(content, cursor, font, callbackLMB)
			rl.EndScissorMode()
			if closed {
				pendingClose = append(pendingClose, p.ID)
			}
		}
	}
	for _, id := range pendingClose {
		s.Close(id)
	}
}

func drawFloatingChrome(p *FloatingPanel, font rl.Font, cursor rl.Vector2, isTop bool) {
	// Drop shadow (cheap: two darker rectangles offset).
	shadow := p.Bounds
	shadow.X += 3
	shadow.Y += 4
	rl.DrawRectangleRec(shadow, rl.Color{R: 0, G: 0, B: 0, A: 80})

	rl.DrawRectangleRec(p.Bounds, floatingBG)
	rl.DrawRectangleLinesEx(p.Bounds, floatingBorderPx, floatingBorder)

	title := floatingTitleRect(p)
	bg := floatingTitleBG
	if isTop && pointInRect(cursor, title) {
		bg = floatingTitleHotBG
	}
	rl.DrawRectangleRec(title, bg)

	const titleSize int32 = 14
	rl.DrawTextEx(font, p.Title,
		rl.Vector2{X: title.X + 8, Y: title.Y + (floatingTitleH-float32(titleSize))*0.5},
		float32(titleSize), 1.0, floatingTitleColor)

	closeR := floatingCloseRect(p)
	bgC := floatingCloseBG
	if pointInRect(cursor, closeR) {
		bgC = floatingCloseHotBG
	}
	rl.DrawRectangleRec(closeR, bgC)
	rl.DrawRectangleLinesEx(closeR, 1, floatingBorder)
	// X glyph - two diagonals.
	pad := float32(4)
	rl.DrawLineEx(
		rl.Vector2{X: closeR.X + pad, Y: closeR.Y + pad},
		rl.Vector2{X: closeR.X + closeR.Width - pad, Y: closeR.Y + closeR.Height - pad},
		2, floatingCloseGlyph)
	rl.DrawLineEx(
		rl.Vector2{X: closeR.X + closeR.Width - pad, Y: closeR.Y + pad},
		rl.Vector2{X: closeR.X + pad, Y: closeR.Y + closeR.Height - pad},
		2, floatingCloseGlyph)

	// BR-corner resize grip - three diagonal strokes pointing into the
	// panel. Brighter tint while cursor is over it.
	resR := floatingResizeRect(p)
	gripCol := floatingResizeGrip
	if pointInRect(cursor, resR) {
		gripCol = floatingResizeHot
	}
	x1 := resR.X + resR.Width - 2
	y1 := resR.Y + resR.Height - 2
	for i := 0; i < 3; i++ {
		off := float32(i)*4 + 3
		rl.DrawLineEx(
			rl.Vector2{X: x1 - off, Y: y1},
			rl.Vector2{X: x1, Y: y1 - off},
			1.5, gripCol)
	}
}
