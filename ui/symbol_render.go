package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// SymbolPalette maps Affiliation → fill / outline colour pair. APP-6
// standard: friend=blue, hostile=red, neutral=green, unknown=yellow.
type SymbolPalette struct {
	Fill    rl.Color
	Outline rl.Color
}

var symbolPalette = [...]SymbolPalette{
	components.AffilUnknown:  {Fill: rl.Color{R: 220, G: 210, B: 60, A: 230}, Outline: rl.Color{R: 80, G: 70, B: 0, A: 255}},
	components.AffilFriend:   {Fill: rl.Color{R: 80, G: 130, B: 230, A: 230}, Outline: rl.Color{R: 20, G: 40, B: 100, A: 255}},
	components.AffilHostile:  {Fill: rl.Color{R: 220, G: 70, B: 70, A: 230}, Outline: rl.Color{R: 90, G: 10, B: 10, A: 255}},
	components.AffilNeutral:  {Fill: rl.Color{R: 110, G: 200, B: 110, A: 230}, Outline: rl.Color{R: 20, G: 80, B: 20, A: 255}},
}

// DrawSymbol paints one APP-6-style symbol at `center`, sized to extend
// `half` pixels from centre in both axes. alpha multiplies the fill /
// outline channels (used for contact age fade).
func DrawSymbol(spec components.SymbolSpec, center rl.Vector2, half float32, alpha float32) {
	pal := symbolPalette[spec.Affiliation]
	fill := pal.Fill
	outline := pal.Outline
	fill.A = uint8(float32(fill.A) * alpha)
	outline.A = uint8(float32(outline.A) * alpha)
	drawSymbolFrame(spec.Affiliation, center, half, fill, outline)
	drawSymbolIcon(spec.Icon, center, half, outline)
}

// SymbolBounds is the frame's screen rectangle. Selection rings and hit
// tests need the same geometry drawSymbolFrame paints, per affiliation.
func SymbolBounds(a components.Affiliation, c rl.Vector2, half float32) rl.Rectangle {
	w, h := half, half
	switch a {
	case components.AffilFriend:
		w = half * 1.5
	case components.AffilUnknown:
		w, h = half*1.2, half*1.2
	}
	return rl.Rectangle{X: c.X - w, Y: c.Y - h, Width: w * 2, Height: h * 2}
}

// InflateRect grows a rect by d on every side.
func InflateRect(r rl.Rectangle, d float32) rl.Rectangle {
	return rl.Rectangle{
		X: r.X - d, Y: r.Y - d,
		Width: r.Width + 2*d, Height: r.Height + 2*d,
	}
}

func drawSymbolFrame(a components.Affiliation, c rl.Vector2, half float32,
	fill, outline rl.Color) {
	switch a {
	case components.AffilFriend:
		// Rectangle wider than tall, ~1.5:1 ratio.
		w := half * 1.5
		h := half
		x := c.X - w
		y := c.Y - h
		rl.DrawRectangleV(rl.Vector2{X: x, Y: y}, rl.Vector2{X: w * 2, Y: h * 2}, fill)
		rl.DrawRectangleLinesEx(rl.Rectangle{X: x, Y: y, Width: w * 2, Height: h * 2}, 1.5, outline)
	case components.AffilHostile:
		// Diamond - 4-vertex rhombus rotated 45°.
		v1 := rl.Vector2{X: c.X, Y: c.Y - half}
		v2 := rl.Vector2{X: c.X + half, Y: c.Y}
		v3 := rl.Vector2{X: c.X, Y: c.Y + half}
		v4 := rl.Vector2{X: c.X - half, Y: c.Y}
		rl.DrawTriangle(v1, v4, v2, fill)
		rl.DrawTriangle(v2, v4, v3, fill)
		rl.DrawLineEx(v1, v2, 1.5, outline)
		rl.DrawLineEx(v2, v3, 1.5, outline)
		rl.DrawLineEx(v3, v4, 1.5, outline)
		rl.DrawLineEx(v4, v1, 1.5, outline)
	case components.AffilNeutral:
		// Square - aligned axes, slightly smaller than friend rect.
		s := half
		x := c.X - s
		y := c.Y - s
		rl.DrawRectangleV(rl.Vector2{X: x, Y: y}, rl.Vector2{X: s * 2, Y: s * 2}, fill)
		rl.DrawRectangleLinesEx(rl.Rectangle{X: x, Y: y, Width: s * 2, Height: s * 2}, 1.5, outline)
	default:
		// Unknown - simplified cloverleaf (rounded-corner rect for now;
		// 4-lobe path can come later when we move to baked textures).
		s := half * 1.2
		x := c.X - s
		y := c.Y - s
		rl.DrawRectangleRounded(rl.Rectangle{X: x, Y: y, Width: s * 2, Height: s * 2}, 0.45, 6, fill)
		rl.DrawRectangleRoundedLines(rl.Rectangle{X: x, Y: y, Width: s * 2, Height: s * 2}, 0.45, 6, outline)
	}
}

func drawSymbolIcon(icon components.IconKind, c rl.Vector2, half float32, col rl.Color) {
	switch icon {
	case components.IconInfantry:
		// Crossed X covering ~70% of the frame.
		off := half * 0.65
		rl.DrawLineEx(rl.Vector2{X: c.X - off, Y: c.Y - off*0.6},
			rl.Vector2{X: c.X + off, Y: c.Y + off*0.6}, 2, col)
		rl.DrawLineEx(rl.Vector2{X: c.X + off, Y: c.Y - off*0.6},
			rl.Vector2{X: c.X - off, Y: c.Y + off*0.6}, 2, col)
	case components.IconArmor:
		// Filled horizontal oval / pill.
		w := half * 0.95
		h := half * 0.45
		rl.DrawEllipse(int32(c.X), int32(c.Y), w, h, col)
	case components.IconArtillery:
		// Filled dot centre.
		rl.DrawCircleV(c, half*0.4, col)
	case components.IconAirborne:
		// Upward chevron — two diagonal lines meeting above centre.
		off := half * 0.7
		rl.DrawLineEx(rl.Vector2{X: c.X - off, Y: c.Y + off*0.3},
			rl.Vector2{X: c.X, Y: c.Y - off*0.4}, 2, col)
		rl.DrawLineEx(rl.Vector2{X: c.X + off, Y: c.Y + off*0.3},
			rl.Vector2{X: c.X, Y: c.Y - off*0.4}, 2, col)
	case components.IconRecon:
		// Single diagonal slash.
		off := half * 0.7
		rl.DrawLineEx(rl.Vector2{X: c.X - off, Y: c.Y + off*0.6},
			rl.Vector2{X: c.X + off, Y: c.Y - off*0.6}, 2, col)
	case components.IconHQ:
		// Small flag-rectangle on a stick.
		stick := half * 0.6
		rl.DrawLineEx(rl.Vector2{X: c.X - half*0.5, Y: c.Y + half*0.3},
			rl.Vector2{X: c.X - half*0.5, Y: c.Y - stick}, 1.5, col)
		rl.DrawRectangleV(rl.Vector2{X: c.X - half*0.5, Y: c.Y - stick},
			rl.Vector2{X: half * 0.8, Y: half * 0.4}, col)
	case components.IconSupply:
		// Vertical bar.
		rl.DrawLineEx(rl.Vector2{X: c.X, Y: c.Y - half*0.6},
			rl.Vector2{X: c.X, Y: c.Y + half*0.6}, 2, col)
	case components.IconNavalShip:
		// Wavy line.
		w := half * 0.7
		rl.DrawLineEx(rl.Vector2{X: c.X - w, Y: c.Y},
			rl.Vector2{X: c.X - w*0.3, Y: c.Y - w*0.3}, 1.5, col)
		rl.DrawLineEx(rl.Vector2{X: c.X - w*0.3, Y: c.Y - w*0.3},
			rl.Vector2{X: c.X + w*0.3, Y: c.Y + w*0.3}, 1.5, col)
		rl.DrawLineEx(rl.Vector2{X: c.X + w*0.3, Y: c.Y + w*0.3},
			rl.Vector2{X: c.X + w, Y: c.Y}, 1.5, col)
	case components.IconAircraft:
		// Upward arrow.
		rl.DrawLineEx(rl.Vector2{X: c.X, Y: c.Y + half*0.5},
			rl.Vector2{X: c.X, Y: c.Y - half*0.5}, 2, col)
		rl.DrawLineEx(rl.Vector2{X: c.X, Y: c.Y - half*0.5},
			rl.Vector2{X: c.X - half*0.4, Y: c.Y - half*0.1}, 2, col)
		rl.DrawLineEx(rl.Vector2{X: c.X, Y: c.Y - half*0.5},
			rl.Vector2{X: c.X + half*0.4, Y: c.Y - half*0.1}, 2, col)
	}
}

// DefaultSpecForDimension picks an Icon from the perceived dimension when
// no preset override exists. Drives the symbol shown for a fresh Sensor
// contact + own unit baseline.
func DefaultSpecForDimension(a components.Affiliation, d components.Dimension) components.SymbolSpec {
	icon := components.IconNone
	switch d {
	case components.DimInfantryClass:
		icon = components.IconInfantry
	case components.DimVehicleClass:
		icon = components.IconArmor
	case components.DimAirClass:
		icon = components.IconAircraft
	case components.DimNavalClass:
		icon = components.IconNavalShip
	}
	return components.SymbolSpec{
		Affiliation: a,
		Dimension:   d,
		Icon:        icon,
	}
}

// BuiltinSymbologyPresets is the 12 quick-pick combos seeded into the
// SymbologyPresets resource on startup if missing. Names are stable so the
// RMB context menu (Track 18.5.F) can lookup by name without index drift.
func BuiltinSymbologyPresets() []components.SymbologyPreset {
	combos := [...]struct {
		affil components.Affiliation
		dim   components.Dimension
		icon  components.IconKind
		name  string
	}{
		{components.AffilHostile, components.DimInfantryClass, components.IconInfantry, "hostile_infantry"},
		{components.AffilHostile, components.DimVehicleClass, components.IconArmor, "hostile_vehicle"},
		{components.AffilHostile, components.DimAirClass, components.IconAircraft, "hostile_air"},
		{components.AffilHostile, components.DimNavalClass, components.IconNavalShip, "hostile_naval"},
		{components.AffilNeutral, components.DimInfantryClass, components.IconInfantry, "neutral_infantry"},
		{components.AffilNeutral, components.DimVehicleClass, components.IconArmor, "neutral_vehicle"},
		{components.AffilNeutral, components.DimAirClass, components.IconAircraft, "neutral_air"},
		{components.AffilNeutral, components.DimNavalClass, components.IconNavalShip, "neutral_naval"},
		{components.AffilUnknown, components.DimInfantryClass, components.IconInfantry, "unknown_infantry"},
		{components.AffilUnknown, components.DimVehicleClass, components.IconArmor, "unknown_vehicle"},
		{components.AffilUnknown, components.DimAirClass, components.IconAircraft, "unknown_air"},
		{components.AffilUnknown, components.DimNavalClass, components.IconNavalShip, "unknown_naval"},
	}
	out := make([]components.SymbologyPreset, 0, len(combos))
	for _, c := range combos {
		out = append(out, components.SymbologyPreset{
			Name: c.name,
			Spec: components.SymbolSpec{
				Affiliation: c.affil,
				Dimension:   c.dim,
				Icon:        c.icon,
			},
		})
	}
	return out
}
