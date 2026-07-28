package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/ui"
)

// handleChrome runs the splitter / corner / chevron / floating-panel layer.
// It owns the cursor shape and claims LMB before any world handler sees it.
func (g *Game) handleChrome() {
	// Priority chain for LMB: open menu; splitter (resize one divider);
	// chevron (open menu); corner grip (resize / split); title bar (move).
	splitterHover := g.UI.PanelMgr.SplitterAt(g.Frame.Cursor)
	cornerHover, cornerPos := g.UI.PanelMgr.CornerAt(g.Frame.Cursor)
	chevronHover := chevronLeafAt(g.UI.PanelMgr, g.Frame.Cursor)
	titleHover := g.UI.PanelMgr.TitleBarAt(g.Frame.Cursor)
	switch {
	case g.UI.PanelMgr.IsDragging():
		if sp := g.UI.PanelMgr.DraggingSplitter(); sp != nil {
			if sp.Orient == ui.SplitVertical {
				rl.SetMouseCursor(rl.MouseCursorResizeEW)
			} else {
				rl.SetMouseCursor(rl.MouseCursorResizeNS)
			}
		}
	case g.UI.PanelMgr.IsCornerDragging():
		rl.SetMouseCursor(ui.CornerCursor(g.UI.PanelMgr.CornerDragPos()))
	case g.UI.PanelMgr.TitleDragActive():
		rl.SetMouseCursor(rl.MouseCursorResizeAll)
	case g.UI.Floating.IsResizing() || g.UI.Floating.ResizeHover(g.Frame.Cursor):
		if c, ok := g.UI.Floating.ResizeCursorAt(g.Frame.Cursor); ok {
			rl.SetMouseCursor(c)
		} else {
			rl.SetMouseCursor(rl.MouseCursorResizeNWSE)
		}
	case splitterHover != nil:
		if splitterHover.Orient == ui.SplitVertical {
			rl.SetMouseCursor(rl.MouseCursorResizeEW)
		} else {
			rl.SetMouseCursor(rl.MouseCursorResizeNS)
		}
	case cornerHover != nil:
		rl.SetMouseCursor(ui.CornerCursor(cornerPos))
	case chevronHover != nil:
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
	default:
		rl.SetMouseCursor(rl.MouseCursorDefault)
	}

	// Floating panels eat LMB first; returns true when consumed.
	floatingConsumed := g.UI.Floating.HandleInput(g.Frame.Cursor,
		rl.IsMouseButtonPressed(rl.MouseButtonLeft),
		rl.IsMouseButtonDown(rl.MouseButtonLeft),
		rl.IsKeyPressed(rl.KeyEscape),
		g.UI.ScreenW, g.UI.ScreenH)

	// LMB press dispatch. lmbDown is the raw press; lmbPress is the gated
	// form. Menu-open and floating-panel presses bypass workspace handlers.
	overFloating := g.UI.Floating.HitTest(g.Frame.Cursor) != nil
	g.Frame.LMBDown = rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !floatingConsumed && !overFloating
	g.Frame.LMBPress = g.Frame.LMBDown && !g.UI.PanelMgr.IsDragging() &&
		!g.UI.PanelMgr.IsCornerDragging() && !g.UI.PanelMgr.IsTitleDragging()
	if g.UI.ChevronMenu.Open && g.Frame.LMBDown {
		if idx := g.UI.ChevronMenu.HitItem(g.hudFont, g.Frame.Cursor); idx >= 0 {
			it := g.UI.ChevronMenu.Items[idx]
			if !it.Disabled {
				handleMenuItem(g.UI.PanelMgr, &g.UI.ChevronMenu, it, g.floatSpawn)
				saveLayout(g.UI.PanelMgr)
			}
			g.UI.ChevronMenu.Close()
		} else {
			g.UI.ChevronMenu.Close()
		}
		g.syncPanelRects()
	} else if g.Frame.LMBPress && splitterHover != nil {
		g.UI.PanelMgr.BeginDrag(splitterHover)
	} else if g.Frame.LMBPress && chevronHover != nil {
		isRoot := chevronHover == g.UI.PanelMgr.Workspace
		ch := ui.ChevronRect(ui.Panel{Bounds: chevronHover.Bounds})
		g.UI.ChevronMenu.OpenAt(chevronHover, ch, isRoot)
	} else if g.Frame.LMBPress && cornerHover != nil {
		g.UI.PanelMgr.BeginCornerDrag(cornerHover, cornerPos, g.Frame.Cursor)
	} else if g.Frame.LMBPress && titleHover != nil {
		g.UI.PanelMgr.BeginTitleDrag(titleHover, g.Frame.Cursor)
	}

	if g.UI.PanelMgr.IsDragging() {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			g.UI.PanelMgr.UpdateDrag(g.Frame.Cursor)
		} else if g.UI.PanelMgr.EndDrag() {
			saveLayout(g.UI.PanelMgr)
		}
		g.syncPanelRects()
	}
	if g.UI.PanelMgr.IsCornerDragging() {
		switch {
		case rl.IsKeyPressed(rl.KeyEscape):
			g.UI.PanelMgr.CancelCornerDrag()
		case rl.IsMouseButtonDown(rl.MouseButtonLeft):
			g.UI.PanelMgr.UpdateCornerDrag(g.Frame.Cursor)
		default:
			if g.UI.PanelMgr.CommitCornerDrag(g.Frame.Cursor) {
				saveLayout(g.UI.PanelMgr)
			}
		}
		g.syncPanelRects()
	}
	if g.UI.PanelMgr.IsTitleDragging() {
		switch {
		case rl.IsKeyPressed(rl.KeyEscape):
			g.UI.PanelMgr.CancelTitleDrag()
		case rl.IsMouseButtonDown(rl.MouseButtonLeft):
			g.UI.PanelMgr.UpdateTitleDrag(g.Frame.Cursor)
		default:
			if g.UI.PanelMgr.CommitTitleDrag(g.Frame.Cursor) {
				saveLayout(g.UI.PanelMgr)
			}
			g.syncPanelRects()
		}
	}
}

// syncPanelRects re-reads the two panels whose rects the render half caches
// and resizes the scene render target to match.
func (g *Game) syncPanelRects() {
	g.Frame.Panel3D = g.UI.PanelMgr.Get(ui.Panel3D)
	g.Frame.PanelMap = g.UI.PanelMgr.Get(ui.PanelMap)
	g.UI.Scene3DRT.EnsureSize(g.Frame.Panel3D)
}

// chevronLeafAt returns the workspace leaf whose chevron button is under the cursor.
func chevronLeafAt(panelMgr *ui.PanelManager, cursor rl.Vector2) *ui.LayoutNode {
	if panelMgr == nil || panelMgr.Workspace == nil {
		return nil
	}
	var hit *ui.LayoutNode
	panelMgr.Workspace.WalkLeaves(func(l *ui.LayoutNode) {
		if hit != nil {
			return
		}
		ch := ui.ChevronRect(ui.Panel{Bounds: l.Bounds})
		if cursor.X >= ch.X && cursor.X < ch.X+ch.Width &&
			cursor.Y >= ch.Y && cursor.Y < ch.Y+ch.Height {
			hit = l
		}
	})
	return hit
}

// handleMenuItem dispatches a chevron-menu selection: switch widget, close
// (merge with sibling), or detach into a floating panel via floatSpawn.
func handleMenuItem(panelMgr *ui.PanelManager, menu *ui.ChevronMenu, it ui.MenuItem,
	floatSpawn func(id ui.PanelID, title string, bounds rl.Rectangle)) {
	leaf := menu.Leaf
	if leaf == nil {
		return
	}
	switch it.Kind {
	case ui.MenuItemSwitch:
		if it.Target == leaf.Panel {
			return
		}
		// If the target widget is already shown, swap contents so each
		// PanelID appears at most once.
		if other := panelMgr.Workspace.FindLeaf(it.Target); other != nil {
			ui.SwapPanels(leaf, other)
		} else {
			leaf.Panel = it.Target
			leaf.Title = ui.WidgetTitle(it.Target)
		}
	case ui.MenuItemClose:
		if leaf.Parent == nil {
			return
		}
		wasRootChild := leaf.Parent == panelMgr.Workspace
		sib := ui.MergeIntoSibling(leaf)
		if wasRootChild && sib != nil {
			panelMgr.SetWorkspace(sib)
		}
	case ui.MenuItemFloat:
		if leaf.Parent == nil || floatSpawn == nil {
			return
		}
		// Snapshot leaf state before tearing it out of the tree.
		id := leaf.Panel
		title := leaf.Title
		bounds := leaf.Bounds
		wasRootChild := leaf.Parent == panelMgr.Workspace
		sib := ui.MergeIntoSibling(leaf)
		if wasRootChild && sib != nil {
			panelMgr.SetWorkspace(sib)
		}
		floatSpawn(id, title, bounds)
	}
	panelMgr.Recompute(int32(rl.GetScreenWidth()), int32(rl.GetScreenHeight()))
}
