package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// drawInspectorOrderSection renders the squad's current Order + up to 2
// queued orders as an inline timeline: each row carries a progress bar +
// kind / state / target text + optional AT pill. Returns the y-coord after
// the section. Phase 15 M15.C.5 reformat.
func drawInspectorOrderSection(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	drawText(ctx.Font, "Orders:", x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH

	if ctx.OrderQueueMap == nil {
		drawText(ctx.Font, "  (orders unavailable)",
			x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	head := ctx.OrderQueueMap.Get(squad)
	if head == nil || head.First == (ecs.Entity{}) {
		drawText(ctx.Font, "  No order", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}

	// Read squad's RoE so each row can flag AttackMove that the engagement
	// mode silently overrides (M15.C.4). HoldFire blocks opportunistic fire
	// even when the order carries the AttackMove flag.
	eMode := components.HoldFire
	if ctx.EngagementRulesMap != nil {
		if er := ctx.EngagementRulesMap.Get(squad); er != nil {
			eMode = er.Mode
		}
	}

	cur := head.First
	y = drawOrderRow(ctx, cur, true, x, y, width, eMode)
	const maxQueued = 2
	queued := 0
	for queued < maxQueued {
		ch := ctx.OrderChainMap.Get(cur)
		if ch == nil || ch.Next == (ecs.Entity{}) || !ctx.World.Alive(ch.Next) {
			break
		}
		cur = ch.Next
		queued++
		y = drawOrderRow(ctx, cur, false, x, y, width, eMode)
	}
	return y
}

// drawOrderRow renders one timeline row: progress bar + kind / state /
// target. `active` highlights the head order's row background. When the
// order carries OrderParamAttackMove an "AT" pill is appended on the right;
// HoldFire RoE strikes the pill through and adds a warning sub-row. Returns
// the new y.
func drawOrderRow(ctx InspectorCtx, ord ecs.Entity, active bool, x, y, width int32, eMode components.EngagementMode) int32 {
	kind := ctx.OrderKindMap.Get(ord)
	target := ctx.OrderTargetMap.Get(ord)
	state := ctx.OrderStateMap.Get(ord)
	if kind == nil || target == nil || state == nil {
		drawText(ctx.Font, "  (?)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	progress := float32(0)
	if pr := ctx.OrderProgressMap.Get(ord); pr != nil {
		progress = pr.Value
	}
	drawOrderProgressBar(x, y, width, active, state.Code, progress)

	label := orderKindLabel(kind.Code)
	stateLbl := orderStateLabel(state.Code)
	targetLbl := orderTargetLabel(ctx, kind.Code, target)
	color := inspectorText
	if !active {
		color = inspectorTextDim
	}
	if state.Code == components.OrderStateBlocked || state.Code == components.OrderStateFailed {
		color = rl.Color{R: 230, G: 110, B: 80, A: 255}
	}
	progressTxt := ""
	if progress > 0 {
		progressTxt = fmt.Sprintf(" %d%%", int(progress*100))
	}
	drawText(ctx.Font, fmt.Sprintf("  %-12s %-10s%s %s",
		label, stateLbl, progressTxt, targetLbl),
		x, y, inspectorFontSize, color)

	hasAttackMove := ctx.OrderAttackMoveMap != nil && ctx.OrderAttackMoveMap.Get(ord) != nil
	if hasAttackMove {
		blocked := eMode == components.HoldFire
		drawAttackMovePill(ctx.Font, x, y, width, blocked)
		if blocked {
			y += inspectorRowH
			drawText(ctx.Font, "   AttackMove ignored - RoE is HoldFire",
				x, y, inspectorFontSize, rl.Color{R: 230, G: 170, B: 90, A: 255})
		}
	}
	return y + inspectorRowH
}

// drawOrderProgressBar paints the row's progress bar underlay - a thin
// filled rectangle behind the text that visualises OrderProgress.Value. The
// active head order gets a brighter fill; queued rows get a dimmer track.
// State-driven colour (blocked / failed render in warning orange).
func drawOrderProgressBar(rowX, rowY, width int32, active bool, stateCode components.OrderStateCode, progress float32) {
	const padY int32 = 1
	track := rl.Color{R: 30, G: 35, B: 44, A: 255}
	fill := rl.Color{R: 80, G: 130, B: 200, A: 200}
	if !active {
		fill = rl.Color{R: 60, G: 80, B: 120, A: 180}
	}
	if stateCode == components.OrderStateBlocked || stateCode == components.OrderStateFailed {
		fill = rl.Color{R: 230, G: 110, B: 80, A: 200}
	}
	rowH := inspectorRowH - 2*padY
	rl.DrawRectangle(rowX, rowY+padY, width, rowH, track)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	fillW := int32(float32(width) * progress)
	if fillW > 0 {
		rl.DrawRectangle(rowX, rowY+padY, fillW, rowH, fill)
	}
	if active {
		// Bright outline for the head order so the eye locks on it.
		rl.DrawRectangleLines(rowX, rowY+padY, width, rowH, rl.Color{R: 110, G: 160, B: 220, A: 220})
	}
}

// drawAttackMovePill renders a small "AT" pill at the right edge of the row.
// When `blocked` is true, the pill background is dimmed and a strike-through
// line runs across it; the caller adds an explanatory sub-row below.
func drawAttackMovePill(font rl.Font, rowX, rowY, width int32, blocked bool) {
	const pillW int32 = 24
	const pillPadY int32 = 2
	pillX := rowX + width - pillW
	pillY := rowY + pillPadY
	pillH := inspectorRowH - 2*pillPadY
	bg := srChipActive
	fg := contrastTextColor(bg)
	if blocked {
		bg = rl.Color{R: 60, G: 50, B: 50, A: 255}
		fg = rl.Color{R: 200, G: 130, B: 110, A: 255}
	}
	rl.DrawRectangle(pillX, pillY, pillW, pillH, bg)
	rl.DrawRectangleLines(pillX, pillY, pillW, pillH, srChipBorder)
	size := rl.MeasureTextEx(font, "AT", float32(inspectorFontSize), 1)
	rl.DrawTextEx(font, "AT", rl.Vector2{
		X: float32(pillX) + (float32(pillW)-size.X)*0.5,
		Y: float32(pillY) + (float32(pillH)-size.Y)*0.5,
	}, float32(inspectorFontSize), 1, fg)
	if blocked {
		midY := float32(pillY) + float32(pillH)*0.5
		rl.DrawLineEx(
			rl.Vector2{X: float32(pillX) + 3, Y: midY},
			rl.Vector2{X: float32(pillX+pillW) - 3, Y: midY},
			2, rl.Color{R: 230, G: 110, B: 80, A: 255})
	}
}

// orderKindLabel reads the spec table's Name. Phase 14.5 M14.5.0 - old
// hand-maintained switch replaced.
func orderKindLabel(k components.OrderKindCode) string {
	if spec := components.SpecForOrderKind(k); spec.Name != "" {
		return spec.Name
	}
	return "?"
}

func orderStateLabel(s components.OrderStateCode) string {
	switch s {
	case components.OrderStateIssued:
		return "issued"
	case components.OrderStateInProgress:
		return "in prog"
	case components.OrderStateBlocked:
		return "blocked"
	case components.OrderStateCompleted:
		return "done"
	case components.OrderStateCancelled:
		return "cancel"
	case components.OrderStateFailed:
		return "failed"
	}
	return "?"
}

// orderTargetLabel produces a short string identifying the order's target -
// "@ (x, z)" for Pos targets, "@ Building #X" / "@ Trench #N" for entity
// targets. Avoids floats with sub-metre noise.
func orderTargetLabel(ctx InspectorCtx, kind components.OrderKindCode, t *components.OrderTarget) string {
	if t.Entity != (ecs.Entity{}) {
		switch kind {
		case components.OrderKindGarrison:
			return fmt.Sprintf("@ Building #%X", t.Entity.ID()&0xFFF)
		case components.OrderKindOccupyTrench:
			idx := -1
			if ctx.TrenchRootMap != nil {
				if r := ctx.TrenchRootMap.Get(t.Entity); r != nil {
					idx = r.Index
				}
			}
			if idx >= 0 {
				return fmt.Sprintf("@ Trench %d", idx)
			}
			return fmt.Sprintf("@ Trench #%X", t.Entity.ID()&0xFFF)
		}
	}
	wx := float32(t.Pos.Chunk.X)*components.ChunkSize + t.Pos.Local.X
	wz := float32(t.Pos.Chunk.Z)*components.ChunkSize + t.Pos.Local.Z
	return fmt.Sprintf("@ (%.0f, %.0f)", wx, wz)
}
