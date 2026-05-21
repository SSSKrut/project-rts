package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// InspectorMaps is the bundle of ECS handles the inspector reads. Built once
// at startup via NewInspectorMaps(world); embedded into InspectorCtx so call
// sites stay `ctx.StanceMap.Get(...)` style with no manual plumbing in
// main.go (was ~30 fields hand-wired per frame pre-refactor).
type InspectorMaps struct {
	PosMap                   *ecs.Map[components.WorldPos]
	StanceMap                *ecs.Map[components.Stance]
	MotionMap                *ecs.Map[components.Motion]
	ThreatMap                *ecs.Map[components.Threat]
	EquipmentMap             *ecs.Map[components.Equipment]
	SquadMemberMap           *ecs.Map[components.SquadMember]
	RosterMap                *ecs.Map[components.CommandRoster]
	FormationDataMap         *ecs.Map[components.FormationData]
	MacroPathMap             *ecs.Map[components.MacroPath]
	SquadFilter              *ecs.Filter2[components.Squad, components.CommandRoster]
	OrderQueueMap            *ecs.Map[components.OrderQueueHead]
	OrderKindMap             *ecs.Map[components.OrderKind]
	OrderStateMap            *ecs.Map[components.OrderState]
	OrderTargetMap           *ecs.Map[components.OrderTarget]
	OrderProgressMap         *ecs.Map[components.OrderProgress]
	OrderChainMap            *ecs.Map[components.OrderChain]
	BuildingMap              *ecs.Map[components.Building]
	TrenchRootMap            *ecs.Map[components.TrenchRoot]
	RoleMap                  *ecs.Map[components.UnitRole]
	MovementProfileMap       *ecs.Map[components.MovementProfile]
	EngagementRulesMap       *ecs.Map[components.EngagementRules]
	BehaviorRulesMap         *ecs.Map[components.BehaviorRules]
	StaminaMap               *ecs.Map[components.Stamina]
	OrderAttackMoveMap       *ecs.Map[components.OrderParamAttackMove]
	OrderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	HPMap                    *ecs.Map[components.HP]
	FactionMap               *ecs.Map[components.Faction]
	TacticalOverrideMap      *ecs.Map[components.TacticalOverride]
	SquadStateMap            *ecs.Map[components.SquadState]
	IndividualPositionMap    *ecs.Map[components.IndividualPosition]
	ActiveDoctrineMap        *ecs.Map[components.ActiveDoctrine]
	ActiveAutonomyMap        *ecs.Map[components.ActiveAutonomy]
	BehaviorRulesEditMap     *ecs.Map[components.BehaviorRulesEdit]
}

// NewInspectorMaps pre-builds every read-handle the inspector needs. Cheap
// to call - Ark Map[T] is a thin archetype-aware accessor, not the storage
// itself, so duplicating with other systems' handles costs nothing.
func NewInspectorMaps(world *ecs.World) InspectorMaps {
	return InspectorMaps{
		PosMap:                   ecs.NewMap[components.WorldPos](world),
		StanceMap:                ecs.NewMap[components.Stance](world),
		MotionMap:                ecs.NewMap[components.Motion](world),
		ThreatMap:                ecs.NewMap[components.Threat](world),
		EquipmentMap:             ecs.NewMap[components.Equipment](world),
		SquadMemberMap:           ecs.NewMap[components.SquadMember](world),
		RosterMap:                ecs.NewMap[components.CommandRoster](world),
		FormationDataMap:         ecs.NewMap[components.FormationData](world),
		MacroPathMap:             ecs.NewMap[components.MacroPath](world),
		SquadFilter:              ecs.NewFilter2[components.Squad, components.CommandRoster](world),
		OrderQueueMap:            ecs.NewMap[components.OrderQueueHead](world),
		OrderKindMap:             ecs.NewMap[components.OrderKind](world),
		OrderStateMap:            ecs.NewMap[components.OrderState](world),
		OrderTargetMap:           ecs.NewMap[components.OrderTarget](world),
		OrderProgressMap:         ecs.NewMap[components.OrderProgress](world),
		OrderChainMap:            ecs.NewMap[components.OrderChain](world),
		BuildingMap:              ecs.NewMap[components.Building](world),
		TrenchRootMap:            ecs.NewMap[components.TrenchRoot](world),
		RoleMap:                  ecs.NewMap[components.UnitRole](world),
		MovementProfileMap:       ecs.NewMap[components.MovementProfile](world),
		EngagementRulesMap:       ecs.NewMap[components.EngagementRules](world),
		BehaviorRulesMap:         ecs.NewMap[components.BehaviorRules](world),
		StaminaMap:               ecs.NewMap[components.Stamina](world),
		OrderAttackMoveMap:       ecs.NewMap[components.OrderParamAttackMove](world),
		OrderMovementOverrideMap: ecs.NewMap[components.OrderParamMovementProfile](world),
		HPMap:                    ecs.NewMap[components.HP](world),
		FactionMap:               ecs.NewMap[components.Faction](world),
		TacticalOverrideMap:      ecs.NewMap[components.TacticalOverride](world),
		SquadStateMap:            ecs.NewMap[components.SquadState](world),
		IndividualPositionMap:    ecs.NewMap[components.IndividualPosition](world),
		ActiveDoctrineMap:        ecs.NewMap[components.ActiveDoctrine](world),
		ActiveAutonomyMap:        ecs.NewMap[components.ActiveAutonomy](world),
		BehaviorRulesEditMap:     ecs.NewMap[components.BehaviorRulesEdit](world),
	}
}

// InspectorCtx bundles all the data the inspector reads. Built once per frame
// in main.go and passed by value. The Maps slot is embedded so call sites
// stay `ctx.StanceMap.Get(...)` via Go field promotion.
type InspectorCtx struct {
	InspectorMaps
	World    *ecs.World
	Selected []ecs.Entity
	Hovered  ecs.Entity
	Font     rl.Font
	// EventLog isn't a map - kept separate from InspectorMaps so the resource
	// stays explicit at the call site.
	EventLog *components.EventLog
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
	// at the end of the draw - caller's DrawScrollbar reads it next frame. Nil
	// -> behave as if scroll==0 with no measurement (legacy callers).
	Scroll *ScrollState
	// SquadColor picks a stable palette colour from a squad entity so the
	// inspector and the map render use the same shade. Injected as a func to
	// avoid a UI -> render-package cycle. Phase 14 M14.6: signature takes
	// ecs.Entity (not just ID) so the colour function can read Faction off the
	// squad entity directly.
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
	inspectorFontSize int32 = 16
	inspectorRowH     int32 = 22
	inspectorPadX     int32 = 10
	inspectorPadY     int32 = 8
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
//
// Section helpers live in sibling files:
//
//	inspector_unit.go      single-unit panel + role / faction / stance labels
//	inspector_squad.go     single-squad panel + formation / macro labels
//	inspector_orders.go    order rows + progress bar + AttackMove pill
//	inspector_override.go  TacticalOverride block + threat label
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
	// Reserve room for the scrollbar on the right so chips/text aren't drawn
	// under it. Scrollbar width is small (8 px) so subtracting per-row would
	// be wasteful; just shrink usable width once.
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
		// Total used height = (endY + scrollOffset) - y0. Add inspectorPadY of
		// bottom padding so the last row isn't hugging the panel edge.
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
	// roster (or any non-empty subset - the inspector still shows the squad).
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

	// Phase 15 M15.C.2 - recent events feed. Surfaces KIA / OrderCompleted /
	// SuppressionStart even when no squad is selected.
	if ctx.EventLog != nil && ctx.EventLog.Count > 0 {
		y += inspectorRowH
		drawText(ctx.Font, "Recent events:", x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
		for _, ev := range ctx.EventLog.Latest(8) {
			color := inspectorText
			switch ev.Kind {
			case components.EventKIA, components.EventOrderFailed:
				color = rl.Color{R: 230, G: 110, B: 80, A: 255}
			case components.EventSuppressionStart:
				color = rl.Color{R: 230, G: 170, B: 90, A: 255}
			}
			drawText(ctx.Font,
				fmt.Sprintf("[%5.1fs] %-10s %s", ev.At,
					components.EventKindLabel(ev.Kind), ev.Text),
				x, y, inspectorFontSize, color)
			y += inspectorRowH
		}
	}
	return y
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

func drawText(font rl.Font, s string, x, y, size int32, c rl.Color) {
	rl.DrawTextEx(font, s,
		rl.Vector2{X: float32(x), Y: float32(y)},
		float32(size), 1.0, c)
}
