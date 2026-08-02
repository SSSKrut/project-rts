package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// The Behavior panel: a squad's standing rules (Doctrine / Autonomy /
// Movement / Engagement / Behavior) plus its average stamina. Configuration,
// not status — it lives apart from the Inspector because it is written rarely
// and read on demand, while the Inspector is glanced at constantly.
//
// Stateless by design: every value shown is a component, so the panel is a
// pure function of the world. No singleton, unlike the formation editor.
// Drawing is ui/widget.go primitives; this file is layout and semantics.

// BehaviorMaps bundles the write-handles the standing-rule sections need.
type BehaviorMaps struct {
	RosterMap            *ecs.Map[components.CommandRoster]
	SquadMemberMap       *ecs.Map[components.SquadMember]
	MovementProfileMap   *ecs.Map[components.MovementProfile]
	EngagementRulesMap   *ecs.Map[components.EngagementRules]
	BehaviorRulesMap     *ecs.Map[components.BehaviorRules]
	BehaviorRulesEditMap *ecs.Map[components.BehaviorRulesEdit]
	ActiveDoctrineMap    *ecs.Map[components.ActiveDoctrine]
	ActiveAutonomyMap    *ecs.Map[components.ActiveAutonomy]
	StaminaMap           *ecs.Map[components.Stamina]
}

func NewBehaviorMaps(world *ecs.World) BehaviorMaps {
	return BehaviorMaps{
		RosterMap:            ecs.NewMap[components.CommandRoster](world),
		SquadMemberMap:       ecs.NewMap[components.SquadMember](world),
		MovementProfileMap:   ecs.NewMap[components.MovementProfile](world),
		EngagementRulesMap:   ecs.NewMap[components.EngagementRules](world),
		BehaviorRulesMap:     ecs.NewMap[components.BehaviorRules](world),
		BehaviorRulesEditMap: ecs.NewMap[components.BehaviorRulesEdit](world),
		ActiveDoctrineMap:    ecs.NewMap[components.ActiveDoctrine](world),
		ActiveAutonomyMap:    ecs.NewMap[components.ActiveAutonomy](world),
		StaminaMap:           ecs.NewMap[components.Stamina](world),
	}
}

// BehaviorCtx is built per frame and passed by value, like InspectorCtx.
type BehaviorCtx struct {
	BehaviorMaps
	World        *ecs.World
	Selected     []ecs.Entity
	Font         rl.Font
	Cursor       rl.Vector2
	LMBPressed   bool
	PanelFocused bool
	Scroll       *ScrollState

	st Style
	in WidgetInput
}

// DrawBehaviorPanel renders the standing rules of the squad behind the
// current selection. Selection that resolves to no single squad gets a hint
// instead — the rules are squad-scoped, there is nothing to edit for a
// soloist or for a mixed bag of squads.
func DrawBehaviorPanel(panel Panel, ctx BehaviorCtx) {
	ctx.st = InspectorStyle(ctx.Font)
	ctx.in = WidgetInput{Cursor: ctx.Cursor, Press: ctx.LMBPressed, Enabled: ctx.PanelFocused}
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	sc := BeginScroll(content, ctx.Scroll, float32(inspectorPadX), float32(inspectorPadY))
	defer sc.End()

	squad, ok := behaviorSquad(ctx)
	if !ok {
		col := Column{X: sc.Col.X, Y: sc.Col.Y, W: sc.Col.W}
		TextRow(&col, &ctx.st, behaviorEmptyHint(ctx), ctx.st.TextDim)
		sc.Col.Y = col.Y
		return
	}

	col := Column{X: sc.Col.X, Y: sc.Col.Y, W: sc.Col.W}
	TextRow(&col, &ctx.st,
		fmt.Sprintf("Squad #%X", squad.ID()&0xFFF), ctx.st.Text)
	col.Skip(ctx.st.RowH * 0.5)

	sc.Col.Y = float32(drawStandingRulesSections(ctx, squad,
		int32(col.X), int32(col.Y), int32(col.W)))
}

// behaviorSquad resolves the selection to one squad: every selected entity
// must belong to the same one.
func behaviorSquad(ctx BehaviorCtx) (ecs.Entity, bool) {
	if len(ctx.Selected) == 0 || ctx.SquadMemberMap == nil {
		return ecs.Entity{}, false
	}
	squad, homo := groupSelectedHelper(ctx.Selected, ctx.SquadMemberMap)
	if !homo || squad == (ecs.Entity{}) || !ctx.World.Alive(squad) {
		return ecs.Entity{}, false
	}
	return squad, true
}

func behaviorEmptyHint(ctx BehaviorCtx) string {
	if len(ctx.Selected) == 0 {
		return "Select a squad to edit its standing rules"
	}
	return "Standing rules are per squad - select one squad"
}

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
	srChipH      float32 = 18
	srChipGap    float32 = 4
	srRowGap     float32 = 4
	srSectionGap float32 = 6
)

// drawStandingRulesSections returns the next free Y. Defensive nil checks
// keep callers safe across hot-reloads.
func drawStandingRulesSections(ctx BehaviorCtx, squad ecs.Entity, x, y, width int32) int32 {
	if !ctx.World.Alive(squad) {
		return y
	}

	mp := ctx.MovementProfileMap.Get(squad)
	er := ctx.EngagementRulesMap.Get(squad)
	br := ctx.BehaviorRulesMap.Get(squad)

	col := Column{X: float32(x), Y: float32(y), W: float32(width)}

	// Doctrine must run before the section chips so the highlights reflect
	// the freshly-applied fields.
	if mp != nil && er != nil && br != nil && ctx.ActiveDoctrineMap != nil {
		drawDoctrineSection(ctx, &col, squad, mp, er, br)
		col.Skip(srSectionGap)
	}
	if mp != nil {
		drawMovementSection(ctx, &col, squad, mp)
		col.Skip(srSectionGap)
	}
	if er != nil {
		drawEngagementSection(ctx, &col, er)
		col.Skip(srSectionGap)
	}
	if br != nil {
		// AutonomySpec writes through into BehaviorRules skipping dirty bits.
		if ctx.ActiveAutonomyMap != nil {
			drawAutonomySection(ctx, &col, squad, br)
			col.Skip(srSectionGap)
		}
		drawBehaviorSection(ctx, &col, squad, br)
		col.Skip(srSectionGap)
	}
	return int32(col.Y)
}

// drawAutonomySection: click applies AutonomySpec to br (skipping dirty
// fields) and stamps ActiveAutonomy.Code for highlight.
func drawAutonomySection(ctx BehaviorCtx, col *Column, squad ecs.Entity,
	br *components.BehaviorRules) {
	Header(col, &ctx.st, "Autonomy")

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
	row := col.Band(srChipH)
	for i, a := range autonomies {
		active := components.AutonomyMatches(a, *br, dirty)
		if Chip(ctx.in, &ctx.st, SplitX(row, i, 4, srChipGap),
			components.AutonomyName(a), active) {
			components.ApplyAutonomy(a, br, dirty)
			if ctx.ActiveAutonomyMap.Has(squad) {
				*ctx.ActiveAutonomyMap.Get(squad) = components.ActiveAutonomy{Code: a}
			} else {
				ctx.ActiveAutonomyMap.Add(squad, &components.ActiveAutonomy{Code: a})
			}
		}
	}
}

// drawDoctrineSection: click writes DoctrineSpec into the squad's three
// components and stamps ActiveDoctrine.Code.
func drawDoctrineSection(ctx BehaviorCtx, col *Column, squad ecs.Entity,
	mp *components.MovementProfile, er *components.EngagementRules,
	br *components.BehaviorRules) {
	Header(col, &ctx.st, "Doctrine")

	doctrines := [4]components.DoctrineCode{
		components.DoctrinePatrol, components.DoctrineAssault,
		components.DoctrineStealth, components.DoctrineDefense,
	}
	row := col.Band(srChipH)
	for i, d := range doctrines {
		active := components.DoctrineMatches(d, *mp, *er, *br)
		if Chip(ctx.in, &ctx.st, SplitX(row, i, 4, srChipGap),
			components.DoctrineName(d), active) {
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
}

func drawMovementSection(ctx BehaviorCtx, col *Column, squad ecs.Entity,
	mp *components.MovementProfile) {
	Header(col, &ctx.st, "Movement")

	presets := [6]components.MovementPreset{
		components.PresetDefault, components.PresetCautious, components.PresetRush,
		components.PresetSprint, components.PresetStealth, components.PresetProneCrawl,
	}
	rows := [2]rl.Rectangle{}
	for i := range rows {
		rows[i] = col.Band(srChipH)
		col.Skip(srChipGap)
	}
	for i, p := range presets {
		cell := SplitX(rows[i/3], i%3, 3, srChipGap)
		if Chip(ctx.in, &ctx.st, cell, components.PresetName(p), profileMatchesPreset(*mp, p)) {
			*mp = components.ApplyPreset(p)
		}
	}

	pace := col.Band(srChipH)
	col.Skip(srRowGap)
	if cycle(ctx, SplitX(pace, 0, 2, srChipGap), "Pace: "+components.PaceName(mp.Pace)) {
		mp.Pace = (mp.Pace + 1) % 3
	}
	if cycle(ctx, SplitX(pace, 1, 2, srChipGap), "Stance: "+components.StanceName(mp.Stance)) {
		mp.Stance = (mp.Stance + 1) % 3
	}

	posture := col.Band(srChipH)
	col.Skip(srRowGap)
	if cycle(ctx, SplitX(posture, 0, 2, srChipGap), "Posture: "+components.PostureName(mp.Posture)) {
		if mp.Posture == components.PostureStandard {
			mp.Posture = components.PostureQuiet
		} else {
			mp.Posture = components.PostureStandard
		}
	}
	if cycle(ctx, SplitX(posture, 1, 2, srChipGap), "Path: "+components.PathStyleName(mp.PathStyle)) {
		mp.PathStyle = (mp.PathStyle + 1) % 4
	}

	drawStaminaBar(ctx, col.Band(srChipH), squadStaminaAverage(ctx, squad))
}

func drawEngagementSection(ctx BehaviorCtx, col *Column, er *components.EngagementRules) {
	Header(col, &ctx.st, "Engagement")

	modes := [3]components.EngagementMode{
		components.HoldFire, components.ReturnFire, components.FreeFire,
	}
	row := col.Band(srChipH)
	col.Skip(srRowGap)
	for i, m := range modes {
		if Chip(ctx.in, &ctx.st, SplitX(row, i, 3, srChipGap),
			components.EngagementModeName(m), er.Mode == m) {
			er.Mode = m
		}
	}

	targets := [4]struct {
		label string
		field *bool
	}{
		{"Inf", &er.FireOnInf}, {"Arm", &er.FireOnArm},
		{"Air", &er.FireOnAir}, {"Struct", &er.FireOnStruct},
	}
	row = col.Band(srChipH)
	col.Skip(srRowGap)
	for i, t := range targets {
		if Chip(ctx.in, &ctx.st, SplitX(row, i, 4, srChipGap), t.label, *t.field) {
			*t.field = !*t.field
		}
	}

	// Standoff cycles Any -> Close -> Medium -> Long -> Any. SectorHalfDot
	// is read-only here; the edit UI ships with DefendPosition.
	if cycle(ctx, col.Band(srChipH), "Standoff: "+components.StandoffName(er.Standoff)) {
		er.Standoff = (er.Standoff + 1) % 4
	}
	col.Skip(srRowGap)

	sectorLabel := "Sector: free"
	if er.SectorHalfDot > 0 {
		halfDeg := math.Acos(float64(er.SectorHalfDot)) * 180.0 / math.Pi
		yawDeg := float64(er.SectorYaw) * 180.0 / math.Pi
		sectorLabel = fmt.Sprintf("Sector: yaw %.0f deg, half %.0f deg", yawDeg, halfDeg)
	}
	TextRow(col, &ctx.st, sectorLabel, ctx.st.TextDim)
}

func drawBehaviorSection(ctx BehaviorCtx, col *Column, squad ecs.Entity,
	br *components.BehaviorRules) {
	Header(col, &ctx.st, "Behavior")

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

	toggles := [4]struct {
		label string
		field *bool
		bit   components.BehaviorRulesField
	}{
		{"Auto-reposition", &br.AllowAutoReposition, components.DirtyAutoReposition},
		{"Auto-stance", &br.AllowAutoStance, components.DirtyAutoStance},
		{"Hold until ordered", &br.HoldUntilOrdered, components.DirtyHoldUntilOrdered},
		{"Allow return fire", &br.AllowReturnFire, components.DirtyAllowReturnFire},
	}
	for _, t := range toggles {
		if Toggle(ctx.in, &ctx.st, col.Band(srChipH), t.label, *t.field) {
			*t.field = !*t.field
			markDirty(t.bit)
		}
		col.Skip(2)
	}

	const step float32 = 0.05
	const valueW float32 = 50
	row := col.Band(srChipH)
	buttonW := (row.Width - valueW - 2*srChipGap) / 2
	minus := rl.Rectangle{X: row.X, Y: row.Y, Width: buttonW, Height: row.Height}
	plus := rl.Rectangle{X: row.X + buttonW + srChipGap + valueW + srChipGap, Y: row.Y,
		Width: buttonW, Height: row.Height}
	if Chip(ctx.in, &ctx.st, minus, "-", false) {
		br.SuppressionThreshold = clamp01(br.SuppressionThreshold - step)
		markDirty(components.DirtySuppressionThreshold)
	}
	Text(&ctx.st, rl.Rectangle{X: minus.X + buttonW + srChipGap, Y: row.Y + 2},
		fmt.Sprintf("Supp: %.2f", br.SuppressionThreshold), ctx.st.Text)
	if Chip(ctx.in, &ctx.st, plus, "+", false) {
		br.SuppressionThreshold = clamp01(br.SuppressionThreshold + step)
		markDirty(components.DirtySuppressionThreshold)
	}
}

// cycle is a Chip that never latches — the caller advances the value.
func cycle(ctx BehaviorCtx, r rl.Rectangle, label string) bool {
	return Chip(ctx.in, &ctx.st, r, label, false)
}

// drawStaminaBar: ratio < 0 means "no readings", drawn as a flat dim track
// with "-" label.
func drawStaminaBar(ctx BehaviorCtx, r rl.Rectangle, ratio float32) {
	label := "Stamina: -"
	if ratio >= 0 {
		colour := srStaminaHigh
		if ratio < 0.2 {
			colour = srStaminaLow
		} else if ratio < 0.5 {
			colour = srStaminaMid
		}
		Bar(r, ratio, srBarTrack, colour, 1)
		label = fmt.Sprintf("Stamina: %.0f%%", ratio*100)
	} else {
		Bar(r, 0, srBarTrack, rl.Color{}, 1)
	}
	rl.DrawRectangleLinesEx(r, 1, srChipBorder)
	Text(&ctx.st, rl.Rectangle{X: r.X + 4, Y: r.Y + 1}, label, ctx.st.Text)
}

// squadStaminaAverage returns -1 when no readable Stamina components found.
func squadStaminaAverage(ctx BehaviorCtx, squad ecs.Entity) float32 {
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
	return p == components.ApplyPreset(preset)
}
