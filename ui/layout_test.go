package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Default preset at 1600x900: Field 0..1200 | (Inspector / Map) 1200..1600,
// Timeline across the bottom.
func testMgr() *PanelManager {
	m := NewPanelManager()
	m.Recompute(1600, 900)
	return m
}

func leafOrder(n *LayoutNode) []PanelID {
	var out []PanelID
	n.WalkLeaves(func(l *LayoutNode) { out = append(out, l.Panel) })
	return out
}

func sameOrder(a, b []PanelID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func gripStart(leaf *LayoutNode, pos CornerPos) rl.Vector2 {
	r := CornerHandleRect(leaf, pos)
	return rl.Vector2{X: r.X + r.Width*0.5, Y: r.Y + r.Height*0.5}
}

func dragCorner(m *PanelManager, leaf *LayoutNode, pos CornerPos, dx, dy float32) {
	start := gripStart(leaf, pos)
	end := rl.Vector2{X: start.X + dx, Y: start.Y + dy}
	m.BeginCornerDrag(leaf, pos, start)
	m.UpdateCornerDrag(end)
	m.CommitCornerDrag(end)
}

// Pulling a grip outward grows the pane, shrinks the neighbour, and leaves
// the pane order alone — the swap this replaced was the bug.
func TestCornerResizeGrowsPaneWithoutReorder(t *testing.T) {
	m := testMgr()
	before := leafOrder(m.Workspace)
	field := m.Workspace.FindLeaf(Panel3D)

	dragCorner(m, field, CornerBR, 200, 0)

	field = m.Workspace.FindLeaf(Panel3D)
	insp := m.Workspace.FindLeaf(PanelInspect)
	if got := field.Bounds.Width; got < 1399 || got > 1401 {
		t.Errorf("field width = %.1f, want ~1400", got)
	}
	if field.Bounds.X != 0 {
		t.Errorf("field moved to x=%.1f, want 0", field.Bounds.X)
	}
	if got := insp.Bounds.Width; got < 199 || got > 201 {
		t.Errorf("neighbour width = %.1f, want ~200", got)
	}
	if after := leafOrder(m.Workspace); !sameOrder(before, after) {
		t.Errorf("pane order changed: %v -> %v", before, after)
	}
}

// Pushing a neighbour that is a whole subtree can only squeeze it to the
// minimum — no accidental collapse of several panes.
func TestCornerResizeClampsAtMinimum(t *testing.T) {
	m := testMgr()
	before := leafOrder(m.Workspace)
	field := m.Workspace.FindLeaf(Panel3D)

	dragCorner(m, field, CornerBR, 5000, 0)

	insp := m.Workspace.FindLeaf(PanelInspect)
	if insp == nil {
		t.Fatal("neighbour subtree was eaten, want clamp")
	}
	if got := insp.Bounds.Width; got < panelMinW-1 || got > panelMinW+1 {
		t.Errorf("neighbour width = %.1f, want ~%.0f", got, panelMinW)
	}
	if after := leafOrder(m.Workspace); !sameOrder(before, after) {
		t.Errorf("pane order changed: %v -> %v", before, after)
	}
}

// Dragging past a leaf neighbour's far edge eats THAT neighbour; the dragged
// pane survives and takes the space.
func TestCornerMergeEatsNeighbourNotSource(t *testing.T) {
	m := testMgr()
	insp := m.Workspace.FindLeaf(PanelInspect)
	col := insp.Parent.Bounds

	dragCorner(m, insp, CornerBR, 0, col.Height+40)

	if m.Workspace.FindLeaf(PanelMap) != nil {
		t.Error("Map (the neighbour dragged over) is still in the tree")
	}
	kept := m.Workspace.FindLeaf(PanelInspect)
	if kept == nil {
		t.Fatal("Inspector (the dragged pane) was removed")
	}
	if got := kept.Bounds.Height; got < col.Height-1 || got > col.Height+1 {
		t.Errorf("Inspector height = %.1f, want %.1f", got, col.Height)
	}
}

// A drag returning to the grab point leaves the layout untouched.
func TestCornerDragBackToOriginRestores(t *testing.T) {
	m := testMgr()
	field := m.Workspace.FindLeaf(Panel3D)
	split := field.Parent
	before := split.Ratio

	start := gripStart(field, CornerBR)
	m.BeginCornerDrag(field, CornerBR, start)
	m.UpdateCornerDrag(rl.Vector2{X: start.X + 200, Y: start.Y})
	m.UpdateCornerDrag(start)
	if m.CommitCornerDrag(start) {
		t.Error("commit reported a change after returning to origin")
	}
	if split.Ratio != before {
		t.Errorf("ratio = %.3f, want %.3f", split.Ratio, before)
	}
}

func TestCornerDragCancelRestores(t *testing.T) {
	m := testMgr()
	field := m.Workspace.FindLeaf(Panel3D)
	split := field.Parent
	before := split.Ratio

	start := gripStart(field, CornerBR)
	m.BeginCornerDrag(field, CornerBR, start)
	m.UpdateCornerDrag(rl.Vector2{X: start.X + 300, Y: start.Y})
	m.CancelCornerDrag()

	if split.Ratio != before {
		t.Errorf("ratio = %.3f, want %.3f", split.Ratio, before)
	}
}

// Inward drag splits at the cursor: the divider lands where the preview drew
// it and the original keeps the side it was dragged from.
func TestCornerSplitLandsUnderCursor(t *testing.T) {
	m := testMgr()
	field := m.Workspace.FindLeaf(Panel3D)
	start := gripStart(field, CornerBR)
	end := rl.Vector2{X: 500, Y: start.Y}

	m.BeginCornerDrag(field, CornerBR, start)
	m.UpdateCornerDrag(end)
	if kind := m.CornerDragKindNow(); kind != CornerSplitVert {
		t.Fatalf("mode = %d, want CornerSplitVert", kind)
	}
	if !m.CommitCornerDrag(end) {
		t.Fatal("split not committed")
	}

	kept := m.Workspace.FindLeaf(Panel3D)
	if kept.Bounds.X != 0 {
		t.Errorf("original pane moved to x=%.1f, want 0", kept.Bounds.X)
	}
	if got := kept.Bounds.X + kept.Bounds.Width; got < 499 || got > 501 {
		t.Errorf("divider at x=%.1f, want ~500 (cursor)", got)
	}
	fresh := kept.Parent.Children[1]
	if fresh.Panel == Panel3D {
		t.Error("new pane duplicates the widget kind; Get() would render it empty")
	}
	if fresh.Bounds.X < 499 || fresh.Bounds.X > 501 {
		t.Errorf("new pane starts at x=%.1f, want ~500", fresh.Bounds.X)
	}
}

// Title-bar drag is the move gesture: the pane leaves its slot and takes half
// of the drop target, nothing else is reshuffled.
func TestTitleDragMovesPaneIntoTargetHalf(t *testing.T) {
	m := testMgr()
	mp := m.Workspace.FindLeaf(PanelMap)
	field := m.Workspace.FindLeaf(Panel3D)
	fieldBounds := field.Bounds

	start := rl.Vector2{X: PanelTitleRect(mp.Bounds).X + 40, Y: PanelTitleRect(mp.Bounds).Y + 8}
	drop := rl.Vector2{X: fieldBounds.X + 20, Y: fieldBounds.Y + fieldBounds.Height*0.5}
	m.BeginTitleDrag(mp, start)
	m.UpdateTitleDrag(drop)
	if !m.CommitTitleDrag(drop) {
		t.Fatal("title drag not committed")
	}

	mp = m.Workspace.FindLeaf(PanelMap)
	field = m.Workspace.FindLeaf(Panel3D)
	if mp == nil || field == nil {
		t.Fatal("panes lost after move")
	}
	if mp.Bounds.X != fieldBounds.X {
		t.Errorf("Map x=%.1f, want %.1f (left half of Field's old slot)", mp.Bounds.X, fieldBounds.X)
	}
	if field.Bounds.X <= mp.Bounds.X {
		t.Errorf("Field x=%.1f should sit right of Map x=%.1f", field.Bounds.X, mp.Bounds.X)
	}
	// The vacated slot goes to the Inspector, which now owns the whole column.
	insp := m.Workspace.FindLeaf(PanelInspect)
	if insp.Bounds.Height < 600 {
		t.Errorf("Inspector height = %.1f, want the full right column", insp.Bounds.Height)
	}
}

// Dropping onto a direct sibling reorders the pair instead of destroying one.
func TestTitleDragOntoSiblingReorders(t *testing.T) {
	m := testMgr()
	insp := m.Workspace.FindLeaf(PanelInspect)
	mp := m.Workspace.FindLeaf(PanelMap)

	start := rl.Vector2{X: PanelTitleRect(insp.Bounds).X + 40, Y: PanelTitleRect(insp.Bounds).Y + 8}
	drop := rl.Vector2{X: mp.Bounds.X + mp.Bounds.Width*0.5, Y: mp.Bounds.Y + mp.Bounds.Height - 20}
	m.BeginTitleDrag(insp, start)
	m.UpdateTitleDrag(drop)
	if !m.CommitTitleDrag(drop) {
		t.Fatal("title drag not committed")
	}

	insp = m.Workspace.FindLeaf(PanelInspect)
	mp = m.Workspace.FindLeaf(PanelMap)
	if insp == nil || mp == nil {
		t.Fatal("a pane was lost during a move")
	}
	if insp.Bounds.Y <= mp.Bounds.Y {
		t.Errorf("Inspector y=%.1f should now sit below Map y=%.1f", insp.Bounds.Y, mp.Bounds.Y)
	}
}

func TestTitleClickWithoutMotionIsNoop(t *testing.T) {
	m := testMgr()
	before := leafOrder(m.Workspace)
	mp := m.Workspace.FindLeaf(PanelMap)

	start := rl.Vector2{X: PanelTitleRect(mp.Bounds).X + 40, Y: PanelTitleRect(mp.Bounds).Y + 8}
	m.BeginTitleDrag(mp, start)
	m.UpdateTitleDrag(rl.Vector2{X: start.X + 2, Y: start.Y + 1})
	if m.CommitTitleDrag(rl.Vector2{X: start.X + 2, Y: start.Y + 1}) {
		t.Error("a click on the title bar changed the layout")
	}
	if after := leafOrder(m.Workspace); !sameOrder(before, after) {
		t.Errorf("pane order changed: %v -> %v", before, after)
	}
}

// The divider drag keeps working and stays order-preserving.
func TestSplitterDragResizesInPlace(t *testing.T) {
	m := testMgr()
	before := leafOrder(m.Workspace)
	field := m.Workspace.FindLeaf(Panel3D)
	sp := m.SplitterAt(rl.Vector2{X: field.Bounds.X + field.Bounds.Width, Y: 400})
	if sp == nil {
		t.Fatal("no splitter under the divider")
	}
	m.BeginDrag(sp)
	m.UpdateDrag(rl.Vector2{X: 900, Y: 400})
	if !m.EndDrag() {
		t.Fatal("splitter drag reported no change")
	}
	if got := m.Workspace.FindLeaf(Panel3D).Bounds.Width; got < 899 || got > 901 {
		t.Errorf("field width = %.1f, want ~900", got)
	}
	if after := leafOrder(m.Workspace); !sameOrder(before, after) {
		t.Errorf("pane order changed: %v -> %v", before, after)
	}
}
