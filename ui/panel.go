// Package ui hosts the multi-panel UI layer introduced in Phase 10.
//
// The package is deliberately ECS-agnostic: panels are screen-space rectangles
// with a content callback or direct draw, no World queries inside this file.
// Each panel's content lives in its own file (scene3d.go, inspector.go,
// time.go, map_render.go) and is wired by main.go.
//
// L1 layout = fixed grid. Two presets (Field / Command) swap which slot the
// 3D scene and the map occupy. L3 (splitters) / L4 (movable + dock zones) is
// Phase 22 — this file's exported surface (PanelManager.{Recompute, FocusedAt,
// Get}) is what stays stable across that migration.
package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// SplitterID identifies which inter-panel splitter the cursor is hovering /
// dragging. Phase 13.5 M13.5.2 introduces two splitters; Phase 22 split-tree
// will generalise to a per-node identity.
type SplitterID uint8

const (
	// SplitterNone — cursor isn't on any splitter.
	SplitterNone SplitterID = iota
	// SplitterMain — vertical line between the big slot (3D in Field preset /
	// Map in Command) and the right column (Inspector + side view stacked).
	// Drag horizontally to redistribute width via RightColRatio.
	SplitterMain
	// SplitterRight — horizontal line inside the right column between
	// Inspector (top) and the side view (bottom). Drag vertically to
	// redistribute height via InspectorRatio.
	SplitterRight
)

// splitterGrabRadius is the hit-zone half-thickness around a splitter line.
// Visual splitter = the existing 1-px panel border; hit-zone wider so the
// cursor can grab without pixel-precision (Fitts).
const splitterGrabRadius float32 = 6

// panelMinW / panelMinH — min sizes used when clamping splitter drags so a
// panel can't be shrunk to invisibility. Inspector quick-bar chips become
// unusable below ~180 px wide; panelMinH=100 prevents zero-height drag.
const (
	panelMinW float32 = 180
	panelMinH float32 = 100
)

// PanelID identifies one of the four MVP panels. String-typed instead of an
// enum because it shows up in debug overlays and HUD labels.
type PanelID string

const (
	Panel3D       PanelID = "3d"
	PanelMap      PanelID = "map"
	PanelInspect  PanelID = "inspector"
	PanelTime     PanelID = "time"
	PanelNone     PanelID = ""
)

// LayoutPreset chooses which slot holds the 3D scene vs the map. Tab toggles
// the two presets at runtime.
type LayoutPreset uint8

const (
	PresetField   LayoutPreset = iota // 3D is the big slot, map is the side slot.
	PresetCommand                     // Map is the big slot, 3D is the side slot.
)

// Panel — one screen-space rectangle. Bounds recomputed by LayoutManager on
// resize / preset swap. Title is shown in the panel's top-left corner.
type Panel struct {
	ID     PanelID
	Bounds rl.Rectangle
	Title  string
}

// ScrollState — per-panel vertical scroll position + measured content height.
//
// Phase 13.5 M13.5.3: Inspector renders with `y -= OffsetY` so contents shift
// up under the scissor; at the end of the draw it writes the total used
// height into ContentHeight so the scrollbar can size its thumb. Phase 21
// will extend other panels (Map / Time stay fixed since they don't overflow).
type ScrollState struct {
	OffsetY       float32
	ContentHeight float32
}

// PanelManager owns the fixed list of panels and the active LayoutPreset.
// Panels slice order doubles as Z-order — first = bottom, last = top. In L1
// nothing overlaps so this only matters for the FocusedAt fallback when two
// rects share a border pixel.
//
// Phase 13.5 M13.5.1: RightColRatio / InspectorRatio are mutable so splitter
// drag (M13.5.2) can resize panels at runtime; loadLayout (M13.5.5) restores
// them from save/layout.json on startup. screenW / screenH are remembered
// from the latest Recompute so UpdateDrag can re-apply ratios without the
// caller passing them every frame.
type PanelManager struct {
	Panels  []Panel
	Layout  LayoutPreset
	focused PanelID

	// Phase 13.5 — mutable layout ratios.
	RightColRatio  float32 // 0..1, side column's share of width
	InspectorRatio float32 // 0..1, inspector's share of side column height

	// Cached screen dimensions from the latest Recompute. UpdateDrag uses
	// these to convert cursor delta → ratio delta without re-querying raylib
	// inside ui/.
	screenW, screenH int32

	// Splitter drag state (Phase 13.5 M13.5.2). dragging == SplitterNone
	// means no drag in progress.
	dragging        SplitterID
	dragStartRatio  float32 // ratio snapshot at BeginDrag (for EndDrag-changed flag)
	dragInitialDirt bool    // tracks whether the drag actually mutated ratio

	// Per-panel scroll state (Phase 13.5 M13.5.3). Indexed parallel to
	// Panels — Scroll[i] belongs to Panels[i].
	Scroll [4]ScrollState
}

// NewPanelManager constructs the manager with four empty-bounds panels in
// canonical order. Caller must call Recompute(screenW, screenH) before the
// first draw — bounds are zero until then.
func NewPanelManager() *PanelManager {
	return &PanelManager{
		Panels: []Panel{
			{ID: Panel3D, Title: "Field"},
			{ID: PanelMap, Title: "Map"},
			{ID: PanelInspect, Title: "Inspector"},
			{ID: PanelTime, Title: "Time"},
		},
		Layout:         PresetField,
		focused:        PanelNone,
		RightColRatio:  DefaultRightColRatio,
		InspectorRatio: DefaultInspectorRatio,
	}
}

// Recompute rebuilds every panel's Bounds from the current screen size and
// active LayoutPreset. Idempotent — call after resize, preset toggle, or any
// time the cached rects might be stale.
func (m *PanelManager) Recompute(screenW, screenH int32) {
	m.screenW = screenW
	m.screenH = screenH
	var rects map[PanelID]rl.Rectangle
	switch m.Layout {
	case PresetCommand:
		rects = layoutCommand(screenW, screenH, m.RightColRatio, m.InspectorRatio)
	default:
		rects = layoutField(screenW, screenH, m.RightColRatio, m.InspectorRatio)
	}
	for i := range m.Panels {
		if r, ok := rects[m.Panels[i].ID]; ok {
			m.Panels[i].Bounds = r
		}
	}
}

// TogglePreset flips Field ↔ Command. Caller is responsible for Recompute()
// after — keeping the calls separate lets main.go also realloc the 3D RT in
// the same place.
func (m *PanelManager) TogglePreset() {
	if m.Layout == PresetField {
		m.Layout = PresetCommand
	} else {
		m.Layout = PresetField
	}
}

// FocusedAt returns the topmost panel whose Bounds contains cursor. Returns
// PanelNone when cursor is outside every panel (e.g. on the OS title bar). In
// L1 panels don't overlap; iteration goes back-to-front to match the L3/L4
// Z-order rule for free.
func (m *PanelManager) FocusedAt(cursor rl.Vector2) PanelID {
	for i := len(m.Panels) - 1; i >= 0; i-- {
		p := &m.Panels[i]
		if pointInRect(cursor, p.Bounds) {
			m.focused = p.ID
			return p.ID
		}
	}
	m.focused = PanelNone
	return PanelNone
}

// Get returns the Panel by ID. Returns a zero Panel{} when the ID is unknown —
// callers that hand out a fixed compile-time ID can assume Get always
// succeeds.
func (m *PanelManager) Get(id PanelID) Panel {
	for i := range m.Panels {
		if m.Panels[i].ID == id {
			return m.Panels[i]
		}
	}
	return Panel{}
}

// IsFocused is the per-frame gate used by input handlers. Reads the cached
// focusedPanel from the last FocusedAt; call FocusedAt at the top of each
// frame.
func (m *PanelManager) IsFocused(id PanelID) bool {
	return m.focused == id
}

// Focused returns the cached focused panel ID. Useful when the same ID is
// needed in multiple input blocks — call FocusedAt once and re-read via this.
func (m *PanelManager) Focused() PanelID { return m.focused }

// CursorLocal converts a screen-space cursor to a panel-local Vector2 (cursor
// relative to the panel's top-left). Used by raycast / marquee / picking in
// the 3D and map panels.
func CursorLocal(cursor rl.Vector2, p Panel) rl.Vector2 {
	return rl.Vector2{X: cursor.X - p.Bounds.X, Y: cursor.Y - p.Bounds.Y}
}

// ScrollByID returns a pointer to the per-panel scroll state. Returns nil
// if the ID is unknown — callers handing out a fixed compile-time ID can
// assume non-nil.
func (m *PanelManager) ScrollByID(id PanelID) *ScrollState {
	for i := range m.Panels {
		if m.Panels[i].ID == id {
			return &m.Scroll[i]
		}
	}
	return nil
}

func pointInRect(p rl.Vector2, r rl.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

// splitterMainX returns the X coord of the vertical Main splitter line. It
// sits at the boundary between the big slot (left column) and the right
// column. In both Field and Command presets this is the same — only the
// content of the big slot changes (3D vs Map).
func (m *PanelManager) splitterMainX() float32 {
	return float32(m.screenW) - float32(m.screenW)*m.RightColRatio
}

// splitterRightY returns the Y coord of the horizontal Right splitter line.
// It sits inside the right column between Inspector (top) and the side view
// (bottom). Inspector lives in the same screen position in both presets.
func (m *PanelManager) splitterRightY() float32 {
	contentH := float32(m.screenH - timeBarHeight)
	return contentH * m.InspectorRatio
}

// SplitterAt returns the splitter under `cursor`, or SplitterNone if none.
// Hit-zone half-thickness = splitterGrabRadius around the splitter line.
// Right splitter is only valid inside the right column's X range.
func (m *PanelManager) SplitterAt(cursor rl.Vector2) SplitterID {
	if m.screenW <= 0 || m.screenH <= 0 {
		return SplitterNone
	}
	contentH := float32(m.screenH - timeBarHeight)
	// Time-bar area — splitters don't extend below contentH.
	if cursor.Y >= contentH {
		return SplitterNone
	}
	mainX := m.splitterMainX()
	rightY := m.splitterRightY()

	// Right splitter check first — its Y-band overlaps with Main's X-band at
	// the corner, but a horizontal cursor sweep inside the right column should
	// land on Right, not Main. So Right wins when cursor is inside the right
	// column AND within Y-grab of the right splitter.
	if cursor.X >= mainX-splitterGrabRadius && cursor.X <= float32(m.screenW) {
		if cursor.Y >= rightY-splitterGrabRadius && cursor.Y <= rightY+splitterGrabRadius {
			return SplitterRight
		}
	}
	// Main splitter: vertical line at mainX, spanning full content height.
	if cursor.X >= mainX-splitterGrabRadius && cursor.X <= mainX+splitterGrabRadius {
		return SplitterMain
	}
	return SplitterNone
}

// IsDragging reports whether a splitter drag is currently in progress.
func (m *PanelManager) IsDragging() bool {
	return m.dragging != SplitterNone
}

// DraggingSplitter returns the currently dragged splitter (SplitterNone if
// no drag is active). Useful for cursor-icon override during drag.
func (m *PanelManager) DraggingSplitter() SplitterID {
	return m.dragging
}

// BeginDrag marks the start of a splitter drag and snapshots the current
// ratio so EndDrag can decide whether a save is needed.
func (m *PanelManager) BeginDrag(splitter SplitterID) {
	m.dragging = splitter
	m.dragInitialDirt = false
	switch splitter {
	case SplitterMain:
		m.dragStartRatio = m.RightColRatio
	case SplitterRight:
		m.dragStartRatio = m.InspectorRatio
	default:
		m.dragging = SplitterNone
	}
}

// UpdateDrag applies the current cursor position to the active splitter,
// clamps to min-size constraints, mutates the ratio, and recomputes panel
// bounds. No-op if no drag is in progress.
func (m *PanelManager) UpdateDrag(cursor rl.Vector2) {
	if m.dragging == SplitterNone || m.screenW <= 0 || m.screenH <= 0 {
		return
	}
	switch m.dragging {
	case SplitterMain:
		// Splitter follows cursor.X. RightW = screenW - cursor.X.
		// Clamp so both leftW and rightW ≥ panelMinW.
		x := cursor.X
		minX := panelMinW
		maxX := float32(m.screenW) - panelMinW
		if minX > maxX {
			// Window too narrow for both mins — meet in the middle.
			minX = float32(m.screenW) * 0.5
			maxX = minX
		}
		if x < minX {
			x = minX
		}
		if x > maxX {
			x = maxX
		}
		newRatio := (float32(m.screenW) - x) / float32(m.screenW)
		if newRatio != m.RightColRatio {
			m.RightColRatio = newRatio
			m.dragInitialDirt = true
		}
	case SplitterRight:
		// Splitter follows cursor.Y inside the right column.
		// inspectorH = cursor.Y; sideH = contentH - cursor.Y.
		contentH := float32(m.screenH - timeBarHeight)
		y := cursor.Y
		minY := panelMinH
		maxY := contentH - panelMinH
		if minY > maxY {
			minY = contentH * 0.5
			maxY = minY
		}
		if y < minY {
			y = minY
		}
		if y > maxY {
			y = maxY
		}
		newRatio := y / contentH
		if newRatio != m.InspectorRatio {
			m.InspectorRatio = newRatio
			m.dragInitialDirt = true
		}
	}
	m.Recompute(m.screenW, m.screenH)
}

// EndDrag finalises a splitter drag. Returns true if the ratio actually
// changed during the drag — callers use this to gate layout persistence
// writes so we don't re-save on no-op clicks.
func (m *PanelManager) EndDrag() bool {
	if m.dragging == SplitterNone {
		return false
	}
	changed := m.dragInitialDirt
	m.dragging = SplitterNone
	m.dragInitialDirt = false
	return changed
}

// AbortDrag cancels a drag and reverts the ratio to its pre-drag value.
// Used when an external event (Tab preset toggle, window resize) should
// pre-empt the drag.
func (m *PanelManager) AbortDrag() {
	if m.dragging == SplitterNone {
		return
	}
	switch m.dragging {
	case SplitterMain:
		m.RightColRatio = m.dragStartRatio
	case SplitterRight:
		m.InspectorRatio = m.dragStartRatio
	}
	m.dragging = SplitterNone
	m.dragInitialDirt = false
	m.Recompute(m.screenW, m.screenH)
}
