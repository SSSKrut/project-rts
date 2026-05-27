package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Immediate-mode quick-bars for Squad standing rules
// (MovementProfile / EngagementRules / BehaviorRules) + Stamina avg.
// PanelFocused gates clicks so a drag from elsewhere doesn't toggle a chip.

var (
	srSectionHdr  = rl.Color{R: 90, G: 100, B: 120, A: 255}
	srChipBG      = rl.Color{R: 36, G: 42, B: 52, A: 255}
	srChipActive  = rl.Color{R: 80, G: 130, B: 200, A: 255}
	srChipHover   = rl.Color{R: 58, G: 66, B: 80, A: 255}
	srChipBorder  = rl.Color{R: 20, G: 22, B: 28, A: 220}
	srStaminaHigh = rl.Color{R: 80, G: 200, B: 80, A: 255}
	srStaminaMid  = rl.Color{R: 220, G: 200, B: 50, A: 255}
	srStaminaLow  = rl.Color{R: 220, G: 60, B: 60, A: 255}
	srBarTrack    = rl.Color{R: 30, G: 32, B: 38, A: 255}
)

const (
	srChipH      int32 = 18
	srChipGap    int32 = 4
	srRowGap     int32 = 4
	srSectionGap int32 = 6
)

// drawStandingRulesSections returns the next free Y. Defensive nil checks
// keep callers safe across hot-reloads.
func drawStandingRulesSections(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	if !ctx.World.Alive(squad) {
		return y
	}

	mp := ctx.MovementProfileMap.Get(squad)
	er := ctx.EngagementRulesMap.Get(squad)
	br := ctx.BehaviorRulesMap.Get(squad)

	// Doctrine must run before the section chips so the highlights reflect
	// the freshly-applied fields.
	if mp != nil && er != nil && br != nil && ctx.ActiveDoctrineMap != nil {
		y = drawDoctrineSection(ctx, squad, mp, er, br, x, y, width)
		y += srSectionGap
	}

	if mp != nil {
		y = drawMovementSection(ctx, squad, mp, x, y, width)
		y += srSectionGap
	}
	if er != nil {
		y = drawEngagementSection(ctx, er, x, y, width)
		y += srSectionGap
	}
	if br != nil {
		// AutonomySpec writes through into BehaviorRules skipping dirty bits.
		if ctx.ActiveAutonomyMap != nil {
			y = drawAutonomySection(ctx, squad, br, x, y, width)
			y += srSectionGap
		}
		y = drawBehaviorSection(ctx, squad, br, x, y, width)
		y += srSectionGap
	}
	return y
}

// drawAutonomySection: click applies AutonomySpec to br (skipping dirty
// fields) and stamps ActiveAutonomy.Code for highlight.
func drawAutonomySection(
	ctx InspectorCtx,
	squad ecs.Entity,
	br *components.BehaviorRules,
	x, y, width int32,
) int32 {
	drawText(ctx.Font, "Autonomy", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	dirty := components.BehaviorRulesField(0)
	if ctx.BehaviorRulesEditMap != nil {
		if ed := ctx.BehaviorRulesEditMap.Get(squad); ed != nil {
			dirty = ed.DirtyMask
		}
	}

	autonomies := [4]components.AutonomyCode{
		components.AutonomyStrict, components.AutonomyCautious,
		components.AutonomyAdaptive, components.AutonomySurvival,
	}
	chipW := (width - 3*srChipGap) / 4
	for i, a := range autonomies {
		cx := x + int32(i)*(chipW+srChipGap)
		active := components.AutonomyMatches(a, *br, dirty)
		if drawChip(ctx, cx, y, chipW, srChipH, components.AutonomyName(a), active) {
			components.ApplyAutonomy(a, br, dirty)
			if ctx.ActiveAutonomyMap.Has(squad) {
				*ctx.ActiveAutonomyMap.Get(squad) = components.ActiveAutonomy{Code: a}
			} else {
				ctx.ActiveAutonomyMap.Add(squad, &components.ActiveAutonomy{Code: a})
			}
		}
	}
	y += srChipH
	return y
}

// drawDoctrineSection: click writes DoctrineSpec into the squad's three
// components and stamps ActiveDoctrine.Code.
func drawDoctrineSection(
	ctx InspectorCtx,
	squad ecs.Entity,
	mp *components.MovementProfile,
	er *components.EngagementRules,
	br *components.BehaviorRules,
	x, y, width int32,
) int32 {
	drawText(ctx.Font, "Doctrine", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	doctrines := [4]components.DoctrineCode{
		components.DoctrinePatrol, components.DoctrineAssault,
		components.DoctrineStealth, components.DoctrineDefense,
	}
	chipW := (width - 3*srChipGap) / 4
	for i, d := range doctrines {
		cx := x + int32(i)*(chipW+srChipGap)
		active := components.DoctrineMatches(d, *mp, *er, *br)
		if drawChip(ctx, cx, y, chipW, srChipH, components.DoctrineName(d), active) {
			spec := components.DoctrineSpecs[d]
			*mp = spec.Movement
			*er = spec.Engage
			*br = spec.Behavior
			if ctx.ActiveDoctrineMap.Has(squad) {
				*ctx.ActiveDoctrineMap.Get(squad) = components.ActiveDoctrine{Code: d}
			} else {
				ctx.ActiveDoctrineMap.Add(squad, &components.ActiveDoctrine{Code: d})
			}
		}
	}
	y += srChipH
	return y
}

func drawMovementSection(ctx InspectorCtx, squad ecs.Entity, mp *components.MovementProfile, x, y, width int32) int32 {
	drawText(ctx.Font, "Movement", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	chipW := (width - 2*srChipGap) / 3
	presets := [6]components.MovementPreset{
		components.PresetDefault, components.PresetCautious, components.PresetRush,
		components.PresetSprint, components.PresetStealth, components.PresetProneCrawl,
	}
	for i, p := range presets {
		col := i % 3
		row := i / 3
		cx := x + int32(col)*(chipW+srChipGap)
		cy := y + int32(row)*(srChipH+srChipGap)
		active := profileMatchesPreset(*mp, p)
		if drawChip(ctx, cx, cy, chipW, srChipH, components.PresetName(p), active) {
			*mp = components.ApplyPreset(p)
		}
	}
	y += 2*(srChipH+srChipGap) - srChipGap + srRowGap

	colW := (width - srChipGap) / 2
	if drawCyclicField(ctx, x, y, colW, srChipH, "Pace: "+components.PaceName(mp.Pace)) {
		mp.Pace = (mp.Pace + 1) % 3
	}
	if drawCyclicField(ctx, x+colW+srChipGap, y, colW, srChipH, "Stance: "+components.StanceName(mp.Stance)) {
		mp.Stance = (mp.Stance + 1) % 3
	}
	y += srChipH + srRowGap

	if drawCyclicField(ctx, x, y, colW, srChipH, "Posture: "+components.PostureName(mp.Posture)) {
		if mp.Posture == components.PostureStandard {
			mp.Posture = components.PostureQuiet
		} else {
			mp.Posture = components.PostureStandard
		}
	}
	if drawCyclicField(ctx, x+colW+srChipGap, y, colW, srChipH, "Path: "+components.PathStyleName(mp.PathStyle)) {
		mp.PathStyle = (mp.PathStyle + 1) % 4
	}
	y += srChipH + srRowGap

	avg := squadStaminaAverage(ctx, squad)
	drawStaminaBar(ctx, x, y, width, srChipH, avg)
	y += srChipH
	return y
}

func drawEngagementSection(ctx InspectorCtx, er *components.EngagementRules, x, y, width int32) int32 {
	drawText(ctx.Font, "Engagement", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	modes := [3]components.EngagementMode{components.HoldFire, components.ReturnFire, components.FreeFire}
	chipW := (width - 2*srChipGap) / 3
	for i, m := range modes {
		cx := x + int32(i)*(chipW+srChipGap)
		if drawChip(ctx, cx, y, chipW, srChipH, components.EngagementModeName(m), er.Mode == m) {
			er.Mode = m
		}
	}
	y += srChipH + srRowGap

	targetW := (width - 3*srChipGap) / 4
	type targetSpec struct {
		label string
		field *bool
	}
	targets := [4]targetSpec{
		{"Inf", &er.FireOnInf}, {"Arm", &er.FireOnArm},
		{"Air", &er.FireOnAir}, {"Struct", &er.FireOnStruct},
	}
	for i, t := range targets {
		cx := x + int32(i)*(targetW+srChipGap)
		if drawChip(ctx, cx, y, targetW, srChipH, t.label, *t.field) {
			*t.field = !*t.field
		}
	}
	y += srChipH + srRowGap

	// Standoff cycles Any -> Close -> Medium -> Long -> Any. SectorHalfDot
	// is read-only here; the edit UI ships with DefendPosition.
	standoffLabel := "Standoff: " + components.StandoffName(er.Standoff)
	if drawCyclicField(ctx, x, y, width, srChipH, standoffLabel) {
		er.Standoff = (er.Standoff + 1) % 4
	}
	y += srChipH + srRowGap
	sectorLabel := "Sector: free"
	if er.SectorHalfDot > 0 {
		halfDeg := math.Acos(float64(er.SectorHalfDot)) * 180.0 / math.Pi
		yawDeg := float64(er.SectorYaw) * 180.0 / math.Pi
		sectorLabel = fmt.Sprintf("Sector: yaw %.0f deg, half %.0f deg", yawDeg, halfDeg)
	}
	drawText(ctx.Font, sectorLabel, x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH
	return y
}

func drawBehaviorSection(ctx InspectorCtx, squad ecs.Entity, br *components.BehaviorRules, x, y, width int32) int32 {
	drawText(ctx.Font, "Behavior", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	// Individual field edits flip the DirtyMask bit so the next Autonomy
	// chip click skips them.
	markDirty := func(bit components.BehaviorRulesField) {
		if ctx.BehaviorRulesEditMap == nil {
			return
		}
		if ed := ctx.BehaviorRulesEditMap.Get(squad); ed != nil {
			ed.DirtyMask |= bit
			return
		}
		ctx.BehaviorRulesEditMap.Add(squad, &components.BehaviorRulesEdit{DirtyMask: bit})
	}

	type toggleSpec struct {
		label string
		field *bool
		bit   components.BehaviorRulesField
	}
	toggles := [4]toggleSpec{
		{"Auto-reposition", &br.AllowAutoReposition, components.DirtyAutoReposition},
		{"Auto-stance", &br.AllowAutoStance, components.DirtyAutoStance},
		{"Hold until ordered", &br.HoldUntilOrdered, components.DirtyHoldUntilOrdered},
		{"Allow return fire", &br.AllowReturnFire, components.DirtyAllowReturnFire},
	}
	for _, t := range toggles {
		if drawToggleRow(ctx, x, y, width, srChipH, t.label, *t.field) {
			*t.field = !*t.field
			markDirty(t.bit)
		}
		y += srChipH + 2
	}

	const step = 0.05
	const valueW int32 = 50
	buttonW := (width - valueW - 2*srChipGap) / 2
	if drawChip(ctx, x, y, buttonW, srChipH, "-", false) {
		br.SuppressionThreshold -= step
		if br.SuppressionThreshold < 0 {
			br.SuppressionThreshold = 0
		}
		markDirty(components.DirtySuppressionThreshold)
	}
	label := fmt.Sprintf("%.2f", br.SuppressionThreshold)
	drawText(ctx.Font, "Supp: "+label,
		x+buttonW+srChipGap, y+2, inspectorFontSize, inspectorText)
	if drawChip(ctx, x+buttonW+srChipGap+valueW+srChipGap, y, buttonW, srChipH, "+", false) {
		br.SuppressionThreshold += step
		if br.SuppressionThreshold > 1 {
			br.SuppressionThreshold = 1
		}
		markDirty(components.DirtySuppressionThreshold)
	}
	y += srChipH
	return y
}

func drawChip(ctx InspectorCtx, x, y, w, h int32, label string, active bool) bool {
	r := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(w), Height: float32(h)}
	hover := rectContains(r, ctx.Cursor) && ctx.PanelFocused
	bg := srChipBG
	if active {
		bg = srChipActive
	} else if hover {
		bg = srChipHover
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, srChipBorder)
	textColor := inspectorText
	if active {
		textColor = contrastTextColor(bg)
	}
	size := rl.MeasureTextEx(ctx.Font, label, float32(inspectorFontSize), 1)
	rl.DrawTextEx(ctx.Font, label, rl.Vector2{
		X: float32(x) + (float32(w)-size.X)*0.5,
		Y: float32(y) + (float32(h)-size.Y)*0.5,
	}, float32(inspectorFontSize), 1, textColor)
	return hover && ctx.LMBPressed
}

// drawCyclicField is drawChip with active=false; caller cycles the value.
func drawCyclicField(ctx InspectorCtx, x, y, w, h int32, label string) bool {
	return drawChip(ctx, x, y, w, h, label, false)
}

func drawToggleRow(ctx InspectorCtx, x, y, w, h int32, label string, on bool) bool {
	r := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(w), Height: float32(h)}
	hover := rectContains(r, ctx.Cursor) && ctx.PanelFocused
	bg := srChipBG
	if hover {
		bg = srChipHover
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, srChipBorder)
	mark := "[ ]"
	markColor := inspectorTextDim
	if on {
		mark = "[X]"
		markColor = srChipActive
	}
	drawText(ctx.Font, mark+" "+label, x+4, y+1, inspectorFontSize, markColor)
	if !on {
		// Override the dim mark so the label stays readable.
		drawText(ctx.Font, mark+" "+label, x+4, y+1, inspectorFontSize, inspectorText)
	}
	return hover && ctx.LMBPressed
}

// drawStaminaBar: ratio < 0 means "no readings", drawn as a flat dim
// track with "-" label.
func drawStaminaBar(ctx InspectorCtx, x, y, w, h int32, ratio float32) {
	track := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(w), Height: float32(h)}
	rl.DrawRectangleRec(track, srBarTrack)
	rl.DrawRectangleLinesEx(track, 1, srChipBorder)
	label := "Stamina: -"
	if ratio >= 0 {
		fillW := float32(w-2) * ratio
		colour := srStaminaHigh
		if ratio < 0.2 {
			colour = srStaminaLow
		} else if ratio < 0.5 {
			colour = srStaminaMid
		}
		rl.DrawRectangle(x+1, y+1, int32(fillW), h-2, colour)
		label = fmt.Sprintf("Stamina: %.0f%%", ratio*100)
	}
	drawText(ctx.Font, label, x+4, y+1, inspectorFontSize, inspectorText)
}

// squadStaminaAverage returns -1 when no readable Stamina components found.
func squadStaminaAverage(ctx InspectorCtx, squad ecs.Entity) float32 {
	if ctx.StaminaMap == nil {
		return -1
	}
	roster := ctx.RosterMap.Get(squad)
	if roster == nil || roster.Count == 0 {
		return -1
	}
	var sum float32
	var n int
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		st := ctx.StaminaMap.Get(mem)
		if st == nil || st.MaxLevel <= 0 {
			continue
		}
		sum += st.Current / st.MaxLevel
		n++
	}
	if n == 0 {
		return -1
	}
	return sum / float32(n)
}

// profileMatchesPreset uses strict equality so hand-edited fields don't
// light the chip as "partially active".
func profileMatchesPreset(p components.MovementProfile, preset components.MovementPreset) bool {
	want := components.ApplyPreset(preset)
	return p == want
}

func rectContains(r rl.Rectangle, c rl.Vector2) bool {
	return c.X >= r.X && c.X < r.X+r.Width && c.Y >= r.Y && c.Y < r.Y+r.Height
}
