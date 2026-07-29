package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SymbolApplyRequest is the cross-package signal raised when the player
// clicks [Apply to selection]. main.go consumes it, writes the right
// override component (Unit vs Contact), and clears the flag. Avoids the ui
// package needing posMap / world handles for write paths.
var SymbolApplyRequest struct {
	Active bool
	Spec   components.SymbolSpec
}

// SymbolEditor is a singleton widget that hosts the APP-6 builder. main.go
// owns one instance and dispatches DrawPanel when the chevron menu places
// PanelSymbology into a leaf. State is in-progress until [Apply] writes it
// out to the world.
type SymbolEditor struct {
	InProgress components.SymbolSpec
	// SelectionFn returns the currently selected entity (Unit or Contact)
	// so the editor can flip [Apply to selection] to a disabled state when
	// nothing's selected. Nil → assume something selected.
	SelectionFn func() ecs.Entity
}

func NewSymbolEditor(selFn func() ecs.Entity) *SymbolEditor {
	return &SymbolEditor{
		InProgress: components.SymbolSpec{
			Affiliation: components.AffilHostile,
			Dimension:   components.DimInfantryClass,
			Icon:        components.IconInfantry,
		},
		SelectionFn: selFn,
	}
}

var (
	seBG       = rl.Color{R: 22, G: 28, B: 38, A: 255}
	seRowText  = rl.Color{R: 220, G: 220, B: 220, A: 255}
	seRowDim   = rl.Color{R: 130, G: 130, B: 130, A: 255}
	seBtnIdle  = rl.Color{R: 38, G: 50, B: 70, A: 230}
	seBtnHover = rl.Color{R: 60, G: 80, B: 110, A: 240}
	seBtnSel   = rl.Color{R: 80, G: 120, B: 180, A: 240}
	seBtnDis   = rl.Color{R: 35, G: 40, B: 50, A: 230}
)

const (
	seRowH      float32 = 26
	seRowGap    float32 = 4
	seBtnW      float32 = 70
	seBtnGap    float32 = 4
	seSectionH  float32 = 18
	seLabelFont float32 = 13
)

// DrawPanel renders the editor inside the workspace leaf for PanelSymbology.
func (e *SymbolEditor) DrawPanel(panel Panel, font rl.Font, cursor rl.Vector2, lmbPress, focused bool) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, seBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	pad := float32(10)
	x := content.X + pad
	y := content.Y + pad
	rightX := content.X + content.Width - pad

	// Affiliation row.
	y = e.drawSectionLabel(font, x, y, "Affiliation")
	affilOpts := []struct {
		v     components.Affiliation
		label string
	}{
		{components.AffilFriend, "Friend"},
		{components.AffilHostile, "Hostile"},
		{components.AffilNeutral, "Neutral"},
		{components.AffilUnknown, "Unknown"},
	}
	bx := x
	for _, o := range affilOpts {
		sel := e.InProgress.Affiliation == o.v
		if e.drawButton(font, bx, y, seBtnW, seRowH, o.label, sel, true, cursor, lmbPress && focused) {
			e.InProgress.Affiliation = o.v
		}
		bx += seBtnW + seBtnGap
	}
	y += seRowH + seRowGap*2

	// Dimension row.
	y = e.drawSectionLabel(font, x, y, "Dimension")
	dimOpts := []struct {
		v     components.Dimension
		label string
	}{
		{components.DimInfantryClass, "Infantry"},
		{components.DimVehicleClass, "Vehicle"},
		{components.DimAirClass, "Air"},
		{components.DimNavalClass, "Naval"},
		{components.DimUnknownClass, "Unknown"},
	}
	bx = x
	for _, o := range dimOpts {
		sel := e.InProgress.Dimension == o.v
		if e.drawButton(font, bx, y, seBtnW, seRowH, o.label, sel, true, cursor, lmbPress && focused) {
			e.InProgress.Dimension = o.v
			// Auto-snap Icon to dimension's canonical glyph for convenience.
			e.InProgress.Icon = canonicalIconFor(o.v)
		}
		bx += seBtnW + seBtnGap
		if bx+seBtnW > rightX {
			bx = x
			y += seRowH + seRowGap
		}
	}
	y += seRowH + seRowGap*2

	// Icon row.
	y = e.drawSectionLabel(font, x, y, "Icon")
	iconOpts := []struct {
		v     components.IconKind
		label string
	}{
		{components.IconInfantry, "Infantry"},
		{components.IconArmor, "Armor"},
		{components.IconArtillery, "Artillery"},
		{components.IconAirborne, "Airborne"},
		{components.IconRecon, "Recon"},
		{components.IconHQ, "HQ"},
		{components.IconSupply, "Supply"},
		{components.IconNavalShip, "Ship"},
		{components.IconAircraft, "Aircraft"},
		{components.IconNone, "None"},
	}
	bx = x
	for _, o := range iconOpts {
		sel := e.InProgress.Icon == o.v
		if e.drawButton(font, bx, y, seBtnW, seRowH, o.label, sel, true, cursor, lmbPress && focused) {
			e.InProgress.Icon = o.v
		}
		bx += seBtnW + seBtnGap
		if bx+seBtnW > rightX {
			bx = x
			y += seRowH + seRowGap
		}
	}
	y += seRowH + seRowGap*3

	// Preview pane.
	y = e.drawSectionLabel(font, x, y, "Preview")
	previewCenter := rl.Vector2{X: x + 40, Y: y + 40}
	DrawSymbol(e.InProgress, previewCenter, 32, 1.0)
	y += 92

	// Apply button — disabled when no entity selected.
	hasSel := e.SelectionFn != nil && e.SelectionFn() != (ecs.Entity{})
	if e.drawButton(font, x, y, seBtnW*2.5, seRowH+4, "Apply to selection", false, hasSel, cursor, lmbPress && focused) && hasSel {
		SymbolApplyRequest.Active = true
		SymbolApplyRequest.Spec = e.InProgress
	}
}

func (e *SymbolEditor) drawSectionLabel(font rl.Font, x, y float32, label string) float32 {
	rl.DrawTextEx(font, label, rl.Vector2{X: x, Y: y}, seLabelFont, 1, seRowDim)
	return y + seSectionH
}

// drawButton returns true on click. selected -> highlighted; enabled=false ->
// dimmed and click-ignored.
func (e *SymbolEditor) drawButton(font rl.Font, x, y, w, h float32, label string,
	selected, enabled bool, cursor rl.Vector2, lmbPress bool) bool {
	st := SymbologyStyle(font)
	r := rl.Rectangle{X: x, Y: y, Width: w, Height: h}
	if !enabled {
		ChipDisabled(&st, r, label)
		return false
	}
	return Chip(WidgetInput{Cursor: cursor, Press: lmbPress, Enabled: true}, &st, r, label, selected)
}

func canonicalIconFor(d components.Dimension) components.IconKind {
	switch d {
	case components.DimInfantryClass:
		return components.IconInfantry
	case components.DimVehicleClass:
		return components.IconArmor
	case components.DimAirClass:
		return components.IconAircraft
	case components.DimNavalClass:
		return components.IconNavalShip
	}
	return components.IconNone
}
