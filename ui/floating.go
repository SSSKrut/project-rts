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

	// PanelID identifies the widget currently shown in this floater. Used
	// by the switch-content menu to mark the active item and decide what
	// other widgets are available. Zero (PanelNone) hides the switch
	// button so non-widget floaters (future asset picker, dialogs) don't
	// pretend to be switchable.
	PanelID PanelID

	// RenderFor builds a FloatingRenderFn for a given PanelID. Set this
	// to enable the switch button - the manager calls it on switch to
	// produce the new Render closure. Title is auto-updated via
	// WidgetTitle(newID).
	RenderFor func(id PanelID) FloatingRenderFn
}

// FloatingState - the manager.
type FloatingState struct {
	Panels []*FloatingPanel // last index = topmost Z

	// Active move drag. dragIdx = -1 when no drag in progress.
	dragIdx       int
	dragOffset    rl.Vector2
	dragStartRect rl.Rectangle

	// Active resize drag. resizeIdx = -1 when not resizing.
	// resizeEdges tells which sides of the panel the drag mutates;
	// resizeStart/resizeRect capture the press-time anchor so cursor
	// delta translates linearly to a new bounds.
	resizeIdx   int
	resizeEdges resizeEdges
	resizeStart rl.Vector2
	resizeRect  rl.Rectangle

	// Active switch-content menu. switchMenuIdx = panel index whose menu
	// is open (-1 = closed). Anchor rect on the title bar drives popup
	// placement (flips up / left when overflowing).
	switchMenuIdx    int
	switchMenuAnchor rl.Rectangle
}

// resizeEdges records which sides of a floating panel a resize drag is
// pulling. Any combination is valid; corners set two flags, edges one.
type resizeEdges struct {
	Left, Right, Top, Bottom bool
}

// any reports whether at least one side is engaged.
func (e resizeEdges) any() bool { return e.Left || e.Right || e.Top || e.Bottom }

// NewFloatingState constructs an empty manager.
func NewFloatingState() *FloatingState {
	return &FloatingState{dragIdx: -1, resizeIdx: -1, switchMenuIdx: -1}
}

const (
	floatingTitleH    float32 = 24
	floatingBorderPx  float32 = 1
	floatingCloseSize float32 = 16
	floatingClosePad  float32 = 4
	// floatingSwitchSize and the layout below match floatingCloseSize so
	// the title-bar buttons read as a single right-aligned tool group:
	// [...Title...] [Switch] [X]
	floatingSwitchSize float32 = 16
	floatingSwitchPad  float32 = 2
	// floatingResizeSz controls the BR corner-grip glyph (cosmetic only -
	// the actual hit zone is computed by detectResizeEdges so every side
	// + corner is grabable at floatingResizeBand pixels.)
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

// Open adds (or replaces) a panel by ID and raises it to the top.
// When p.Render is nil but p.RenderFor + p.PanelID are set, the manager
// resolves the renderer here so callers don't have to duplicate the call.
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
	// Replace if same ID already open.
	for i, existing := range s.Panels {
		if existing.ID == p.ID {
			s.Panels = append(s.Panels[:i], s.Panels[i+1:]...)
			break
		}
	}
	s.Panels = append(s.Panels, p)
}

// SwitchPanelContent swaps the widget shown in the floater at `idx` to
// `newID`. Rebuilds Render via RenderFor and updates Title via
// WidgetTitle. No-op when RenderFor is nil (panel wasn't built switchable)
// or when the new ID equals the current one.
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
// floating panel (cursor over one, in an active drag/resize, or hovering
// a resize edge that sticks slightly outside panel bounds). main.go folds
// this into chromeBusy so content-layer LMB handlers skip while a floater
// owns the cursor.
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

// switchRect returns the switch-content button rect sitting just left of
// the X. Returns a zero-size rect when the panel isn't switchable (no
// RenderFor / PanelID), so callers can skip drawing + hit-testing it.
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

// floatingResizeRect returns the BR corner resize grip rect (cosmetic
// glyph location only - the hit-test is wider, see detectResizeEdges).
func floatingResizeRect(p *FloatingPanel) rl.Rectangle {
	return rl.Rectangle{
		X:      p.Bounds.X + p.Bounds.Width - floatingResizeSz,
		Y:      p.Bounds.Y + p.Bounds.Height - floatingResizeSz,
		Width:  floatingResizeSz,
		Height: floatingResizeSz,
	}
}

// detectResizeEdges returns which sides of `p` are under the cursor
// (within floatingResizeBand). At most one side per axis; corners come
// out as two flags. Returns the zero value when the cursor isn't near
// any edge of `p`.
func detectResizeEdges(p *FloatingPanel, cursor rl.Vector2) resizeEdges {
	if p == nil {
		return resizeEdges{}
	}
	// Outer envelope: panel bounds expanded by floatingResizeBand so the
	// cursor can grab even slightly outside the panel's pixels.
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

// CursorForEdges maps a resize-edges combination to the appropriate
// system cursor. main.go calls ResizeCursorAt(cursor) to drive cursor
// swaps without knowing the edge details.
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

// IsResizing reports an active edge / corner resize drag.
func (s *FloatingState) IsResizing() bool { return s.resizeIdx >= 0 }

// ResizeCursorAt returns the cursor that fits the topmost panel's resize
// zone under `cursor`. Falls back to (Default,false) when no panel edge
// is under the cursor. Drives main.go's system-cursor swap chain.
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
		// Don't fall through other panels: once cursor is inside this
		// panel's outer envelope, lower panels are occluded.
		if pointInRect(cursor, s.Panels[i].Bounds) {
			return rl.MouseCursorDefault, false
		}
	}
	return rl.MouseCursorDefault, false
}

// ResizeHover reports whether the cursor is over any panel's edge or
// corner (any resize zone). Kept for backwards compatibility with
// main.go; new code should call ResizeCursorAt for the cursor too.
func (s *FloatingState) ResizeHover(cursor rl.Vector2) bool {
	_, ok := s.ResizeCursorAt(cursor)
	return ok
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
		if s.switchMenuIdx >= 0 {
			s.switchMenuIdx = -1
			return true
		}
		s.Close(s.Panels[len(s.Panels)-1].ID)
		return true
	}

	// Open switch menu wins LMB: hit-test items first, fall through to
	// dismiss on outside click. Menu floats above panel chrome so we
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
			// Click inside the menu rect but on the disabled (current)
			// row: absorb so it doesn't bleed into the panel below.
			if pointInRect(cursor, r) {
				s.switchMenuIdx = -1
				return true
			}
			// Click on the same switch button: toggle off; click elsewhere:
			// dismiss and fall through so the press can start a panel
			// drag / select / etc.
			s.switchMenuIdx = -1
			if pointInRect(cursor, floatingSwitchRect(p)) {
				return true
			}
		}
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
	// Walk top-down so the topmost panel wins. Test edge resize zones
	// before move-drag / body so the title bar's top-pixel band still
	// triggers vertical resize and the BR corner still starts the
	// canonical resize even though it overlaps with the BR resize-grip
	// glyph.
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
			// X close click wins over edges even at the panel's outer
			// corner so a clean BR-X press still closes.
			if pointInRect(cursor, floatingCloseRect(p)) {
				s.Close(p.ID)
				return true
			}
			// Switch-content button toggles the menu.
			if swR := floatingSwitchRect(p); swR.Width > 0 && pointInRect(cursor, swR) {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.switchMenuIdx = newIdx
				s.switchMenuAnchor = floatingSwitchRect(s.Panels[newIdx])
				return true
			}
			// Resize zone (any edge or corner).
			if edges := detectResizeEdges(p, cursor); edges.any() {
				s.raise(i)
				newIdx := len(s.Panels) - 1
				s.resizeIdx = newIdx
				s.resizeEdges = edges
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
			if pointInRect(cursor, p.Bounds) {
				s.raise(i)
			}
		}
		break
	}
	return consumed
}

// applyResizeDrag mutates `p.Bounds` according to which edges the user is
// dragging and the cursor delta from press time. Clamps to MinW/MinH and
// to screen edges (the panel never grows past the window or shrinks below
// its minimum size).
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

	// Switch-content button (chevron glyph) just left of the X.
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

// Switch-content menu rendering. Lightweight popup anchored to the
// floater's switch button; lists every WorkspacePanelKinds entry. Current
// PanelID is shown as a disabled, bullet-prefixed row.
const (
	floatingMenuItemH float32 = 24
	floatingMenuFontH int32   = 13
	floatingMenuPadX  float32 = 8
	floatingMenuPadY  float32 = 4
	floatingMenuMinW  float32 = 150
)

var (
	floatingMenuBG       = rl.Color{R: 26, G: 30, B: 38, A: 245}
	floatingMenuBorder   = rl.Color{R: 70, G: 80, B: 95, A: 255}
	floatingMenuHoverBG  = rl.Color{R: 50, G: 90, B: 130, A: 240}
	floatingMenuText     = rl.Color{R: 220, G: 226, B: 232, A: 255}
	floatingMenuTextDim  = rl.Color{R: 110, G: 120, B: 130, A: 255}
)

// floatingSwitchMenuRect computes the menu's screen rect, clamped to
// stay on screen (flip up + right-align when needed).
func floatingSwitchMenuRect(anchor rl.Rectangle) rl.Rectangle {
	rows := len(WorkspacePanelKinds)
	height := float32(rows)*floatingMenuItemH + 2*floatingMenuPadY
	width := floatingMenuMinW
	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())
	// Right-align with the anchor by default (button sits on the right
	// edge of the title bar) so the menu doesn't drift past the screen
	// edge for default panel placements.
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

// floatingSwitchMenuHit returns the WorkspacePanelKinds index under the
// cursor, or -1 when the cursor is outside the menu / over the current
// (disabled) row.
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

// drawSwitchMenu paints the switch popup for the currently open menu (if
// any). Drawn after DrawAll so it overlays every panel and the chrome.
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

// DrawSwitchMenu is the public entry point - main.go calls it after
// DrawAll so the menu overlays every panel chrome. No-op when no menu is
// currently open.
func (s *FloatingState) DrawSwitchMenu(font rl.Font, cursor rl.Vector2) {
	s.drawSwitchMenu(font, cursor)
}

// SwitchMenuOpen reports whether the switch popup is currently visible.
// Used by main.go to fold into chromeBusy so content layers stay quiet
// while the menu owns the click.
func (s *FloatingState) SwitchMenuOpen() bool { return s.switchMenuIdx >= 0 }
