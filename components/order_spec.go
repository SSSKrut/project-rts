package components

// Phase 14.5 P1 — Spec table pattern. Each `OrderKindCode` gets a typed
// `OrderKindSpec` row in `OrderKindSpecs`, replacing scattered switch dispatch
// across order_resolver / weapon / pie_menu / inspector / map_render /
// command. Readers index the array by enum-code, no fallback path needed —
// the compile-time assert below catches any new enum value that forgot to
// extend the table.
//
// Pattern rules:
//   - One spec table per enum. Don't dual-purpose ("OrderKindSpecs also holds
//     audio data" etc.) — split into a sibling table when fields multiply.
//   - Readers DO NOT switch on the enum; they read fields. The only legitimate
//     switch is the one in `OrderResolverSystem.checkCompletion` that
//     dispatches by `Spec.Completion`, because each rule has a fundamentally
//     different shape (some need a roster walk, others a timer comparison).
//   - Pie-menu segment order is implicit in iteration order over the spec
//     table where `InPieMenu == true`. Adding a new kind to the menu = setting
//     one flag, no separate `pieSegments` slice to keep in sync.

// CompletionRuleKind discriminates how `OrderResolverSystem.checkCompletion`
// decides an order is done. Each kind is one switch arm in the resolver —
// new rules add a single case there. Spec.Completion picks which arm runs.
type CompletionRuleKind uint8

const (
	// CompletionArrivalRadius — MoveTo / Patrol / OccupyTrench / Garrison
	// fallback. "Squad center within arrivalRadius of target.Pos" — the
	// per-kind radius lives in OrderKindSpec.ArrivalRadius.
	CompletionArrivalRadius CompletionRuleKind = iota
	// CompletionTargetDeath — AttackTarget. "target.Entity is not alive."
	// Combined with `MaxOutOfRangeSeconds` (Issue #10 fix) which transitions
	// to Failed when the squad sat out-of-range too long.
	CompletionTargetDeath
	// CompletionTimer — SuppressFire. Spec.DurationSeconds gates the end.
	// Read from OrderParamSuppress.StartTime + duration.
	CompletionTimer
	// CompletionNever — DefendPosition. Only Cancelled via the player's H key.
	CompletionNever
	// CompletionEveryMemberOnFloor — Garrison (M14.5.6 will wire). All roster
	// members must be inside the building footprint AND on a FloorNavGrid
	// cell. Phase 14.5 simple: 100% of roster. Phase 15 may loosen to 50%.
	CompletionEveryMemberOnFloor
)

// OrderKindSpec is the per-kind metadata row. Fields cover every site that
// previously hard-coded a switch on OrderKindCode.
//
// Sparse fields are intentional (e.g. ArrivalRadius is meaningless for
// SuppressFire) — see PHASE-14.5.md §Заметки на полях. Refactor to per-aspect
// specs only if the field count outgrows ~12.
type OrderKindSpec struct {
	Code OrderKindCode
	// Name — human-readable short label. Read by Inspector queued-order chip,
	// pie menu label, log messages.
	Name string
	// MapIconGlyph — single rune used as a label hint near the map icon.
	// Geometry of the icon itself is still hand-drawn in
	// `ui/map_render.go::drawOrderIcon` because raylib's text path differs
	// from primitive draws — but the glyph is canonical here.
	MapIconGlyph rune
	// InPieMenu — true ⇒ pie menu offers this kind. False = scaffold-only
	// (AttackTarget is RMB-on-enemy, no pie segment).
	InPieMenu bool
	// PieSegmentOrder — render order in the pie menu (only consulted when
	// InPieMenu == true). 0 = top of the wheel; increases clockwise. Gaps
	// between numbers are fine — table builds the segments[] in sorted order.
	PieSegmentOrder uint8
	// NeedsEntity / NeedsTerrain — hit-test compatibility. Used by
	// `resolveTargetIntoOrder` when a pie commit overrides the auto-detected
	// kind. If `NeedsEntity` is true and the hit kind is incompatible, the
	// kind override falls back to MoveTo (clicking "Garrison" on empty
	// terrain is a misclick — Phase 11 behaviour preserved).
	NeedsEntity  bool
	NeedsTerrain bool
	// OverridesHoldFire — true ⇒ when the squad's EngagementRules.Mode is
	// HoldFire, this active order still allows firing. Closes ISSUES #9
	// (AttackTarget did not override HoldFire). Set on AttackTarget and
	// SuppressFire only.
	OverridesHoldFire bool
	// DrivesMacroPath — true ⇒ SquadMacroPathSystem builds a path to the
	// target. False = squad holds in place (AttackTarget / SuppressFire let
	// WeaponSystem do the work).
	DrivesMacroPath bool
	// Completion — which checkCompletion arm runs.
	Completion CompletionRuleKind
	// ArrivalRadius — only meaningful for Completion == CompletionArrivalRadius
	// or as the fallback radius for Garrison/OccupyTrench when the
	// entity-resolution arm misses. Metres.
	ArrivalRadius float32
	// DurationSeconds — only meaningful for Completion == CompletionTimer
	// (SuppressFire default = 30 s).
	DurationSeconds float32
	// MaxOutOfRangeSeconds — Issue #10 fix. Applies to CompletionTargetDeath:
	// if the squad has been out of effective weapon range of `target.Entity`
	// for this many seconds without the target dying, the order transitions
	// to Failed. 0 = disabled (legacy behaviour). Default 8 s for
	// AttackTarget.
	MaxOutOfRangeSeconds float32
}

// OrderKindCount is the count of enum values. Update if a new OrderKindCode
// is appended. The compile-time assert below catches missed updates.
const OrderKindCount OrderKindCode = OrderKindSuppressFire + 1

// OrderKindSpecs — canonical metadata table. Index by OrderKindCode.
//
// IMPORTANT: when adding a new OrderKindCode value, also append a row here
// and bump OrderKindCount. The compile-time check `var _ = orderKindSpecCheck`
// fires a build error otherwise.
var OrderKindSpecs = [OrderKindCount]OrderKindSpec{
	OrderKindMoveTo: {
		Code: OrderKindMoveTo, Name: "Move", MapIconGlyph: 'M',
		InPieMenu: true, PieSegmentOrder: 0,
		NeedsTerrain: true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindGarrison: {
		Code: OrderKindGarrison, Name: "Garrison", MapIconGlyph: 'G',
		InPieMenu: true, PieSegmentOrder: 1,
		NeedsEntity:     true,
		DrivesMacroPath: true,
		// Phase 14.6 M14.6.2 wires the dedicated rule: order Completes only
		// when every live roster member is inside the building's footprint
		// AABB AND standing on a Floor entity (any storey). ArrivalRadius
		// remains as a defensive fallback when Building lookup misses.
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
		NeedsTerrain: true,
		DrivesMacroPath: true,
		Completion:      CompletionNever,
	},
	OrderKindPatrol: {
		Code: OrderKindPatrol, Name: "Patrol", MapIconGlyph: 'P',
		InPieMenu: true, PieSegmentOrder: 4,
		NeedsTerrain: true,
		DrivesMacroPath: true,
		Completion:      CompletionArrivalRadius, ArrivalRadius: 2.5,
	},
	OrderKindAttackTarget: {
		Code: OrderKindAttackTarget, Name: "Attack", MapIconGlyph: 'A',
		// No pie segment — RMB-on-enemy resolves this kind directly.
		NeedsEntity:       true,
		OverridesHoldFire: true,
		// Squad does not move toward target — Phase 14 hold-in-place. Phase
		// 15 may enable opportunistic relocation for AttackMove-tagged orders.
		DrivesMacroPath:      false,
		Completion:           CompletionTargetDeath,
		MaxOutOfRangeSeconds: 8.0, // Issue #10 fix.
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
}

// Compile-time guard: if OrderKindCount drifts away from the table length,
// the build breaks here with a clear array-size mismatch.
var _ = [OrderKindCount]OrderKindSpec(OrderKindSpecs)

// SpecForOrderKind returns a pointer into the table. Pointer (not value) so
// callers can compare fields without copying the 60-byte struct on each
// access. Index range is enforced — out-of-range code returns the MoveTo row
// as a defensive default (should never happen at runtime, but cheaper than
// panicking on a corrupt OrderKind component during a crash dump).
func SpecForOrderKind(code OrderKindCode) *OrderKindSpec {
	if int(code) >= len(OrderKindSpecs) {
		return &OrderKindSpecs[OrderKindMoveTo]
	}
	return &OrderKindSpecs[code]
}
