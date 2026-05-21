package ui

import rl "github.com/gen2brain/raylib-go/raylib"

// Phase 18.C tree-of-splits. The workspace below the TopBar is a recursive
// binary tree: each node is either a Leaf (one PanelID = one widget) or a
// Split (two children stacked horizontally or vertically with a ratio).
//
// Splitter drags mutate Split.Ratio. Corner-drag on a Leaf wraps it in a
// new Split and inserts a duplicate Leaf next to it. The chevron menu on
// a Leaf can swap its Panel with another Leaf in the tree (forbidding two
// instances of the same PanelID) or merge the Leaf into its sibling.

// NodeKind discriminates Leaf vs Split.
type NodeKind uint8

const (
	NodeLeaf NodeKind = iota
	NodeSplit
)

// SplitOrient picks the divider axis. Vertical = children stacked
// horizontally (left/right), Horizontal = children stacked vertically
// (top/bottom). Naming follows the divider line, not the child arrangement.
type SplitOrient uint8

const (
	SplitVertical   SplitOrient = iota // divider is a vertical line; children laid out left/right
	SplitHorizontal                    // divider is a horizontal line; children laid out top/bottom
)

// MinSplitRatio / MaxSplitRatio clamp split ratios so panels don't shrink
// below ~panelMinW/H equivalent (sized at recompute against the leaf rect).
const (
	MinSplitRatio float32 = 0.05
	MaxSplitRatio float32 = 0.95
)

// LayoutNode is the recursive workspace cell. Parent is back-pointer for
// merge / find-sibling. Bounds is filled by Recompute every frame.
type LayoutNode struct {
	Kind     NodeKind
	Panel    PanelID
	Title    string
	Bounds   rl.Rectangle
	Orient   SplitOrient
	Ratio    float32
	Children [2]*LayoutNode
	Parent   *LayoutNode
}

// NewLeaf constructs a leaf showing the given widget.
func NewLeaf(id PanelID, title string) *LayoutNode {
	return &LayoutNode{Kind: NodeLeaf, Panel: id, Title: title}
}

// NewSplit constructs a split with two children. ratio is the first child's
// share. children Parents are wired automatically.
func NewSplit(o SplitOrient, ratio float32, a, b *LayoutNode) *LayoutNode {
	n := &LayoutNode{Kind: NodeSplit, Orient: o, Ratio: ratio, Children: [2]*LayoutNode{a, b}}
	if a != nil {
		a.Parent = n
	}
	if b != nil {
		b.Parent = n
	}
	return n
}

// IsLeaf is the Leaf check.
func (n *LayoutNode) IsLeaf() bool { return n != nil && n.Kind == NodeLeaf }

// IsSplit is the Split check.
func (n *LayoutNode) IsSplit() bool { return n != nil && n.Kind == NodeSplit }

// Compute walks the tree DFS and assigns Bounds to every node. Caller
// passes the rect to fill (i.e. the workspace area below the top bar).
func (n *LayoutNode) Compute(rect rl.Rectangle) {
	if n == nil {
		return
	}
	n.Bounds = rect
	if n.Kind != NodeSplit {
		return
	}
	r := n.Ratio
	if r < MinSplitRatio {
		r = MinSplitRatio
	}
	if r > MaxSplitRatio {
		r = MaxSplitRatio
	}
	switch n.Orient {
	case SplitVertical:
		firstW := rect.Width * r
		a := rl.Rectangle{X: rect.X, Y: rect.Y, Width: firstW, Height: rect.Height}
		b := rl.Rectangle{X: rect.X + firstW, Y: rect.Y, Width: rect.Width - firstW, Height: rect.Height}
		n.Children[0].Compute(a)
		n.Children[1].Compute(b)
	case SplitHorizontal:
		firstH := rect.Height * r
		a := rl.Rectangle{X: rect.X, Y: rect.Y, Width: rect.Width, Height: firstH}
		b := rl.Rectangle{X: rect.X, Y: rect.Y + firstH, Width: rect.Width, Height: rect.Height - firstH}
		n.Children[0].Compute(a)
		n.Children[1].Compute(b)
	}
}

// WalkLeaves visits every leaf in subtree order.
func (n *LayoutNode) WalkLeaves(fn func(leaf *LayoutNode)) {
	if n == nil {
		return
	}
	if n.IsLeaf() {
		fn(n)
		return
	}
	n.Children[0].WalkLeaves(fn)
	n.Children[1].WalkLeaves(fn)
}

// WalkSplits visits every split in subtree order.
func (n *LayoutNode) WalkSplits(fn func(split *LayoutNode)) {
	if n == nil {
		return
	}
	if n.IsSplit() {
		fn(n)
		n.Children[0].WalkSplits(fn)
		n.Children[1].WalkSplits(fn)
	}
}

// FindLeaf returns the first Leaf in BFS order whose Panel == id, or nil.
func (n *LayoutNode) FindLeaf(id PanelID) *LayoutNode {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.Panel == id {
			return n
		}
		return nil
	}
	if a := n.Children[0].FindLeaf(id); a != nil {
		return a
	}
	return n.Children[1].FindLeaf(id)
}

// LeafAt returns the first leaf whose Bounds contain `p`, or nil. Iterates
// in reverse Z order (later leaves win) — for now there's no Z, so first
// match is fine.
func (n *LayoutNode) LeafAt(p rl.Vector2) *LayoutNode {
	var hit *LayoutNode
	n.WalkLeaves(func(l *LayoutNode) {
		if hit != nil {
			return
		}
		if pointInRect(p, l.Bounds) {
			hit = l
		}
	})
	return hit
}

// SplitLeaf wraps `leaf` in a new Split node. The original leaf goes to
// one side (originalSide), a duplicate (same PanelID) to the other. Caller
// chooses orientation + ratio. Tree root pointer may need to be updated:
// returns the new Split node, which replaces `leaf` in its parent.
func SplitLeaf(leaf *LayoutNode, orient SplitOrient, ratio float32, originalSide int) *LayoutNode {
	if leaf == nil || !leaf.IsLeaf() {
		return nil
	}
	if originalSide < 0 || originalSide > 1 {
		originalSide = 0
	}
	// Snapshot the old parent BEFORE NewSplit reparents `leaf` (otherwise
	// reading leaf.Parent below sees the new split → self-referential
	// Children[0] = split → infinite Compute recursion).
	oldParent := leaf.Parent
	dup := NewLeaf(leaf.Panel, leaf.Title)
	var a, b *LayoutNode
	if originalSide == 0 {
		a, b = leaf, dup
	} else {
		a, b = dup, leaf
	}
	split := NewSplit(orient, ratio, a, b)
	split.Parent = oldParent
	if oldParent != nil {
		if oldParent.Children[0] == leaf {
			oldParent.Children[0] = split
		} else {
			oldParent.Children[1] = split
		}
	}
	// NewSplit already set leaf.Parent = split via its loop, so no extra
	// reparenting is needed here.
	return split
}

// Sibling returns the other child of leaf.Parent (or nil if leaf is root).
func (n *LayoutNode) Sibling() *LayoutNode {
	if n == nil || n.Parent == nil {
		return nil
	}
	if n.Parent.Children[0] == n {
		return n.Parent.Children[1]
	}
	return n.Parent.Children[0]
}

// MergeIntoSibling collapses `leaf` into its sibling: the parent split is
// replaced by the sibling subtree. Returns the new root for the subtree
// previously rooted at leaf.Parent. Caller must update the workspace root
// pointer if leaf.Parent was the root.
func MergeIntoSibling(leaf *LayoutNode) *LayoutNode {
	if leaf == nil || leaf.Parent == nil {
		return leaf
	}
	parent := leaf.Parent
	sib := leaf.Sibling()
	if sib == nil {
		return leaf
	}
	sib.Parent = parent.Parent
	if parent.Parent != nil {
		if parent.Parent.Children[0] == parent {
			parent.Parent.Children[0] = sib
		} else {
			parent.Parent.Children[1] = sib
		}
	}
	return sib
}

// SwapPanels swaps the PanelID + Title between two leaves. Used when the
// chevron menu picks a target that's already shown elsewhere.
func SwapPanels(a, b *LayoutNode) {
	if a == nil || b == nil || !a.IsLeaf() || !b.IsLeaf() {
		return
	}
	a.Panel, b.Panel = b.Panel, a.Panel
	a.Title, b.Title = b.Title, a.Title
}

// DockSide picks which edge of a target leaf a dropped source will dock at.
type DockSide uint8

const (
	DockNone DockSide = iota
	DockLeft
	DockRight
	DockTop
	DockBottom
)

// DockSideFor classifies which edge of `target` the cursor is closest to,
// using normalized X / Y diamond regions. Returns DockNone for a degenerate
// target.
func DockSideFor(target *LayoutNode, cursor rl.Vector2) DockSide {
	if target == nil {
		return DockNone
	}
	b := target.Bounds
	if b.Width <= 0 || b.Height <= 0 {
		return DockNone
	}
	rx := (cursor.X - b.X) / b.Width
	ry := (cursor.Y - b.Y) / b.Height
	dxFromEdge := rx
	if 1-rx < dxFromEdge {
		dxFromEdge = 1 - rx
	}
	dyFromEdge := ry
	if 1-ry < dyFromEdge {
		dyFromEdge = 1 - ry
	}
	if dxFromEdge < dyFromEdge {
		if rx < 0.5 {
			return DockLeft
		}
		return DockRight
	}
	if ry < 0.5 {
		return DockTop
	}
	return DockBottom
}

// DockWrapBounds returns the rect of the subtree that DockNear will wrap
// when docking `source` at `target` on `side`. Equal to target.Parent.Bounds
// when target has a parent; falls back to target.Bounds when target is root.
// Used by the drag preview to highlight what area will be reorganised.
func DockWrapBounds(target *LayoutNode) rl.Rectangle {
	if target == nil {
		return rl.Rectangle{}
	}
	if target.Parent != nil {
		return target.Parent.Bounds
	}
	return target.Bounds
}

// DockHighlightRect returns the rect inside DockWrapBounds where the source
// will land. Used by the drag preview.
func DockHighlightRect(target *LayoutNode, side DockSide) rl.Rectangle {
	b := DockWrapBounds(target)
	if b.Width <= 0 || b.Height <= 0 {
		return rl.Rectangle{}
	}
	switch side {
	case DockLeft:
		return rl.Rectangle{X: b.X, Y: b.Y, Width: b.Width * 0.5, Height: b.Height}
	case DockRight:
		return rl.Rectangle{X: b.X + b.Width*0.5, Y: b.Y, Width: b.Width * 0.5, Height: b.Height}
	case DockTop:
		return rl.Rectangle{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height * 0.5}
	case DockBottom:
		return rl.Rectangle{X: b.X, Y: b.Y + b.Height*0.5, Width: b.Width, Height: b.Height * 0.5}
	}
	return rl.Rectangle{}
}

// DockNear restructures the tree so `source` becomes a strip on `side` of
// the subtree containing `target`. The wrap point is target.Parent (so the
// new strip spans the full "level" the target lives in); if target is the
// root, wrap target itself. Returns the new Split node (which replaces the
// wrap point in its old parent). Caller must ensure `source` has already
// been detached from the tree before calling.
func DockNear(source, target *LayoutNode, side DockSide) *LayoutNode {
	if source == nil || target == nil || side == DockNone {
		return nil
	}
	wrap := target.Parent
	if wrap == nil {
		wrap = target
	}
	oldParent := wrap.Parent

	var newSplit *LayoutNode
	switch side {
	case DockLeft:
		newSplit = NewSplit(SplitVertical, 0.5, source, wrap)
	case DockRight:
		newSplit = NewSplit(SplitVertical, 0.5, wrap, source)
	case DockTop:
		newSplit = NewSplit(SplitHorizontal, 0.5, source, wrap)
	case DockBottom:
		newSplit = NewSplit(SplitHorizontal, 0.5, wrap, source)
	default:
		return nil
	}
	newSplit.Parent = oldParent
	if oldParent != nil {
		if oldParent.Children[0] == wrap {
			oldParent.Children[0] = newSplit
		} else {
			oldParent.Children[1] = newSplit
		}
	}
	// NewSplit already wired source.Parent and wrap.Parent to newSplit.
	return newSplit
}
