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
	// SquadColor picks a stable palette colour from a squad entity ID so the
	// inspector and the map render use the same shade. Injected as a func to
	// avoid a UI → render-package cycle.
	SquadColor func(id uint32) rl.Color
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
//   - Empty selection → "No selection" + list every Squad on the scene.
//   - Single Unit     → stance / motion / suppression / squad ref / equip.
//   - Single Squad    → name / members / formation / macro state / roster.
//   - Multi-select    → counts (units, squads).
//
// Hovered entity (a unit or squad) is highlighted by a tinted row background.
func DrawInspector(panel Panel, ctx InspectorCtx) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	x := int32(content.X) + inspectorPadX
	y := int32(content.Y) + inspectorPadY

	switch describeSelection(ctx) {
	case selEmpty:
		drawInspectorEmpty(ctx, x, y, int32(content.Width)-2*inspectorPadX)
	case selSingleUnit:
		drawInspectorUnit(ctx, ctx.Selected[0], x, y)
	case selSingleSquad:
		// One squad selected = the roster's count matches len(selected) and
		// every member shares the same Squad. We resolve the squad through
		// the first member's SquadMember.
		if sm := ctx.SquadMemberMap.Get(ctx.Selected[0]); sm != nil {
			drawInspectorSquad(ctx, sm.Squad, x, y, int32(content.Width)-2*inspectorPadX)
		}
	case selMulti:
		drawInspectorMulti(ctx, x, y)
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
// self-contained (no UI → main import).
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

func drawInspectorEmpty(ctx InspectorCtx, x, y, width int32) {
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
			colorChip = ctx.SquadColor(ent.ID())
		}
		rl.DrawRectangle(x, y+2, 10, 10, colorChip)
		drawText(ctx.Font, fmt.Sprintf("Squad #%X -%d members",
			ent.ID()&0xFFF, roster.Count),
			x+16, y, inspectorFontSize, rowText)
		y += inspectorRowH
	}
}

func drawInspectorUnit(ctx InspectorCtx, ent ecs.Entity, x, y int32) {
	if !ctx.World.Alive(ent) {
		drawText(ctx.Font, "(unit no longer alive)", x, y, inspectorFontSize, inspectorTextDim)
		return
	}
	drawText(ctx.Font, fmt.Sprintf("Unit #%X", ent.ID()&0xFFFF),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	drawText(ctx.Font, "Role: Rifleman (placeholder)",
		x, y, inspectorFontSize, inspectorTextDim)
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
}

func drawInspectorSquad(ctx InspectorCtx, squad ecs.Entity, x, y, width int32) {
	if !ctx.World.Alive(squad) {
		drawText(ctx.Font, "(squad destroyed)", x, y, inspectorFontSize, inspectorTextDim)
		return
	}
	roster := ctx.RosterMap.Get(squad)
	if roster == nil {
		drawText(ctx.Font, "(no roster)", x, y, inspectorFontSize, inspectorTextDim)
		return
	}

	colorChip := rl.Color{R: 80, G: 80, B: 80, A: 255}
	if ctx.SquadColor != nil {
		colorChip = ctx.SquadColor(squad.ID())
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
	drawText(ctx.Font, "Roster:", x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH

	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		bg := rl.Color{}
		if isSelected(ctx.Selected, mem) {
			bg = inspectorRowSelectBG
		}
		if ctx.Hovered == mem {
			bg = inspectorRowHoverBG
		}
		if bg.A != 0 {
			rl.DrawRectangle(x-2, y-2, width, inspectorRowH, bg)
		}
		stance := "-"
		if st := ctx.StanceMap.Get(mem); st != nil {
			stance = stanceLabel(st.Code)
		}
		drawText(ctx.Font, fmt.Sprintf("  %d  #%-6X %s", i, mem.ID()&0xFFFFFF, stance),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
}

func drawInspectorMulti(ctx InspectorCtx, x, y int32) {
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
}

func isSelected(selected []ecs.Entity, e ecs.Entity) bool {
	for i := range selected {
		if selected[i] == e {
			return true
		}
	}
	return false
}

func stanceLabel(s components.StanceCode) string {
	switch s {
	case components.StanceCrouch:
		return "Crouch"
	case components.StanceProne:
		return "Prone"
	default:
		return "Stand"
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
