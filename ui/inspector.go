package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// InspectorMaps bundles ECS read-handles. Built once at startup via
// NewInspectorMaps; embedded into InspectorCtx so call sites stay
// `ctx.StanceMap.Get(...)` via Go field promotion.
type InspectorMaps struct {
	PosMap                *ecs.Map[components.WorldPos]
	StanceMap             *ecs.Map[components.Stance]
	MotionMap             *ecs.Map[components.Motion]
	ThreatMap             *ecs.Map[components.Threat]
	EquipmentMap          *ecs.Map[components.Equipment]
	SquadMemberMap        *ecs.Map[components.SquadMember]
	RosterMap             *ecs.Map[components.CommandRoster]
	FormationDataMap      *ecs.Map[components.FormationData]
	MacroPathMap          *ecs.Map[components.MacroPath]
	SquadFilter           *ecs.Filter2[components.Squad, components.CommandRoster]
	OrderQueueMap         *ecs.Map[components.OrderQueueHead]
	OrderKindMap          *ecs.Map[components.OrderKind]
	OrderStateMap         *ecs.Map[components.OrderState]
	OrderTargetMap        *ecs.Map[components.OrderTarget]
	OrderProgressMap      *ecs.Map[components.OrderProgress]
	OrderChainMap         *ecs.Map[components.OrderChain]
	TrenchRootMap         *ecs.Map[components.TrenchRoot]
	RoleMap               *ecs.Map[components.UnitRole]
	EngagementRulesMap    *ecs.Map[components.EngagementRules]
	BehaviorRulesMap      *ecs.Map[components.BehaviorRules]
	StaminaMap            *ecs.Map[components.Stamina]
	OrderAttackMoveMap    *ecs.Map[components.OrderParamAttackMove]
	OrderEngagementMap    *ecs.Map[components.OrderParamEngagementOverride]
	HPMap                 *ecs.Map[components.HP]
	FactionMap            *ecs.Map[components.Faction]
	TacticalOverrideMap   *ecs.Map[components.TacticalOverride]
	SquadStateMap         *ecs.Map[components.SquadState]
	IndividualPositionMap *ecs.Map[components.IndividualPosition]
	ContactMap            *ecs.Map[components.Contact]
	ContactOverrideMap    *ecs.Map[components.ContactSymbolOverride]
	VehicleMap            *ecs.Map[components.Vehicle]
	RoadFollowerMap       *ecs.Map[components.RoadFollower]
	WeaponMap             *ecs.Map[components.Weapon]
	VehicleOverrideMap    *ecs.Map[components.VehicleOverride]
}

func NewInspectorMaps(world *ecs.World) InspectorMaps {
	return InspectorMaps{
		PosMap:                ecs.NewMap[components.WorldPos](world),
		StanceMap:             ecs.NewMap[components.Stance](world),
		MotionMap:             ecs.NewMap[components.Motion](world),
		ThreatMap:             ecs.NewMap[components.Threat](world),
		EquipmentMap:          ecs.NewMap[components.Equipment](world),
		SquadMemberMap:        ecs.NewMap[components.SquadMember](world),
		RosterMap:             ecs.NewMap[components.CommandRoster](world),
		FormationDataMap:      ecs.NewMap[components.FormationData](world),
		MacroPathMap:          ecs.NewMap[components.MacroPath](world),
		SquadFilter:           ecs.NewFilter2[components.Squad, components.CommandRoster](world),
		OrderQueueMap:         ecs.NewMap[components.OrderQueueHead](world),
		OrderKindMap:          ecs.NewMap[components.OrderKind](world),
		OrderStateMap:         ecs.NewMap[components.OrderState](world),
		OrderTargetMap:        ecs.NewMap[components.OrderTarget](world),
		OrderProgressMap:      ecs.NewMap[components.OrderProgress](world),
		OrderChainMap:         ecs.NewMap[components.OrderChain](world),
		TrenchRootMap:         ecs.NewMap[components.TrenchRoot](world),
		RoleMap:               ecs.NewMap[components.UnitRole](world),
		EngagementRulesMap:    ecs.NewMap[components.EngagementRules](world),
		BehaviorRulesMap:      ecs.NewMap[components.BehaviorRules](world),
		StaminaMap:            ecs.NewMap[components.Stamina](world),
		OrderAttackMoveMap:    ecs.NewMap[components.OrderParamAttackMove](world),
		OrderEngagementMap:    ecs.NewMap[components.OrderParamEngagementOverride](world),
		HPMap:                 ecs.NewMap[components.HP](world),
		FactionMap:            ecs.NewMap[components.Faction](world),
		TacticalOverrideMap:   ecs.NewMap[components.TacticalOverride](world),
		SquadStateMap:         ecs.NewMap[components.SquadState](world),
		IndividualPositionMap: ecs.NewMap[components.IndividualPosition](world),
		ContactMap:            ecs.NewMap[components.Contact](world),
		ContactOverrideMap:    ecs.NewMap[components.ContactSymbolOverride](world),
		VehicleMap:            ecs.NewMap[components.Vehicle](world),
		RoadFollowerMap:       ecs.NewMap[components.RoadFollower](world),
		WeaponMap:             ecs.NewMap[components.Weapon](world),
		VehicleOverrideMap:    ecs.NewMap[components.VehicleOverride](world),
	}
}

// InspectorCtx is built once per frame and passed by value. PanelFocused
// gates chip clicks so they don't react to drags from other panels.
type InspectorCtx struct {
	InspectorMaps
	// Behavior is the standing-rule bundle, borrowed READ-ONLY for the quick
	// badges. Editing lives in the Behavior panel; sharing the bundle keeps
	// the handles in one place instead of re-registering them here.
	Behavior BehaviorMaps
	World    *ecs.World
	Selected []ecs.Entity
	Hovered  ecs.Entity
	Font     rl.Font
	EventLog *components.EventLog
	// Now is sim time in seconds (App.Elapsed) — the same clock
	// ContactSystem stamps into Contact.LastSeenTime.
	Now          float32
	Cursor       rl.Vector2
	LMBPressed   bool
	PanelFocused bool
	// Nil Scroll -> behave as if scroll==0 with no measurement.
	// DrawInspector writes total content height into ContentHeight at
	// end of draw; caller's DrawScrollbar reads it next frame.
	Scroll *ScrollState
	// SquadColor is injected to avoid a UI -> render-package cycle.
	SquadColor func(ent ecs.Entity) rl.Color
	// RoadGraph resolves RoadFollower.Edge into a kind label.
	RoadGraph *components.RoadGraph

	// Widget palette and pointer state, filled by DrawInspector and carried
	// down by value so section functions don't rebuild them per call.
	st Style
	in WidgetInput
}

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
	inspectorHighlight   = rl.Color{R: 240, G: 190, B: 90, A: 255}
)

// DrawInspector dispatches on selection:
//   - Empty -> "No selection" + list every Squad.
//   - Single Unit / Single Squad / Multi -> per-flavour panels.
//
// When ctx.Scroll != nil, the initial y is shifted by Scroll.OffsetY and
// the total used height is written back into Scroll.ContentHeight at the
// end of draw so the scrollbar can size and clamp on the next frame.
func DrawInspector(panel Panel, ctx InspectorCtx) {
	ctx.st = InspectorStyle(ctx.Font)
	ctx.in = WidgetInput{Cursor: ctx.Cursor, Press: ctx.LMBPressed, Enabled: ctx.PanelFocused}
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	sc := BeginScroll(content, ctx.Scroll, float32(inspectorPadX), float32(inspectorPadY))
	defer sc.End()

	x := int32(sc.Col.X)
	y := int32(sc.Col.Y)
	usableWidth := int32(sc.Col.W)

	var endY int32
	switch describeSelection(ctx) {
	case selEmpty:
		endY = drawInspectorEmpty(ctx, x, y, usableWidth)
	case selSingleUnit:
		endY = drawInspectorUnit(ctx, ctx.Selected[0], x, y, usableWidth)
	case selSingleVehicle:
		endY = drawInspectorVehicle(ctx, ctx.Selected[0], x, y, usableWidth)
	case selSingleSquad:
		if sm := ctx.SquadMemberMap.Get(ctx.Selected[0]); sm != nil {
			endY = drawInspectorSquad(ctx, sm.Squad, x, y, usableWidth)
		} else {
			endY = y
		}
	case selSingleContact:
		endY = drawInspectorContact(ctx, ctx.Selected[0], x, y, usableWidth)
	case selMulti:
		endY = drawInspectorMulti(ctx, x, y, usableWidth)
	default:
		endY = y
	}

	sc.Col.Y = float32(endY)
}

type selectionKind uint8

const (
	selEmpty selectionKind = iota
	selSingleUnit
	selSingleVehicle
	selSingleSquad
	selSingleContact
	selMulti
)

func describeSelection(ctx InspectorCtx) selectionKind {
	if len(ctx.Selected) == 0 {
		return selEmpty
	}
	if len(ctx.Selected) == 1 && ctx.ContactMap != nil {
		if ctx.ContactMap.Has(ctx.Selected[0]) {
			return selSingleContact
		}
	}
	if len(ctx.Selected) == 1 && ctx.VehicleMap != nil {
		if ctx.VehicleMap.Has(ctx.Selected[0]) {
			return selSingleVehicle
		}
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
	return selSingleSquad
}

// groupSelectedHelper mirrors main.groupSelected to keep this package
// free of a main import.
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

// SelectSquadRequest is raised when a row in the "Squads on field" list is
// clicked. main.go resolves it into a unit selection — the ui package has no
// business knowing what "selected" means.
var SelectSquadRequest struct {
	Active bool
	Squad  ecs.Entity
}

// SelectUnitRequest is the same contract for a roster row or a squad-bar
// card: narrow the selection to one member. Additive mirrors Shift+LMB in the
// 3D view — toggle instead of replace.
var SelectUnitRequest struct {
	Active   bool
	Unit     ecs.Entity
	Additive bool
}

// OpenWidgetRequest asks the host to float a widget — raised by the quick
// badges ("show me where these are edited"). The ui package can't spawn
// floaters itself; the host owns the render closures.
var OpenWidgetRequest struct {
	Active bool
	Panel  PanelID
}

func drawInspectorEmpty(ctx InspectorCtx, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	TextRow(&col, &ctx.st, "No selection", ctx.st.Text)
	col.Skip(ctx.st.RowH)
	TextRow(&col, &ctx.st, "Squads on field:", ctx.st.TextDim)

	q := ctx.SquadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		ent := q.Entity()

		row := col.Band(ctx.st.RowH)
		// The band is inset so a highlighted row reads as a list item rather
		// than as text with a box around it.
		band := rl.Rectangle{X: row.X - 2, Y: row.Y - 2, Width: row.Width, Height: row.Height}
		if ctx.in.Hover(band) || ctx.Hovered == ent {
			rl.DrawRectangleRec(band, inspectorRowHoverBG)
		}
		if ctx.in.Clicked(band) {
			SelectSquadRequest.Active = true
			SelectSquadRequest.Squad = ent
		}

		colorChip := rl.Color{R: 80, G: 80, B: 80, A: 255}
		if ctx.SquadColor != nil {
			colorChip = ctx.SquadColor(ent)
		}
		rl.DrawRectangle(int32(row.X), int32(row.Y)+2, 10, 10, colorChip)
		Text(&ctx.st, rl.Rectangle{X: row.X + 16, Y: row.Y},
			fmt.Sprintf("Squad #%X -%d members", ent.ID()&0xFFF, roster.Count),
			ctx.st.Text)
	}

	if ctx.EventLog != nil && ctx.EventLog.Count > 0 {
		col.Skip(ctx.st.RowH)
		TextRow(&col, &ctx.st, "Recent events:", ctx.st.TextDim)
		for _, ev := range ctx.EventLog.Latest(8) {
			color := ctx.st.Text
			switch ev.Kind {
			case components.EventKIA, components.EventOrderFailed:
				color = rl.Color{R: 230, G: 110, B: 80, A: 255}
			case components.EventSuppressionStart:
				color = rl.Color{R: 230, G: 170, B: 90, A: 255}
			}
			TextRow(&col, &ctx.st, fmt.Sprintf("[%5.1fs] %-10s %s", ev.At,
				components.EventKindLabel(ev.Kind), ev.Text), color)
		}
	}
	return int32(col.Y)
}

func drawInspectorMulti(ctx InspectorCtx, x, y, width int32) int32 {
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
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	TextRow(&col, &ctx.st, "Mixed selection", ctx.st.Text)
	col.Skip(ctx.st.RowH)
	TextRow(&col, &ctx.st, fmt.Sprintf("Units selected: %d", units), ctx.st.Text)
	TextRow(&col, &ctx.st, fmt.Sprintf("Squads touched: %d", len(squadSet)), ctx.st.Text)
	TextRow(&col, &ctx.st, fmt.Sprintf("Soloists:       %d", soloists), ctx.st.Text)
	return int32(col.Y)
}
