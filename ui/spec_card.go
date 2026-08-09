package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// The spec card (Phase 19.7 M5): a read-only projection of the spec tables
// the sim already runs on. It answers "why does my PKM not hurt that BTR"
// without becoming an encyclopedia — one level of depth, no prose, no
// history. Sea Power earns its encyclopedia by being about the hardware; we
// are about the squad, so the card stops at the numbers that change a
// decision.

type SpecSubject struct {
	Valid     bool
	IsVehicle bool
	Weapon    components.WeaponKind
	Vehicle   components.VehicleKind
}

func WeaponSubject(k components.WeaponKind) SpecSubject {
	return SpecSubject{Valid: true, Weapon: k}
}

func VehicleSubject(k components.VehicleKind) SpecSubject {
	return SpecSubject{Valid: true, IsVehicle: true, Vehicle: k}
}

// SpecCardRequest asks the game to show (or re-point) the card. Raised by an
// inspector row and by the card's own weapon rows — the single hop allowed
// from a hull to one of its barrels.
var SpecCardRequest struct {
	Active  bool
	Subject SpecSubject
}

type SpecCardCtx struct {
	Subject      SpecSubject
	Font         rl.Font
	Cursor       rl.Vector2
	LMBPressed   bool
	PanelFocused bool
	Scroll       *ScrollState
}

func SpecCardTitle(s SpecSubject) string {
	if !s.Valid {
		return "Spec"
	}
	if s.IsVehicle {
		return components.SpecForVehicle(s.Vehicle).Name
	}
	return components.SpecForWeapon(s.Weapon).Name
}

func DrawSpecCard(panel Panel, ctx SpecCardCtx) {
	st := InspectorStyle(ctx.Font)
	in := WidgetInput{Cursor: ctx.Cursor, Press: ctx.LMBPressed, Enabled: ctx.PanelFocused}
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	sc := BeginScroll(content, ctx.Scroll, float32(inspectorPadX), float32(inspectorPadY))
	defer sc.End()
	col := &sc.Col

	if !ctx.Subject.Valid {
		TextRow(col, &st, "Click a weapon or vehicle row", st.TextDim)
		TextRow(col, &st, "in the Inspector.", st.TextDim)
		return
	}
	if ctx.Subject.IsVehicle {
		drawVehicleSpec(col, &st, in, components.SpecForVehicle(ctx.Subject.Vehicle))
		return
	}
	drawWeaponSpec(col, &st, components.SpecForWeapon(ctx.Subject.Weapon))
}

func drawWeaponSpec(col *Column, st *Style, s *components.WeaponSpec) {
	Header(col, st, s.Name)
	col.Skip(st.RowH * 0.3)
	specRow(col, st, "Range", fmt.Sprintf("%.0f m", s.RangeM))
	specRow(col, st, "Rate of fire", fmt.Sprintf("%.1f /s", s.RoF))
	specRow(col, st, "Damage", fmt.Sprintf("%d", s.Damage))
	specRow(col, st, "Magazine", fmt.Sprintf("%d", s.Ammo))
	specRow(col, st, "Dispersion", fmt.Sprintf("%.4f rad", s.Dispersion))
	if s.SplashRadius > 0 {
		specRow(col, st, "Splash", fmt.Sprintf("%.1f m (falloff %.1f)",
			s.SplashRadius, s.SplashFalloff))
	}
	col.Skip(st.RowH * 0.5)
	Header(col, st, "Damage vs armour class")
	// A ~0 multiplier is not a weak shot but a skipped target: the gunner
	// will not fire at that class at all.
	specRow(col, st, "vs Soft", multiplierLabel(s.VsSoft))
	specRow(col, st, "vs Light", multiplierLabel(s.VsLight))
	specRow(col, st, "vs Heavy", multiplierLabel(s.VsHeavy))
}

func drawVehicleSpec(col *Column, st *Style, in WidgetInput, s *components.VehicleSpec) {
	Header(col, st, s.Name)
	col.Skip(st.RowH * 0.3)
	specRow(col, st, "HP", fmt.Sprintf("%.0f", s.HP))
	specRow(col, st, "Armour class", armorClassLabel(s.Class))
	specRow(col, st, "Speed road", fmt.Sprintf("%.0f m/s", s.MaxSpeedRoad))
	specRow(col, st, "Speed offroad", fmt.Sprintf("%.0f m/s", s.MaxSpeedOffroad))
	specRow(col, st, "Speed reverse", fmt.Sprintf("%.0f m/s", s.MaxSpeedReverse))
	specRow(col, st, "Turn radius", fmt.Sprintf("%.1f m", s.TurnRadiusM))
	specRow(col, st, "Seats", fmt.Sprintf("%d", s.SeatCount))
	col.Skip(st.RowH * 0.5)
	Header(col, st, "Armour by sector")
	specRow(col, st, "Front", fmt.Sprintf("%.1f", s.ArmorFront))
	specRow(col, st, "Side", fmt.Sprintf("%.1f", s.ArmorSide))
	specRow(col, st, "Rear", fmt.Sprintf("%.1f", s.ArmorRear))
	col.Skip(st.RowH * 0.5)
	Header(col, st, "Signature")
	specRow(col, st, "Sensor range", fmt.Sprintf("%.0f m", s.SensorRangeM))
	specRow(col, st, "Detectability", fmt.Sprintf("x%.2f", s.DetectMul))
	specRow(col, st, "Noise radius", fmt.Sprintf("%.0f m", s.NoiseRadiusM))
	if s.WeaponCount > 0 {
		col.Skip(st.RowH * 0.5)
		Header(col, st, "Armament")
		for i := uint8(0); i < s.WeaponCount && int(i) < len(s.WeaponKinds); i++ {
			kind := s.WeaponKinds[i]
			row := col.Band(st.RowH)
			band := rl.Rectangle{X: row.X - 2, Y: row.Y - 2, Width: row.Width, Height: row.Height}
			if in.Hover(band) {
				rl.DrawRectangleRec(band, inspectorRowHoverBG)
			}
			if in.Clicked(band) {
				SpecCardRequest.Active = true
				SpecCardRequest.Subject = WeaponSubject(kind)
			}
			ws := components.SpecForWeapon(kind)
			TextClipped(st, row, fmt.Sprintf("%-10s %.0f m, dmg %d",
				ws.Name, ws.RangeM, ws.Damage), st.Text)
		}
	}
}

// specLinkRow is an inspector row that opens the card on click. Hover is the
// only affordance — a chip per row would drown the panel in chrome.
func specLinkRow(col *Column, st *Style, in WidgetInput, text string, subj SpecSubject) {
	row := col.Band(st.RowH)
	band := rl.Rectangle{X: row.X - 2, Y: row.Y - 2, Width: row.Width, Height: row.Height}
	if in.Hover(band) {
		rl.DrawRectangleRec(band, inspectorRowHoverBG)
	}
	if in.Clicked(band) {
		SpecCardRequest.Active = true
		SpecCardRequest.Subject = subj
	}
	TextClipped(st, row, text, st.Text)
}

func specRow(col *Column, st *Style, label, value string) {
	row := col.Band(st.RowH)
	TextClipped(st, row, fmt.Sprintf("%-14s %s", label, value), st.Text)
}

func multiplierLabel(m float32) string {
	if m < 0.01 {
		return "- (will not engage)"
	}
	return fmt.Sprintf("x%.2f", m)
}

func armorClassLabel(c components.ArmorClass) string {
	switch c {
	case components.ArmorClassSoft:
		return "Soft"
	case components.ArmorClassLight:
		return "Light"
	case components.ArmorClassHeavy:
		return "Heavy"
	}
	return "?"
}
