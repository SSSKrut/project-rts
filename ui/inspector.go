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
	ContactMap               *ecs.Map[components.Contact]
	ContactOverrideMap       *ecs.Map[components.ContactSymbolOverride]
	UnitOverrideMap          *ecs.Map[components.UnitSymbolOverride]
	VehicleMap               *ecs.Map[components.Vehicle]
	RoadFollowerMap          *ecs.Map[components.RoadFollower]
	WeaponMap                *ecs.Map[components.Weapon]
	VehicleOverrideMap       *ecs.Map[components.VehicleOverride]
}

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
		ContactMap:               ecs.NewMap[components.Contact](world),
		ContactOverrideMap:       ecs.NewMap[components.ContactSymbolOverride](world),
		UnitOverrideMap:          ecs.NewMap[components.UnitSymbolOverride](world),
		VehicleMap:               ecs.NewMap[components.Vehicle](world),
		RoadFollowerMap:          ecs.NewMap[components.RoadFollower](world),
		WeaponMap:                ecs.NewMap[components.Weapon](world),
		VehicleOverrideMap:       ecs.NewMap[components.VehicleOverride](world),
	}
}

// InspectorCtx is built once per frame and passed by value. PanelFocused
// gates chip clicks so they don't react to drags from other panels.
type InspectorCtx struct {
	InspectorMaps
	World        *ecs.World
	Selected     []ecs.Entity
	Hovered      ecs.Entity
	Font         rl.Font
	EventLog     *components.EventLog
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
		endY = drawInspectorUnit(ctx, ctx.Selected[0], x, y)
	case selSingleVehicle:
		endY = drawInspectorVehicle(ctx, ctx.Selected[0], x, y)
	case selSingleSquad:
		if sm := ctx.SquadMemberMap.Get(ctx.Selected[0]); sm != nil {
			endY = drawInspectorSquad(ctx, sm.Squad, x, y, usableWidth)
		} else {
			endY = y
		}
	case selSingleContact:
		endY = drawInspectorContact(ctx, ctx.Selected[0], x, y)
	case selMulti:
		endY = drawInspectorMulti(ctx, x, y)
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

// drawText is the int32 shorthand the inspector rows are written in; it
// funnels into the same Text primitive every other surface uses.
func drawText(font rl.Font, s string, x, y, size int32, c rl.Color) {
	st := Style{Font: font, FontSize: float32(size)}
	Text(&st, rl.Rectangle{X: float32(x), Y: float32(y)}, s, c)
}
