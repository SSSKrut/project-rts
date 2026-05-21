// Package ui hosts the multi-panel UI layer introduced in Phase 10.
//
// Phase 18.C migrated to a tree-of-splits workspace below a fixed top bar.
// Layout is a recursive LayoutNode (Leaf | Split); Recompute walks it DFS
// and fills Bounds on every node. PanelManager.Get(id) looks up the leaf
// rect via the cache built during Recompute. Splitter drag mutates the
// matching Split node's Ratio. Corner-drag wraps a leaf in a new Split;
// the chevron menu can swap a leaf's widget or merge it with its sibling.
package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// SplitterID identifies a draggable divider. Phase 18.C swapped the old
// enum for the *LayoutNode of the Split being dragged; SplitterNone == nil.
// Type alias keeps existing call sites readable.
type SplitterID = *LayoutNode

// SplitterNone marks "no splitter under the cursor". Tested with == nil.
var SplitterNone SplitterID = nil

// splitterGrabRadius is the hit-zone half-thickness around a splitter line.
const splitterGrabRadius float32 = 6

// panelMinW / panelMinH bound how small a single leaf can shrink during a
// splitter drag.
const (
	panelMinW float32 = 180
	panelMinH float32 = 100
)

// PanelID identifies a widget kind. Each kind has its own draw callback in
// main.go; multiple leaves may not show the same kind at once (chevron
// menu enforces this via swap).
type PanelID string

const (
	Panel3D       PanelID = "3d"
	PanelMap      PanelID = "map"
	PanelInspect  PanelID = "inspector"
	PanelTopBar   PanelID = "topbar"
	PanelTimeline PanelID = "timeline"
	PanelNone     PanelID = ""
)

// Panel is the screen-space rectangle handed back to draw callers. ID
// identifies the widget kind, Bounds is the cached rect from the latest
// Recompute, Title is the chrome label.
type Panel struct {
	ID     PanelID
	Bounds rl.Rectangle
	Title  string
}

// ScrollState is the per-leaf vertical scroll position + measured content
// height. Stored per PanelID in PanelManager.scroll - keyed by widget kind
// rather than tree position so a chevron swap doesn't reset scroll for the
// widget the player is reading.
type ScrollState struct {
	OffsetY       float32
	ContentHeight float32
}

// PanelManager owns the fixed top-bar panel + the workspace tree. Layout
// is the active preset for save/load bookkeeping (mutations on the tree
// don't change it; Tab toggles content of two key leaves rather than
// rebuilding the tree).
type PanelManager struct {
	TopBar    Panel
	Workspace *LayoutNode
	Layout    LayoutPreset

	scroll  map[PanelID]*ScrollState
	focused PanelID

	screenW, screenH int32

	// dragging splitter (Phase 18.C: pointer to a Split node).
	dragging       SplitterID
	dragStartRatio float32
	dragDirty      bool

	// corner-drag bookkeeping (Phase 18.C). cornerLeaf is the leaf whose
	// corner is being dragged; cornerStart holds the press cursor; once
	// motion crosses cornerThreshold the system commits a new Split.
	cornerLeaf  *LayoutNode
	cornerStart rl.Vector2
	cornerDirty bool
}

// NewPanelManager builds the default Field-preset tree.
func NewPanelManager() *PanelManager {
	return &PanelManager{
		TopBar:    Panel{ID: PanelTopBar, Title: ""},
		Workspace: presetFieldTree(),
		Layout:    PresetField,
		scroll:    map[PanelID]*ScrollState{},
		focused:   PanelNone,
	}
}

// SetWorkspace installs a fresh tree (used by layout persistence load).
func (m *PanelManager) SetWorkspace(n *LayoutNode) {
	if n == nil {
		n = presetFieldTree()
	}
	m.Workspace = n
}

// Recompute rebuilds every leaf's Bounds for the current screen size.
func (m *PanelManager) Recompute(screenW, screenH int32) {
	m.screenW = screenW
	m.screenH = screenH
	m.TopBar.Bounds = TopBarRect(screenW)
	if m.Workspace != nil {
		m.Workspace.Compute(WorkspaceRect(screenW, screenH))
	}
}

// TogglePreset flips Field <-> Command by SWAPPING the contents of the
// Panel3D and PanelMap leaves rather than rebuilding the tree, so the
// user's drag-edited layout is preserved.
func (m *PanelManager) TogglePreset() {
	if m.Workspace == nil {
		return
	}
	a := m.Workspace.FindLeaf(Panel3D)
	b := m.Workspace.FindLeaf(PanelMap)
	if a == nil || b == nil {
		return
	}
	SwapPanels(a, b)
	if m.Layout == PresetField {
		m.Layout = PresetCommand
	} else {
		m.Layout = PresetField
	}
}

// FocusedAt returns the PanelID under the cursor. Top bar wins above the
// workspace; PanelNone if the cursor is somewhere weird (off-screen).
func (m *PanelManager) FocusedAt(cursor rl.Vector2) PanelID {
	if pointInRect(cursor, m.TopBar.Bounds) {
		m.focused = PanelTopBar
		return PanelTopBar
	}
	if leaf := m.Workspace.LeafAt(cursor); leaf != nil {
		m.focused = leaf.Panel
		return leaf.Panel
	}
	m.focused = PanelNone
	return PanelNone
}

// Get returns the Panel for `id` (TopBar special-cased). For workspace IDs
// the first leaf in the tree is returned. Returns a zero Panel when the
// widget isn't currently in the tree.
func (m *PanelManager) Get(id PanelID) Panel {
	if id == PanelTopBar {
		return m.TopBar
	}
	leaf := m.Workspace.FindLeaf(id)
	if leaf == nil {
		return Panel{}
	}
	return Panel{ID: leaf.Panel, Bounds: leaf.Bounds, Title: leaf.Title}
}

// LeafFor returns the actual *LayoutNode for a widget kind. Used by the
// chevron menu / corner drag, which need to mutate the tree.
func (m *PanelManager) LeafFor(id PanelID) *LayoutNode {
	if m.Workspace == nil {
		return nil
	}
	return m.Workspace.FindLeaf(id)
}

// LeafAt returns the workspace leaf under `cursor` (nil if cursor is in
// the top bar or outside the window).
func (m *PanelManager) LeafAt(cursor rl.Vector2) *LayoutNode {
	if pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	return m.Workspace.LeafAt(cursor)
}

// IsFocused / Focused mirror the old API.
func (m *PanelManager) IsFocused(id PanelID) bool { return m.focused == id }
func (m *PanelManager) Focused() PanelID          { return m.focused }

// CursorLocal converts a screen-space cursor to a panel-local Vector2.
func CursorLocal(cursor rl.Vector2, p Panel) rl.Vector2 {
	return rl.Vector2{X: cursor.X - p.Bounds.X, Y: cursor.Y - p.Bounds.Y}
}

// ScrollByID returns (creating on demand) the scroll state for a widget.
func (m *PanelManager) ScrollByID(id PanelID) *ScrollState {
	if id == PanelTopBar {
		return nil
	}
	if s, ok := m.scroll[id]; ok {
		return s
	}
	s := &ScrollState{}
	m.scroll[id] = s
	return s
}

func pointInRect(p rl.Vector2, r rl.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

// SplitterAt returns the Split node whose divider sits under cursor (within
// splitterGrabRadius), or nil. Walks the workspace tree; the first hit
// wins which is fine because dividers never overlap.
func (m *PanelManager) SplitterAt(cursor rl.Vector2) SplitterID {
	if m.Workspace == nil {
		return nil
	}
	// Cursor in top bar - no workspace splitters live there.
	if pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	var hit *LayoutNode
	m.Workspace.WalkSplits(func(sp *LayoutNode) {
		if hit != nil {
			return
		}
		if cursorOnDivider(cursor, sp) {
			hit = sp
		}
	})
	return hit
}

// cursorOnDivider reports whether cursor sits within splitterGrabRadius of
// the split's divider line. The divider is the inside edge of the first
// child's rect.
func cursorOnDivider(cursor rl.Vector2, sp *LayoutNode) bool {
	if sp == nil || !sp.IsSplit() {
		return false
	}
	a := sp.Children[0].Bounds
	switch sp.Orient {
	case SplitVertical:
		x := a.X + a.Width
		return cursor.X >= x-splitterGrabRadius && cursor.X <= x+splitterGrabRadius &&
			cursor.Y >= sp.Bounds.Y && cursor.Y <= sp.Bounds.Y+sp.Bounds.Height
	case SplitHorizontal:
		y := a.Y + a.Height
		return cursor.Y >= y-splitterGrabRadius && cursor.Y <= y+splitterGrabRadius &&
			cursor.X >= sp.Bounds.X && cursor.X <= sp.Bounds.X+sp.Bounds.Width
	}
	return false
}

// IsDragging reports an active splitter drag.
func (m *PanelManager) IsDragging() bool { return m.dragging != nil }

// DraggingSplitter returns the currently dragged Split (or nil).
func (m *PanelManager) DraggingSplitter() SplitterID { return m.dragging }

// BeginDrag starts a splitter drag, snapshotting the current ratio.
func (m *PanelManager) BeginDrag(sp SplitterID) {
	if sp == nil || !sp.IsSplit() {
		return
	}
	m.dragging = sp
	m.dragStartRatio = sp.Ratio
	m.dragDirty = false
}

// UpdateDrag applies the current cursor position to the active splitter,
// clamped to min sizes on either side.
func (m *PanelManager) UpdateDrag(cursor rl.Vector2) {
	if m.dragging == nil {
		return
	}
	sp := m.dragging
	r := computeRatioFromCursor(sp, cursor)
	if r != sp.Ratio {
		sp.Ratio = r
		m.dragDirty = true
		m.Recompute(m.screenW, m.screenH)
	}
}

// EndDrag finalises a splitter drag. Returns true if the ratio actually
// changed during the drag (caller persists layout on change).
func (m *PanelManager) EndDrag() bool {
	if m.dragging == nil {
		return false
	}
	changed := m.dragDirty
	m.dragging = nil
	m.dragDirty = false
	return changed
}

// AbortDrag reverts the splitter to its pre-drag ratio.
func (m *PanelManager) AbortDrag() {
	if m.dragging == nil {
		return
	}
	m.dragging.Ratio = m.dragStartRatio
	m.dragging = nil
	m.dragDirty = false
	m.Recompute(m.screenW, m.screenH)
}

// computeRatioFromCursor clamps the drag so neither child shrinks below
// the corresponding min size.
func computeRatioFromCursor(sp *LayoutNode, cursor rl.Vector2) float32 {
	b := sp.Bounds
	switch sp.Orient {
	case SplitVertical:
		if b.Width <= 0 {
			return sp.Ratio
		}
		min := panelMinW
		max := b.Width - panelMinW
		if min > max {
			min = b.Width * 0.5
			max = min
		}
		x := cursor.X - b.X
		if x < min {
			x = min
		}
		if x > max {
			x = max
		}
		return clamp01(x / b.Width)
	case SplitHorizontal:
		if b.Height <= 0 {
			return sp.Ratio
		}
		min := panelMinH
		max := b.Height - panelMinH
		if min > max {
			min = b.Height * 0.5
			max = min
		}
		y := cursor.Y - b.Y
		if y < min {
			y = min
		}
		if y > max {
			y = max
		}
		return clamp01(y / b.Height)
	}
	return sp.Ratio
}

func clamp01(v float32) float32 {
	if v < MinSplitRatio {
		return MinSplitRatio
	}
	if v > MaxSplitRatio {
		return MaxSplitRatio
	}
	return v
}
