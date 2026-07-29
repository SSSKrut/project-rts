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
	sePad       float32 = 10
)

// DrawPanel renders the editor inside the workspace leaf for PanelSymbology.
// scroll may be nil (floating instances), in which case the body is simply
// clipped as before.
func (e *SymbolEditor) DrawPanel(panel Panel, font rl.Font, cursor rl.Vector2,
	lmbPress, focused bool, scroll *ScrollState) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, seBG)

	st := SymbologyStyle(font)
	in := WidgetInput{Cursor: cursor, Press: lmbPress, Enabled: focused}
	sc := BeginScroll(content, scroll, sePad, sePad)
	defer sc.End()
	col := &sc.Col

	e.section(col, &st, "Affiliation")
	affils := [4]struct {
		v     components.Affiliation
		label string
	}{
		{components.AffilFriend, "Friend"},
		{components.AffilHostile, "Hostile"},
		{components.AffilNeutral, "Neutral"},
		{components.AffilUnknown, "Unknown"},
	}
	flow := NewFlow(col, seRowH, seBtnGap)
	for _, o := range affils {
		if Chip(in, &st, flow.Next(seBtnW), o.label, e.InProgress.Affiliation == o.v) {
			e.InProgress.Affiliation = o.v
		}
	}
	flow.End()
	col.Skip(seRowGap * 2)

	e.section(col, &st, "Dimension")
	dims := [5]struct {
		v     components.Dimension
		label string
	}{
		{components.DimInfantryClass, "Infantry"},
		{components.DimVehicleClass, "Vehicle"},
		{components.DimAirClass, "Air"},
		{components.DimNavalClass, "Naval"},
		{components.DimUnknownClass, "Unknown"},
	}
	flow = NewFlow(col, seRowH, seBtnGap)
	for _, o := range dims {
		if Chip(in, &st, flow.Next(seBtnW), o.label, e.InProgress.Dimension == o.v) {
			e.InProgress.Dimension = o.v
			// Auto-snap Icon to dimension's canonical glyph for convenience.
			e.InProgress.Icon = canonicalIconFor(o.v)
		}
	}
	flow.End()
	col.Skip(seRowGap * 2)

	e.section(col, &st, "Icon")
	icons := [10]struct {
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
	flow = NewFlow(col, seRowH, seBtnGap)
	for _, o := range icons {
		if Chip(in, &st, flow.Next(seBtnW), o.label, e.InProgress.Icon == o.v) {
			e.InProgress.Icon = o.v
		}
	}
	flow.End()
	col.Skip(seRowGap * 3)

	e.section(col, &st, "Preview")
	preview := col.Band(80)
	DrawSymbol(e.InProgress, rl.Vector2{X: preview.X + 40, Y: preview.Y + 40}, 32, 1.0)
	col.Skip(12)

	// Apply is disabled when nothing is selected to write onto.
	hasSel := e.SelectionFn != nil && e.SelectionFn() != (ecs.Entity{})
	apply := col.Band(seRowH + 4)
	apply.Width = seBtnW * 2.5
	if !hasSel {
		ChipDisabled(&st, apply, "Apply to selection")
	} else if Chip(in, &st, apply, "Apply to selection", false) {
		SymbolApplyRequest.Active = true
		SymbolApplyRequest.Spec = e.InProgress
	}
}

func (e *SymbolEditor) section(col *Column, st *Style, label string) {
	Text(st, col.Band(seSectionH), label, st.TextDim)
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
