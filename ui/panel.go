// Package ui hosts the multi-panel UI layer introduced in Phase 10.
//
// The package is deliberately ECS-agnostic: panels are screen-space rectangles
// with a content callback or direct draw, no World queries inside this file.
// Each panel's content lives in its own file (scene3d.go, inspector.go,
// time.go, map_render.go) and is wired by main.go.
//
// L1 layout = fixed grid. Two presets (Field / Command) swap which slot the
// 3D scene and the map occupy. L3 (splitters) / L4 (movable + dock zones) is
// Phase 22 — this file's exported surface (PanelManager.{Recompute, FocusedAt,
// Get}) is what stays stable across that migration.
package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// PanelID identifies one of the four MVP panels. String-typed instead of an
// enum because it shows up in debug overlays and HUD labels.
type PanelID string

const (
	Panel3D       PanelID = "3d"
	PanelMap      PanelID = "map"
	PanelInspect  PanelID = "inspector"
	PanelTime     PanelID = "time"
	PanelNone     PanelID = ""
)

// LayoutPreset chooses which slot holds the 3D scene vs the map. Tab toggles
// the two presets at runtime.
type LayoutPreset uint8

const (
	PresetField   LayoutPreset = iota // 3D is the big slot, map is the side slot.
	PresetCommand                     // Map is the big slot, 3D is the side slot.
)

// Panel — one screen-space rectangle. Bounds recomputed by LayoutManager on
// resize / preset swap. Title is shown in the panel's top-left corner.
type Panel struct {
	ID     PanelID
	Bounds rl.Rectangle
	Title  string
}

// PanelManager owns the fixed list of panels and the active LayoutPreset.
// Panels slice order doubles as Z-order — first = bottom, last = top. In L1
// nothing overlaps so this only matters for the FocusedAt fallback when two
// rects share a border pixel.
type PanelManager struct {
	Panels  []Panel
	Layout  LayoutPreset
	focused PanelID
}

// NewPanelManager constructs the manager with four empty-bounds panels in
// canonical order. Caller must call Recompute(screenW, screenH) before the
// first draw — bounds are zero until then.
func NewPanelManager() *PanelManager {
	return &PanelManager{
		Panels: []Panel{
			{ID: Panel3D, Title: "Field"},
			{ID: PanelMap, Title: "Map"},
			{ID: PanelInspect, Title: "Inspector"},
			{ID: PanelTime, Title: "Time"},
		},
		Layout:  PresetField,
		focused: PanelNone,
	}
}

// Recompute rebuilds every panel's Bounds from the current screen size and
// active LayoutPreset. Idempotent — call after resize, preset toggle, or any
// time the cached rects might be stale.
func (m *PanelManager) Recompute(screenW, screenH int32) {
	var rects map[PanelID]rl.Rectangle
	switch m.Layout {
	case PresetCommand:
		rects = layoutCommand(screenW, screenH)
	default:
		rects = layoutField(screenW, screenH)
	}
	for i := range m.Panels {
		if r, ok := rects[m.Panels[i].ID]; ok {
			m.Panels[i].Bounds = r
		}
	}
}

// TogglePreset flips Field ↔ Command. Caller is responsible for Recompute()
// after — keeping the calls separate lets main.go also realloc the 3D RT in
// the same place.
func (m *PanelManager) TogglePreset() {
	if m.Layout == PresetField {
		m.Layout = PresetCommand
	} else {
		m.Layout = PresetField
	}
}

// FocusedAt returns the topmost panel whose Bounds contains cursor. Returns
// PanelNone when cursor is outside every panel (e.g. on the OS title bar). In
// L1 panels don't overlap; iteration goes back-to-front to match the L3/L4
// Z-order rule for free.
func (m *PanelManager) FocusedAt(cursor rl.Vector2) PanelID {
	for i := len(m.Panels) - 1; i >= 0; i-- {
		p := &m.Panels[i]
		if pointInRect(cursor, p.Bounds) {
			m.focused = p.ID
			return p.ID
		}
	}
	m.focused = PanelNone
	return PanelNone
}

// Get returns the Panel by ID. Returns a zero Panel{} when the ID is unknown —
// callers that hand out a fixed compile-time ID can assume Get always
// succeeds.
func (m *PanelManager) Get(id PanelID) Panel {
	for i := range m.Panels {
		if m.Panels[i].ID == id {
			return m.Panels[i]
		}
	}
	return Panel{}
}

// IsFocused is the per-frame gate used by input handlers. Reads the cached
// focusedPanel from the last FocusedAt; call FocusedAt at the top of each
// frame.
func (m *PanelManager) IsFocused(id PanelID) bool {
	return m.focused == id
}

// Focused returns the cached focused panel ID. Useful when the same ID is
// needed in multiple input blocks — call FocusedAt once and re-read via this.
func (m *PanelManager) Focused() PanelID { return m.focused }

// CursorLocal converts a screen-space cursor to a panel-local Vector2 (cursor
// relative to the panel's top-left). Used by raycast / marquee / picking in
// the 3D and map panels.
func CursorLocal(cursor rl.Vector2, p Panel) rl.Vector2 {
	return rl.Vector2{X: cursor.X - p.Bounds.X, Y: cursor.Y - p.Bounds.Y}
}

func pointInRect(p rl.Vector2, r rl.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}
