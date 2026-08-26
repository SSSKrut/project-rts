package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

type ContextMenu struct {
	Active      bool
	Origin      rl.Vector2
	PopupRect   rl.Rectangle
	Sections    []ContextMenuSection
	HoveredItem int // index into the flat item list, -1 if no hover
	SourcePanel PanelID
}

// Empty Header = no header strip.
type ContextMenuSection struct {
	Header string
	Items  []ContextMenuItem
}

type ContextMenuItem struct {
	Label   string
	Tooltip string
	Glyph   rune
	Kind    components.OrderKindCode
	Enabled bool
	// LevelEntity is set for "Occupy L*N*" items so the caller can target the
	// specific Floor entity. Zero = not a floor-picker.
	LevelEntity ecs.Entity
	// HoldFireCrouchPreset is the "Hidden position" flag — caller attaches
	// MovementProfile{Stance: Crouch, ...} + HoldFire RoE override.
	HoldFireCrouchPreset bool
	// MovePreset overrides the squad's standing movement profile for the
	// duration of THIS order only. PresetDefault (zero) leaves the standing
	// rule alone, so an item that says nothing about tempo changes nothing.
	MovePreset components.MovementPreset
	// Tag is a caller-defined opaque action code. Lets the same widget host
	// the building popup, the contact popup, and any future variants without
	// each one needing its own enum field. Zero = not set.
	Tag uint16
	// ContactSpec carries the SymbolSpec a contact-popup apply-preset row
	// commits when clicked. Only meaningful when Tag tags this item as
	// "apply preset" (caller-defined).
	ContactSpec components.SymbolSpec
}

type ContextMenuResult struct {
	Cancelled bool
	Committed bool
	Item      ContextMenuItem
}

const (
	ctxContextMenuItemH      float32 = 30
	ctxMenuHeaderH           float32 = 20
	ctxContextMenuItemPadX   float32 = 10
	ctxMenuGlyphCellW        float32 = 26
	ctxMenuPopupWidth        float32 = 230
	ctxContextMenuSectionGap float32 = 4
	ctxMenuPopupOffset       float32 = 14
	ctxMenuTooltipPad        float32 = 6
	ctxMenuTooltipMaxW       float32 = 280
)

// Begin opens the popup near `origin`, auto-flipping if it would spill past
// `bounds`.
func (m *ContextMenu) Begin(origin rl.Vector2, sections []ContextMenuSection, source PanelID, bounds rl.Rectangle) {
	m.Origin = origin
	m.Sections = sections
	m.SourcePanel = source
	m.Active = true
	m.HoveredItem = -1
	m.PopupRect = m.layout(origin, bounds)
}

func (m *ContextMenu) Reset() {
	m.Active = false
	m.HoveredItem = -1
	m.Sections = nil
	m.SourcePanel = PanelNone
}

func (m *ContextMenu) IsActive() bool { return m.Active }

func (m *ContextMenu) Tick(cursor rl.Vector2, lmbPressed, escPressed bool) ContextMenuResult {
	if !m.Active {
		return ContextMenuResult{}
	}
	m.HoveredItem = m.itemAt(cursor)
	if escPressed {
		m.Reset()
		return ContextMenuResult{Cancelled: true}
	}
	if lmbPressed {
		if m.HoveredItem >= 0 {
			flat := m.flatItems()
			item := flat[m.HoveredItem]
			m.Reset()
			if item.Enabled {
				return ContextMenuResult{Committed: true, Item: item}
			}
			return ContextMenuResult{Cancelled: true}
		}
		m.Reset()
		return ContextMenuResult{Cancelled: true}
	}
	return ContextMenuResult{}
}

// HoveredLevel returns zero for no hover or a non-level item.
func (m *ContextMenu) HoveredLevel() ecs.Entity {
	if !m.Active || m.HoveredItem < 0 {
		return ecs.Entity{}
	}
	flat := m.flatItems()
	if m.HoveredItem >= len(flat) {
		return ecs.Entity{}
	}
	return flat[m.HoveredItem].LevelEntity
}

func (m *ContextMenu) HoveredItemDetails() (ContextMenuItem, bool) {
	if !m.Active || m.HoveredItem < 0 {
		return ContextMenuItem{}, false
	}
	flat := m.flatItems()
	if m.HoveredItem >= len(flat) {
		return ContextMenuItem{}, false
	}
	return flat[m.HoveredItem], true
}

func (m *ContextMenu) HoveredKind() (components.OrderKindCode, bool) {
	if !m.Active || m.HoveredItem < 0 {
		return 0, false
	}
	flat := m.flatItems()
	if m.HoveredItem >= len(flat) {
		return 0, false
	}
	return flat[m.HoveredItem].Kind, true
}

func (m *ContextMenu) flatItems() []ContextMenuItem {
	var n int
	for _, sec := range m.Sections {
		n += len(sec.Items)
	}
	flat := make([]ContextMenuItem, 0, n)
	for _, sec := range m.Sections {
		flat = append(flat, sec.Items...)
	}
	return flat
}

func (m *ContextMenu) itemAt(cursor rl.Vector2) int {
	if !pointInRect(cursor, m.PopupRect) {
		return -1
	}
	y := m.PopupRect.Y
	idx := 0
	for _, sec := range m.Sections {
		if sec.Header != "" {
			y += ctxMenuHeaderH
		}
		for range sec.Items {
			r := rl.Rectangle{
				X:      m.PopupRect.X,
				Y:      y,
				Width:  m.PopupRect.Width,
				Height: ctxContextMenuItemH,
			}
			if pointInRect(cursor, r) {
				return idx
			}
			y += ctxContextMenuItemH
			idx++
		}
		y += ctxContextMenuSectionGap
	}
	return -1
}

func (m *ContextMenu) layout(origin rl.Vector2, bounds rl.Rectangle) rl.Rectangle {
	h := float32(0)
	for _, sec := range m.Sections {
		if sec.Header != "" {
			h += ctxMenuHeaderH
		}
		h += ctxContextMenuItemH * float32(len(sec.Items))
		h += ctxContextMenuSectionGap
	}
	w := ctxMenuPopupWidth
	x := origin.X + ctxMenuPopupOffset
	y := origin.Y + ctxMenuPopupOffset
	if x+w > bounds.X+bounds.Width {
		x = origin.X - w - ctxMenuPopupOffset
	}
	if x < bounds.X+2 {
		x = bounds.X + 2
	}
	if y+h > bounds.Y+bounds.Height {
		y = bounds.Y + bounds.Height - h - 2
	}
	if y < bounds.Y+2 {
		y = bounds.Y + 2
	}
	return rl.Rectangle{X: x, Y: y, Width: w, Height: h}
}

var (
	ctxMenuBG                 = rl.Color{R: 18, G: 22, B: 30, A: 235}
	ctxMenuBorder             = rl.Color{R: 60, G: 70, B: 90, A: 255}
	ctxMenuHeaderBG           = rl.Color{R: 30, G: 36, B: 48, A: 255}
	ctxMenuHeaderText         = rl.Color{R: 140, G: 160, B: 200, A: 255}
	ctxContextMenuItemText    = rl.Color{R: 220, G: 230, B: 240, A: 255}
	ctxContextMenuItemDimText = rl.Color{R: 130, G: 140, B: 150, A: 220}
	ctxMenuHover              = rl.Color{R: 50, G: 90, B: 140, A: 200}
	ctxMenuGlyphCol           = rl.Color{R: 200, G: 210, B: 230, A: 235}
	ctxMenuTooltipBG          = rl.Color{R: 14, G: 18, B: 24, A: 240}
)

// Draw must run after panel chrome so the popup sits over scene + UI.
func (m *ContextMenu) Draw(font rl.Font, cursor rl.Vector2) {
	if !m.Active {
		return
	}
	rl.DrawRectangleRec(m.PopupRect, ctxMenuBG)
	rl.DrawRectangleLinesEx(m.PopupRect, 1, ctxMenuBorder)

	y := m.PopupRect.Y
	idx := 0
	for _, sec := range m.Sections {
		if sec.Header != "" {
			headerR := rl.Rectangle{
				X: m.PopupRect.X, Y: y,
				Width: m.PopupRect.Width, Height: ctxMenuHeaderH,
			}
			rl.DrawRectangleRec(headerR, ctxMenuHeaderBG)
			const hSz int32 = 12
			rl.DrawTextEx(font, sec.Header,
				rl.Vector2{X: headerR.X + 8, Y: headerR.Y + 4},
				float32(hSz), 1.0, ctxMenuHeaderText)
			y += ctxMenuHeaderH
		}
		for _, item := range sec.Items {
			r := rl.Rectangle{
				X: m.PopupRect.X, Y: y,
				Width: m.PopupRect.Width, Height: ctxContextMenuItemH,
			}
			if idx == m.HoveredItem && item.Enabled {
				rl.DrawRectangleRec(r, ctxMenuHover)
			}
			const gSz int32 = 17
			glyphCol := ctxMenuGlyphCol
			if !item.Enabled {
				glyphCol = ctxContextMenuItemDimText
			}
			if item.Glyph != 0 {
				gw := rl.MeasureTextEx(font, string(item.Glyph), float32(gSz), 1.0).X
				rl.DrawTextEx(font, string(item.Glyph),
					rl.Vector2{
						X: r.X + ctxContextMenuItemPadX + (ctxMenuGlyphCellW-gw)*0.5,
						Y: r.Y + (r.Height-float32(gSz))*0.5,
					},
					float32(gSz), 1.0, glyphCol)
			}
			const lSz int32 = 14
			labelCol := ctxContextMenuItemText
			if !item.Enabled {
				labelCol = ctxContextMenuItemDimText
			}
			rl.DrawTextEx(font, item.Label,
				rl.Vector2{
					X: r.X + ctxContextMenuItemPadX + ctxMenuGlyphCellW,
					Y: r.Y + (r.Height-float32(lSz))*0.5,
				},
				float32(lSz), 1.0, labelCol)
			y += ctxContextMenuItemH
			idx++
		}
		y += ctxContextMenuSectionGap
	}

	if m.HoveredItem >= 0 {
		flat := m.flatItems()
		if m.HoveredItem < len(flat) {
			item := flat[m.HoveredItem]
			if item.Tooltip != "" {
				const tSz int32 = 12
				tw := rl.MeasureTextEx(font, item.Tooltip, float32(tSz), 1.0).X
				if tw > ctxMenuTooltipMaxW {
					tw = ctxMenuTooltipMaxW
				}
				tipR := rl.Rectangle{
					X:      m.PopupRect.X,
					Y:      m.PopupRect.Y - float32(tSz) - ctxMenuTooltipPad*2,
					Width:  tw + ctxMenuTooltipPad*2,
					Height: float32(tSz) + ctxMenuTooltipPad*2,
				}
				if tipR.Y < 0 {
					tipR.Y = m.PopupRect.Y + m.PopupRect.Height + ctxMenuTooltipPad
				}
				rl.DrawRectangleRec(tipR, ctxMenuTooltipBG)
				rl.DrawRectangleLinesEx(tipR, 1, ctxMenuBorder)
				rl.DrawTextEx(font, item.Tooltip,
					rl.Vector2{X: tipR.X + ctxMenuTooltipPad, Y: tipR.Y + ctxMenuTooltipPad},
					float32(tSz), 1.0, ctxContextMenuItemText)
			}
		}
	}
}
