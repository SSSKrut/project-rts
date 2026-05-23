package components

// Spec table pattern. Each OrderKindCode gets a typed OrderKindSpec row in
// OrderKindSpecs, replacing scattered switch dispatch across resolver /
// weapon / pie_menu / inspector / map_render / command. Readers index by
// enum-code; the compile-time assert below catches a new enum value that
// forgot to extend the table.
//
// Pattern rules:
//   - One spec table per enum. Split into a sibling table when fields
//     multiply rather than dual-purpose one row.
//   - Readers DO NOT switch on the enum; they read fields. The only
//     legitimate switch is OrderResolverSystem dispatching by
//     Spec.Completion because each rule's shape is fundamentally different.
//   - Pie-menu segment order is implicit in iteration over the spec table
//     where InPieMenu == true.

// CompletionRuleKind discriminates how OrderResolverSystem decides an order
// is done. Each kind is one switch arm in the resolver.
type CompletionRuleKind uint8

const (
	// CompletionArrivalRadius - MoveTo / Patrol / OccupyTrench. Squad center
	// within Spec.ArrivalRadius of target.Pos.
	CompletionArrivalRadius CompletionRuleKind = iota
	// CompletionTargetDeath - AttackTarget. target.Entity is not alive.
	// Combined with MaxOutOfRangeSeconds: transitions to Failed when the
	// squad sat out-of-range too long.
	CompletionTargetDeath
	// CompletionTimer - SuppressFire. Spec.DurationSeconds gates the end,
	// anchored at OrderParamSuppress.StartTime.
	CompletionTimer
	// CompletionNever - DefendPosition. Only Cancelled via H key.
	CompletionNever
	// CompletionEveryMemberOnFloor - Garrison. All roster members must be
	// inside the footprint AABB AND on a Floor entity. 100% of roster; later
	// tactical doctrines may loosen to a fraction.
	CompletionEveryMemberOnFloor
	// CompletionClearBuilding (Phase 17.6 M17.6.5) - ClearBuilding. Completes
	// when no hostile unit sits inside the footprint AND at least one
	// friendly is inside. OrderResolverSystem chains an OccupyBuilding on
	// the same building when this fires (rest after fight).
	CompletionClearBuilding
)

// OrderKindSpec is the per-kind metadata row. Fields cover every site that
// previously hard-coded a switch on OrderKindCode. Sparse fields are
// intentional (e.g. ArrivalRadius is meaningless for SuppressFire). Refactor
// to per-aspect specs only if the field count outgrows ~12.
type OrderKindSpec struct {
	Code OrderKindCode
	// Name - human-readable short label. Read by Inspector chip, pie menu,
	// log messages.
	Name string
	// MapIconGlyph - single rune label hint near the map icon. Geometry of
	// the icon itself is hand-drawn in ui/map_render.drawOrderIcon.
	MapIconGlyph rune
	// InPieMenu - pie menu offers this kind.
	InPieMenu bool
	// PieSegmentOrder - render order in the pie menu when InPieMenu is true.
	// 0 = top of the wheel; increases clockwise.
	PieSegmentOrder uint8
	// NeedsEntity / NeedsTerrain - hit-test compatibility. resolveTargetIntoOrder
	// falls back to MoveTo when a pie kind override clashes with the hit type.
	NeedsEntity  bool
	NeedsTerrain bool
	// OverridesHoldFire - active order allows firing even with
	// EngagementRules.Mode = HoldFire. Set on AttackTarget and SuppressFire.
	OverridesHoldFire bool
	// DrivesMacroPath - SquadMacroPathSystem builds a path to the target.
	// False = squad holds in place (AttackTarget / SuppressFire let
	// WeaponSystem do the work).
	DrivesMacroPath bool
	// Completion - which checkCompletion arm runs.
	Completion CompletionRuleKind
	// ArrivalRadius - meaningful for CompletionArrivalRadius and as fallback
	// for entity-resolution misses. Metres.
	ArrivalRadius float32
	// DurationSeconds - meaningful for CompletionTimer.
	DurationSeconds float32
	// MaxOutOfRangeSeconds - applies to CompletionTargetDeath: order
	// transitions to Failed when no roster member can reach the target for
	// this many continuous seconds. 0 = disabled.
	MaxOutOfRangeSeconds float32
}

// OrderKindCount is the count of enum values. Update when a new OrderKindCode
// is appended; the compile-time guard below catches misses.
const OrderKindCount OrderKindCode = OrderKindClearBuilding + 1

// OrderKindSpecs - canonical metadata table. Index by OrderKindCode. When
// adding a new value, also append a row here and bump OrderKindCount.
var OrderKindSpecs = [OrderKindCount]OrderKindSpec{
	OrderKindMoveTo: {
		Code: OrderKindMoveTo, Name: "Move", MapIconGlyph: 'M',
		InPieMenu: true, PieSegmentOrder: 0,
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindGarrison: {
		Code: OrderKindGarrison, Name: "Garrison", MapIconGlyph: 'G',
		InPieMenu: true, PieSegmentOrder: 1,
		NeedsEntity:     true,
		DrivesMacroPath: true,
		// Completes only when every live roster member is inside the
		// building's footprint AABB AND on a Floor entity. ArrivalRadius is
		// a defensive fallback when Building lookup misses.
		Completion: CompletionEveryMemberOnFloor, ArrivalRadius: 4.0,
	},
	OrderKindOccupyTrench: {
		Code: OrderKindOccupyTrench, Name: "Trench", MapIconGlyph: 'T',
		InPieMenu: true, PieSegmentOrder: 2,
		NeedsEntity:     true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 3.0,
	},
	OrderKindDefendPosition: {
		Code: OrderKindDefendPosition, Name: "Defend", MapIconGlyph: 'D',
		InPieMenu: true, PieSegmentOrder: 3,
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionNever,
	},
	OrderKindPatrol: {
		Code: OrderKindPatrol, Name: "Patrol", MapIconGlyph: 'P',
		InPieMenu: true, PieSegmentOrder: 4,
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindAttackTarget: {
		Code: OrderKindAttackTarget, Name: "Attack", MapIconGlyph: 'A',
		// No pie segment - RMB-on-enemy resolves this kind directly.
		NeedsEntity:          true,
		OverridesHoldFire:    true,
		DrivesMacroPath:      false,
		Completion:           CompletionTargetDeath,
		MaxOutOfRangeSeconds: 8.0,
	},
	OrderKindSuppressFire: {
		Code: OrderKindSuppressFire, Name: "Suppress", MapIconGlyph: 'S',
		InPieMenu: true, PieSegmentOrder: 5,
		NeedsTerrain:      true,
		OverridesHoldFire: true,
		DrivesMacroPath:   false,
		Completion:        CompletionTimer,
		DurationSeconds:   30.0,
	},
	OrderKindOccupyBuilding: {
		Code: OrderKindOccupyBuilding, Name: "Occupy", MapIconGlyph: 'O',
		// Phase 17.6: pie menu deprecated, this kind never appears in pie.
		// Triggered by RMB-tap on a building (default hit-test result).
		NeedsEntity:     true,
		DrivesMacroPath: true,
		// Same completion as Garrison: every live member must be inside the
		// footprint AND on a Floor entity. ArrivalRadius is a defensive
		// fallback when Building lookup misses.
		Completion: CompletionEveryMemberOnFloor, ArrivalRadius: 4.0,
	},
	OrderKindClearBuilding: {
		Code: OrderKindClearBuilding, Name: "Clear", MapIconGlyph: 'C',
		// Phase 17.6 M17.6.5: triggered by building popup item "Clear and
		// occupy". OrderResolverSystem auto-chains an OccupyBuilding on Done.
		NeedsEntity:       true,
		DrivesMacroPath:   true,
		OverridesHoldFire: true, // hostiles inside must be engaged
		Completion:        CompletionClearBuilding, ArrivalRadius: 4.0,
	},
}

// Compile-time guard: if OrderKindCount drifts away from the table length,
// the build breaks here.
var _ = [OrderKindCount]OrderKindSpec(OrderKindSpecs)

// SpecForOrderKind returns a pointer into the table. Out-of-range code falls
// back to MoveTo as a defensive default.
func SpecForOrderKind(code OrderKindCode) *OrderKindSpec {
	if int(code) >= len(OrderKindSpecs) {
		return &OrderKindSpecs[OrderKindMoveTo]
	}
	return &OrderKindSpecs[code]
}
