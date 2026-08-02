package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// drawInspectorOrderSection renders the head order + up to 2 queued
// orders as an inline timeline.
func drawInspectorOrderSection(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	TextRow(&col, &ctx.st, "Orders:", ctx.st.TextDim)

	if ctx.OrderQueueMap == nil {
		TextRow(&col, &ctx.st, "  (orders unavailable)", ctx.st.TextDim)
		return int32(col.Y)
	}
	head := ctx.OrderQueueMap.Get(squad)
	if head == nil || head.First == (ecs.Entity{}) {
		TextRow(&col, &ctx.st, "  No order", ctx.st.TextDim)
		return int32(col.Y)
	}

	// HoldFire silently blocks AttackMove; the per-row pill flags it.
	eMode := components.HoldFire
	if ctx.EngagementRulesMap != nil {
		if er := ctx.EngagementRulesMap.Get(squad); er != nil {
			eMode = er.Mode
		}
	}

	cur := head.First
	drawOrderRow(ctx, &col, cur, true, eMode)
	const maxQueued = 2
	for queued := 0; queued < maxQueued; queued++ {
		ch := ctx.OrderChainMap.Get(cur)
		if ch == nil || ch.Next == (ecs.Entity{}) || !ctx.World.Alive(ch.Next) {
			break
		}
		cur = ch.Next
		drawOrderRow(ctx, &col, cur, false, eMode)
	}
	return int32(col.Y)
}

// drawOrderRow appends an "AT" pill when the order carries
// OrderParamAttackMove. HoldFire RoE strikes the pill through and adds
// a warning sub-row.
func drawOrderRow(ctx InspectorCtx, col *Column, ord ecs.Entity, active bool,
	eMode components.EngagementMode) {
	kind := ctx.OrderKindMap.Get(ord)
	target := ctx.OrderTargetMap.Get(ord)
	state := ctx.OrderStateMap.Get(ord)
	if kind == nil || target == nil || state == nil {
		TextRow(col, &ctx.st, "  (?)", ctx.st.TextDim)
		return
	}
	progress := float32(0)
	if pr := ctx.OrderProgressMap.Get(ord); pr != nil {
		progress = pr.Value
	}

	row := col.Band(ctx.st.RowH)
	drawOrderProgressBar(row, active, state.Code, progress)

	color := ctx.st.Text
	if !active {
		color = ctx.st.TextDim
	}
	if state.Code == components.OrderStateBlocked || state.Code == components.OrderStateFailed {
		color = rl.Color{R: 230, G: 110, B: 80, A: 255}
	}
	progressTxt := ""
	if progress > 0 {
		progressTxt = fmt.Sprintf(" %d%%", int(progress*100))
	}
	hasAttackMove := ctx.OrderAttackMoveMap != nil && ctx.OrderAttackMoveMap.Get(ord) != nil
	hasRoEOverride := ctx.OrderEngagementMap != nil && ctx.OrderEngagementMap.Get(ord) != nil

	// Reserve the pill strip so a long target label can't run under it.
	textRect := row
	for _, present := range [2]bool{hasAttackMove, hasRoEOverride} {
		if present {
			textRect.Width -= orderPillW + 4
		}
	}
	TextClipped(&ctx.st, textRect, fmt.Sprintf("  %-12s %-10s%s %s",
		orderKindLabel(kind.Code), orderStateLabel(state.Code), progressTxt,
		orderTargetLabel(ctx, kind.Code, target)), color)

	if hasAttackMove {
		drawAttackMovePill(ctx, orderPillRect(row, 0), eMode == components.HoldFire)
	}
	// "Hidden position" carries a HoldFire RoE override — surface it, the
	// player otherwise cannot tell it apart from a plain Occupy.
	if hasRoEOverride {
		slot := 0
		if hasAttackMove {
			slot = 1
		}
		drawRoEOverridePill(ctx, orderPillRect(row, slot))
	}
	if hasAttackMove && eMode == components.HoldFire {
		TextRowClipped(col, &ctx.st, "   AttackMove ignored - RoE is HoldFire",
			rl.Color{R: 230, G: 170, B: 90, A: 255})
	}
}

const orderPillW float32 = 24

// orderPillRect places pill `slot` from the row's right edge outwards.
func orderPillRect(row rl.Rectangle, slot int) rl.Rectangle {
	const padY float32 = 2
	return rl.Rectangle{
		X:      row.X + row.Width - orderPillW - float32(slot)*(orderPillW+4),
		Y:      row.Y + padY,
		Width:  orderPillW,
		Height: row.Height - 2*padY,
	}
}

// drawRoEOverridePill marks an order-scoped RoE override ("HF").
func drawRoEOverridePill(ctx InspectorCtx, r rl.Rectangle) {
	st := ctx.st
	st.Fill = rl.Color{R: 55, G: 65, B: 60, A: 255}
	st.Text = rl.Color{R: 150, G: 210, B: 160, A: 255}
	rl.DrawRectangleRec(r, st.Fill)
	rl.DrawRectangleLinesEx(r, 1, srChipBorder)
	TextCentered(&st, r, "HF", st.Text)
}

func drawOrderProgressBar(r rl.Rectangle, active bool,
	stateCode components.OrderStateCode, progress float32) {
	const padY float32 = 1
	fill := rl.Color{R: 80, G: 130, B: 200, A: 200}
	if !active {
		fill = rl.Color{R: 60, G: 80, B: 120, A: 180}
	}
	if stateCode == components.OrderStateBlocked || stateCode == components.OrderStateFailed {
		fill = rl.Color{R: 230, G: 110, B: 80, A: 200}
	}
	track := rl.Rectangle{X: r.X, Y: r.Y + padY, Width: r.Width, Height: r.Height - 2*padY}
	Bar(track, progress, rl.Color{R: 30, G: 35, B: 44, A: 255}, fill, 0)
	if active {
		// Bright outline for the head order so the eye locks on it.
		rl.DrawRectangleLinesEx(track, 1, rl.Color{R: 110, G: 160, B: 220, A: 220})
	}
}

// drawAttackMovePill: when blocked, dims background and strikes through.
func drawAttackMovePill(ctx InspectorCtx, r rl.Rectangle, blocked bool) {
	st := ctx.st
	st.Fill = srChipActive
	fg := contrastTextColor(st.Fill)
	if blocked {
		st.Fill = rl.Color{R: 60, G: 50, B: 50, A: 255}
		fg = rl.Color{R: 200, G: 130, B: 110, A: 255}
	}
	rl.DrawRectangleRec(r, st.Fill)
	rl.DrawRectangleLinesEx(r, 1, srChipBorder)
	TextCentered(&st, r, "AT", fg)
	if blocked {
		midY := r.Y + r.Height*0.5
		rl.DrawLineEx(
			rl.Vector2{X: r.X + 3, Y: midY},
			rl.Vector2{X: r.X + r.Width - 3, Y: midY},
			2, rl.Color{R: 230, G: 110, B: 80, A: 255})
	}
}

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

func orderTargetLabel(ctx InspectorCtx, kind components.OrderKindCode, t *components.OrderTarget) string {
	if t.Entity != (ecs.Entity{}) {
		switch kind {
		case components.OrderKindGarrison:
			return fmt.Sprintf("@ windows Bldg #%X", t.Entity.ID()&0xFFF)
		case components.OrderKindOccupyBuilding, components.OrderKindClearBuilding:
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
