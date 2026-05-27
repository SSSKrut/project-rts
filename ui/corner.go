package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Blender-style corner drag. Each workspace leaf carries grab handles in
// all four corners; from any corner the drag does the same thing. Two modes:
//
//   - INSIDE source at release: split (larger of |dx|/|dy| picks axis; sign
//     picks which side keeps the original content).
//   - OUTSIDE source at release with a sibling: merge (source absorbed,
//     sibling expands).
//
// Top-edge corners are shifted below the title bar so they don't collide
// with the chevron or title text.

const (
	cornerGrabSize float32 = 14
	cornerCommitPx float32 = 8
)

var (
	cornerHandleColor    = rl.Color{R: 110, G: 120, B: 140, A: 220}
	cornerHandleHotColor = rl.Color{R: 200, G: 220, B: 240, A: 240}
	cornerPreviewColor   = rl.Color{R: 90, G: 200, B: 255, A: 200}
	cornerMergeOverlay   = rl.Color{R: 70, G: 78, B: 88, A: 170}
	cornerMergeBorder    = rl.Color{R: 220, G: 90, B: 90, A: 220}
	cornerDockHighlight  = rl.Color{R: 90, G: 200, B: 255, A: 110}
	cornerDockBorder     = rl.Color{R: 90, G: 200, B: 255, A: 230}
)

type CornerPos uint8

const (
	CornerBR CornerPos = iota
	CornerBL
	CornerTR
	CornerTL
)

var AllCornerPos = [...]CornerPos{CornerBR, CornerBL, CornerTR, CornerTL}

type CornerDragKind uint8

const (
	CornerDragNone   CornerDragKind = iota
	CornerSplitVert                 // horizontal motion → vertical divider
	CornerSplitHoriz                // vertical motion → horizontal divider
	CornerMerge                     // cursor on source's direct sibling → close source
	CornerDock                      // cursor on a non-sibling leaf → restructure
)

func CornerHandleRect(leaf *LayoutNode, pos CornerPos) rl.Rectangle {
	if leaf == nil {
		return rl.Rectangle{}
	}
	b := leaf.Bounds
	sz := cornerGrabSize
	switch pos {
	case CornerBR:
		return rl.Rectangle{X: b.X + b.Width - sz, Y: b.Y + b.Height - sz, Width: sz, Height: sz}
	case CornerBL:
		return rl.Rectangle{X: b.X, Y: b.Y + b.Height - sz, Width: sz, Height: sz}
	case CornerTR:
		return rl.Rectangle{X: b.X + b.Width - sz, Y: b.Y + float32(titleBarHeight) + 2, Width: sz, Height: sz}
	case CornerTL:
		return rl.Rectangle{X: b.X, Y: b.Y + float32(titleBarHeight) + 2, Width: sz, Height: sz}
	}
	return rl.Rectangle{}
}

func (m *PanelManager) CornerAt(cursor rl.Vector2) *LayoutNode {
	if m.Workspace == nil {
		return nil
	}
	if pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	var hit *LayoutNode
	m.Workspace.WalkLeaves(func(l *LayoutNode) {
		if hit != nil {
			return
		}
		for _, p := range AllCornerPos {
			if pointInRect(cursor, CornerHandleRect(l, p)) {
				hit = l
				return
			}
		}
	})
	return hit
}

func (m *PanelManager) BeginCornerDrag(leaf *LayoutNode, cursor rl.Vector2) {
	m.cornerLeaf = leaf
	m.cornerStart = cursor
	m.cornerDirty = false
}

func (m *PanelManager) IsCornerDragging() bool       { return m.cornerLeaf != nil }
func (m *PanelManager) CornerDragLeaf() *LayoutNode  { return m.cornerLeaf }
func (m *PanelManager) CornerDragOrigin() rl.Vector2 { return m.cornerStart }

// CornerDragMode classifies the in-progress drag:
//   - Cursor inside source.Bounds → split (axis from larger |dx|/|dy|).
//   - Cursor on source.Sibling()  → merge (close source).
//   - Cursor on another leaf      → dock (full-level strip on nearest edge).
//   - Else → none.
func (m *PanelManager) CornerDragMode(cursor rl.Vector2) CornerDragKind {
	if m.cornerLeaf == nil {
		return CornerDragNone
	}
	dx := cursor.X - m.cornerStart.X
	dy := cursor.Y - m.cornerStart.Y
	adx, ady := abs32(dx), abs32(dy)
	if adx < cornerCommitPx && ady < cornerCommitPx {
		return CornerDragNone
	}
	if pointInRect(cursor, m.cornerLeaf.Bounds) {
		if adx > ady {
			return CornerSplitVert
		}
		return CornerSplitHoriz
	}
	target := m.cornerTargetAt(cursor)
	if target == nil {
		return CornerDragNone
	}
	if target == m.cornerLeaf.Sibling() {
		return CornerMerge
	}
	return CornerDock
}

// cornerTargetAt returns the leaf under cursor that is NOT the source.
func (m *PanelManager) cornerTargetAt(cursor rl.Vector2) *LayoutNode {
	if m.Workspace == nil {
		return nil
	}
	if pointInRect(cursor, m.TopBar.Bounds) {
		return nil
	}
	hit := m.Workspace.LeafAt(cursor)
	if hit == m.cornerLeaf {
		return nil
	}
	return hit
}

func (m *PanelManager) CornerDragTarget(cursor rl.Vector2) *LayoutNode {
	return m.cornerTargetAt(cursor)
}

// CornerDragSide is only meaningful for CornerDock mode.
func (m *PanelManager) CornerDragSide(cursor rl.Vector2) DockSide {
	target := m.cornerTargetAt(cursor)
	if target == nil {
		return DockNone
	}
	return DockSideFor(target, cursor)
}

func (m *PanelManager) CancelCornerDrag() {
	m.cornerLeaf = nil
	m.cornerDirty = false
}

func (m *PanelManager) CommitCornerDrag(cursor rl.Vector2) bool {
	leaf := m.cornerLeaf
	if leaf == nil {
		m.CancelCornerDrag()
		return false
	}
	mode := m.CornerDragMode(cursor)
	target := m.cornerTargetAt(cursor)
	side := DockNone
	if target != nil {
		side = DockSideFor(target, cursor)
	}
	m.cornerLeaf = nil

	switch mode {
	case CornerSplitVert, CornerSplitHoriz:
		return m.commitSplit(leaf, mode, cursor)
	case CornerMerge:
		return m.commitMerge(leaf)
	case CornerDock:
		return m.commitDock(leaf, target, side)
	}
	return false
}

func (m *PanelManager) commitSplit(leaf *LayoutNode, mode CornerDragKind, cursor rl.Vector2) bool {
	b := leaf.Bounds
	dx := cursor.X - m.cornerStart.X
	dy := cursor.Y - m.cornerStart.Y

	var orient SplitOrient
	var ratio float32
	var originalSide int
	switch mode {
	case CornerSplitVert:
		orient = SplitVertical
		if dx < 0 {
			split := -dx
			if split > b.Width-panelMinW {
				split = b.Width - panelMinW
			}
			if split < panelMinW {
				split = panelMinW
			}
			ratio = split / b.Width
			originalSide = 1
		} else {
			split := dx
			if split > b.Width-panelMinW {
				split = b.Width - panelMinW
			}
			if split < panelMinW {
				split = panelMinW
			}
			ratio = (b.Width - split) / b.Width
			originalSide = 0
		}
	case CornerSplitHoriz:
		orient = SplitHorizontal
		if dy < 0 {
			split := -dy
			if split > b.Height-panelMinH {
				split = b.Height - panelMinH
			}
			if split < panelMinH {
				split = panelMinH
			}
			ratio = split / b.Height
			originalSide = 1
		} else {
			split := dy
			if split > b.Height-panelMinH {
				split = b.Height - panelMinH
			}
			if split < panelMinH {
				split = panelMinH
			}
			ratio = (b.Height - split) / b.Height
			originalSide = 0
		}
	}

	newSplit := SplitLeaf(leaf, orient, clamp01(ratio), originalSide)
	if newSplit == nil {
		return false
	}
	if leaf == m.Workspace {
		m.Workspace = newSplit
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

func (m *PanelManager) commitMerge(leaf *LayoutNode) bool {
	if leaf == nil || leaf.Parent == nil {
		return false
	}
	wasRootChild := leaf.Parent == m.Workspace
	sib := MergeIntoSibling(leaf)
	if sib == nil {
		return false
	}
	if wasRootChild {
		m.Workspace = sib
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

// commitDock detaches `source` from its old slot and re-attaches as a strip
// on `side` of the subtree containing `target`. MergeIntoSibling can shift
// `target`'s ancestor pointers, so DockNear re-reads them.
func (m *PanelManager) commitDock(source, target *LayoutNode, side DockSide) bool {
	if source == nil || target == nil || source == target || side == DockNone {
		return false
	}
	if source.Parent == nil {
		return false
	}
	sourceWasRootChild := source.Parent == m.Workspace
	sib := MergeIntoSibling(source)
	if sib == nil {
		return false
	}
	if sourceWasRootChild {
		m.Workspace = sib
	}
	wrap := target.Parent
	if wrap == nil {
		wrap = target
	}
	wrapWasRoot := wrap == m.Workspace
	newSplit := DockNear(source, target, side)
	if newSplit == nil {
		return false
	}
	if wrapWasRoot {
		m.Workspace = newSplit
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

func DrawCornerHandles(m *PanelManager, cursor rl.Vector2) {
	if m == nil || m.Workspace == nil {
		return
	}
	hot := m.CornerAt(cursor)
	m.Workspace.WalkLeaves(func(l *LayoutNode) {
		for _, p := range AllCornerPos {
			r := CornerHandleRect(l, p)
			c := cornerHandleColor
			if (l == hot && pointInRect(cursor, r)) || l == m.cornerLeaf {
				c = cornerHandleHotColor
			}
			drawCornerGlyph(r, p, c)
		}
	})
}

func drawCornerGlyph(r rl.Rectangle, pos CornerPos, c rl.Color) {
	var ax, ay float32
	var dx, dy float32
	switch pos {
	case CornerBR:
		ax, ay = r.X+r.Width-2, r.Y+r.Height-2
		dx, dy = -1, -1
	case CornerBL:
		ax, ay = r.X+2, r.Y+r.Height-2
		dx, dy = +1, -1
	case CornerTR:
		ax, ay = r.X+r.Width-2, r.Y+2
		dx, dy = -1, +1
	case CornerTL:
		ax, ay = r.X+2, r.Y+2
		dx, dy = +1, +1
	}
	for i := 0; i < 3; i++ {
		off := float32(i)*4 + 3
		rl.DrawLineEx(
			rl.Vector2{X: ax + dx*off, Y: ay},
			rl.Vector2{X: ax, Y: ay + dy*off},
			1.5, c)
	}
}

func DrawCornerDragPreview(m *PanelManager, cursor rl.Vector2) {
	if m == nil || m.cornerLeaf == nil {
		return
	}
	mode := m.CornerDragMode(cursor)
	switch mode {
	case CornerSplitVert:
		b := m.cornerLeaf.Bounds
		x := cursor.X
		if x < b.X+panelMinW {
			x = b.X + panelMinW
		}
		if x > b.X+b.Width-panelMinW {
			x = b.X + b.Width - panelMinW
		}
		rl.DrawLineEx(
			rl.Vector2{X: x, Y: b.Y + 4},
			rl.Vector2{X: x, Y: b.Y + b.Height - 4},
			2, cornerPreviewColor)
	case CornerSplitHoriz:
		b := m.cornerLeaf.Bounds
		y := cursor.Y
		if y < b.Y+panelMinH {
			y = b.Y + panelMinH
		}
		if y > b.Y+b.Height-panelMinH {
			y = b.Y + b.Height - panelMinH
		}
		rl.DrawLineEx(
			rl.Vector2{X: b.X + 4, Y: y},
			rl.Vector2{X: b.X + b.Width - 4, Y: y},
			2, cornerPreviewColor)
	case CornerMerge:
		sib := m.cornerLeaf.Sibling()
		if sib == nil {
			return
		}
		rl.DrawRectangleRec(sib.Bounds, cornerMergeOverlay)
		rl.DrawRectangleLinesEx(m.cornerLeaf.Bounds, 2, cornerMergeBorder)
		drawMergeArrow(m.cornerLeaf.Bounds, sib.Bounds)
	case CornerDock:
		target := m.cornerTargetAt(cursor)
		if target == nil {
			return
		}
		side := DockSideFor(target, cursor)
		wrap := DockWrapBounds(target)
		highlight := DockHighlightRect(target, side)
		rl.DrawRectangleRec(wrap, cornerMergeOverlay)
		rl.DrawRectangleRec(highlight, cornerDockHighlight)
		rl.DrawRectangleLinesEx(highlight, 2, cornerDockBorder)
		rl.DrawRectangleLinesEx(m.cornerLeaf.Bounds, 2, cornerDockBorder)
		drawMergeArrow(m.cornerLeaf.Bounds, highlight)
	}
}

func drawMergeArrow(src, dst rl.Rectangle) {
	from := rl.Vector2{X: src.X + src.Width*0.5, Y: src.Y + src.Height*0.5}
	to := rl.Vector2{X: dst.X + dst.Width*0.5, Y: dst.Y + dst.Height*0.5}
	rl.DrawLineEx(from, to, 4, cornerMergeBorder)
	dx := to.X - from.X
	dy := to.Y - from.Y
	d := float32Sqrt(dx*dx + dy*dy)
	if d < 1 {
		return
	}
	ux, uy := dx/d, dy/d
	tipX, tipY := to.X-ux*12, to.Y-uy*12
	px, py := -uy, ux
	rl.DrawLineEx(
		rl.Vector2{X: tipX + px*7, Y: tipY + py*7},
		to, 4, cornerMergeBorder)
	rl.DrawLineEx(
		rl.Vector2{X: tipX - px*7, Y: tipY - py*7},
		to, 4, cornerMergeBorder)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func float32Sqrt(v float32) float32 {
	if v <= 0 {
		return 0
	}
	return float32(math.Sqrt(float64(v)))
}
