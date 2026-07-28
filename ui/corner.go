package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Corner grips. Each workspace leaf carries a grab handle in all four
// corners; what the drag does is picked by direction:
//
//   - OUTWARD (away from the pane) → resize: the two dividers bounding the
//     pane at that corner follow the cursor, so the pane grows and the
//     neighbours shrink. Nothing moves in the tree.
//   - OUTWARD past a neighbour's far edge → that neighbour is eaten and the
//     pane takes its place (only when the neighbour is a single leaf).
//   - INWARD (into the pane) → split at the cursor; the swept side becomes
//     the new pane.
//
// Moving a pane elsewhere is the title-bar drag (title_drag.go), not this.
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
	cornerResizeBorder   = rl.Color{R: 90, G: 200, B: 255, A: 160}
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
	CornerSplitVert                 // inward horizontal motion → vertical divider
	CornerSplitHoriz                // inward vertical motion → horizontal divider
	CornerResize                    // outward → bounding dividers follow the cursor
	CornerMerge                     // outward past a neighbour → neighbour is eaten
)

// edgeDir names one side of a pane.
type edgeDir uint8

const (
	edgeRight edgeDir = iota
	edgeLeft
	edgeBottom
	edgeTop
)

func cornerDirX(p CornerPos) edgeDir {
	if p == CornerBR || p == CornerTR {
		return edgeRight
	}
	return edgeLeft
}

func cornerDirY(p CornerPos) edgeDir {
	if p == CornerBR || p == CornerBL {
		return edgeBottom
	}
	return edgeTop
}

// edgeSign is +1 when the edge's outward direction is +X / +Y.
func edgeSign(d edgeDir) float32 {
	if d == edgeRight || d == edgeBottom {
		return 1
	}
	return -1
}

// neighbourChild is the index of the child living past dir's divider.
func neighbourChild(d edgeDir) int {
	if d == edgeRight || d == edgeBottom {
		return 1
	}
	return 0
}

// boundingSplit returns the ancestor split whose divider forms `leaf`'s edge
// on `dir` — the divider a resize on that side must move. nil when the leaf
// reaches the workspace border there.
func boundingSplit(leaf *LayoutNode, dir edgeDir) *LayoutNode {
	for n := leaf; n != nil && n.Parent != nil; n = n.Parent {
		p := n.Parent
		want := SplitVertical
		if dir == edgeBottom || dir == edgeTop {
			want = SplitHorizontal
		}
		if p.Orient == want && p.Children[neighbourChild(dir)] != n {
			return p
		}
	}
	return nil
}

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

// CornerCursor: BR/TL sit on the NWSE diagonal, BL/TR on NESW.
func CornerCursor(pos CornerPos) rl.MouseCursor {
	if pos == CornerBR || pos == CornerTL {
		return rl.MouseCursorResizeNWSE
	}
	return rl.MouseCursorResizeNESW
}

func (m *PanelManager) CornerAt(cursor rl.Vector2) (*LayoutNode, CornerPos) {
	if m.Workspace == nil || pointInRect(cursor, m.TopBar.Bounds) {
		return nil, CornerBR
	}
	var hit *LayoutNode
	hitPos := CornerBR
	m.Workspace.WalkLeaves(func(l *LayoutNode) {
		if hit != nil {
			return
		}
		for _, p := range AllCornerPos {
			if pointInRect(cursor, CornerHandleRect(l, p)) {
				hit, hitPos = l, p
				return
			}
		}
	})
	return hit, hitPos
}

// BeginCornerDrag snapshots the two bounding dividers plus the offset from
// the grab point to the pane edge, so the edge doesn't jump to the cursor on
// the first frame.
func (m *PanelManager) BeginCornerDrag(leaf *LayoutNode, pos CornerPos, cursor rl.Vector2) {
	if leaf == nil || !leaf.IsLeaf() {
		return
	}
	m.cornerLeaf = leaf
	m.cornerPos = pos
	m.cornerStart = cursor
	m.cornerMode = CornerDragNone
	m.cornerDirty = false
	m.cornerMergeSplit = nil

	b := leaf.Bounds
	m.cornerSplitX = boundingSplit(leaf, cornerDirX(pos))
	m.cornerSplitY = boundingSplit(leaf, cornerDirY(pos))
	if cornerDirX(pos) == edgeRight {
		m.cornerOff.X = b.X + b.Width - cursor.X
	} else {
		m.cornerOff.X = b.X - cursor.X
	}
	if cornerDirY(pos) == edgeBottom {
		m.cornerOff.Y = b.Y + b.Height - cursor.Y
	} else {
		m.cornerOff.Y = b.Y - cursor.Y
	}
	if m.cornerSplitX != nil {
		m.cornerRatioX = m.cornerSplitX.Ratio
	}
	if m.cornerSplitY != nil {
		m.cornerRatioY = m.cornerSplitY.Ratio
	}
}

func (m *PanelManager) IsCornerDragging() bool       { return m.cornerLeaf != nil }
func (m *PanelManager) CornerDragLeaf() *LayoutNode  { return m.cornerLeaf }
func (m *PanelManager) CornerDragPos() CornerPos     { return m.cornerPos }
func (m *PanelManager) CornerDragOrigin() rl.Vector2 { return m.cornerStart }
func (m *PanelManager) CornerDragKindNow() CornerDragKind {
	return m.cornerMode
}

// UpdateCornerDrag classifies the gesture once (sticky for the rest of the
// drag) and, in resize mode, applies the new ratios live. Returning to the
// grab point releases the classification and restores the original ratios.
func (m *PanelManager) UpdateCornerDrag(cursor rl.Vector2) {
	if m.cornerLeaf == nil {
		return
	}
	dx := cursor.X - m.cornerStart.X
	dy := cursor.Y - m.cornerStart.Y
	if abs32(dx) < cornerCommitPx && abs32(dy) < cornerCommitPx {
		if m.cornerMode == CornerResize || m.cornerMode == CornerMerge {
			m.restoreCornerRatios()
			m.Recompute(m.screenW, m.screenH)
		}
		m.cornerMode = CornerDragNone
		m.cornerMergeSplit = nil
		return
	}
	if m.cornerMode == CornerDragNone {
		outX := dx * edgeSign(cornerDirX(m.cornerPos))
		outY := dy * edgeSign(cornerDirY(m.cornerPos))
		if outX > 0 || outY > 0 {
			m.cornerMode = CornerResize
		} else {
			m.cornerMode = splitAxis(dx, dy)
		}
	}
	switch m.cornerMode {
	case CornerResize, CornerMerge:
		m.applyCornerResize(cursor)
	case CornerSplitVert, CornerSplitHoriz:
		m.cornerMode = splitAxis(dx, dy)
	}
}

func splitAxis(dx, dy float32) CornerDragKind {
	if abs32(dx) > abs32(dy) {
		return CornerSplitVert
	}
	return CornerSplitHoriz
}

// cornerEdgeCursor maps the cursor into divider space (see cornerOff).
func (m *PanelManager) cornerEdgeCursor(cursor rl.Vector2) rl.Vector2 {
	return rl.Vector2{X: cursor.X + m.cornerOff.X, Y: cursor.Y + m.cornerOff.Y}
}

func (m *PanelManager) applyCornerResize(cursor rl.Vector2) {
	edge := m.cornerEdgeCursor(cursor)
	m.cornerMergeSplit = nil
	best := float32(0)

	if sp := m.cornerSplitX; sp != nil {
		m.setSplitRatio(sp, computeRatioFromCursor(sp, edge))
		if over, ok := mergeOvershoot(sp, cornerDirX(m.cornerPos), edge); ok && over > best {
			best, m.cornerMergeSplit, m.cornerMergeDir = over, sp, cornerDirX(m.cornerPos)
		}
	}
	if sp := m.cornerSplitY; sp != nil {
		m.setSplitRatio(sp, computeRatioFromCursor(sp, edge))
		if over, ok := mergeOvershoot(sp, cornerDirY(m.cornerPos), edge); ok && over > best {
			best, m.cornerMergeSplit, m.cornerMergeDir = over, sp, cornerDirY(m.cornerPos)
		}
	}
	if m.cornerMergeSplit != nil {
		m.cornerMode = CornerMerge
	} else {
		m.cornerMode = CornerResize
	}
	m.Recompute(m.screenW, m.screenH)
}

// mergeOvershoot reports how far past the neighbour's far edge the cursor is.
// Only a leaf neighbour can be eaten — collapsing a whole subtree by accident
// is not worth the convenience.
func mergeOvershoot(sp *LayoutNode, dir edgeDir, cursor rl.Vector2) (float32, bool) {
	victim := sp.Children[neighbourChild(dir)]
	if victim == nil || !victim.IsLeaf() {
		return 0, false
	}
	b := sp.Bounds
	var over float32
	switch dir {
	case edgeRight:
		over = cursor.X - (b.X + b.Width)
	case edgeLeft:
		over = b.X - cursor.X
	case edgeBottom:
		over = cursor.Y - (b.Y + b.Height)
	case edgeTop:
		over = b.Y - cursor.Y
	}
	return over, over > 0
}

func (m *PanelManager) setSplitRatio(sp *LayoutNode, r float32) {
	if r != sp.Ratio {
		sp.Ratio = r
		m.cornerDirty = true
	}
}

func (m *PanelManager) restoreCornerRatios() {
	if m.cornerSplitX != nil {
		m.cornerSplitX.Ratio = m.cornerRatioX
	}
	if m.cornerSplitY != nil {
		m.cornerSplitY.Ratio = m.cornerRatioY
	}
	m.cornerDirty = false
}

func (m *PanelManager) clearCornerDrag() {
	m.cornerLeaf = nil
	m.cornerMode = CornerDragNone
	m.cornerSplitX = nil
	m.cornerSplitY = nil
	m.cornerMergeSplit = nil
	m.cornerDirty = false
}

func (m *PanelManager) CancelCornerDrag() {
	if m.cornerLeaf == nil {
		return
	}
	m.restoreCornerRatios()
	m.clearCornerDrag()
	m.Recompute(m.screenW, m.screenH)
}

// CommitCornerDrag returns true when the layout changed (caller persists).
func (m *PanelManager) CommitCornerDrag(cursor rl.Vector2) bool {
	leaf, pos, mode := m.cornerLeaf, m.cornerPos, m.cornerMode
	sp, dir, dirty := m.cornerMergeSplit, m.cornerMergeDir, m.cornerDirty
	m.clearCornerDrag()
	if leaf == nil {
		return false
	}
	switch mode {
	case CornerSplitVert, CornerSplitHoriz:
		return m.commitSplit(leaf, pos, mode, cursor)
	case CornerMerge:
		if m.eatNeighbour(sp, dir) {
			return true
		}
		return dirty
	case CornerResize:
		return dirty
	}
	return false
}

// eatNeighbour drops the leaf past dir's divider; the split collapses into
// the side the dragged pane lives on.
func (m *PanelManager) eatNeighbour(sp *LayoutNode, dir edgeDir) bool {
	if sp == nil || !sp.IsSplit() {
		return false
	}
	victim := sp.Children[neighbourChild(dir)]
	if victim == nil {
		return false
	}
	wasRoot := sp == m.Workspace
	sib := MergeIntoSibling(victim)
	if sib == nil {
		return false
	}
	if wasRoot {
		m.Workspace = sib
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

// commitSplit puts the divider under the cursor and gives the swept side to
// the new pane, which takes the first widget kind not already on screen.
func (m *PanelManager) commitSplit(leaf *LayoutNode, pos CornerPos, mode CornerDragKind, cursor rl.Vector2) bool {
	b := leaf.Bounds
	var orient SplitOrient
	var ratio float32
	originalSide := 0
	switch mode {
	case CornerSplitVert:
		if b.Width <= 0 {
			return false
		}
		orient = SplitVertical
		ratio = splitRatioAt(cursor.X-b.X, b.Width, panelMinW)
		if cornerDirX(pos) == edgeLeft {
			originalSide = 1
		}
	case CornerSplitHoriz:
		if b.Height <= 0 {
			return false
		}
		orient = SplitHorizontal
		ratio = splitRatioAt(cursor.Y-b.Y, b.Height, panelMinH)
		if cornerDirY(pos) == edgeTop {
			originalSide = 1
		}
	default:
		return false
	}

	split := SplitLeaf(leaf, orient, clamp01(ratio), originalSide)
	if split == nil {
		return false
	}
	if leaf == m.Workspace {
		m.Workspace = split
	}
	if fresh := split.Children[1-originalSide]; fresh != nil {
		if id, ok := m.firstUnusedKind(); ok {
			fresh.Panel, fresh.Title = id, WidgetTitle(id)
		}
	}
	m.Recompute(m.screenW, m.screenH)
	return true
}

// splitRatioAt clamps a divider offset so neither side drops below min.
func splitRatioAt(v, total, min float32) float32 {
	if total <= 0 {
		return 0.5
	}
	lo, hi := min, total-min
	if lo > hi {
		lo, hi = total*0.5, total*0.5
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v / total
}

// firstUnusedKind keeps a fresh pane from duplicating a widget already in the
// tree — Get(PanelID) resolves to the first matching leaf, so a duplicate
// would render empty.
func (m *PanelManager) firstUnusedKind() (PanelID, bool) {
	used := map[PanelID]bool{}
	m.Workspace.WalkLeaves(func(l *LayoutNode) { used[l.Panel] = true })
	for _, id := range WorkspacePanelKinds {
		if !used[id] {
			return id, true
		}
	}
	return PanelNone, false
}

func DrawCornerHandles(m *PanelManager, cursor rl.Vector2) {
	if m == nil || m.Workspace == nil {
		return
	}
	hot, _ := m.CornerAt(cursor)
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

// DrawCornerDragPreview: resize needs no preview (the panes move live), so
// only the split line and the merge warning are drawn.
func DrawCornerDragPreview(m *PanelManager, cursor rl.Vector2) {
	if m == nil || m.cornerLeaf == nil {
		return
	}
	b := m.cornerLeaf.Bounds
	switch m.cornerMode {
	case CornerSplitVert:
		x := b.X + b.Width*splitRatioAt(cursor.X-b.X, b.Width, panelMinW)
		rl.DrawLineEx(
			rl.Vector2{X: x, Y: b.Y + 4},
			rl.Vector2{X: x, Y: b.Y + b.Height - 4},
			2, cornerPreviewColor)
	case CornerSplitHoriz:
		y := b.Y + b.Height*splitRatioAt(cursor.Y-b.Y, b.Height, panelMinH)
		rl.DrawLineEx(
			rl.Vector2{X: b.X + 4, Y: y},
			rl.Vector2{X: b.X + b.Width - 4, Y: y},
			2, cornerPreviewColor)
	case CornerResize:
		rl.DrawRectangleLinesEx(b, 2, cornerResizeBorder)
	case CornerMerge:
		sp := m.cornerMergeSplit
		if sp == nil {
			return
		}
		victim := sp.Children[neighbourChild(m.cornerMergeDir)]
		if victim == nil {
			return
		}
		rl.DrawRectangleRec(victim.Bounds, cornerMergeOverlay)
		rl.DrawRectangleLinesEx(victim.Bounds, 2, cornerMergeBorder)
		rl.DrawRectangleLinesEx(b, 2, cornerDockBorder)
		drawMergeArrow(b, victim.Bounds)
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
