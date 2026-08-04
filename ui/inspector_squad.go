package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawInspectorSquad(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	if !ctx.World.Alive(squad) {
		TextRow(&col, &ctx.st, "(squad destroyed)", ctx.st.TextDim)
		return int32(col.Y)
	}
	roster := ctx.RosterMap.Get(squad)
	if roster == nil {
		TextRow(&col, &ctx.st, "(no roster)", ctx.st.TextDim)
		return int32(col.Y)
	}

	head := col.Band(ctx.st.RowH)
	colorChip := rl.Color{R: 80, G: 80, B: 80, A: 255}
	if ctx.SquadColor != nil {
		colorChip = ctx.SquadColor(squad)
	}
	rl.DrawRectangle(int32(head.X), int32(head.Y)+3, 12, 12, colorChip)
	TextClipped(&ctx.st, rl.Rectangle{X: head.X + 18, Y: head.Y,
		Width: head.Width - 18, Height: head.Height},
		fmt.Sprintf("Squad #%X", squad.ID()&0xFFF), ctx.st.Text)

	TextRowClipped(&col, &ctx.st, fmt.Sprintf("Members: %d / %d",
		roster.Count, components.SquadRosterSize), ctx.st.Text)

	if fd := ctx.FormationDataMap.Get(squad); fd != nil {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Formation: %s  spacing %.1f m",
			formationLabel(fd.Type), fd.Spacing), ctx.st.Text)
	}
	if mp := ctx.MacroPathMap.Get(squad); mp != nil {
		TextRowClipped(&col, &ctx.st, macroPathLabel(mp), ctx.st.Text)
		// Only show when active so the row stays quiet during normal movement.
		if mp.WaitingForStragglers {
			TextRowClipped(&col, &ctx.st, fmt.Sprintf("Waiting for stragglers (%d/%d)",
				mp.StragglerCaughtUp, mp.StragglerTotal),
				rl.Color{R: 230, G: 170, B: 90, A: 255})
		}
	}
	// Idle hides the row; Engaged / Scrambling get a coloured row so the
	// player notices reactive behaviour.
	if ctx.SquadStateMap != nil {
		if state := ctx.SquadStateMap.Get(squad); state != nil {
			if label := squadStateLabel(state.Code); label != "" {
				color := ctx.st.TextDim
				if state.Code == components.SquadStateScrambling {
					color = rl.Color{R: 230, G: 110, B: 80, A: 255}
				}
				TextRowClipped(&col, &ctx.st, "State:     "+label, color)
			}
		}
	}
	// The brain's plan: what it is doing WITH the order, in the same shape as
	// the per-unit override row — every interception is visible.
	if ctx.SquadPlanMap != nil {
		if plan := ctx.SquadPlanMap.Get(squad); plan != nil {
			if label := components.SquadPlanLabel(plan.Mode); label != "" {
				if plan.Mode == components.SquadPlanClearSeq {
					label += " - " + components.ClearPhaseLabel(plan.Phase)
					if plan.Phase == components.ClearPhaseSweep {
						label += fmt.Sprintf(" L%d", plan.Floor)
					}
				}
				TextRowClipped(&col, &ctx.st, "Plan:      "+label,
					rl.Color{R: 235, G: 195, B: 105, A: 255})
			}
		}
	}
	col.Skip(ctx.st.RowH * 0.5)

	col.Y = float32(drawQuickBadges(ctx, squad, int32(col.X), int32(col.Y), int32(col.W)))
	col.Skip(ctx.st.RowH * 0.5)

	col.Y = float32(drawInspectorOrderSection(ctx, squad, int32(col.X), int32(col.Y), int32(col.W)))
	col.Skip(ctx.st.RowH * 0.5)

	TextRow(&col, &ctx.st, "Roster:", ctx.st.TextDim)
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		drawRosterRow(ctx, &col, mem, i)
	}
	return int32(col.Y)
}

// drawRosterRow doubles as a picker: clicking narrows the selection to this
// member, which is the way to reach a man who isn't visible in the 3D view.
func drawRosterRow(ctx InspectorCtx, col *Column, mem ecs.Entity, slot uint8) {
	row := col.Band(ctx.st.RowH)
	role := roleOf(ctx, mem)

	// Selection / hover override the role tint so focus stays unambiguous.
	bg := components.RoleColor(role)
	bg.A = 90
	if isSelected(ctx.Selected, mem) {
		bg = inspectorRowSelectBG
	}
	if ctx.Hovered == mem || ctx.in.Hover(row) {
		bg = inspectorRowHoverBG
	}
	rl.DrawRectangleRec(row, bg)
	if ctx.in.Clicked(row) {
		SelectUnitRequest.Active = true
		SelectUnitRequest.Unit = mem
	}

	stance := "-"
	if st := ctx.StanceMap.Get(mem); st != nil {
		stance = stanceLabel(st.Code)
	}
	drawRoleChip(ctx, row, role,
		fmt.Sprintf("%d  #%-6X %s", slot, mem.ID()&0xFFFFFF, stance))
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

// drawQuickBadges is the read-only echo of the Behavior panel: what the squad's
// standing rules currently say, without the means to change them here. Clicking
// the row opens the panel that does. Contextual visibility, not a control.
func drawQuickBadges(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	mp := ctx.Behavior.MovementProfileMap
	er := ctx.Behavior.EngagementRulesMap
	if mp == nil || er == nil {
		return y
	}
	profile := mp.Get(squad)
	rules := er.Get(squad)
	if profile == nil || rules == nil {
		return y
	}

	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	row := col.Band(srChipH)
	labels := [4]string{
		components.PaceName(profile.Pace),
		components.StanceName(profile.Stance),
		components.EngagementModeName(rules.Mode),
		autonomyLabelFor(ctx.Behavior, squad),
	}
	hovered := ctx.in.Hover(row)
	for i, label := range labels {
		cell := SplitX(row, i, len(labels), srChipGap)
		bg := srChipBG
		if hovered {
			bg = srChipHover
		}
		rl.DrawRectangleRec(cell, bg)
		rl.DrawRectangleLinesEx(cell, 1, srChipBorder)
		TextCentered(&ctx.st, cell, label, ctx.st.TextDim)
	}
	if ctx.in.Clicked(row) {
		OpenWidgetRequest.Active = true
		OpenWidgetRequest.Panel = PanelBehavior
	}
	return int32(col.Y)
}

func macroPathLabel(mp *components.MacroPath) string {
	if !mp.HasGoal {
		return "Macro:     Idle"
	}
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
