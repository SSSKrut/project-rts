package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// In-game overlay windows (not OS-level). FloatingState holds panels in Z
// order (last = top); clicking a panel raises it.

// FloatingRenderFn returns true to request that the panel close at end of
// frame. lmbPress is true on the frame the user pressed LMB inside this
// panel.
type FloatingRenderFn func(content rl.Rectangle, cursor rl.Vector2, font rl.Font, lmbPress bool) bool

type FloatingPanel struct {
	ID     string
	Title  string
	Bounds rl.Rectangle
	// MinW/MinH default to 120x80 when zero.
	MinW, MinH float32
	Render     FloatingRenderFn
	OnClose    func()

	// PanelID identifies the widget shown here; PanelNone hides the
	// switch button so non-widget floaters don't pretend to be switchable.
	PanelID PanelID

	// RenderFor enables the switch button by building a new Render
	// closure for the chosen PanelID; Title is updated via WidgetTitle.
	RenderFor func(id PanelID) FloatingRenderFn
}

type FloatingState struct {
	Panels []*FloatingPanel

	dragIdx       int
	dragOffset    rl.Vector2
	dragStartRect rl.Rectangle

	resizeIdx   int
	resizeEdges resizeEdges
	resizeStart rl.Vector2
	resizeRect  rl.Rectangle

	switchMenuIdx    int
	switchMenuAnchor rl.Rectangle
}

// resizeEdges: any combination valid; corners set two flags, edges one.
type resizeEdges struct {
	Left, Right, Top, Bottom bool
}

func (e resizeEdges) any() bool { return e.Left || e.Right || e.Top || e.Bottom }

func NewFloatingState() *FloatingState {
	return &FloatingState{dragIdx: -1, resizeIdx: -1, switchMenuIdx: -1}
}

const (
	floatingTitleH     float32 = 24
	floatingBorderPx   float32 = 1
	floatingCloseSize  float32 = 16
	floatingClosePad   float32 = 4
	floatingSwitchSize float32 = 16
	floatingSwitchPad  float32 = 2
	// floatingResizeSz is the BR corner-grip glyph; the actual hit zone is
	// every side + corner within floatingResizeBand pixels via
	// detectResizeEdges.
	floatingResizeSz   float32 = 14
	floatingResizeBand float32 = 5
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
	floatingSwitchBG    = rl.Color{R: 56, G: 70, B: 90, A: 200}
	floatingSwitchHotBG = rl.Color{R: 80, G: 110, B: 160, A: 230}
	floatingResizeGrip  = rl.Color{R: 130, G: 145, B: 165, A: 220}
	floatingResizeHot   = rl.Color{R: 220, G: 230, B: 245, A: 240}
)

// Open adds (or replaces) a panel by ID and raises it. When Render is nil
// but RenderFor + PanelID are set, the manager resolves the renderer.
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
	if p.Render == nil && p.RenderFor != nil && p.PanelID != PanelNone {
		p.Render = p.RenderFor(p.PanelID)
	}
	for i, existing := range s.Panels {
		if existing.ID == p.ID {
			s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
			break
		}
	}
	s.Panels = append(s.Panels, p)
}

// SwitchPanelContent is a no-op when RenderFor is nil or newID equals
// the current PanelID.
func (s *FloatingState) SwitchPanelContent(idx int, newID PanelID) {
	if idx < 0 || idx >= len(s.Panels) {
		return
	}
	p := s.Panels[idx]
	if p.RenderFor == nil || newID == PanelNone || newID == p.PanelID {
		return
	}
	p.PanelID = newID
	p.Title = WidgetTitle(newID)
	p.Render = p.RenderFor(newID)
}

// Close fires OnClose if defined.
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

func (s *FloatingState) IsOpen(id string) bool {
	for _, p := range s.Panels {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (s *FloatingState) Get(id string) *FloatingPanel {
	for _, p := range s.Panels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *FloatingState) Count() int { return len(s.Panels) }

func (s *FloatingState) HitTest(cursor rl.Vector2) *FloatingPanel {
	for i := len(s.Panels) - 1; i >= 0; i-- {
		if pointInRect(cursor, s.Panels[i].Bounds) {
			return s.Panels[i]
		}
	}
	return nil
}

// IsBusy folds into main.go's chromeBusy so content-layer LMB handlers
// skip while a floater owns the cursor. Includes cursor-over-panel, active
// drag/resize, and resize-edge hover (slightly outside panel bounds).
func (s *FloatingState) IsBusy(cursor rl.Vector2) bool {
	if s.dragIdx >= 0 || s.resizeIdx >= 0 {
		return true
	}
	if s.HitTest(cursor) != nil {
		return true
	}
	for i := len(s.Panels) - 1; i >= 0; i-- {
		if detectResizeEdges(s.Panels[i], cursor).any() {
			return true
		}
	}
	return false
}

func (s *FloatingState) IsDragging() bool { return s.dragIdx >= 0 }

func floatingTitleRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X,
		Y:      p.Bounds.Y,
		Width:  p.Bounds.Width,
		Height: floatingTitleH,
	}
}

func floatingCloseRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X + p.Bounds.Width - floatingCloseSize - floatingClosePad,
		Y:      p.Bounds.Y + (floatingTitleH-floatingCloseSize)*0.5,
		Width:  floatingCloseSize,
		Height: floatingCloseSize,
	}
}

// floatingSwitchRect returns a zero-size rect when the panel isn't switchable.
func floatingSwitchRect(p *FloatingPanel) rl.Rectangle {
	if p.RenderFor == nil || p.PanelID == PanelNone {
		return rl.Rectangle{}
	}
	closeR := floatingCloseRect(p)
	return rl.Rectangle{
		X:      closeR.X - floatingSwitchSize - floatingSwitchPad,
		Y:      p.Bounds.Y + (floatingTitleH-floatingSwitchSize)*0.5,
		Width:  floatingSwitchSize,
		Height: floatingSwitchSize,
	}
}

// floatingResizeRect is the BR grip glyph location; hit-test is wider
// (see detectResizeEdges).
func floatingResizeRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X + p.Bounds.Width - floatingResizeSz,
		Y:      p.Bounds.Y + p.Bounds.Height - floatingResizeSz,
		Width:  floatingResizeSz,
		Height: floatingResizeSz,
	}
}

// detectResizeEdges checks sides of `p` within floatingResizeBand of cursor.
// At most one side per axis; corners come out as two flags.
func detectResizeEdges(p *FloatingPanel, cursor rl.Vector2) resizeEdges {
	if p == nil {
		return resizeEdges{}
	}
	// Outer envelope expanded by floatingResizeBand so cursor can grab
	// slightly outside the panel's pixels.
	outer := rl.Rectangle{
		X:      p.Bounds.X - floatingResizeBand,
		Y:      p.Bounds.Y - floatingResizeBand,
		Width:  p.Bounds.Width + 2*floatingResizeBand,
		Height: p.Bounds.Height + 2*floatingResizeBand,
	}
	if !pointInRect(cursor, outer) {
		return resizeEdges{}
	}
	var e resizeEdges
	if cursor.X <= p.Bounds.X+floatingResizeBand {
		e.Left = true
	} else if cursor.X >= p.Bounds.X+p.Bounds.Width-floatingResizeBand {
		e.Right = true
	}
	if cursor.Y <= p.Bounds.Y+floatingResizeBand {
		e.Top = true
	} else if cursor.Y >= p.Bounds.Y+p.Bounds.Height-floatingResizeBand {
		e.Bottom = true
	}
	return e
}

func cursorForEdges(e resizeEdges) (rl.MouseCursor, bool) {
	if !e.any() {
		return rl.MouseCursorDefault, false
	}
	switch {
	case e.Top && e.Left, e.Bottom && e.Right:
		return rl.MouseCursorResizeNWSE, true
	case e.Top && e.Right, e.Bottom && e.Left:
		return rl.MouseCursorResizeNESW, true
	case e.Top || e.Bottom:
		return rl.MouseCursorResizeNS, true
	case e.Left || e.Right:
		return rl.MouseCursorResizeEW, true
	}
	return rl.MouseCursorDefault, false
}

func (s *FloatingState) IsResizing() bool { return s.resizeIdx >= 0 }

// ResizeCursorAt drives main.go's system-cursor swap chain.
func (s *FloatingState) ResizeCursorAt(cursor rl.Vector2) (rl.MouseCursor, bool) {
	if s.resizeIdx >= 0 && s.resizeIdx < len(s.Panels) {
		if c, ok := cursorForEdges(s.resizeEdges); ok {
			return c, true
		}
	}
	for i := len(s.Panels) - 1; i >= 0; i-- {
		if e := detectResizeEdges(s.Panels[i], cursor); e.any() {
			return cursorForEdges(e)
		}
		// Once cursor is inside this panel's outer envelope, lower
		// panels are occluded — don't fall through.
		if pointInRect(cursor, s.Panels[i].Bounds) {
			return rl.MouseCursorDefault, false
		}
	}
	return rl.MouseCursorDefault, false
}

func (s *FloatingState) ResizeHover(cursor rl.Vector2) bool {
	_, ok := s.ResizeCursorAt(cursor)
	return ok
}

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

// HandleInput must run BEFORE other LMB-press handlers; returns true when
// LMB this frame was consumed by floater chrome (raise, drag-start, X
// click). escPress closes the topmost panel.
func (s *FloatingState) HandleInput(cursor rl.Vector2, lmbPress, lmbDown, escPress bool, screenW, screenH int32) bool {
	if escPress && len(s.Panels) > 0 {
		if s.switchMenuIdx >= 0 {
			s.switchMenuIdx = -1
			return true
		}
		s.Close(s.Panels[len(s.Panels)-1].ID)
		return true
	}

	// Open switch menu wins LMB — it floats above panel chrome, so
	// process it before any per-panel zones.
	if s.switchMenuIdx >= 0 && lmbPress {
		if s.switchMenuIdx >= len(s.Panels) {
			s.switchMenuIdx = -1
		} else {
			p := s.Panels[s.switchMenuIdx]
			r := floatingSwitchMenuRect(s.switchMenuAnchor)
			if idx := floatingSwitchMenuHit(p, r, cursor); idx >= 0 {
				newID := WorkspacePanelKinds[idx]
				if newID != p.PanelID {
					s.SwitchPanelContent(s.switchMenuIdx, newID)
				}
				s.switchMenuIdx = -1
				return true
			}
			// Click inside menu but on the disabled (current) row:
			// absorb so it doesn't bleed into the panel below.
			if pointInRect(cursor, r) {
				s.switchMenuIdx = -1
				return true
			}
			// Click on the switch button toggles; click elsewhere
			// dismisses and falls through so the press can start a drag.
			s.switchMenuIdx = -1
			if pointInRect(cursor, floatingSwitchRect(p)) {
				return true
			}
		}
	}

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

	if s.resizeIdx >= 0 {
		if !lmbDown {
			s.resizeIdx = -1
			s.resizeEdges = resizeEdges{}
			return true
		}
		if s.resizeIdx >= len(s.Panels) {
			s.resizeIdx = -1
			s.resizeEdges = resizeEdges{}
		} else {
			p := s.Panels[s.resizeIdx]
			s.applyResizeDrag(p, cursor, screenW, screenH)
			return true
		}
	}

	consumed := false
	// Walk top-down so the topmost panel wins. Edge resize zones are
	// tested before move-drag / body so the title bar's top-pixel band
	// still triggers vertical resize and the BR corner overlap with the
	// resize-grip glyph still starts a resize.
	for i := len(s.Panels) - 1; i >= 0; i-- {
		p := s.Panels[i]
		envelope := rl.Rectangle{
			X:      p.Bounds.X - floatingResizeBand,
			Y:      p.Bounds.Y - floatingResizeBand,
			Width:  p.Bounds.Width + 2*floatingResizeBand,
			Height: p.Bounds.Height + 2*floatingResizeBand,
		}
		if !pointInRect(cursor, envelope) {
			continue
		}
		if lmbPress {
			// X close wins over edges even at the panel's outer corner.
			if pointInRect(cursor, floatingCloseRect(p)) {
				s.Close(p.ID)
				return true
			}
			if swR := floatingSwitchRect(p); swR.Width > 0 && pointInRect(cursor, swR) {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.switchMenuIdx = newIdx
				s.switchMenuAnchor = floatingSwitchRect(s.Panels[newIdx])
				return true
			}
			if edges := detectResizeEdges(p, cursor); edges.any() {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.resizeIdx = newIdx
				s.resizeEdges = edges
				s.resizeStart = cursor
				s.resizeRect = s.Panels[newIdx].Bounds
				return true
			}
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
			if pointInRect(cursor, p.Bounds) {
				s.raise(i)
			}
		}
		break
	}
	return consumed
}

// applyResizeDrag clamps to MinW/MinH and to screen edges.
func (s *FloatingState) applyResizeDrag(p *FloatingPanel, cursor rl.Vector2, screenW, screenH int32) {
	dx := cursor.X - s.resizeStart.X
	dy := cursor.Y - s.resizeStart.Y
	bounds := s.resizeRect
	if s.resizeEdges.Right {
		newW := bounds.Width + dx
		if newW < p.MinW {
			newW = p.MinW
		}
		if bounds.X+newW > float32(screenW)-2 {
			newW = float32(screenW) - 2 - bounds.X
		}
		bounds.Width = newW
	}
	if s.resizeEdges.Bottom {
		newH := bounds.Height + dy
		if newH < p.MinH {
			newH = p.MinH
		}
		if bounds.Y+newH > float32(screenH)-2 {
			newH = float32(screenH) - 2 - bounds.Y
		}
		bounds.Height = newH
	}
	if s.resizeEdges.Left {
		newX := bounds.X + dx
		newW := bounds.Width - dx
		if newW < p.MinW {
			newX = bounds.X + (bounds.Width - p.MinW)
			newW = p.MinW
		}
		if newX < 2 {
			newW -= 2 - newX
			newX = 2
		}
		bounds.X = newX
		bounds.Width = newW
	}
	if s.resizeEdges.Top {
		newY := bounds.Y + dy
		newH := bounds.Height - dy
		if newH < p.MinH {
			newY = bounds.Y + (bounds.Height - p.MinH)
			newH = p.MinH
		}
		if newY < 2 {
			newH -= 2 - newY
			newY = 2
		}
		bounds.Y = newY
		bounds.Height = newH
	}
	p.Bounds = bounds
}

func (s *FloatingState) raise(i int) {
	if i < 0 || i >= len(s.Panels)-1 {
		return
	}
	p := s.Panels[i]
	s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
	s.Panels = append(s.Panels, p)
}

// clampPanelToScreen keeps the title bar at least partially on screen so
// the user can always grab and drag the panel back.
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

// DrawAll renders every panel bottom-up; content callbacks receive
// lmbPress scoped to whether the press happened inside this panel.
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
	pad := float32(4)
	rl.DrawLineEx(
		rl.Vector2{X: closeR.X + pad, Y: closeR.Y + pad},
		rl.Vector2{X: closeR.X + closeR.Width - pad, Y: closeR.Y + closeR.Height - pad},
		2, floatingCloseGlyph)
	rl.DrawLineEx(
		rl.Vector2{X: closeR.X + closeR.Width - pad, Y: closeR.Y + pad},
		rl.Vector2{X: closeR.X + pad, Y: closeR.Y + closeR.Height - pad},
		2, floatingCloseGlyph)

	if swR := floatingSwitchRect(p); swR.Width > 0 {
		bg := floatingSwitchBG
		if pointInRect(cursor, swR) {
			bg = floatingSwitchHotBG
		}
		rl.DrawRectangleRec(swR, bg)
		rl.DrawRectangleLinesEx(swR, 1, floatingBorder)
		cx := swR.X + swR.Width*0.5
		cy := swR.Y + swR.Height*0.5 + 1
		w := float32(4)
		h := float32(3)
		rl.DrawTriangle(
			rl.Vector2{X: cx - w, Y: cy - h},
			rl.Vector2{X: cx, Y: cy + h},
			rl.Vector2{X: cx + w, Y: cy - h},
			floatingCloseGlyph)
	}

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

const (
	floatingMenuItemH float32 = 24
	floatingMenuFontH int32   = 13
	floatingMenuPadX  float32 = 8
	floatingMenuPadY  float32 = 4
	floatingMenuMinW  float32 = 150
)

var (
	floatingMenuBG      = rl.Color{R: 26, G: 30, B: 38, A: 245}
	floatingMenuBorder  = rl.Color{R: 70, G: 80, B: 95, A: 255}
	floatingMenuHoverBG = rl.Color{R: 50, G: 90, B: 130, A: 240}
	floatingMenuText    = rl.Color{R: 220, G: 226, B: 232, A: 255}
	floatingMenuTextDim = rl.Color{R: 110, G: 120, B: 130, A: 255}
)

func floatingSwitchMenuRect(anchor rl.Rectangle) rl.Rectangle {
	rows := len(WorkspacePanelKinds)
	height := float32(rows)*floatingMenuItemH + 2*floatingMenuPadY
	width := floatingMenuMinW
	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())
	// Right-align with the anchor (button sits on the right of the title
	// bar) so default placements don't drift past the screen edge.
	x := anchor.X + anchor.Width - width
	if x < 2 {
		x = 2
	}
	if x+width > screenW-2 {
		x = screenW - 2 - width
	}
	y := anchor.Y + anchor.Height + 2
	if y+height > screenH-2 {
		y = anchor.Y - height - 2
	}
	if y < 2 {
		y = 2
	}
	return rl.Rectangle{X: x, Y: y, Width: width, Height: height}
}

// floatingSwitchMenuHit returns -1 when cursor is outside the menu or
// over the disabled (current) row.
func floatingSwitchMenuHit(p *FloatingPanel, r rl.Rectangle, cursor rl.Vector2) int {
	if !pointInRect(cursor, r) {
		return -1
	}
	yRel := cursor.Y - (r.Y + floatingMenuPadY)
	idx := int(yRel / floatingMenuItemH)
	if idx < 0 || idx >= len(WorkspacePanelKinds) {
		return -1
	}
	if WorkspacePanelKinds[idx] == p.PanelID {
		return -1
	}
	return idx
}

func (s *FloatingState) drawSwitchMenu(font rl.Font, cursor rl.Vector2) {
	if s.switchMenuIdx < 0 || s.switchMenuIdx >= len(s.Panels) {
		return
	}
	p := s.Panels[s.switchMenuIdx]
	r := floatingSwitchMenuRect(s.switchMenuAnchor)
	rl.DrawRectangleRec(r, floatingMenuBG)
	rl.DrawRectangleLinesEx(r, 1, floatingMenuBorder)
	hovered := floatingSwitchMenuHit(p, r, cursor)
	for i, id := range WorkspacePanelKinds {
		rowY := r.Y + floatingMenuPadY + float32(i)*floatingMenuItemH
		row := rl.Rectangle{X: r.X + 1, Y: rowY, Width: r.Width - 2, Height: floatingMenuItemH}
		label := WidgetTitle(id)
		col := floatingMenuText
		if id == p.PanelID {
			col = floatingMenuTextDim
			label = "• " + label
		} else if i == hovered {
			rl.DrawRectangleRec(row, floatingMenuHoverBG)
		}
		rl.DrawTextEx(font, label,
			rl.Vector2{X: row.X + floatingMenuPadX, Y: row.Y + (floatingMenuItemH-float32(floatingMenuFontH))*0.5},
			float32(floatingMenuFontH), 1.0, col)
	}
}

// DrawSwitchMenu must run after DrawAll so the menu overlays every panel
// chrome.
func (s *FloatingState) DrawSwitchMenu(font rl.Font, cursor rl.Vector2) {
	s.drawSwitchMenu(font, cursor)
}

// SwitchMenuOpen folds into chromeBusy so content layers stay quiet while
// the menu owns the click.
func (s *FloatingState) SwitchMenuOpen() bool { return s.switchMenuIdx >= 0 }
