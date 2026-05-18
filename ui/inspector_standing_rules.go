package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Phase 13 M13.6 - Inspector quick-bars for Squad standing rules
// (MovementProfile / EngagementRules / BehaviorRules) + Stamina avg.
//
// Layout (single-squad view, drawn under the Roster section):
//
//   ┌─ Movement ────────────────────────────┐
//   │ Presets:  [Default][Cautious][Rush]   │
//   │           [Sprint ][Stealth ][Crawl ] │
//   │ Pace:    [Walk    ]  Stance:  [Stand] │
//   │ Posture: [Standard]  Path:    [Direct]│
//   │ Stamina avg: ████████░░ 85%           │
//   ├─ Engagement ──────────────────────────┤
//   │ Mode:     [Hold ][Return][Free ]      │
//   │ Targets:  [Inf ][Arm ][Air ][Struct]  │
//   ├─ Behavior ────────────────────────────┤
//   │ [done] Auto-reposition under fire        │
//   │ [done] Auto-stance change                │
//   │ [ ] Hold until ordered                │
//   │ [done] Allow return fire                 │
//   │ Suppression threshold: [-][+] 0.30    │
//   └───────────────────────────────────────┘
//
// Interaction model: immediate-mode. Each "button" is an inline rect + click
// test against (ctx.Cursor, ctx.LMBPressed). PanelFocused gates clicks so a
// drag from elsewhere doesn't accidentally toggle a chip.

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
	srChipH    int32 = 18
	srChipGap  int32 = 4
	srRowGap   int32 = 4
	srSectionGap int32 = 6
)

// drawStandingRulesSections renders the Movement / Engagement / Behavior
// quick-bars for one squad. Returns the next free Y position.
//
// `squad` must be alive and have all three standing-rule components (the
// SquadService writes them at template instantiation; defensive nil checks
// keep us safe across hot-reloads).
func drawStandingRulesSections(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	if !ctx.World.Alive(squad) {
		return y
	}

	// All three sections operate on this squad - bulk-mutate for multi-select
	// is deferred (open question 4). Phase 13 single-squad scope.
	mp := ctx.MovementProfileMap.Get(squad)
	er := ctx.EngagementRulesMap.Get(squad)
	br := ctx.BehaviorRulesMap.Get(squad)

	if mp != nil {
		y = drawMovementSection(ctx, squad, mp, x, y, width)
		y += srSectionGap
	}
	if er != nil {
		y = drawEngagementSection(ctx, er, x, y, width)
		y += srSectionGap
	}
	if br != nil {
		y = drawBehaviorSection(ctx, br, x, y, width)
		y += srSectionGap
	}
	return y
}

func drawMovementSection(ctx InspectorCtx, squad ecs.Entity, mp *components.MovementProfile, x, y, width int32) int32 {
	drawText(ctx.Font, "Movement", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	// Six preset chips in 2 rows × 3.
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

	// 4 cyclic field buttons in a 2×2 grid.
	colW := (width - srChipGap) / 2
	// Pace.
	if drawCyclicField(ctx, x, y, colW, srChipH, "Pace: "+components.PaceName(mp.Pace)) {
		mp.Pace = (mp.Pace + 1) % 3
	}
	// Stance.
	if drawCyclicField(ctx, x+colW+srChipGap, y, colW, srChipH, "Stance: "+components.StanceName(mp.Stance)) {
		mp.Stance = (mp.Stance + 1) % 3
	}
	y += srChipH + srRowGap

	// Posture.
	if drawCyclicField(ctx, x, y, colW, srChipH, "Posture: "+components.PostureName(mp.Posture)) {
		if mp.Posture == components.PostureStandard {
			mp.Posture = components.PostureQuiet
		} else {
			mp.Posture = components.PostureStandard
		}
	}
	// PathStyle.
	if drawCyclicField(ctx, x+colW+srChipGap, y, colW, srChipH, "Path: "+components.PathStyleName(mp.PathStyle)) {
		mp.PathStyle = (mp.PathStyle + 1) % 4
	}
	y += srChipH + srRowGap

	// Stamina avg bar. Averaged across roster members; if no members or no
	// Stamina components, draw "-".
	avg := squadStaminaAverage(ctx, squad)
	drawStaminaBar(ctx, x, y, width, srChipH, avg)
	y += srChipH
	return y
}

func drawEngagementSection(ctx InspectorCtx, er *components.EngagementRules, x, y, width int32) int32 {
	drawText(ctx.Font, "Engagement", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	// Mode: 3 mutually exclusive chips.
	modes := [3]components.EngagementMode{components.HoldFire, components.ReturnFire, components.FreeFire}
	chipW := (width - 2*srChipGap) / 3
	for i, m := range modes {
		cx := x + int32(i)*(chipW+srChipGap)
		if drawChip(ctx, cx, y, chipW, srChipH, components.EngagementModeName(m), er.Mode == m) {
			er.Mode = m
		}
	}
	y += srChipH + srRowGap

	// Target type toggles: 4 chips.
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
	y += srChipH
	return y
}

func drawBehaviorSection(ctx InspectorCtx, br *components.BehaviorRules, x, y, width int32) int32 {
	drawText(ctx.Font, "Behavior", x, y, inspectorFontSize, srSectionHdr)
	y += inspectorRowH

	// Boolean toggles as rows: [done] / [ ] + label.
	type toggleSpec struct {
		label string
		field *bool
	}
	toggles := [4]toggleSpec{
		{"Auto-reposition", &br.AllowAutoReposition},
		{"Auto-stance", &br.AllowAutoStance},
		{"Hold until ordered", &br.HoldUntilOrdered},
		{"Allow return fire", &br.AllowReturnFire},
	}
	for _, t := range toggles {
		if drawToggleRow(ctx, x, y, width, srChipH, t.label, *t.field) {
			*t.field = !*t.field
		}
		y += srChipH + 2
	}

	// SuppressionThreshold: [-] [value] [+] in a row.
	const step = 0.05
	const valueW int32 = 50
	buttonW := (width - valueW - 2*srChipGap) / 2
	if drawChip(ctx, x, y, buttonW, srChipH, "-", false) {
		br.SuppressionThreshold -= step
		if br.SuppressionThreshold < 0 {
			br.SuppressionThreshold = 0
		}
	}
	label := fmt.Sprintf("%.2f", br.SuppressionThreshold)
	drawText(ctx.Font, "Supp: "+label,
		x+buttonW+srChipGap, y+2, inspectorFontSize, inspectorText)
	if drawChip(ctx, x+buttonW+srChipGap+valueW+srChipGap, y, buttonW, srChipH, "+", false) {
		br.SuppressionThreshold += step
		if br.SuppressionThreshold > 1 {
			br.SuppressionThreshold = 1
		}
	}
	y += srChipH
	return y
}

// drawChip renders a small clickable chip with an active / hover background.
// Returns true if the chip was clicked this frame.
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

// drawCyclicField - same visual as drawChip but never "active"; click cycles
// the underlying value (caller handles the wrap-around).
func drawCyclicField(ctx InspectorCtx, x, y, w, h int32, label string) bool {
	return drawChip(ctx, x, y, w, h, label, false)
}

// drawToggleRow - left-aligned label with a leading [done]/[ ] indicator. Click
// anywhere in the row flips the boolean.
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
		// Override after the dim mark - keep the label readable.
		drawText(ctx.Font, mark+" "+label, x+4, y+1, inspectorFontSize, inspectorText)
	}
	return hover && ctx.LMBPressed
}

// drawStaminaBar - fill ratio 0..1, colour by zone. Renders inline in the
// Movement section. `ratio < 0` indicates "no Stamina readings" - draws as a
// flat dim track with "-" label.
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

// squadStaminaAverage returns the mean Current/MaxLevel across the squad's
// live roster. -1 if no readable Stamina components were found.
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

// profileMatchesPreset - chip is "active" when every field of the live profile
// matches the preset. Strict match avoids "partially active" highlights when
// the player has hand-edited one field after applying a preset.
func profileMatchesPreset(p components.MovementProfile, preset components.MovementPreset) bool {
	want := components.ApplyPreset(preset)
	return p == want
}

// rectContains is a small XZ-style hit test. Inspector cursor is in screen
// coords (raylib defaults), same as the panel content rect, so a direct
// comparison works.
func rectContains(r rl.Rectangle, c rl.Vector2) bool {
	return c.X >= r.X && c.X < r.X+r.Width && c.Y >= r.Y && c.Y < r.Y+r.Height
}
