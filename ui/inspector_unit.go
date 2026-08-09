package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawInspectorUnit(ctx InspectorCtx, ent ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	if !ctx.World.Alive(ent) {
		TextRow(&col, &ctx.st, "(unit no longer alive)", ctx.st.TextDim)
		return int32(col.Y)
	}
	TextRow(&col, &ctx.st, fmt.Sprintf("Unit #%X", ent.ID()&0xFFFF), ctx.st.Text)

	role := roleOf(ctx, ent)
	drawRoleChip(ctx, col.Band(ctx.st.RowH), role, "Role: "+role.String())
	col.Skip(ctx.st.RowH)

	if st := ctx.StanceMap.Get(ent); st != nil {
		TextRowClipped(&col, &ctx.st, "Stance:    "+stanceLabel(st.Code), ctx.st.Text)
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Motion:    %.2f m/s  yaw %.0f deg",
			mo.Speed, mo.Yaw*180.0/math.Pi), ctx.st.Text)
	}
	if th := ctx.ThreatMap.Get(ent); th != nil {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Threat:    %.2f %s  supp %.2f",
			th.Total, threatStateLabel(th.State), th.Suppression), ctx.st.Text)
	}
	// Override / Reason / Resume surfaces AI-driven control so the
	// player understands why the unit just bolted from its formation slot.
	if ctx.TacticalOverrideMap != nil {
		if ov := ctx.TacticalOverrideMap.Get(ent); ov != nil {
			drawOverrideBlock(ctx, &col, ent, ov)
		}
	}
	if ctx.StaminaMap != nil {
		if st := ctx.StaminaMap.Get(ent); st != nil && st.MaxLevel > 0 {
			TextRowClipped(&col, &ctx.st, fmt.Sprintf("Stamina:   %.2f / %.2f",
				st.Current, st.MaxLevel), ctx.st.Text)
		}
	}
	if ctx.HPMap != nil {
		if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
			TextRowClipped(&col, &ctx.st, fmt.Sprintf("HP:        %.1f / %.1f",
				hp.Current, hp.Max), ctx.st.Text)
		}
	}
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(ent); f != nil {
			TextRowClipped(&col, &ctx.st, "Faction:   "+factionLabel(f.ID), ctx.st.Text)
		}
	}
	if sm := ctx.SquadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Squad:     #%X slot %d",
			sm.Squad.ID()&0xFFF, sm.SlotIndex), ctx.st.Text)
	} else {
		TextRowClipped(&col, &ctx.st, "Squad:     - (soloist)", ctx.st.TextDim)
	}
	// "Return to formation" only when the unit is hand-placed
	// (IndividualPosition present). Click removes the marker;
	// FormationSystem snaps the unit back on the next tick.
	if ctx.IndividualPositionMap != nil && ctx.IndividualPositionMap.Has(ent) {
		row := col.Band(srChipH)
		chip := rl.Rectangle{X: row.X, Y: row.Y, Width: 160, Height: row.Height}
		if Chip(ctx.in, &ctx.st, chip, "Return to formation", false) {
			ctx.IndividualPositionMap.Remove(ent)
		}
		col.Skip(2)
	}
	if eq := ctx.EquipmentMap.Get(ent); eq != nil {
		drawPrimaryWeaponRow(ctx, &col, eq)
	}
	return int32(col.Y)
}

// drawPrimaryWeaponRow names the weapon instead of printing its entity id —
// the id was unreadable, and the row is the way into the spec card.
func drawPrimaryWeaponRow(ctx InspectorCtx, col *Column, eq *components.Equipment) {
	if eq.Primary == (ecs.Entity{}) || !ctx.World.Alive(eq.Primary) {
		TextRowClipped(col, &ctx.st, "Primary:   -", ctx.st.TextDim)
		return
	}
	wc := ctx.WeaponMap.Get(eq.Primary)
	if wc == nil {
		TextRowClipped(col, &ctx.st, "Primary:   -", ctx.st.TextDim)
		return
	}
	spec := components.SpecForWeapon(wc.Kind)
	specLinkRow(col, &ctx.st, ctx.in,
		fmt.Sprintf("Primary:   %-8s ammo %d", spec.Name, wc.Ammo),
		WeaponSubject(wc.Kind))
}

// drawRoleChip paints the role's colour swatch with its short label, then the
// caption beside it. Shared by the single-unit header and the roster rows.
func drawRoleChip(ctx InspectorCtx, row rl.Rectangle, role components.UnitRoleKind, caption string) {
	const chipW float32 = 24
	chip := rl.Rectangle{X: row.X, Y: row.Y + 2, Width: chipW, Height: row.Height - 4}
	tint := components.RoleColor(role)
	rl.DrawRectangleRec(chip, tint)
	rl.DrawRectangleLinesEx(chip, 1, rl.Color{R: 20, G: 20, B: 20, A: 220})
	TextCentered(&ctx.st, chip, role.ShortLabel(), contrastTextColor(tint))
	if caption != "" {
		TextClipped(&ctx.st, rl.Rectangle{
			X: row.X + chipW + 8, Y: row.Y,
			Width: row.Width - chipW - 8, Height: row.Height,
		}, caption, ctx.st.Text)
	}
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
