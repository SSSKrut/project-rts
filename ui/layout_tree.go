package ui

import rl "github.com/gen2brain/raylib-go/raylib"

type NodeKind uint8

const (
	NodeLeaf NodeKind = iota
	NodeSplit
)

// SplitOrient names the divider line, not the child arrangement.
// Vertical divider line = children stacked left/right.
type SplitOrient uint8

const (
	SplitVertical   SplitOrient = iota // divider is a vertical line; children laid out left/right
	SplitHorizontal                    // divider is a horizontal line; children laid out top/bottom
)

const (
	MinSplitRatio float32 = 0.05
	MaxSplitRatio float32 = 0.95
)

// Bounds is filled by Recompute every frame.
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

func NewLeaf(id PanelID, title string) *LayoutNode {
	return &LayoutNode{Kind: NodeLeaf, Panel: id, Title: title}
}

// NewSplit wires children's Parents to the new split.
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

func (n *LayoutNode) IsLeaf() bool  { return n != nil && n.Kind == NodeLeaf }
func (n *LayoutNode) IsSplit() bool { return n != nil && n.Kind == NodeSplit }

// Compute walks DFS and assigns Bounds to every node.
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

// SplitLeaf wraps `leaf` in a new Split with a duplicate Leaf on the
// opposite side. Caller updates the tree root pointer to the returned
// Split if leaf was the root.
func SplitLeaf(leaf *LayoutNode, orient SplitOrient, ratio float32, originalSide int) *LayoutNode {
	if leaf == nil || !leaf.IsLeaf() {
		return nil
	}
	if originalSide < 0 || originalSide > 1 {
		originalSide = 0
	}
	// Snapshot oldParent BEFORE NewSplit reparents leaf: otherwise
	// leaf.Parent points at the new split, becoming self-referential
	// and infinitely recursing Compute.
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
	return split
}

func (n *LayoutNode) Sibling() *LayoutNode {
	if n == nil || n.Parent == nil {
		return nil
	}
	if n.Parent.Children[0] == n {
		return n.Parent.Children[1]
	}
	return n.Parent.Children[0]
}

// MergeIntoSibling replaces leaf.Parent with the sibling subtree.
// Caller must update the workspace root pointer if leaf.Parent was root.
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

func SwapPanels(a, b *LayoutNode) {
	if a == nil || b == nil || !a.IsLeaf() || !b.IsLeaf() {
		return
	}
	a.Panel, b.Panel = b.Panel, a.Panel
	a.Title, b.Title = b.Title, a.Title
}

type DockSide uint8

const (
	DockNone DockSide = iota
	DockLeft
	DockRight
	DockTop
	DockBottom
)

// DockSideFor uses normalized X / Y diamond regions.
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

// DockWrapBounds returns the rect of the subtree DockNear will wrap. Used
// by the drag preview to highlight what area will be reorganised.
func DockWrapBounds(target *LayoutNode) rl.Rectangle {
	if target == nil {
		return rl.Rectangle{}
	}
	if target.Parent != nil {
		return target.Parent.Bounds
	}
	return target.Bounds
}

// DockHighlightRect returns the rect inside DockWrapBounds where source
// will land.
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

// DockNear wraps target.Parent (or target if root) in a new Split with
// `source` on `side`. Caller must detach source from the tree first.
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
	return newSplit
}
