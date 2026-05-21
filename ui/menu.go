package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Phase 18.C chevron popup menu: anchored to a leaf, lists swappable widget
// kinds + Close pane. Stateless other than (Open, Anchor, Leaf); main.go
// owns the lifecycle (open on chevron-click, dispatch on item-click, close
// on outside-click or ESC).

// MenuItemKind discriminates between a widget-switch item and the close
// action. Phase 18.D may add more (split-here, dock-out).
type MenuItemKind uint8

const (
	MenuItemSwitch MenuItemKind = iota
	MenuItemClose
)

// MenuItem is one row in the chevron menu.
type MenuItem struct {
	Kind     MenuItemKind
	Target   PanelID // valid for MenuItemSwitch
	Label    string
	Disabled bool
}

// ChevronMenu is the runtime state of the popup. Chevron holds the source
// chevron rect on screen; Rect() uses it to position the menu below by
// default and flip above / right-align when it would otherwise overflow the
// screen.
type ChevronMenu struct {
	Open    bool
	Leaf    *LayoutNode
	Chevron rl.Rectangle
	Items   []MenuItem
}

const (
	menuItemH      float32 = 26
	menuPad        float32 = 6
	menuFontSize   int32   = 14
	menuMinWidth   float32 = 160
	menuBorderSize float32 = 1
)

var (
	menuBG          = rl.Color{R: 26, G: 30, B: 38, A: 240}
	menuBorderColor = rl.Color{R: 60, G: 70, B: 85, A: 255}
	menuHoverBG     = rl.Color{R: 50, G: 90, B: 130, A: 240}
	menuTextColor   = rl.Color{R: 220, G: 226, B: 232, A: 255}
	menuTextDim     = rl.Color{R: 110, G: 120, B: 130, A: 255}
	menuSep         = rl.Color{R: 50, G: 55, B: 65, A: 255}
)

// OpenAt populates the menu for `leaf` and anchors it to the chevron rect.
// Items list: every WorkspacePanelKinds entry as a switch action (current
// widget shown disabled), plus Close pane (disabled when leaf is root).
func (m *ChevronMenu) OpenAt(leaf *LayoutNode, chevron rl.Rectangle, isRoot bool) {
	if leaf == nil {
		m.Open = false
		return
	}
	m.Leaf = leaf
	m.Chevron = chevron
	m.Items = m.Items[:0]
	for _, id := range WorkspacePanelKinds {
		it := MenuItem{
			Kind:   MenuItemSwitch,
			Target: id,
			Label:  WidgetTitle(id),
		}
		if id == leaf.Panel {
			it.Disabled = true
			it.Label = "• " + it.Label
		}
		m.Items = append(m.Items, it)
	}
	m.Items = append(m.Items, MenuItem{
		Kind:     MenuItemClose,
		Label:    "Close pane",
		Disabled: isRoot,
	})
	m.Open = true
}

// Close hides the menu.
func (m *ChevronMenu) Close() { m.Open = false; m.Leaf = nil }

// Rect returns the menu's screen rectangle, clamped so it stays fully on
// screen. Default: anchored below the chevron, left edge aligned with
// chevron's left edge. If that would overflow the bottom, the menu flips
// to open above the chevron. If it would overflow the right, the right
// edge aligns with the chevron's right edge instead of the left.
func (m *ChevronMenu) Rect(font rl.Font) rl.Rectangle {
	if !m.Open {
		return rl.Rectangle{}
	}
	width := menuMinWidth
	for _, it := range m.Items {
		w := rl.MeasureTextEx(font, it.Label, float32(menuFontSize), 1.0).X + menuPad*2 + 12
		if w > width {
			width = w
		}
	}
	height := menuItemH*float32(len(m.Items)) + menuPad*2 + 2
	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())

	// Horizontal: left-align to chevron by default, flip to right-align if
	// it would overflow. Final clamp keeps the menu on screen even when
	// the chevron itself sits near an edge.
	x := m.Chevron.X
	if x+width > screenW {
		x = m.Chevron.X + m.Chevron.Width - width
	}
	if x+width > screenW {
		x = screenW - width - 2
	}
	if x < 2 {
		x = 2
	}

	// Vertical: below chevron by default; flip above if would overflow.
	y := m.Chevron.Y + m.Chevron.Height
	if y+height > screenH {
		y = m.Chevron.Y - height
	}
	if y < 2 {
		y = 2
	}
	if y+height > screenH {
		y = screenH - height - 2
	}
	return rl.Rectangle{X: x, Y: y, Width: width, Height: height}
}

// HitItem returns the menu item under `cursor` (-1 if none).
func (m *ChevronMenu) HitItem(font rl.Font, cursor rl.Vector2) int {
	if !m.Open {
		return -1
	}
	r := m.Rect(font)
	if !pointInRect(cursor, r) {
		return -1
	}
	yRel := cursor.Y - (r.Y + menuPad)
	idx := int(yRel / menuItemH)
	if idx < 0 || idx >= len(m.Items) {
		return -1
	}
	return idx
}

// Draw paints the menu under its anchor. Caller draws after every other
// frame element so the menu sits on top of all chrome.
func (m *ChevronMenu) Draw(font rl.Font, cursor rl.Vector2) {
	if !m.Open {
		return
	}
	r := m.Rect(font)
	rl.DrawRectangleRec(r, menuBG)
	rl.DrawRectangleLinesEx(r, menuBorderSize, menuBorderColor)
	hovered := m.HitItem(font, cursor)
	for i, it := range m.Items {
		rowY := r.Y + menuPad + float32(i)*menuItemH
		rowRect := rl.Rectangle{X: r.X + 1, Y: rowY, Width: r.Width - 2, Height: menuItemH}
		// Separator between switch items and Close pane.
		if it.Kind == MenuItemClose {
			sepY := rowY - 1
			rl.DrawLine(int32(r.X+menuPad), int32(sepY), int32(r.X+r.Width-menuPad), int32(sepY), menuSep)
		}
		color := menuTextColor
		if it.Disabled {
			color = menuTextDim
		} else if i == hovered {
			rl.DrawRectangleRec(rowRect, menuHoverBG)
		}
		rl.DrawTextEx(font, it.Label,
			rl.Vector2{X: rowRect.X + menuPad, Y: rowRect.Y + (menuItemH-float32(menuFontSize))*0.5},
			float32(menuFontSize), 1.0, color)
	}
}
