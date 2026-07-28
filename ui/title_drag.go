package ui

import rl "github.com/gen2brain/raylib-go/raylib"

// Title-bar drag moves a pane: press on a leaf's title bar, drag onto another
// pane, release over the half it should take. The pane leaves its old slot
// (the sibling absorbs the space) and splits the target leaf in two. Corner
// grips resize, title bars move — the two gestures never overlap.

const titleDragCommitPx float32 = 6

func (m *PanelManager) TitleBarAt(cursor rl.Vector2) *LayoutNode {
	if m.Workspace == nil || pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	var hit *LayoutNode
	m.Workspace.WalkLeaves(func(l *LayoutNode) {
		if hit != nil {
			return
		}
		if pointInRect(cursor, PanelTitleRect(l.Bounds)) &&
			!pointInRect(cursor, ChevronRect(Panel{Bounds: l.Bounds})) {
			hit = l
		}
	})
	return hit
}

func (m *PanelManager) BeginTitleDrag(leaf *LayoutNode, cursor rl.Vector2) {
	if leaf == nil || !leaf.IsLeaf() {
		return
	}
	m.titleLeaf = leaf
	m.titleStart = cursor
	m.titleActive = false
}

func (m *PanelManager) UpdateTitleDrag(cursor rl.Vector2) {
	if m.titleLeaf == nil || m.titleActive {
		return
	}
	dx, dy := cursor.X-m.titleStart.X, cursor.Y-m.titleStart.Y
	if dx*dx+dy*dy >= titleDragCommitPx*titleDragCommitPx {
		m.titleActive = true
	}
}

// IsTitleDragging covers the whole press→release window so content layers
// stay quiet even for a plain title click; TitleDragActive is the committed
// drag.
func (m *PanelManager) IsTitleDragging() bool      { return m.titleLeaf != nil }
func (m *PanelManager) TitleDragActive() bool      { return m.titleLeaf != nil && m.titleActive }
func (m *PanelManager) TitleDragLeaf() *LayoutNode { return m.titleLeaf }

func (m *PanelManager) TitleDragTarget(cursor rl.Vector2) *LayoutNode {
	if !m.TitleDragActive() || m.Workspace == nil {
		return nil
	}
	hit := m.Workspace.LeafAt(cursor)
	if hit == m.titleLeaf {
		return nil
	}
	return hit
}

func (m *PanelManager) CancelTitleDrag() {
	m.titleLeaf = nil
	m.titleActive = false
}

func (m *PanelManager) CommitTitleDrag(cursor rl.Vector2) bool {
	src := m.titleLeaf
	target := m.TitleDragTarget(cursor)
	side := DockNone
	if target != nil {
		side = DockSideFor(target, cursor)
	}
	m.CancelTitleDrag()
	if src == nil || target == nil || side == DockNone || src.Parent == nil {
		return false
	}
	srcWasRootChild := src.Parent == m.Workspace
	sib := MergeIntoSibling(src)
	if sib == nil {
		return false
	}
	if srcWasRootChild {
		m.Workspace = sib
	}
	targetWasRoot := target == m.Workspace
	split := DockNear(src, target, side)
	if split == nil {
		return false
	}
	if targetWasRoot {
		m.Workspace = split
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

func DrawTitleDragPreview(m *PanelManager, cursor rl.Vector2) {
	if m == nil || !m.TitleDragActive() {
		return
	}
	target := m.TitleDragTarget(cursor)
	if target == nil {
		return
	}
	side := DockSideFor(target, cursor)
	hl := DockHighlightRect(target, side)
	rl.DrawRectangleRec(target.Bounds, cornerMergeOverlay)
	rl.DrawRectangleRec(hl, cornerDockHighlight)
	rl.DrawRectangleLinesEx(hl, 2, cornerDockBorder)
	rl.DrawRectangleLinesEx(m.titleLeaf.Bounds, 2, cornerDockBorder)
	drawMergeArrow(m.titleLeaf.Bounds, hl)
}
