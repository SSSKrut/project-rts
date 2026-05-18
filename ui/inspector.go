package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// InspectorCtx bundles all the data the inspector reads. Built once per
// frame in main.go and passed by value. Keeping the dependency list explicit
// (rather than pulling from a global) makes it easy to see what the inspector
// touches and to thin it out as roles / orders land in Phase 11/12.
type InspectorCtx struct {
	World            *ecs.World
	Selected         []ecs.Entity
	Hovered          ecs.Entity
	Font             rl.Font
	PosMap           *ecs.Map[components.WorldPos]
	StanceMap        *ecs.Map[components.Stance]
	MotionMap        *ecs.Map[components.Motion]
	SuppressionMap   *ecs.Map[components.Suppression]
	EquipmentMap     *ecs.Map[components.Equipment]
	SquadMemberMap   *ecs.Map[components.SquadMember]
	RosterMap        *ecs.Map[components.CommandRoster]
	FormationDataMap *ecs.Map[components.FormationData]
	MacroPathMap     *ecs.Map[components.MacroPath]
	SquadFilter      *ecs.Filter2[components.Squad, components.CommandRoster]
	// Phase 11 order maps. Inspector reads to render the current Order row.
	OrderQueueMap    *ecs.Map[components.OrderQueueHead]
	OrderKindMap     *ecs.Map[components.OrderKind]
	OrderStateMap    *ecs.Map[components.OrderState]
	OrderTargetMap   *ecs.Map[components.OrderTarget]
	OrderProgressMap *ecs.Map[components.OrderProgress]
	OrderChainMap    *ecs.Map[components.OrderChain]
	BuildingMap      *ecs.Map[components.Building]
	TrenchRootMap    *ecs.Map[components.TrenchRoot]
	// Phase 12 role map. When non-nil drawInspectorUnit / drawInspectorSquad
	// show role-tinted roster rows and a Role: header on the single-unit
	// view. Nil falls back to "Rifleman placeholder" - keeps backwards-compat
	// for any caller that hasn't wired the map yet.
	RoleMap *ecs.Map[components.UnitRole]
	// Phase 13 maps for the quick-bar sections + Stamina display. When all
	// three standing-rule maps are non-nil, drawInspectorSquad appends the
	// Movement / Engagement / Behavior sections under the roster.
	MovementProfileMap       *ecs.Map[components.MovementProfile]
	EngagementRulesMap       *ecs.Map[components.EngagementRules]
	BehaviorRulesMap         *ecs.Map[components.BehaviorRules]
	StaminaMap               *ecs.Map[components.Stamina]
	OrderAttackMoveMap       *ecs.Map[components.OrderParamAttackMove]
	OrderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	// Phase 14 M14.1: HP + Faction read handles. drawInspectorUnit shows the
	// HP row under Stamina; Faction is surfaced in the squad header so the
	// player can tell two squads of the same template apart at a glance.
	HPMap      *ecs.Map[components.HP]
	FactionMap *ecs.Map[components.Faction]
	// Phase 13 click input. Cursor is the current mouse position in screen
	// coords; LMBPressed is true exactly on the frame the left button was
	// pressed (passed through from main.go's rl.IsMouseButtonPressed call);
	// PanelFocused gates clicks so chips don't react to drags / clicks that
	// belong to other panels.
	Cursor       rl.Vector2
	LMBPressed   bool
	PanelFocused bool
	// Phase 13.5 M13.5.3: scroll handle. DrawInspector subtracts Scroll.OffsetY
	// from initial y and writes total content height into Scroll.ContentHeight
	// at the end of the draw - caller's DrawScrollbar reads it next frame.
	// Nil -> behave as if scroll==0 with no measurement (legacy callers).
	Scroll *ScrollState
	// SquadColor picks a stable palette colour from a squad entity so the
	// inspector and the map render use the same shade. Injected as a func to
	// avoid a UI -> render-package cycle. Phase 14 M14.6: signature takes
	// ecs.Entity (not just ID) so the colour function can read Faction off
	// the squad entity directly.
	SquadColor func(ent ecs.Entity) rl.Color
}

// roleOf is a small helper that resolves the unit's role with a Rifleman
// fallback. Mirrors the pattern used by render_world.go::drawUnitCube.
func roleOf(ctx InspectorCtx, ent ecs.Entity) components.UnitRoleKind {
	if ctx.RoleMap == nil {
		return components.RoleRifleman
	}
	if r := ctx.RoleMap.Get(ent); r != nil {
		return r.Kind
	}
	return components.RoleRifleman
}

const (
	inspectorFontSize int32 = 14
	inspectorRowH     int32 = 18
	inspectorPadX     int32 = 8
	inspectorPadY     int32 = 6
)

var (
	inspectorBG          = rl.Color{R: 16, G: 18, B: 22, A: 255}
	inspectorText        = rl.Color{R: 220, G: 224, B: 230, A: 255}
	inspectorTextDim     = rl.Color{R: 140, G: 150, B: 160, A: 255}
	inspectorRowHoverBG  = rl.Color{R: 60, G: 70, B: 95, A: 200}
	inspectorRowSelectBG = rl.Color{R: 30, G: 80, B: 110, A: 180}
)

// DrawInspector paints the inspector panel content (background + text rows).
// Phase 10 M10.4 content tree:
//
//   - Empty selection -> "No selection" + list every Squad on the scene.
//   - Single Unit     -> stance / motion / suppression / squad ref / equip.
//   - Single Squad    -> name / members / formation / macro state / roster.
//   - Multi-select    -> counts (units, squads).
//
// Hovered entity (a unit or squad) is highlighted by a tinted row background.
//
// Phase 13.5 M13.5.3: when ctx.Scroll != nil, the initial y is shifted up by
// Scroll.OffsetY (so on-screen content scrolls), and at the end of the draw
// the total used height is written back into Scroll.ContentHeight so the
// scrollbar can size + clamp correctly. Caller (main.go) draws the scrollbar
// after EndScissorMode.
func DrawInspector(panel Panel, ctx InspectorCtx) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	x := int32(content.X) + inspectorPadX
	y0 := int32(content.Y) + inspectorPadY
	scrollOffset := int32(0)
	if ctx.Scroll != nil {
		scrollOffset = int32(ctx.Scroll.OffsetY)
	}
	// Reserve room for the scrollbar on the right so chips/text aren't
	// drawn under it. Scrollbar width is small (8 px) so subtracting per-row
	// would be wasteful; just shrink usable width once.
	usableWidth := int32(content.Width) - 2*inspectorPadX - int32(scrollbarTrackWidth)
	y := y0 - scrollOffset

	var endY int32
	switch describeSelection(ctx) {
	case selEmpty:
		endY = drawInspectorEmpty(ctx, x, y, usableWidth)
	case selSingleUnit:
		endY = drawInspectorUnit(ctx, ctx.Selected[0], x, y)
	case selSingleSquad:
		// One squad selected = the roster's count matches len(selected) and
		// every member shares the same Squad. We resolve the squad through
		// the first member's SquadMember.
		if sm := ctx.SquadMemberMap.Get(ctx.Selected[0]); sm != nil {
			endY = drawInspectorSquad(ctx, sm.Squad, x, y, usableWidth)
		} else {
			endY = y
		}
	case selMulti:
		endY = drawInspectorMulti(ctx, x, y)
	default:
		endY = y
	}

	if ctx.Scroll != nil {
		// Total used height = (endY + scrollOffset) - y0. Add inspectorPadY
		// of bottom padding so the last row isn't hugging the panel edge.
		ctx.Scroll.ContentHeight = float32(endY+scrollOffset-y0) + float32(inspectorPadY)
	}
}

type selectionKind uint8

const (
	selEmpty selectionKind = iota
	selSingleUnit
	selSingleSquad
	selMulti
)

func describeSelection(ctx InspectorCtx) selectionKind {
	if len(ctx.Selected) == 0 {
		return selEmpty
	}
	commonSquad, homo := groupSelectedHelper(ctx.Selected, ctx.SquadMemberMap)
	if !homo {
		return selMulti
	}
	if commonSquad == (ecs.Entity{}) {
		if len(ctx.Selected) == 1 {
			return selSingleUnit
		}
		return selMulti
	}
	// homogeneous squad: single-squad view if the selection equals the entire
	// roster (or any non-empty subset -the inspector still shows the squad).
	return selSingleSquad
}

// groupSelectedHelper mirrors main.groupSelected to keep this package
// self-contained (no UI -> main import).
func groupSelectedHelper(selected []ecs.Entity,
	squadMemberMap *ecs.Map[components.SquadMember]) (ecs.Entity, bool) {
	var common ecs.Entity
	first := true
	for _, e := range selected {
		var s ecs.Entity
		if m := squadMemberMap.Get(e); m != nil {
			s = m.Squad
		}
		if first {
			common = s
			first = false
			continue
		}
		if common != s {
			return ecs.Entity{}, false
		}
	}
	return common, true
}

func drawInspectorEmpty(ctx InspectorCtx, x, y, width int32) int32 {
	drawText(ctx.Font, "No selection", x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2

	drawText(ctx.Font, "Squads on field:", x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH

	q := ctx.SquadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		ent := q.Entity()

		bg := rl.Color{}
		rowText := inspectorText
		if ctx.Hovered == ent {
			bg = inspectorRowHoverBG
		}
		if bg.A != 0 {
			rl.DrawRectangle(x-2, y-2, width, inspectorRowH, bg)
		}

		colorChip := rl.Color{R: 80, G: 80, B: 80, A: 255}
		if ctx.SquadColor != nil {
			colorChip = ctx.SquadColor(ent)
		}
		rl.DrawRectangle(x, y+2, 10, 10, colorChip)
		drawText(ctx.Font, fmt.Sprintf("Squad #%X -%d members",
			ent.ID()&0xFFF, roster.Count),
			x+16, y, inspectorFontSize, rowText)
		y += inspectorRowH
	}
	return y
}

func drawInspectorUnit(ctx InspectorCtx, ent ecs.Entity, x, y int32) int32 {
	if !ctx.World.Alive(ent) {
		drawText(ctx.Font, "(unit no longer alive)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	drawText(ctx.Font, fmt.Sprintf("Unit #%X", ent.ID()&0xFFFF),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	// Phase 12: role header. ShortLabel pill + full name, tinted by role.
	role := roleOf(ctx, ent)
	tint := components.RoleColor(role)
	rl.DrawRectangle(x, y+2, 24, inspectorRowH-4, tint)
	rl.DrawRectangleLines(x, y+2, 24, inspectorRowH-4, rl.Color{R: 20, G: 20, B: 20, A: 220})
	labelColor := contrastTextColor(tint)
	short := role.ShortLabel()
	size := rl.MeasureTextEx(ctx.Font, short, float32(inspectorFontSize), 1)
	rl.DrawTextEx(ctx.Font, short, rl.Vector2{
		X: float32(x) + 12 - size.X*0.5,
		Y: float32(y) + 2 + (float32(inspectorRowH-4)-size.Y)*0.5,
	}, float32(inspectorFontSize), 1, labelColor)
	drawText(ctx.Font, "Role: "+role.String(), x+32, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2

	if st := ctx.StanceMap.Get(ent); st != nil {
		drawText(ctx.Font, "Stance:    "+stanceLabel(st.Code),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		yawDeg := mo.Yaw * 180.0 / math.Pi
		drawText(ctx.Font, fmt.Sprintf("Motion:    %.2f m/s  yaw %.0f°", mo.Speed, yawDeg),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if sup := ctx.SuppressionMap.Get(ent); sup != nil {
		drawText(ctx.Font, fmt.Sprintf("Suppress:  %.2f", sup.Level),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	// Phase 13 M13.6: per-unit Stamina row. When the unit has no Stamina
	// component (legacy / orphaned spawn) we skip rather than printing zeros.
	if ctx.StaminaMap != nil {
		if st := ctx.StaminaMap.Get(ent); st != nil && st.MaxLevel > 0 {
			drawText(ctx.Font, fmt.Sprintf("Stamina:   %.2f / %.2f", st.Current, st.MaxLevel),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	// Phase 14 M14.1: HP row, sits below Stamina (same "tank" UX). Same
	// nil-skip rule as Stamina.
	if ctx.HPMap != nil {
		if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
			drawText(ctx.Font, fmt.Sprintf("HP:        %.1f / %.1f", hp.Current, hp.Max),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	// Phase 14 M14.1: Faction badge. Player squads stay quiet ("Faction:
	// Player"); enemy factions get a short label so the player can tell two
	// MotorRifle squads (one player, one hostile) apart in the Inspector.
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(ent); f != nil {
			drawText(ctx.Font, "Faction:   "+factionLabel(f.ID),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	if sm := ctx.SquadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		drawText(ctx.Font, fmt.Sprintf("Squad:     #%X slot %d", sm.Squad.ID()&0xFFF, sm.SlotIndex),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	} else {
		drawText(ctx.Font, "Squad:     - (soloist)",
			x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
	}
	if eq := ctx.EquipmentMap.Get(ent); eq != nil {
		drawText(ctx.Font, equipmentSummary(eq),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	return y
}

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
	}
	y += inspectorRowH / 2

	// Phase 11: Order section. Head + up to 2 queued.
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
		// Phase 12: row background tint by role. Selection / hover override
		// the role tint so the focus state stays unambiguous.
		bg := components.RoleColor(role)
		bg.A = 90
		if isSelected(ctx.Selected, mem) {
			bg = inspectorRowSelectBG
		}
		if ctx.Hovered == mem {
			bg = inspectorRowHoverBG
		}
		rl.DrawRectangle(x-2, y-2, width, inspectorRowH, bg)

		// ShortLabel chip on the left so the role reads at a glance even
		// when the row tint is dimmed by selection state.
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

	// Phase 13 M13.6: standing-rule quick-bars under the roster. Guard on the
	// new maps so a caller that hasn't wired them (older test) still gets the
	// Phase 12 layout without quick-bars.
	if ctx.MovementProfileMap != nil && ctx.EngagementRulesMap != nil && ctx.BehaviorRulesMap != nil {
		y += inspectorRowH / 2
		y = drawStandingRulesSections(ctx, squad, x, y, width)
	}
	return y
}

// contrastTextColor returns black or white depending on the perceived
// luminance of `bg`, so the small chips / labels stay readable across the
// full role palette.
func contrastTextColor(bg rl.Color) rl.Color {
	lum := 0.299*float32(bg.R) + 0.587*float32(bg.G) + 0.114*float32(bg.B)
	if lum < 140 {
		return rl.Color{R: 255, G: 255, B: 255, A: 255}
	}
	return rl.Color{R: 0, G: 0, B: 0, A: 255}
}

func drawInspectorMulti(ctx InspectorCtx, x, y int32) int32 {
	units := 0
	soloists := 0
	squadSet := make(map[ecs.Entity]struct{})
	for _, e := range ctx.Selected {
		units++
		if sm := ctx.SquadMemberMap.Get(e); sm != nil && sm.Squad != (ecs.Entity{}) {
			squadSet[sm.Squad] = struct{}{}
		} else {
			soloists++
		}
	}
	drawText(ctx.Font, "Mixed selection", x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2
	drawText(ctx.Font, fmt.Sprintf("Units selected: %d", units),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	drawText(ctx.Font, fmt.Sprintf("Squads touched: %d", len(squadSet)),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	drawText(ctx.Font, fmt.Sprintf("Soloists:       %d", soloists),
		x, y, inspectorFontSize, inspectorText)
	return y + inspectorRowH
}

func isSelected(selected []ecs.Entity, e ecs.Entity) bool {
	for i := range selected {
		if selected[i] == e {
			return true
		}
	}
	return false
}

// stanceLabel reads the canonical name from components.StanceSpecs. Phase
// 14.5 M14.5.1 - switch replaced.
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
	return fmt.Sprintf("Macro:     Moving (wp %d/%d)", mp.Head+1, mp.Count)
}

func equipmentSummary(eq *components.Equipment) string {
	primary := "-"
	if eq.Primary != (ecs.Entity{}) {
		primary = fmt.Sprintf("#%X", eq.Primary.ID()&0xFFFF)
	}
	return "Primary:   " + primary
}

func drawText(font rl.Font, s string, x, y, size int32, c rl.Color) {
	rl.DrawTextEx(font, s,
		rl.Vector2{X: float32(x), Y: float32(y)},
		float32(size), 1.0, c)
}

// drawInspectorOrderSection renders the squad's current Order + up to 2
// queued orders. Returns the y-coord after the section so callers can chain
// the next subsection vertically. PHASE-11.md P8.
func drawInspectorOrderSection(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) int32 {
	drawText(ctx.Font, "Order:", x, y, inspectorFontSize, inspectorTextDim)
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

	// Head order row.
	cur := head.First
	y = drawOrderRow(ctx, cur, "> ", x, y, width)
	// Walk chain, render up to 2 more queued (PHASE-11.md P8).
	const maxQueued = 2
	queued := 0
	for queued < maxQueued {
		ch := ctx.OrderChainMap.Get(cur)
		if ch == nil || ch.Next == (ecs.Entity{}) || !ctx.World.Alive(ch.Next) {
			break
		}
		cur = ch.Next
		queued++
		y = drawOrderRow(ctx, cur, "  ", x, y, width)
	}
	return y
}

// drawOrderRow draws one line: "PREFIX OrderKindName state-bar target".
// Returns the new y. Used for both the head and queued orders.
func drawOrderRow(ctx InspectorCtx, ord ecs.Entity, prefix string, x, y, width int32) int32 {
	kind := ctx.OrderKindMap.Get(ord)
	target := ctx.OrderTargetMap.Get(ord)
	state := ctx.OrderStateMap.Get(ord)
	if kind == nil || target == nil || state == nil {
		drawText(ctx.Font, prefix+"(?)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	label := orderKindLabel(kind.Code)
	stateLbl := orderStateLabel(state.Code)
	targetLbl := orderTargetLabel(ctx, kind.Code, target)
	progressTxt := ""
	if pr := ctx.OrderProgressMap.Get(ord); pr != nil && pr.Value > 0 {
		progressTxt = fmt.Sprintf(" %d%%", int(pr.Value*100))
	}
	color := inspectorText
	if state.Code == components.OrderStateBlocked || state.Code == components.OrderStateFailed {
		color = rl.Color{R: 230, G: 110, B: 80, A: 255}
	}
	drawText(ctx.Font, fmt.Sprintf("%s%-12s %-10s%s %s",
		prefix, label, stateLbl, progressTxt, targetLbl),
		x, y, inspectorFontSize, color)
	_ = width
	return y + inspectorRowH
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

// suppress the unused-math warning if math is no longer referenced after
// edits. Currently used in drawInspectorUnit for yaw -> degrees.
var _ = math.Pi
