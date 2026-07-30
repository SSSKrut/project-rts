package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/gen/buildings"
	"rts-go/ui"
)

var (
	colPanelBG = rl.Color{R: 24, G: 27, B: 32, A: 255}
	colPanelHR = rl.Color{R: 48, G: 54, B: 62, A: 255}
	colErr     = rl.Color{R: 235, G: 110, B: 100, A: 255}
	colWarn    = rl.Color{R: 230, G: 195, B: 90, A: 255}
	colOK      = rl.Color{R: 120, G: 200, B: 140, A: 255}
	colLabel   = rl.Color{R: 190, G: 200, B: 215, A: 235}
)

const sideNames = "SENW"

func (sb *sandbox) drawPanels() {
	in := ui.WidgetInput{
		Cursor:  rl.GetMousePosition(),
		Press:   rl.IsMouseButtonPressed(rl.MouseLeftButton),
		Enabled: true,
	}
	sb.drawParamPanel(in)
	sb.drawIssuePanel(in)
	sb.drawStatusBar()
}

func (sb *sandbox) drawParamPanel(in ui.WidgetInput) {
	h := float32(rl.GetScreenHeight())
	r := rl.Rectangle{X: 0, Y: 0, Width: leftW, Height: h}
	rl.DrawRectangleRec(r, colPanelBG)
	rl.DrawLineEx(rl.Vector2{X: leftW, Y: 0}, rl.Vector2{X: leftW, Y: h}, 1, colPanelHR)

	st := &sb.style
	col := ui.Column{X: panelPad, Y: panelPad, W: leftW - 2*panelPad, Gap: 3}
	p := &sb.params
	changed := false

	ui.Header(&col, st, "TEMPLATE")
	row := col.Row(st.RowH)
	for i, name := range templateNames {
		if ui.Chip(in, st, ui.SplitX(row, i, len(templateNames), 3), name, p.template == i) {
			p.template = i
			changed = true
		}
	}

	live := p.paramLive("kind")
	row = col.Row(st.RowH)
	for i, name := range []string{"House", "Bunker"} {
		cell := ui.SplitX(row, i, 2, 3)
		if !live {
			ui.ChipDisabled(st, cell, name)
			continue
		}
		if ui.Chip(in, st, cell, name, p.bunker == (i == 1)) {
			p.bunker = i == 1
			changed = true
		}
	}

	col.Skip(4)
	ui.Header(&col, st, "SHAPE")
	live = p.paramLive("stories")
	if sb.stepper(in, &col, "Stories", fmt.Sprintf("%d", p.stories), live) != 0 {
		p.stories = clampInt(p.stories+sb.lastStep, 1, 5)
		changed = true
	}
	if sb.stepper(in, &col, "Size X", fmt.Sprintf("%.0f", p.sizeX), live) != 0 {
		p.sizeX = clampF(p.sizeX+float32(sb.lastStep)*2, 4, 40)
		changed = true
	}
	if sb.stepper(in, &col, "Size Z", fmt.Sprintf("%.0f", p.sizeZ), live) != 0 {
		p.sizeZ = clampF(p.sizeZ+float32(sb.lastStep)*2, 4, 40)
		changed = true
	}

	ui.TextRow(&col, st, "Doors", st.TextDim)
	row = col.Row(st.RowH)
	live = p.paramLive("doors")
	for i := 0; i < 4; i++ {
		cell := ui.SplitX(row, i, 4, 3)
		label := string(sideNames[i])
		if !live {
			ui.ChipDisabled(st, cell, label)
			continue
		}
		if ui.Chip(in, st, cell, label, p.doors[i]) {
			p.doors[i] = !p.doors[i]
			changed = true
		}
	}

	row = col.Row(st.RowH)
	if p.paramLive("interior") {
		if ui.Toggle(in, st, row, "Interior walls", p.interior) {
			p.interior = !p.interior
			changed = true
		}
	} else {
		ui.ChipDisabled(st, row, "Interior walls")
	}

	col.Skip(4)
	ui.Header(&col, st, "SEED")
	row = col.Row(st.RowH)
	if ui.Chip(in, st, ui.SplitX(row, 0, 4, 3), "-", false) {
		p.seed--
		changed = true
	}
	ui.TextCentered(st, ui.SplitX(row, 1, 4, 3), fmt.Sprintf("%#x", p.seed), st.Text)
	if ui.Chip(in, st, ui.SplitX(row, 2, 4, 3), "+", false) {
		p.seed++
		changed = true
	}
	if ui.Chip(in, st, ui.SplitX(row, 3, 4, 3), "Roll", false) {
		p.seed = splitMix64(p.seed)
		changed = true
	}

	col.Skip(4)
	ui.Header(&col, st, "MATRIX")
	row = col.Row(st.RowH)
	if ui.Toggle(in, st, row, "Grid of samples", p.matrix) {
		p.matrix = !p.matrix
		changed = true
	}
	if sb.stepper(in, &col, "Cols", fmt.Sprintf("%d", p.cols), p.matrix) != 0 {
		p.cols = clampInt(p.cols+sb.lastStep, 1, 8)
		changed = true
	}
	if sb.stepper(in, &col, "Rows", fmt.Sprintf("%d", p.rows), p.matrix) != 0 {
		p.rows = clampInt(p.rows+sb.lastStep, 1, 6)
		changed = true
	}

	col.Skip(4)
	ui.Header(&col, st, "VIEW")
	row = col.Row(st.RowH)
	for i, name := range wallModeNames {
		if ui.Chip(in, st, ui.SplitX(row, i, len(wallModeNames), 3), name, sb.wallMode == i) {
			sb.wallMode = i
		}
	}

	ui.TextRow(&col, st, "Level", st.TextDim)
	nLevels := sb.maxLevels()
	n := nLevels + 1
	if n > 6 {
		n = 6
	}
	row = col.Row(st.RowH)
	if ui.Chip(in, st, ui.SplitX(row, 0, n, 3), "All", sb.levelFilter < 0) {
		sb.levelFilter = -1
	}
	for i := 0; i+1 < n; i++ {
		if ui.Chip(in, st, ui.SplitX(row, i+1, n, 3), fmt.Sprintf("L%d", i), sb.levelFilter == i) {
			sb.levelFilter = i
		}
	}

	row = col.Row(st.RowH)
	if ui.Toggle(in, st, ui.SplitX(row, 0, 2, 3), "Grid", sb.showGrid) {
		sb.showGrid = !sb.showGrid
	}
	if ui.Toggle(in, st, ui.SplitX(row, 1, 2, 3), "Box", sb.showFootprint) {
		sb.showFootprint = !sb.showFootprint
	}
	row = col.Row(st.RowH)
	if ui.Toggle(in, st, row, "Roof", sb.showRoof) {
		sb.showRoof = !sb.showRoof
	}

	col.Skip(8)
	ui.TextRow(&col, st, "MMB/RMB orbit  wheel zoom", st.TextDim)
	ui.TextRow(&col, st, "F frame  R reroll  M matrix", st.TextDim)

	if changed {
		sb.rebuild()
	}
}

// stepper draws a "label [-] value [+]" row and returns -1/0/+1. The step
// lands in sb.lastStep so callers can apply their own scale.
func (sb *sandbox) stepper(in ui.WidgetInput, col *ui.Column, label, value string, live bool) int {
	st := &sb.style
	r := col.Row(st.RowH)
	ui.Text(st, rl.Rectangle{X: r.X, Y: r.Y + 1}, label, st.TextDim)
	minus := rl.Rectangle{X: r.X + r.Width - 78, Y: r.Y, Width: 22, Height: r.Height}
	val := rl.Rectangle{X: r.X + r.Width - 54, Y: r.Y, Width: 30, Height: r.Height}
	plus := rl.Rectangle{X: r.X + r.Width - 22, Y: r.Y, Width: 22, Height: r.Height}
	if !live {
		ui.ChipDisabled(st, minus, "-")
		ui.TextCentered(st, val, value, st.TextDim)
		ui.ChipDisabled(st, plus, "+")
		return 0
	}
	ui.TextCentered(st, val, value, st.Text)
	if ui.Chip(in, st, minus, "-", false) {
		sb.lastStep = -1
		return -1
	}
	if ui.Chip(in, st, plus, "+", false) {
		sb.lastStep = 1
		return 1
	}
	return 0
}

func (sb *sandbox) drawIssuePanel(in ui.WidgetInput) {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	r := rl.Rectangle{X: w - rightW, Y: 0, Width: rightW, Height: h}
	rl.DrawRectangleRec(r, colPanelBG)
	rl.DrawLineEx(rl.Vector2{X: r.X, Y: 0}, rl.Vector2{X: r.X, Y: h}, 1, colPanelHR)

	st := &sb.style
	errs, warns := 0, 0
	for i := range sb.cells {
		errs += sb.cells[i].errors
		warns += sb.cells[i].warns
	}

	head := ui.Column{X: r.X + panelPad, Y: panelPad, W: rightW - 2*panelPad, Gap: 3}
	ui.Header(&head, st, "VALIDATION")
	verdict := fmt.Sprintf("%d errors   %d warnings", errs, warns)
	vc := colOK
	switch {
	case errs > 0:
		vc = colErr
	case warns > 0:
		vc = colWarn
	}
	ui.TextRow(&head, st, verdict, vc)

	content := rl.Rectangle{X: r.X, Y: head.Y + 4, Width: rightW, Height: h - head.Y - 4 - statusH}
	if in.Hover(content) {
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			sb.scroll.OffsetY -= wheel * 28
		}
	}
	maxOff := sb.scroll.ContentHeight - content.Height
	if maxOff < 0 {
		maxOff = 0
	}
	sb.scroll.OffsetY = clampF(sb.scroll.OffsetY, 0, maxOff)

	sc := ui.BeginScroll(content, &sb.scroll, panelPad, 2)
	if errs+warns == 0 {
		ui.TextRow(&sc.Col, st, "No issues.", colOK)
		ui.TextRow(&sc.Col, st, "", st.TextDim)
		ui.TextRow(&sc.Col, st, "Validate() has no callers in", st.TextDim)
		ui.TextRow(&sc.Col, st, "the game - this panel is its", st.TextDim)
		ui.TextRow(&sc.Col, st, "first consumer.", st.TextDim)
	}
	for ci := range sb.cells {
		c := &sb.cells[ci]
		if len(c.issues) == 0 {
			continue
		}
		ui.TextRow(&sc.Col, st, c.label, st.Header)
		for _, ci2 := range c.issues {
			row := sc.Col.Band(st.RowH)
			col := colWarn
			if ci2.iss.Severity == buildings.SeverityError {
				col = colErr
			}
			if in.Hover(row) {
				rl.DrawRectangleRec(row, st.FillHover)
			}
			ui.Text(st, rl.Rectangle{X: row.X + 6, Y: row.Y + 1},
				trim(fmt.Sprintf("%s: %s", ci2.iss.Code, ci2.iss.Message), 40), col)
			if in.Clicked(row) && ci2.planIdx < len(c.plans) {
				sb.flyTo(issueFocus(c.plans[ci2.planIdx], ci2.iss))
			}
		}
		sc.Col.Skip(4)
	}
	sc.End()
}

func (sb *sandbox) drawStatusBar() {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	r := rl.Rectangle{X: 0, Y: h - statusH, Width: w, Height: statusH}
	rl.DrawRectangleRec(r, colPanelBG)
	rl.DrawLineEx(rl.Vector2{X: 0, Y: r.Y}, rl.Vector2{X: w, Y: r.Y}, 1, colPanelHR)

	st := &sb.style
	var walls, floors, stairs, levels, roofs, split int
	for i := range sb.cells {
		c := &sb.cells[i]
		for _, p := range c.plans {
			walls += len(p.Walls)
			floors += len(p.Floors)
			stairs += len(p.Stairs)
			levels += len(p.Levels)
			roofs += len(p.Roofs)
		}
		if c.oversize {
			split++
		}
	}

	col := ui.Column{X: panelPad, Y: r.Y + 5, W: w - 2*panelPad, Gap: 1}
	line1 := fmt.Sprintf("%s   cells %d   plans %d   walls %d   floors %d   stairs %d   levels %d   roofs %d",
		templateNames[sb.params.template], len(sb.cells), totalPlans(sb.cells),
		walls, floors, stairs, levels, roofs)
	ui.TextRow(&col, st, line1, st.Text)

	if len(sb.cells) > 0 {
		c := &sb.cells[0]
		size := fmt.Sprintf("%.0f x %.0f m", c.bounds.MaxX-c.bounds.MinX, c.bounds.MaxZ-c.bounds.MinZ)
		ui.TextRow(&col, st, fmt.Sprintf("first cell: %s   %s   top %.1f m   (chunk %.0f m)",
			c.label, size, c.topY, components.ChunkSize), st.TextDim)
	}
	// Oversize is the generator's problem; a mere boundary crossing is the
	// sandbox's own placement and says nothing about the plan.
	if split > 0 {
		ui.TextRow(&col, st, fmt.Sprintf(
			"%d cell(s) larger than one chunk - children could not share a Pos.Chunk", split), colFpSplit)
	}
}

// drawCellLabels writes each sample's label above its footprint. Matrix mode
// is unreadable without it.
func (sb *sandbox) drawCellLabels(cam rl.Camera3D, vp rl.Rectangle) {
	if len(sb.cells) < 2 {
		return
	}
	st := &sb.style
	// Labels are drawn over a dense grid; drop any that would land on one
	// already placed rather than stacking unreadable text.
	var taken []rl.Rectangle
	for i := range sb.cells {
		c := &sb.cells[i]
		anchor := rl.Vector3{
			X: 0.5 * (c.bounds.MinX + c.bounds.MaxX),
			Y: c.topY + 1.5,
			Z: 0.5 * (c.bounds.MinZ + c.bounds.MaxZ),
		}
		s, ok := sb.worldToScreen(anchor, cam, vp)
		if !ok || !pointIn(s, vp) {
			continue
		}
		size := rl.MeasureTextEx(st.Font, c.label, st.FontSize, 1)
		box := rl.Rectangle{
			X: s.X - size.X*0.5 - 3, Y: s.Y - size.Y - 2,
			Width: size.X + 6, Height: size.Y + 3,
		}
		if overlapsAny(box, taken) {
			continue
		}
		taken = append(taken, box)
		rl.DrawRectangleRec(box, rl.Color{R: 20, G: 23, B: 28, A: 200})
		ui.Text(st, rl.Rectangle{X: box.X + 3, Y: box.Y + 1}, c.label, colLabel)
	}
}

func overlapsAny(r rl.Rectangle, rs []rl.Rectangle) bool {
	for _, o := range rs {
		if r.X < o.X+o.Width && r.X+r.Width > o.X &&
			r.Y < o.Y+o.Height && r.Y+r.Height > o.Y {
			return true
		}
	}
	return false
}

func totalPlans(cells []cell) int {
	n := 0
	for i := range cells {
		n += len(cells[i].plans)
	}
	return n
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
