package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

type MenuItemKind uint8

const (
	MenuItemSwitch MenuItemKind = iota
	MenuItemClose
	// MenuItemFloat detaches the widget into a floating panel and
	// removes the leaf; the host wires the spawn, menu.go signals intent.
	MenuItemFloat
)

type MenuItem struct {
	Kind     MenuItemKind
	Target   PanelID // valid for MenuItemSwitch
	Label    string
	Disabled bool
}

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

// OpenAt populates Items with every WorkspacePanelKinds entry (current
// widget disabled) plus Close pane (disabled when leaf is root).
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
	// Float pane is disabled for the root leaf (no sibling to merge into)
	// and for Panel3D (the scene render texture is sized to the workspace
	// leaf — detaching would need a second RT, deferred work).
	m.Items = append(m.Items, MenuItem{
		Kind:     MenuItemFloat,
		Label:    "Float pane",
		Disabled: isRoot || leaf.Panel == Panel3D,
	})
	m.Items = append(m.Items, MenuItem{
		Kind:     MenuItemClose,
		Label:    "Close pane",
		Disabled: isRoot,
	})
	m.Open = true
}

func (m *ChevronMenu) Close() { m.Open = false; m.Leaf = nil }

// Rect anchors below the chevron, flipping above on bottom overflow and
// right-aligning on right overflow.
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

// Draw must run after all other UI so the menu sits on top of all chrome.
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
		if it.Kind == MenuItemFloat {
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
