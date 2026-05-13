package components

import "github.com/mlange-42/ark/ecs"

// Phase 11 P1: Order is a first-class ECS entity. Each issued order is its own
// entity composed of several small components — same DOD style as Unit /
// Weapon. Per-kind optional params are separate components added only when
// needed (OrderParamFacing for DefendPosition, OrderParamPatrol for Patrol).
//
// Why ECS-entity instead of "field on Squad"? Three reasons (GAMEDESIGN §3):
//
//  1. Lifecycle is observable — `OrderState.Code` + filters let any system
//     query "every InProgress order" without walking squads.
//  2. Queueing is trivial — `OrderChain.Next` is a pointer, append/cancel is
//     pointer surgery, no slice-shuffling on the hot squad row.
//  3. Phase 19 (multi-squad coordination) needs `OrderGroup{GroupID}` to
//     correlate joint orders — clean to bolt on as another component on the
//     order entity later.

// Order is the marker. Filter target only.
type Order struct{}

// OrderKindCode is the type of order. MVP set (Phase 11):
type OrderKindCode uint8

const (
	OrderKindMoveTo OrderKindCode = iota
	OrderKindGarrison
	OrderKindOccupyTrench
	OrderKindDefendPosition
	OrderKindPatrol
)

// OrderKind on the order entity. Wraps the code so the filter-target is one
// concrete struct — Ark stores by component type, not by raw enum.
type OrderKind struct {
	Code OrderKindCode
}

// OrderStateCode tracks lifecycle. Transitions are owned by
// OrderResolverSystem (P3 in PHASE-11.md).
type OrderStateCode uint8

const (
	// OrderStateIssued — freshly created, awaiting first pass. Signals
	// SquadMacroPathSystem to immediately replan (via ReplanAt = 0).
	OrderStateIssued OrderStateCode = iota
	// OrderStateInProgress — being executed; SquadMacroPathSystem keeps the
	// MacroPath fresh, FormationSystem drives the roster.
	OrderStateInProgress
	// OrderStateBlocked — execution stalled (no path / target dead). Resolver
	// retries or escalates to Failed.
	OrderStateBlocked
	// OrderStateCompleted — squad reached the target. OrderResolverSystem
	// advances the chain head and removes this entity within a tick.
	OrderStateCompleted
	// OrderStateCancelled — player aborted (H key / replaced by new RMB
	// without Shift). Same cleanup path as Completed.
	OrderStateCancelled
	// OrderStateFailed — gave up after Blocked retries. Cleanup same as
	// Cancelled; Inspector / Phase 13 UI may surface this differently.
	OrderStateFailed
)

// OrderState on the order entity.
type OrderState struct {
	Code OrderStateCode
}

// OrderOwner is the back-reference to the Squad entity executing this order.
// One-to-one — every order belongs to exactly one squad. (Joint orders in
// Phase 19 add a separate OrderGroup component, not a list of owners.)
type OrderOwner struct {
	Squad ecs.Entity
}

// OrderTarget bundles the spatial and entity targets. Only one of (Pos,
// Entity) is meaningful per kind:
//   - MoveTo / DefendPosition / Patrol: Pos, Entity = zero
//   - Garrison: Entity = Building root, Pos = footprint center (resolved at
//     issuance and refreshed during execution)
//   - OccupyTrench: Entity = TrenchRoot, Pos = nearest polyline point
type OrderTarget struct {
	Pos    WorldPos
	Entity ecs.Entity
}

// OrderIssuedAt records the session-time when the order was created. Used
// for timeouts (Blocked → Failed after N seconds — Phase 13) and for UI
// "issued 5s ago" display.
//
// Name avoids clash with OrderStateIssued (the lifecycle value).
type OrderIssuedAt struct {
	Time float32
}

// OrderProgress is 0..1 progress toward completion. Currently a coarse signal
// — MoveTo writes 1 - (distToGoal / initialDist). Phase 18 (Engineering)
// will reuse this for Build progress.
type OrderProgress struct {
	Value float32
}

// OrderParamFacing is optional. Present only on DefendPosition orders that
// have an explicit sector. Phase 13 (RoE / sectors) will read this.
type OrderParamFacing struct {
	YawRad float32
}

// OrderParamPatrol is optional. Present only on Patrol orders. Loop = true
// re-issues the chain when the last waypoint is reached (P-note in
// PHASE-11.md: simpler than circular Chain.Next pointer).
type OrderParamPatrol struct {
	Loop bool
}

// OrderChain links an order to the next queued order belonging to the same
// squad. Zero Entity = chain end. Shift+RMB appends to the tail; H clears the
// whole chain via OrderResolverSystem / CancelAllOrders.
type OrderChain struct {
	Next ecs.Entity
}

// OrderQueueHead lives on the Squad entity (not the order entity). First =
// zero ⇒ squad is idle. The chain extends via OrderChain.Next on the head
// order. Phase 11 P2: this is the *primary* order state on the squad;
// MacroPath becomes derived/cached.
type OrderQueueHead struct {
	First ecs.Entity
}

// TrenchRoot is the small marker spawned at startup, one per Trench polyline
// in TrenchNetwork.Lines. Index reverse-references the polyline so order
// resolvers / hit-tests can map an OrderTarget.Entity back to its line data
// without copying the polyline points onto the entity. Mirrors how Building
// roots reference BuildingPlanList.
type TrenchRoot struct {
	Index int
}
