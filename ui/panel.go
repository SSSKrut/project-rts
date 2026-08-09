// Package ui hosts the multi-panel UI layer.
//
// The workspace is a recursive LayoutNode (Leaf | Split) below a fixed
// top bar. Recompute walks it DFS and fills Bounds; PanelManager.Get(id)
// looks up the leaf rect from the cache. Splitter / corner drag and the
// chevron menu mutate the tree.
package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// SplitterID is the *LayoutNode being dragged; SplitterNone == nil.
type SplitterID = *LayoutNode

var SplitterNone SplitterID = nil

const splitterGrabRadius float32 = 6

const (
	panelMinW float32 = 180
	panelMinH float32 = 100
)

// PanelID: the chevron menu forbids two leaves showing the same kind via swap.
type PanelID string

const (
	Panel3D        PanelID = "3d"
	PanelMap       PanelID = "map"
	PanelInspect   PanelID = "inspector"
	PanelTopBar    PanelID = "topbar"
	PanelTimeline  PanelID = "timeline"
	PanelFormation PanelID = "formation"
	PanelSymbology PanelID = "symbology"
	PanelBehavior  PanelID = "behavior"
	PanelEvents    PanelID = "events"
	PanelSpecCard  PanelID = "spec"
	PanelDebug     PanelID = "debug"
	PanelNone      PanelID = ""
)

type Panel struct {
	ID     PanelID
	Bounds rl.Rectangle
	Title  string
}

// ScrollState is keyed by PanelID (widget kind) rather than tree position
// so a chevron swap doesn't reset scroll for the widget the player is
// reading.
type ScrollState struct {
	OffsetY       float32
	ContentHeight float32
}

type PanelManager struct {
	TopBar    Panel
	Workspace *LayoutNode
	Layout    LayoutPreset

	scroll  map[PanelID]*ScrollState
	focused PanelID

	screenW, screenH int32

	dragging       SplitterID
	dragStartRatio float32
	dragDirty      bool

	cornerLeaf       *LayoutNode
	cornerPos        CornerPos
	cornerStart      rl.Vector2
	cornerOff        rl.Vector2
	cornerMode       CornerDragKind
	cornerDirty      bool
	cornerSplitX     *LayoutNode
	cornerSplitY     *LayoutNode
	cornerRatioX     float32
	cornerRatioY     float32
	cornerMergeSplit *LayoutNode
	cornerMergeDir   edgeDir

	titleLeaf   *LayoutNode
	titleStart  rl.Vector2
	titleActive bool
}

func NewPanelManager() *PanelManager {
	return &PanelManager{
		TopBar:    Panel{ID: PanelTopBar, Title: ""},
		Workspace: presetFieldTree(),
		Layout:    PresetField,
		scroll:    map[PanelID]*ScrollState{},
		focused:   PanelNone,
	}
}

func (m *PanelManager) SetWorkspace(n *LayoutNode) {
	if n == nil {
		n = presetFieldTree()
	}
	m.Workspace = n
}

func (m *PanelManager) Recompute(screenW, screenH int32) {
	m.screenW = screenW
	m.screenH = screenH
	m.TopBar.Bounds = TopBarRect(screenW)
	if m.Workspace != nil {
		m.Workspace.Compute(WorkspaceRect(screenW, screenH))
	}
}

// TogglePreset swaps the contents of Panel3D / PanelMap leaves rather
// than rebuilding the tree, so the user's drag-edited layout is preserved.
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

// FocusedAt: top bar wins above the workspace.
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

// Get returns a zero Panel when the widget isn't in the tree.
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

func (m *PanelManager) LeafFor(id PanelID) *LayoutNode {
	if m.Workspace == nil {
		return nil
	}
	return m.Workspace.FindLeaf(id)
}

func (m *PanelManager) LeafAt(cursor rl.Vector2) *LayoutNode {
	if pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	return m.Workspace.LeafAt(cursor)
}

func (m *PanelManager) IsFocused(id PanelID) bool { return m.focused == id }
func (m *PanelManager) Focused() PanelID          { return m.focused }

func CursorLocal(cursor rl.Vector2, p Panel) rl.Vector2 {
	return rl.Vector2{X: cursor.X - p.Bounds.X, Y: cursor.Y - p.Bounds.Y}
}

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

// SplitterAt returns the Split node whose divider sits under cursor
// (within splitterGrabRadius); dividers never overlap so first hit wins.
func (m *PanelManager) SplitterAt(cursor rl.Vector2) SplitterID {
	if m.Workspace == nil {
		return nil
	}
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

// cursorOnDivider: the divider is the inside edge of the first child's rect.
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

func (m *PanelManager) IsDragging() bool             { return m.dragging != nil }
func (m *PanelManager) DraggingSplitter() SplitterID { return m.dragging }

func (m *PanelManager) BeginDrag(sp SplitterID) {
	if sp == nil || !sp.IsSplit() {
		return
	}
	m.dragging = sp
	m.dragStartRatio = sp.Ratio
	m.dragDirty = false
}

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

// EndDrag returns true when the ratio actually changed (caller persists).
func (m *PanelManager) EndDrag() bool {
	if m.dragging == nil {
		return false
	}
	changed := m.dragDirty
	m.dragging = nil
	m.dragDirty = false
	return changed
}

func (m *PanelManager) AbortDrag() {
	if m.dragging == nil {
		return
	}
	m.dragging.Ratio = m.dragStartRatio
	m.dragging = nil
	m.dragDirty = false
	m.Recompute(m.screenW, m.screenH)
}

// computeRatioFromCursor clamps so neither child shrinks below min size.
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
