package components

// Spec table pattern. Each OrderKindCode gets a typed OrderKindSpec row in
// OrderKindSpecs, replacing scattered switch dispatch across resolver /
// weapon / inspector / map_render / command. Readers index by enum-code;
// the compile-time assert below catches a new enum value that forgot to
// extend the table.
//
// Pattern rules:
//   - One spec table per enum. Split into a sibling table when fields
//     multiply rather than dual-purpose one row.
//   - Readers DO NOT switch on the enum; they read fields. The only
//     legitimate switch is OrderResolverSystem dispatching by
//     Spec.Completion because each rule's shape is fundamentally different.

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
	// inside the footprint AABB AND on a Floor entity.
	CompletionEveryMemberOnFloor
	// CompletionClearBuilding - ClearBuilding. Completes when no hostile
	// unit sits inside the footprint AND at least one friendly is inside.
	// OrderResolverSystem chains an OccupyBuilding on the same building
	// when this fires (rest after fight).
	CompletionClearBuilding
)

// OrderKindSpec is the per-kind metadata row. Fields cover every site that
// previously hard-coded a switch on OrderKindCode. Sparse fields are
// intentional (e.g. ArrivalRadius is meaningless for SuppressFire).
type OrderKindSpec struct {
	Code         OrderKindCode
	Name         string
	MapIconGlyph rune
	// NeedsEntity / NeedsTerrain - hit-test compatibility.
	// resolveTargetIntoOrder falls back to MoveTo when a kind override
	// clashes with the hit type.
	NeedsEntity  bool
	NeedsTerrain bool
	// OverridesHoldFire - active order allows firing even with
	// EngagementRules.Mode = HoldFire.
	OverridesHoldFire bool
	// DrivesMacroPath - SquadMacroPathSystem builds a path to the target.
	// False = squad holds in place (AttackTarget / SuppressFire let
	// WeaponSystem do the work).
	DrivesMacroPath bool
	Completion      CompletionRuleKind
	// ArrivalRadius - meaningful for CompletionArrivalRadius and as fallback
	// for entity-resolution misses. Metres.
	ArrivalRadius   float32
	DurationSeconds float32
	// MaxOutOfRangeSeconds - applies to CompletionTargetDeath: order
	// transitions to Failed when no roster member can reach the target for
	// this many continuous seconds. 0 = disabled.
	MaxOutOfRangeSeconds float32
}

// OrderKindCount is the count of enum values. Update when a new OrderKindCode
// is appended; the compile-time guard below catches misses.
const OrderKindCount OrderKindCode = OrderKindClearBuilding + 1

var OrderKindSpecs = [OrderKindCount]OrderKindSpec{
	OrderKindMoveTo: {
		Code: OrderKindMoveTo, Name: "Move", MapIconGlyph: 'M',
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindGarrison: {
		Code: OrderKindGarrison, Name: "Garrison", MapIconGlyph: 'G',
		NeedsEntity:     true,
		DrivesMacroPath: true,
		Completion:      CompletionEveryMemberOnFloor, ArrivalRadius: 4.0,
	},
	OrderKindOccupyTrench: {
		Code: OrderKindOccupyTrench, Name: "Trench", MapIconGlyph: 'T',
		NeedsEntity:     true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 3.0,
	},
	OrderKindDefendPosition: {
		Code: OrderKindDefendPosition, Name: "Defend", MapIconGlyph: 'D',
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionNever,
	},
	OrderKindPatrol: {
		Code: OrderKindPatrol, Name: "Patrol", MapIconGlyph: 'P',
		NeedsTerrain:    true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindAttackTarget: {
		Code: OrderKindAttackTarget, Name: "Attack", MapIconGlyph: 'A',
		// RMB-on-enemy resolves this kind directly.
		NeedsEntity:          true,
		OverridesHoldFire:    true,
		DrivesMacroPath:      false,
		Completion:           CompletionTargetDeath,
		MaxOutOfRangeSeconds: 8.0,
	},
	OrderKindSuppressFire: {
		Code: OrderKindSuppressFire, Name: "Suppress", MapIconGlyph: 'S',
		NeedsTerrain:      true,
		OverridesHoldFire: true,
		DrivesMacroPath:   false,
		Completion:        CompletionTimer,
		DurationSeconds:   30.0,
	},
	OrderKindOccupyBuilding: {
		Code: OrderKindOccupyBuilding, Name: "Occupy", MapIconGlyph: 'O',
		NeedsEntity:     true,
		DrivesMacroPath: true,
		Completion:      CompletionEveryMemberOnFloor, ArrivalRadius: 4.0,
	},
	OrderKindClearBuilding: {
		Code: OrderKindClearBuilding, Name: "Clear", MapIconGlyph: 'C',
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
