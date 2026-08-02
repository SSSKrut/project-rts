package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawInspectorSquad(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	if !ctx.World.Alive(squad) {
		drawText(ctx.Font, "(squad destroyed)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	roster := ctx.RosterMap.Get(squad)
	if roster == nil {
		drawText(ctx.Font, "(no roster)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}

	colorChip := rl.Color{R: 80, G: 80, B: 80, A: 255}
	if ctx.SquadColor != nil {
		colorChip = ctx.SquadColor(squad)
	}
	rl.DrawRectangle(x, y+3, 12, 12, colorChip)
	drawText(ctx.Font, fmt.Sprintf("Squad #%X", squad.ID()&0xFFF),
		x+18, y, inspectorFontSize, inspectorText)
	y += inspectorRowH

	drawText(ctx.Font, fmt.Sprintf("Members: %d / %d", roster.Count, components.SquadRosterSize),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH

	if fd := ctx.FormationDataMap.Get(squad); fd != nil {
		drawText(ctx.Font, fmt.Sprintf("Formation: %s  spacing %.1f m",
			formationLabel(fd.Type), fd.Spacing),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if mp := ctx.MacroPathMap.Get(squad); mp != nil {
		drawText(ctx.Font, macroPathLabel(mp),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
		// Only show when active so the row stays quiet during normal movement.
		if mp.WaitingForStragglers {
			drawText(ctx.Font, fmt.Sprintf("Waiting for stragglers (%d/%d)",
				mp.StragglerCaughtUp, mp.StragglerTotal),
				x, y, inspectorFontSize, rl.Color{R: 230, G: 170, B: 90, A: 255})
			y += inspectorRowH
		}
	}
	// Idle hides the row; Engaged / Scrambling get a coloured row so the
	// player notices reactive behaviour.
	if ctx.SquadStateMap != nil {
		if state := ctx.SquadStateMap.Get(squad); state != nil {
			if label := squadStateLabel(state.Code); label != "" {
				color := inspectorTextDim
				if state.Code == components.SquadStateScrambling {
					color = rl.Color{R: 230, G: 110, B: 80, A: 255}
				}
				drawText(ctx.Font, "State:     "+label,
					x, y, inspectorFontSize, color)
				y += inspectorRowH
			}
		}
	}
	y += inspectorRowH / 2

	y = drawInspectorOrderSection(ctx, squad, x, y, width)
	y += inspectorRowH / 2

	drawText(ctx.Font, "Roster:", x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH

	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		role := roleOf(ctx, mem)
		// Selection / hover override the role tint so focus stays unambiguous.
		bg := components.RoleColor(role)
		bg.A = 90
		if isSelected(ctx.Selected, mem) {
			bg = inspectorRowSelectBG
		}
		if ctx.Hovered == mem {
			bg = inspectorRowHoverBG
		}
		rl.DrawRectangle(x-2, y-2, width, inspectorRowH, bg)

		// ShortLabel chip stays readable when the row tint is dimmed by
		// selection state.
		chip := components.RoleColor(role)
		rl.DrawRectangle(x, y+2, 22, inspectorRowH-4, chip)
		rl.DrawRectangleLines(x, y+2, 22, inspectorRowH-4, rl.Color{R: 20, G: 20, B: 20, A: 200})
		short := role.ShortLabel()
		sizeShort := rl.MeasureTextEx(ctx.Font, short, float32(inspectorFontSize), 1)
		rl.DrawTextEx(ctx.Font, short, rl.Vector2{
			X: float32(x) + 11 - sizeShort.X*0.5,
			Y: float32(y) + 2 + (float32(inspectorRowH-4)-sizeShort.Y)*0.5,
		}, float32(inspectorFontSize), 1, contrastTextColor(chip))

		stance := "-"
		if st := ctx.StanceMap.Get(mem); st != nil {
			stance = stanceLabel(st.Code)
		}
		drawText(ctx.Font, fmt.Sprintf("%d  #%-6X %s", i, mem.ID()&0xFFFFFF, stance),
			x+28, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}

	return y
}

func formationLabel(k components.FormationKind) string {
	switch k {
	case components.FormationLine:
		return "Line"
	case components.FormationColumn:
		return "Column"
	case components.FormationWedge:
		return "Wedge"
	case components.FormationLoose:
		return "Loose"
	default:
		return "?"
	}
}

func macroPathLabel(mp *components.MacroPath) string {
	if !mp.HasGoal {
		return "Macro:     Idle"
	}
	remaining := int(mp.Count) - int(mp.Head)
	if remaining < 0 {
		remaining = 0
	}
	_ = remaining
	return fmt.Sprintf("Macro:     Moving (wp %d/%d)", mp.Head+1, mp.Count)
}

// squadStateLabel returns "" for Idle so callers skip rendering the row.
func squadStateLabel(c components.SquadStateCode) string {
	switch c {
	case components.SquadStateEngaged:
		return "Engaged"
	case components.SquadStateScrambling:
		return "Scrambling"
	}
	return ""
}

func isSelected(selected []ecs.Entity, e ecs.Entity) bool {
	for i := range selected {
		if selected[i] == e {
			return true
		}
	}
	return false
}
