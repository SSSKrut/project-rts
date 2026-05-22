package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// FormationEditorCtx bundles the ECS maps + world handle the editor needs.
// Built once at startup; passed by value to NewFormationEditor. Presets is
// a pointer to the global FormationPresets resource so Save / Apply
// share state with every other open editor.
//
// Tuning fields (zero = use built-in defaults):
//
//   - MinPxPerM / MaxPxPerM bound the wheel-zoom range. Lower MinPxPerM
//     lets the user zoom out to fit huge custom formations; higher
//     MaxPxPerM lets fine drags at half-metre precision.
//   - SnapMeters is the drag-snap step; default 0.5 m. Set to 0 to disable
//     snapping (free placement).
//   - SelectionFn (optional) returns the currently selected squad each
//     frame. When set, the editor re-binds to it so the floating /
//     docked widget follows the player's selection instead of staying
//     pinned to the squad alive at open time. Returning a zero entity
//     leaves the previous bind alone (avoids flicker between selections).
type FormationEditorCtx struct {
	World          *ecs.World
	RosterMap      *ecs.Map[components.CommandRoster]
	FormationMap   *ecs.Map[components.FormationData]
	OrientMap      *ecs.Map[components.FormationOrientation]
	CustomSlotsMap *ecs.Map[components.FormationCustomSlots]
	RoleMap        *ecs.Map[components.UnitRole]
	PosMap         *ecs.Map[components.WorldPos]
	SquadColor     func(ent ecs.Entity) rl.Color
	Presets        *components.FormationPresets

	MinPxPerM   float32
	MaxPxPerM   float32
	SnapMeters  float32
	SelectionFn func() ecs.Entity
}

// FormationEditor renders the concentric-rings UI for one squad. Owns its
// per-instance drag + zoom state. Plug into a FloatingPanel via .Render.
type FormationEditor struct {
	Squad ecs.Entity
	Ctx   FormationEditorCtx

	// Drag state: slot index of the member being dragged (-1 = none).
	draggedSlot int

	// Mouse-wheel zoom. Stored as pixels per metre; the canvas adapts
	// rings + ring-max as the panel is resized so the same PxPerM works
	// at any size. Zero on first frame → seeded from canvas size.
	PxPerM float32

	// Kind/preset dropdown menu state (only relevant for infantry).
	kindMenuOpen bool
	kindMenuRect rl.Rectangle
}

// NewFormationEditor constructs an editor for one squad.
func NewFormationEditor(squad ecs.Entity, ctx FormationEditorCtx) *FormationEditor {
	return &FormationEditor{Squad: squad, Ctx: ctx, draggedSlot: -1}
}

const (
	feHeaderH       float32 = 32
	feDotRadius     float32 = 9
	feDotPickRadius float32 = 12
	feSnapMeters    float32 = 0.5
	feCompassPad    float32 = 18
	// Wheel-zoom range. Built-in defaults; FormationEditorCtx can override.
	// Lower bound = 2 px/m (~half-screen radius of 60+ m) so the user can
	// lay out wide loose / vehicle formations without clipping members at
	// the edge. Upper bound = 200 px/m for half-metre dot-snap precision.
	fePxPerMMin float32 = 2
	fePxPerMMax float32 = 200
	feZoomStep  float32 = 1.15
)

var (
	feHeaderBG      = rl.Color{R: 22, G: 26, B: 32, A: 255}
	feCanvasBG      = rl.Color{R: 14, G: 18, B: 24, A: 255}
	feRingColor     = rl.Color{R: 50, G: 60, B: 78, A: 180}
	feRingLabelColr = rl.Color{R: 110, G: 120, B: 132, A: 200}
	feAxisColor     = rl.Color{R: 40, G: 50, B: 65, A: 200}
	feCompassColor  = rl.Color{R: 200, G: 215, B: 230, A: 230}
	feMovementClr   = rl.Color{R: 100, G: 200, B: 240, A: 230}
	feDotBorder     = rl.Color{R: 240, G: 245, B: 250, A: 255}
	feDotBorderDrag = rl.Color{R: 255, G: 220, B: 90, A: 255}
	feText          = rl.Color{R: 220, G: 226, B: 232, A: 255}
	feTextDim       = rl.Color{R: 140, G: 150, B: 160, A: 255}
	feBtnBG         = rl.Color{R: 38, G: 46, B: 60, A: 255}
	feBtnHotBG      = rl.Color{R: 60, G: 90, B: 130, A: 255}
	feBtnActiveBG   = rl.Color{R: 90, G: 130, B: 200, A: 255}
	feBtnBorder     = rl.Color{R: 80, G: 90, B: 105, A: 255}
)

// DrawPanel renders the editor inside a workspace panel. Mirrors the
// DrawInspector / DrawTimelinePanel signature so main.go can dispatch on
// PanelID. lmbPress mirrors main.go's `chromeBusy()`-gated press edge.
// Workspace flavour ignores the editor's close-request (Render's return
// value) because the chevron "Close pane" already covers panel teardown.
func (e *FormationEditor) DrawPanel(panel Panel, font rl.Font, cursor rl.Vector2, lmbPress bool) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, feCanvasBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()
	_ = e.Render(content, cursor, font, lmbPress)
}

// Render is the FloatingRenderFn entry point. Returns true when the editor
// wants to close itself (squad no longer alive, AND the editor is hosted
// as a floating panel - workspace embedding ignores the return).
func (e *FormationEditor) Render(content rl.Rectangle, cursor rl.Vector2, font rl.Font, lmbPress bool) bool {
	if e == nil || e.Ctx.World == nil {
		return true
	}
	// Follow the active selection when the host wired a SelectionFn -
	// rebinding here keeps the floating + workspace editor in sync with
	// the player's current squad rather than the one alive at open-time.
	// Returning a zero squad means "keep what you have" so the panel
	// doesn't flicker between selections.
	if e.Ctx.SelectionFn != nil {
		if s := e.Ctx.SelectionFn(); s != (ecs.Entity{}) && e.Ctx.World.Alive(s) && s != e.Squad {
			e.Squad = s
			e.draggedSlot = -1
			e.kindMenuOpen = false
		}
	}
	if e.Squad == (ecs.Entity{}) || !e.Ctx.World.Alive(e.Squad) {
		// Workspace host calls e.RenderEmbedded which short-circuits this
		// branch before we get here; floating host treats true as "close
		// the panel". When wired with SelectionFn the editor instead shows
		// a placeholder until a new selection arrives.
		if e.Ctx.SelectionFn != nil {
			e.drawEmpty(content, font)
			return false
		}
		return true
	}
	roster := e.Ctx.RosterMap.Get(e.Squad)
	fd := e.Ctx.FormationMap.Get(e.Squad)
	if roster == nil || fd == nil {
		if e.Ctx.SelectionFn != nil {
			e.drawEmpty(content, font)
			return false
		}
		return true
	}

	header := rl.Rectangle{
		X:      content.X,
		Y:      content.Y,
		Width:  content.Width,
		Height: feHeaderH,
	}
	headerConsumed := e.drawHeader(header, cursor, lmbPress, font, fd)

	canvas := rl.Rectangle{
		X:      content.X + 4,
		Y:      content.Y + feHeaderH + 4,
		Width:  content.Width - 8,
		Height: content.Height - feHeaderH - 8,
	}
	canvasLMB := lmbPress && !headerConsumed && !e.kindMenuOpen
	if canvas.Width > 0 && canvas.Height > 0 {
		e.drawCanvas(canvas, cursor, font, roster, fd, canvasLMB)
	}

	// Kind/preset popup is drawn last so it overlays the canvas. Returns
	// true when an LMB landed on an item or outside the popup; in either
	// case the popup closes and the canvas drag-start handler stayed
	// gated above.
	if e.kindMenuOpen {
		e.drawKindMenu(content, cursor, lmbPress && !headerConsumed, font, fd, roster)
	}
	return false
}

// drawHeader paints the top strip: title + kind dropdown + orientation
// toggle. Returns true when a header click was consumed (used to gate the
// canvas drag-start so the same press doesn't also pick a dot).
func (e *FormationEditor) drawHeader(r rl.Rectangle, cursor rl.Vector2, lmbPress bool, font rl.Font, fd *components.FormationData) bool {
	rl.DrawRectangleRec(r, feHeaderBG)
	const titleSize int32 = 13
	rl.DrawTextEx(font, "Formation",
		rl.Vector2{X: r.X + 8, Y: r.Y + (r.Height-float32(titleSize))*0.5},
		float32(titleSize), 1.0, feText)

	// Orientation toggle on the right.
	btnW := float32(70)
	btnH := r.Height - 8
	btnY := r.Y + 4
	btnX := r.X + r.Width - btnW*2 - 8
	mvtRect := rl.Rectangle{X: btnX, Y: btnY, Width: btnW, Height: btnH}
	nthRect := rl.Rectangle{X: btnX + btnW, Y: btnY, Width: btnW, Height: btnH}
	mode := e.currentOrient()
	drawFeBtn(font, mvtRect, "Movement", cursor, mode == components.OrientMovement)
	drawFeBtn(font, nthRect, "North", cursor, mode == components.OrientNorth)

	// Kind dropdown button between title and toggle.
	kindLabel := e.kindLabelFor(fd)
	kindBtnW := float32(120)
	kindRect := rl.Rectangle{X: r.X + 90, Y: btnY, Width: kindBtnW, Height: btnH}
	drawFeBtn(font, kindRect, kindLabel+"  v", cursor, e.kindMenuOpen)
	e.kindMenuRect = kindRect

	if lmbPress {
		if pointInRect(cursor, mvtRect) {
			e.setOrient(components.OrientMovement)
			return true
		}
		if pointInRect(cursor, nthRect) {
			e.setOrient(components.OrientNorth)
			return true
		}
		if pointInRect(cursor, kindRect) {
			e.kindMenuOpen = !e.kindMenuOpen
			return true
		}
	}
	return false
}

// kindLabelFor returns the dropdown button text - reflects either the
// active preset name (when CustomSlots is present), the preset that was
// last applied, or the kind enum's name for kind-based formations.
func (e *FormationEditor) kindLabelFor(fd *components.FormationData) string {
	if e.Ctx.CustomSlotsMap != nil && e.Ctx.CustomSlotsMap.Has(e.Squad) {
		return "Custom"
	}
	switch fd.Type {
	case components.FormationLine:
		return "Line"
	case components.FormationColumn:
		return "Column"
	case components.FormationWedge:
		return "Wedge"
	case components.FormationLoose:
		return "Loose"
	}
	return "?"
}

// drawCanvas paints concentric rings, axis, compass, and member dots; also
// handles dot drag (begin/update/end) which mutates CustomSlots.
func (e *FormationEditor) drawCanvas(canvas rl.Rectangle, cursor rl.Vector2,
	font rl.Font, roster *components.CommandRoster, fd *components.FormationData, lmbPress bool) {
	rl.DrawRectangleRec(canvas, feCanvasBG)
	cx := canvas.X + canvas.Width*0.5
	cy := canvas.Y + canvas.Height*0.5
	halfPx := canvas.Width
	if canvas.Height < halfPx {
		halfPx = canvas.Height
	}
	halfPx = halfPx*0.5 - feCompassPad
	if halfPx < 10 {
		halfPx = 10
	}

	// Seed PxPerM on first paint so ~6 m fits the initial canvas. After
	// that the user controls it with the wheel.
	if e.PxPerM <= 0 {
		e.PxPerM = halfPx / 6
	}
	// Wheel zoom while cursor inside canvas (and no drag in progress).
	minPx, maxPx := e.zoomRange()
	if pointInRect(cursor, canvas) && e.draggedSlot < 0 {
		if w := rl.GetMouseWheelMove(); w != 0 {
			factor := feZoomStep
			if w < 0 {
				factor = 1 / feZoomStep
			}
			for steps := int(absF32(w)); steps > 0; steps-- {
				e.PxPerM *= factor
			}
			if e.PxPerM < minPx {
				e.PxPerM = minPx
			}
			if e.PxPerM > maxPx {
				e.PxPerM = maxPx
			}
		}
	}
	pxPerM := e.PxPerM
	maxMeters := halfPx / pxPerM

	// Concentric meter rings + labels - count adapts to current zoom so we
	// always see at least 3 rings and at most ~10.
	step := chooseRingStep(maxMeters)
	for m := step; m <= maxMeters; m += step {
		rl.DrawCircleLines(int32(cx), int32(cy), m*pxPerM, feRingColor)
		var label string
		if step >= 1 {
			label = fmt.Sprintf("%dm", int(m+0.5))
		} else {
			label = fmt.Sprintf("%.1fm", m)
		}
		const sz int32 = 10
		w := rl.MeasureTextEx(font, label, float32(sz), 1.0).X
		rl.DrawTextEx(font, label,
			rl.Vector2{X: cx + m*pxPerM - w - 3, Y: cy + 2},
			float32(sz), 1.0, feRingLabelColr)
	}
	// Cross-hair axes.
	rl.DrawLine(int32(canvas.X), int32(cy), int32(canvas.X+canvas.Width), int32(cy), feAxisColor)
	rl.DrawLine(int32(cx), int32(canvas.Y), int32(cx), int32(canvas.Y+canvas.Height), feAxisColor)

	// Compass / forward arrow at top.
	e.drawCompass(cx, canvas.Y+feCompassPad, font, fd)

	// Drag handling first - resolve which slot the cursor would grab and
	// whether to start/update/end the drag this frame.
	lmbDown := rl.IsMouseButtonDown(rl.MouseButtonLeft)
	if e.draggedSlot >= 0 {
		if !lmbDown {
			e.draggedSlot = -1
		} else if int(e.draggedSlot) < int(roster.Count) {
			localX := (cursor.X - cx) / pxPerM
			localY := -(cursor.Y - cy) / pxPerM
			snap := e.snapStep()
			localX = snapTo(localX, snap)
			localY = snapTo(localY, snap)
			if localX > maxMeters {
				localX = maxMeters
			}
			if localX < -maxMeters {
				localX = -maxMeters
			}
			if localY > maxMeters {
				localY = maxMeters
			}
			if localY < -maxMeters {
				localY = -maxMeters
			}
			e.setSlot(uint8(e.draggedSlot), rl.Vector2{X: localX, Y: localY})
		}
	}

	// Draw + hit-test dots.
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !e.Ctx.World.Alive(mem) {
			continue
		}
		slot := e.slotOffset(i, fd)
		dotX := cx + slot.X*pxPerM
		dotY := cy - slot.Y*pxPerM
		// Constrain to canvas.
		col := rl.Color{R: 160, G: 200, B: 240, A: 255}
		if e.Ctx.RoleMap != nil {
			if r := e.Ctx.RoleMap.Get(mem); r != nil {
				col = components.RoleColor(r.Kind)
			}
		}
		border := feDotBorder
		if int(i) == e.draggedSlot {
			border = feDotBorderDrag
		}
		if i == 0 {
			rl.DrawRectangle(int32(dotX-feDotRadius), int32(dotY-feDotRadius),
				int32(feDotRadius*2), int32(feDotRadius*2), col)
			rl.DrawRectangleLines(int32(dotX-feDotRadius), int32(dotY-feDotRadius),
				int32(feDotRadius*2), int32(feDotRadius*2), border)
		} else {
			rl.DrawCircle(int32(dotX), int32(dotY), feDotRadius, col)
			rl.DrawCircleLines(int32(dotX), int32(dotY), feDotRadius, border)
		}
		// Slot index label.
		const sz int32 = 11
		label := fmt.Sprintf("%d", i)
		w := rl.MeasureTextEx(font, label, float32(sz), 1.0).X
		rl.DrawTextEx(font, label,
			rl.Vector2{X: dotX - w*0.5, Y: dotY - float32(sz)*0.5},
			float32(sz), 1.0, contrastTextColor(col))

		// Drag-start: LMB pressed on this dot, no drag in progress. Slot
		// 0 (commander) is locked at centre for infantry-only squads —
		// the formation rotates around the leader and pulling them off
		// centre would orphan the radial coordinate frame. Mixed squads
		// (with vehicles, Phase 19+) unlock slot 0 because vehicle leads
		// often anchor off-centre.
		canDrag := i != 0 || e.allowsCommanderDrag(roster)
		if canDrag && e.draggedSlot < 0 && lmbPress {
			dx := cursor.X - dotX
			dy := cursor.Y - dotY
			if dx*dx+dy*dy <= feDotPickRadius*feDotPickRadius {
				e.draggedSlot = int(i)
				// On first drag, materialise CustomSlots seeded from the
				// current formation kind so existing layout doesn't snap
				// to zero.
				e.ensureCustomSlots(roster, fd)
			}
		}
	}
}

// drawCompass paints the forward indicator at the top of the canvas. North
// mode shows a compass "N" arrow; Movement mode shows a cyan march arrow.
func (e *FormationEditor) drawCompass(cx, topY float32, font rl.Font, fd *components.FormationData) {
	mode := e.currentOrient()
	col := feCompassColor
	label := "N"
	if mode == components.OrientMovement {
		col = feMovementClr
		label = "MARCH"
	}
	// Arrow pointing up from (cx, topY+12) to (cx, topY-4).
	tip := rl.Vector2{X: cx, Y: topY - 6}
	tail := rl.Vector2{X: cx, Y: topY + 14}
	rl.DrawLineEx(tail, tip, 2.5, col)
	rl.DrawTriangle(
		rl.Vector2{X: cx - 5, Y: topY + 2},
		rl.Vector2{X: cx + 5, Y: topY + 2},
		tip, col)
	const sz int32 = 11
	w := rl.MeasureTextEx(font, label, float32(sz), 1.0).X
	rl.DrawTextEx(font, label,
		rl.Vector2{X: cx - w*0.5, Y: topY + 18},
		float32(sz), 1.0, col)
	_ = fd
}

// slotOffset returns the squad-local XY offset (X = right, Y = forward) for
// rendering slot `i`. Prefers CustomSlots if present; otherwise computes
// from FormationOffset with forward = (0,0,1) so the canvas always shows
// the formation in its "canonical" upright pose.
func (e *FormationEditor) slotOffset(i uint8, fd *components.FormationData) rl.Vector2 {
	if e.Ctx.CustomSlotsMap != nil {
		if cs := e.Ctx.CustomSlotsMap.Get(e.Squad); cs != nil {
			return cs.Slots[i]
		}
	}
	// Compute using canonical forward (+Z). FormationOffset returns world
	// XZ; we map world.X → slot.X (right), world.Z → slot.Y (forward).
	wx, wz := formationOffsetCanonical(fd.Type, i, fd.Spacing)
	return rl.Vector2{X: wx, Y: wz}
}

// formationOffsetCanonical computes the kind-based offset in canonical
// orientation (forward = +Z). Duplicates the right/forward maths from
// systems.FormationOffset so this UI package doesn't depend on systems.
func formationOffsetCanonical(kind components.FormationKind, slot uint8, spacing float32) (float32, float32) {
	if slot == 0 {
		return 0, 0
	}
	k := int(slot)
	switch kind {
	case components.FormationLine:
		side := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		return side * sign * spacing, 0
	case components.FormationColumn:
		return 0, -float32(k) * spacing
	case components.FormationWedge:
		row := float32((k + 1) / 2)
		sign := float32(1)
		if k%2 == 0 {
			sign = -1
		}
		return row * sign * spacing, -row * spacing
	case components.FormationLoose:
		// Deterministic radial scatter matching the systems-side hash.
		hash := uint32(slot)*2654435761 ^ 0x9e3779b9
		ang := float32(hash%360) * (3.14159265 / 180)
		r := float32(1+(hash/360)%3) * spacing
		return r * float32(math.Cos(float64(ang))), r * float32(math.Sin(float64(ang)))
	}
	return 0, 0
}

// ensureCustomSlots adds CustomSlots to the squad with current per-kind
// offsets pre-filled, so a drag doesn't snap members to zero.
func (e *FormationEditor) ensureCustomSlots(roster *components.CommandRoster, fd *components.FormationData) {
	if e.Ctx.CustomSlotsMap == nil {
		return
	}
	if e.Ctx.CustomSlotsMap.Has(e.Squad) {
		return
	}
	var cs components.FormationCustomSlots
	for i := uint8(0); i < components.SquadRosterSize; i++ {
		if i < roster.Count {
			wx, wz := formationOffsetCanonical(fd.Type, i, fd.Spacing)
			cs.Slots[i] = rl.Vector2{X: wx, Y: wz}
		}
	}
	e.Ctx.CustomSlotsMap.Add(e.Squad, &cs)
}

// setSlot writes a new offset for the given slot, materialising
// CustomSlots if needed.
func (e *FormationEditor) setSlot(i uint8, offset rl.Vector2) {
	if e.Ctx.CustomSlotsMap == nil {
		return
	}
	cs := e.Ctx.CustomSlotsMap.Get(e.Squad)
	if cs == nil {
		return
	}
	cs.Slots[i] = offset
}

// drawKindMenu renders the dropdown that opens under the Kind button.
// Items: predefined kinds → saved presets → "Save current as preset".
// LMB on an item applies the action and closes the menu. LMB outside the
// menu (and outside the Kind button itself) also closes.
func (e *FormationEditor) drawKindMenu(content rl.Rectangle, cursor rl.Vector2,
	lmbPress bool, font rl.Font, fd *components.FormationData, roster *components.CommandRoster) {
	const itemH float32 = 22
	const headerSz int32 = 11
	const itemSz int32 = 12
	const padX float32 = 8
	const padY float32 = 4
	const minW float32 = 180

	predefined := []struct {
		Label string
		Kind  components.FormationKind
	}{
		{"Line", components.FormationLine},
		{"Column", components.FormationColumn},
		{"Wedge", components.FormationWedge},
		{"Loose", components.FormationLoose},
	}
	presets := []components.FormationPreset{}
	if e.Ctx.Presets != nil {
		presets = e.Ctx.Presets.List
	}
	// Vertical layout: header / kinds / header / presets / footer button.
	rows := 0
	rows++ // "Predefined" header
	rows += len(predefined)
	rows++ // "Presets" header
	if len(presets) == 0 {
		rows++ // "(none)" placeholder
	} else {
		rows += len(presets)
	}
	rows++ // separator
	rows++ // "Save current as preset" footer

	width := minW
	height := float32(rows)*itemH + 2*padY
	x := e.kindMenuRect.X
	y := e.kindMenuRect.Y + e.kindMenuRect.Height + 2
	// Clamp menu within the panel content.
	if x+width > content.X+content.Width-4 {
		x = content.X + content.Width - 4 - width
	}
	if y+height > content.Y+content.Height-4 {
		y = e.kindMenuRect.Y - height - 2 // open upward if no space below
	}
	menu := rl.Rectangle{X: x, Y: y, Width: width, Height: height}
	rl.DrawRectangleRec(menu, feBtnBG)
	rl.DrawRectangleLinesEx(menu, 1, feBtnBorder)

	// Outside click closes the menu (caught before processing items).
	if lmbPress && !pointInRect(cursor, menu) && !pointInRect(cursor, e.kindMenuRect) {
		e.kindMenuOpen = false
		return
	}

	rowY := y + padY
	drawMenuHeader := func(label string) {
		rl.DrawTextEx(font, label,
			rl.Vector2{X: x + padX, Y: rowY + (itemH-float32(headerSz))*0.5},
			float32(headerSz), 1.0, feTextDim)
		rowY += itemH
	}
	drawMenuItem := func(label string, active bool) bool {
		row := rl.Rectangle{X: x + 1, Y: rowY, Width: width - 2, Height: itemH}
		hot := pointInRect(cursor, row)
		switch {
		case active:
			rl.DrawRectangleRec(row, feBtnActiveBG)
		case hot:
			rl.DrawRectangleRec(row, feBtnHotBG)
		}
		rl.DrawTextEx(font, label,
			rl.Vector2{X: x + padX, Y: rowY + (itemH-float32(itemSz))*0.5},
			float32(itemSz), 1.0, feText)
		rowY += itemH
		return lmbPress && hot
	}

	drawMenuHeader("Predefined")
	for _, p := range predefined {
		active := !e.Ctx.CustomSlotsMap.Has(e.Squad) && fd.Type == p.Kind
		if drawMenuItem(p.Label, active) {
			e.applyKind(p.Kind, fd)
			e.kindMenuOpen = false
			return
		}
	}
	drawMenuHeader("Presets")
	if len(presets) == 0 {
		row := rl.Rectangle{X: x + 1, Y: rowY, Width: width - 2, Height: itemH}
		rl.DrawTextEx(font, "  (none yet)",
			rl.Vector2{X: x + padX, Y: rowY + (itemH-float32(itemSz))*0.5},
			float32(itemSz), 1.0, feTextDim)
		rowY += itemH
		_ = row
	} else {
		for i, p := range presets {
			label := p.Name
			if label == "" {
				label = fmt.Sprintf("Preset %d", i+1)
			}
			if drawMenuItem(label, false) {
				e.applyPreset(p, roster)
				e.kindMenuOpen = false
				return
			}
		}
	}
	// Separator.
	rl.DrawLine(int32(x+padX), int32(rowY+itemH*0.5-1),
		int32(x+width-padX), int32(rowY+itemH*0.5-1), feBtnBorder)
	rowY += itemH
	if drawMenuItem("Save current as preset", false) {
		e.saveCurrentAsPreset(fd, roster)
		e.kindMenuOpen = false
		return
	}
}

// applyKind switches the squad to a predefined formation kind, clears any
// custom slots, and resets spacing to the kind's default.
func (e *FormationEditor) applyKind(k components.FormationKind, fd *components.FormationData) {
	fd.Type = k
	fd.Spacing = formationSpacingDefault(k)
	if e.Ctx.CustomSlotsMap != nil && e.Ctx.CustomSlotsMap.Has(e.Squad) {
		e.Ctx.CustomSlotsMap.Remove(e.Squad)
	}
}

// applyPreset copies a saved preset's slot offsets into the squad's
// CustomSlots (creating the component on first apply).
func (e *FormationEditor) applyPreset(p components.FormationPreset, roster *components.CommandRoster) {
	if e.Ctx.CustomSlotsMap == nil {
		return
	}
	var cs components.FormationCustomSlots
	for i := uint8(0); i < components.SquadRosterSize; i++ {
		if i < p.Count && i < roster.Count {
			cs.Slots[i] = p.Slots[i]
		}
	}
	if e.Ctx.CustomSlotsMap.Has(e.Squad) {
		*e.Ctx.CustomSlotsMap.Get(e.Squad) = cs
	} else {
		e.Ctx.CustomSlotsMap.Add(e.Squad, &cs)
	}
}

// saveCurrentAsPreset snapshots the squad's current effective slots into
// a new FormationPreset on the resource. When CustomSlots is present its
// values are taken verbatim; otherwise the kind-based offsets are
// captured so the preset reflects what the user actually sees.
func (e *FormationEditor) saveCurrentAsPreset(fd *components.FormationData, roster *components.CommandRoster) {
	if e.Ctx.Presets == nil {
		return
	}
	var preset components.FormationPreset
	preset.Count = roster.Count
	for i := uint8(0); i < roster.Count; i++ {
		preset.Slots[i] = e.slotOffset(i, fd)
	}
	preset.Name = fmt.Sprintf("Preset %d", len(e.Ctx.Presets.List)+1)
	e.Ctx.Presets.List = append(e.Ctx.Presets.List, preset)
}

// formationSpacingDefault mirrors systems.FormationSpacing so the ui
// package doesn't pull in systems.
func formationSpacingDefault(k components.FormationKind) float32 {
	switch k {
	case components.FormationLine, components.FormationColumn:
		return 2.0
	case components.FormationWedge:
		return 3.0
	case components.FormationLoose:
		return 4.0
	}
	return 2.0
}

// allowsCommanderDrag returns true when the squad contains any non-
// infantry member (vehicles, mounted units), in which case slot 0 is
// draggable. Placeholder for Phase 19: until the Vehicle component
// lands every member is treated as infantry and this always returns
// false (commander locked at centre).
func (e *FormationEditor) allowsCommanderDrag(roster *components.CommandRoster) bool {
	_ = roster
	return false
}

// currentOrient reads the squad's orientation mode (defaulting to Movement
// when absent).
func (e *FormationEditor) currentOrient() components.FormationOrientationMode {
	if e.Ctx.OrientMap == nil {
		return components.OrientMovement
	}
	if o := e.Ctx.OrientMap.Get(e.Squad); o != nil {
		return o.Mode
	}
	return components.OrientMovement
}

// setOrient mutates the squad's orientation. Adds the component on first
// non-default write.
func (e *FormationEditor) setOrient(mode components.FormationOrientationMode) {
	if e.Ctx.OrientMap == nil {
		return
	}
	if o := e.Ctx.OrientMap.Get(e.Squad); o != nil {
		o.Mode = mode
		return
	}
	if mode == components.OrientMovement {
		// Default - no need to add the component.
		return
	}
	e.Ctx.OrientMap.Add(e.Squad, &components.FormationOrientation{Mode: mode})
}

func drawFeBtn(font rl.Font, r rl.Rectangle, label string, cursor rl.Vector2, active bool) {
	bg := feBtnBG
	if active {
		bg = feBtnActiveBG
	} else if pointInRect(cursor, r) {
		bg = feBtnHotBG
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, feBtnBorder)
	const sz int32 = 12
	w := rl.MeasureTextEx(font, label, float32(sz), 1.0).X
	rl.DrawTextEx(font, label,
		rl.Vector2{X: r.X + (r.Width-w)*0.5, Y: r.Y + (r.Height-float32(sz))*0.5},
		float32(sz), 1.0, feText)
}

func snapTo(v, step float32) float32 {
	if step <= 0 {
		return v
	}
	return float32(math.Round(float64(v/step))) * step
}

// chooseRingStep returns the ring spacing (metres) such that the canvas
// shows ~5-9 rings total. Stops scale with the visible maximum so a heavy
// zoom-in shows 0.5m rings, zoom-out goes to 2-5m, deep zoom-out adds
// 25 / 50 m so 100+ m formations stay readable without 30 rings.
func chooseRingStep(maxMeters float32) float32 {
	steps := []float32{0.5, 1, 2, 5, 10, 25, 50}
	for _, s := range steps {
		if maxMeters/s <= 9 {
			return s
		}
	}
	return steps[len(steps)-1]
}

// zoomRange returns the effective min/max PxPerM, preferring overrides on
// the ctx when set so a host can widen or narrow the range without
// touching the package-level defaults.
func (e *FormationEditor) zoomRange() (float32, float32) {
	minPx := e.Ctx.MinPxPerM
	if minPx <= 0 {
		minPx = fePxPerMMin
	}
	maxPx := e.Ctx.MaxPxPerM
	if maxPx <= 0 {
		maxPx = fePxPerMMax
	}
	if maxPx < minPx {
		maxPx = minPx
	}
	return minPx, maxPx
}

// snapStep returns the metric drag snap; negative ctx value disables
// snapping entirely (free placement).
func (e *FormationEditor) snapStep() float32 {
	if e.Ctx.SnapMeters < 0 {
		return 0
	}
	if e.Ctx.SnapMeters > 0 {
		return e.Ctx.SnapMeters
	}
	return feSnapMeters
}

// drawEmpty paints a "select a squad" placeholder for the selection-aware
// flavour (workspace panel, follow-selection floating). Centred dim text;
// no chrome. Cheap enough to call every frame.
func (e *FormationEditor) drawEmpty(r rl.Rectangle, font rl.Font) {
	rl.DrawRectangleRec(r, feCanvasBG)
	const sz int32 = 13
	msg := "Select a squad to edit its formation"
	w := rl.MeasureTextEx(font, msg, float32(sz), 1.0).X
	rl.DrawTextEx(font, msg,
		rl.Vector2{X: r.X + (r.Width-w)*0.5, Y: r.Y + r.Height*0.5 - float32(sz)*0.5},
		float32(sz), 1.0, feTextDim)
}

func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
