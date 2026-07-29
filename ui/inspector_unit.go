package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawInspectorUnit(ctx InspectorCtx, ent ecs.Entity, x, y int32) int32 {
	if !ctx.World.Alive(ent) {
		drawText(ctx.Font, "(unit no longer alive)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	drawText(ctx.Font, fmt.Sprintf("Unit #%X", ent.ID()&0xFFFF),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	role := roleOf(ctx, ent)
	tint := components.RoleColor(role)
	rl.DrawRectangle(x, y+2, 24, inspectorRowH-4, tint)
	rl.DrawRectangleLines(x, y+2, 24, inspectorRowH-4, rl.Color{R: 20, G: 20, B: 20, A: 220})
	labelColor := contrastTextColor(tint)
	short := role.ShortLabel()
	size := rl.MeasureTextEx(ctx.Font, short, float32(inspectorFontSize), 1)
	rl.DrawTextEx(ctx.Font, short, rl.Vector2{
		X: float32(x) + 12 - size.X*0.5,
		Y: float32(y) + 2 + (float32(inspectorRowH-4)-size.Y)*0.5,
	}, float32(inspectorFontSize), 1, labelColor)
	drawText(ctx.Font, "Role: "+role.String(), x+32, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2

	if st := ctx.StanceMap.Get(ent); st != nil {
		drawText(ctx.Font, "Stance:    "+stanceLabel(st.Code),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		yawDeg := mo.Yaw * 180.0 / math.Pi
		drawText(ctx.Font, fmt.Sprintf("Motion:    %.2f m/s  yaw %.0f°", mo.Speed, yawDeg),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if th := ctx.ThreatMap.Get(ent); th != nil {
		drawText(ctx.Font, fmt.Sprintf("Threat:    %.2f %s  supp %.2f",
			th.Total, threatStateLabel(th.State), th.Suppression),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	// Override / Reason / Resume surfaces AI-driven control so the
	// player understands why the unit just bolted from its formation slot.
	if ctx.TacticalOverrideMap != nil {
		if ov := ctx.TacticalOverrideMap.Get(ent); ov != nil {
			y = drawOverrideBlock(ctx, ent, ov, x, y)
		}
	}
	if ctx.StaminaMap != nil {
		if st := ctx.StaminaMap.Get(ent); st != nil && st.MaxLevel > 0 {
			drawText(ctx.Font, fmt.Sprintf("Stamina:   %.2f / %.2f", st.Current, st.MaxLevel),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	if ctx.HPMap != nil {
		if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
			drawText(ctx.Font, fmt.Sprintf("HP:        %.1f / %.1f", hp.Current, hp.Max),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(ent); f != nil {
			drawText(ctx.Font, "Faction:   "+factionLabel(f.ID),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	if sm := ctx.SquadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		drawText(ctx.Font, fmt.Sprintf("Squad:     #%X slot %d", sm.Squad.ID()&0xFFF, sm.SlotIndex),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	} else {
		drawText(ctx.Font, "Squad:     - (soloist)",
			x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
	}
	// "Return to formation" only when the unit is hand-placed
	// (IndividualPosition present). Click removes the marker;
	// FormationSystem snaps the unit back on the next tick.
	if ctx.IndividualPositionMap != nil && ctx.IndividualPositionMap.Has(ent) {
		chip := rl.Rectangle{X: float32(x), Y: float32(y), Width: 160, Height: srChipH}
		if Chip(ctx.in, &ctx.st, chip, "Return to formation", false) {
			ctx.IndividualPositionMap.Remove(ent)
		}
		y += int32(srChipH) + 2
	}
	if eq := ctx.EquipmentMap.Get(ent); eq != nil {
		drawText(ctx.Font, equipmentSummary(eq),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	return y
}

// contrastTextColor picks black or white by perceived luminance of `bg`
// so chips / labels stay readable across the full role palette.
func contrastTextColor(bg rl.Color) rl.Color {
	lum := 0.299*float32(bg.R) + 0.587*float32(bg.G) + 0.114*float32(bg.B)
	if lum < 140 {
		return rl.Color{R: 255, G: 255, B: 255, A: 255}
	}
	return rl.Color{R: 0, G: 0, B: 0, A: 255}
}

func stanceLabel(s components.StanceCode) string {
	return components.SpecForStance(s).Name
}

func factionLabel(id uint8) string {
	switch id {
	case components.FactionPlayer:
		return "Player"
	case components.FactionEnemyRed:
		return "EnemyRed"
	default:
		return fmt.Sprintf("F%d", id)
	}
}

func equipmentSummary(eq *components.Equipment) string {
	primary := "-"
	if eq.Primary != (ecs.Entity{}) {
		primary = fmt.Sprintf("#%X", eq.Primary.ID()&0xFFFF)
	}
	return "Primary:   " + primary
}
