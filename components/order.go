package components

import "github.com/mlange-42/ark/ecs"

// Order is a first-class ECS entity. Per-kind optional params live on
// sibling components added only when needed (OrderParamFacing, OrderParamPatrol).

// Order is the marker. Filter target only.
type Order struct{}

// OrderKindCode is the type of order.
type OrderKindCode uint8

const (
	OrderKindMoveTo OrderKindCode = iota
	OrderKindGarrison
	OrderKindOccupyTrench
	OrderKindDefendPosition
	OrderKindPatrol
	// OrderKindAttackTarget: focus fire on a specific entity (Faction != own).
	// Resolved by RMB on an enemy unit. Completes when target dies.
	OrderKindAttackTarget
	// OrderKindSuppressFire: drench a terrain sector with fire on a timer.
	// Resolved via pie-menu on terrain.
	OrderKindSuppressFire
)

// OrderKind on the order entity. Wraps the code so the filter-target is one
// concrete struct - Ark stores by component type, not by raw enum.
type OrderKind struct {
	Code OrderKindCode
}

// OrderStateCode tracks lifecycle. Transitions are owned by OrderResolverSystem.
type OrderStateCode uint8

const (
	OrderStateIssued OrderStateCode = iota
	OrderStateInProgress
	OrderStateBlocked
	OrderStateCompleted
	OrderStateCancelled
	OrderStateFailed
)

// OrderState on the order entity.
type OrderState struct {
	Code OrderStateCode
}

// OrderOwner is the back-reference to the Squad entity executing this order.
// One-to-one. Joint orders (Phase 20+) add a sibling OrderGroup, not a list.
type OrderOwner struct {
	Squad ecs.Entity
}

// OrderTarget bundles spatial and entity targets. Only one of (Pos, Entity)
// is meaningful per kind:
//   - MoveTo / DefendPosition / Patrol: Pos, Entity = zero
//   - Garrison: Entity = Building root, Pos = floor cell (resolved at runtime)
//   - OccupyTrench: Entity = TrenchRoot, Pos = nearest polyline point
type OrderTarget struct {
	Pos    WorldPos
	Entity ecs.Entity
}

// OrderIssuedAt records the session-time when the order was created. Used for
// timeouts and Inspector "issued 5s ago" display. Name avoids clash with
// OrderStateIssued.
type OrderIssuedAt struct {
	Time float32
}

// OrderProgress is 0..1 progress toward completion. Per-kind semantics:
// MoveTo writes 1 - distToGoal/initialDist; Garrison writes inside/alive.
type OrderProgress struct {
	Value float32
}

// OrderParamFacing is optional. Present only on orders that have an explicit
// arrival yaw (DefendPosition sector, facing-drag MoveTo).
type OrderParamFacing struct {
	YawRad float32
}

// OrderParamPatrol is optional. Loop=true re-issues the chain when the last
// waypoint is reached.
type OrderParamPatrol struct {
	Loop bool
}

// OrderChain links an order to the next queued order belonging to the same
// squad. Zero Entity = chain end. Shift+RMB appends to the tail.
type OrderChain struct {
	Next ecs.Entity
}

// OrderOutOfRangeTracker is added only on orders whose Spec.MaxOutOfRangeSeconds
// is > 0 (AttackTarget). Resolver accumulates `Elapsed` when no roster member
// can reach the target; once it crosses the spec cap the order becomes Failed.
// Resets to zero on any tick where at least one member is in range.
//
// Separate component (vs field on OrderIssuedAt) so the resolver's in-range
// hot path skips the cost when the order doesn't need tracking.
type OrderOutOfRangeTracker struct {
	Elapsed float32
}

// OrderQueueHead lives on the Squad entity. First = zero => squad is idle.
// The chain extends via OrderChain.Next on the head order. This is the primary
// order state on the squad; MacroPath becomes derived/cached.
type OrderQueueHead struct {
	First ecs.Entity
}

// TrenchRoot is the marker spawned at startup, one per Trench polyline in
// TrenchNetwork.Lines. Index reverse-references the polyline so hit-tests can
// map OrderTarget.Entity back to line data without copying points on-entity.
type TrenchRoot struct {
	Index int
}
