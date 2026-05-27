package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Default OrderParamSuppress.Radius (metres) — roughly one cover-cluster.
const suppressDefaultRadius float32 = 8.0

// SuppressFire completion timer.
const suppressDuration float32 = 30.0

// OrderParams bundles the optional per-kind fields IssueOrder accepts.
// Zero values are fine for kinds that don't read them.
type OrderParams struct {
	FacingYawRad     float32
	HasFacing        bool
	PatrolLoop       bool
	MovementOverride *components.MovementProfile
	AttackMove       bool
	// Only SuppressFire reads this.
	Suppress *components.OrderParamSuppress
	// WeaponSystem.shouldFire overrides standing EngagementRules.Mode for
	// this order's duration. Used by "Hidden position" (Mode=HoldFire).
	EngagementOverride *components.EngagementMode
}

// IssueOrder spawns a new Order entity owned by `squad`. append=false
// cancels the existing chain; append=true appends at the tail. Per-kind
// completion lives in OrderResolverSystem.
func (s *SquadService) IssueOrder(
	squad ecs.Entity,
	kind components.OrderKindCode,
	target components.WorldPos,
	entityTarget ecs.Entity,
	appendToQueue bool,
	params OrderParams,
) ecs.Entity {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return ecs.Entity{}
	}
	head := s.orderQueueMap.Get(squad)
	if head == nil {
		return ecs.Entity{}
	}

	if !appendToQueue {
		s.cancelChain(head.First)
		head.First = ecs.Entity{}
	}

	// Explicit player order = "I have control again"; clear AI overrides
	// so members fall back to formation slot on the next tick.
	s.clearTacticalOverrides(squad)

	ord := s.world.NewEntity()
	s.orderMap.Add(ord, &components.Order{})
	s.orderKindMap.Add(ord, &components.OrderKind{Code: kind})
	s.orderStateMap.Add(ord, &components.OrderState{Code: components.OrderStateIssued})
	s.orderOwnerMap.Add(ord, &components.OrderOwner{Squad: squad})
	s.orderTargetMap.Add(ord, &components.OrderTarget{Pos: target, Entity: entityTarget})
	s.orderIssuedAtMap.Add(ord, &components.OrderIssuedAt{Time: s.clock})
	s.orderProgressMap.Add(ord, &components.OrderProgress{})
	s.orderChainMap.Add(ord, &components.OrderChain{})
	if params.HasFacing {
		s.orderFacingMap.Add(ord, &components.OrderParamFacing{YawRad: params.FacingYawRad})
	}
	if kind == components.OrderKindPatrol {
		s.orderPatrolMap.Add(ord, &components.OrderParamPatrol{Loop: params.PatrolLoop})
	}
	if params.MovementOverride != nil {
		s.orderMovementOverrideMap.Add(ord, &components.OrderParamMovementProfile{
			Profile: *params.MovementOverride,
		})
	}
	if params.AttackMove {
		s.orderAttackMoveMap.Add(ord, &components.OrderParamAttackMove{})
		// AttackMove resets hand-placed positions so the squad's macro path
		// doesn't drag stragglers parked behind cover.
		s.clearIndividualPositions(squad)
	}
	if params.EngagementOverride != nil {
		s.orderEngagementOverrideMap.Add(ord, &components.OrderParamEngagementOverride{
			Mode: *params.EngagementOverride,
		})
	}
	// Install out-of-range tracker for kinds whose spec carries a max window.
	if spec := components.SpecForOrderKind(kind); spec.MaxOutOfRangeSeconds > 0 {
		s.orderOutOfRangeMap.Add(ord, &components.OrderOutOfRangeTracker{})
	}
	// Only SuppressFire reads OrderParamSuppress.
	if kind == components.OrderKindSuppressFire {
		sp := components.OrderParamSuppress{}
		if params.Suppress != nil {
			sp = *params.Suppress
		}
		if sp.StartTime == 0 {
			sp.StartTime = s.clock
		}
		if sp.Radius == 0 {
			sp.Radius = suppressDefaultRadius
		}
		s.orderSuppressMap.Add(ord, &sp)
	}

	// MacroPath.ReplanAt = 0 makes SquadMacroPathSystem replan immediately
	// instead of waiting for the 1 s throttle.
	if head.First == (ecs.Entity{}) {
		head.First = ord
		if mp := s.macroPathMap.Get(squad); mp != nil {
			mp.ReplanAt = 0
		}
	} else {
		tail := head.First
		for {
			ch := s.orderChainMap.Get(tail)
			if ch == nil || ch.Next == (ecs.Entity{}) {
				break
			}
			tail = ch.Next
		}
		if ch := s.orderChainMap.Get(tail); ch != nil {
			ch.Next = ord
		}
	}
	return ord
}

// CancelAllOrders aborts the squad's full chain (head + every Next).
func (s *SquadService) CancelAllOrders(squad ecs.Entity) {
	if squad == (ecs.Entity{}) || !s.world.Alive(squad) {
		return
	}
	head := s.orderQueueMap.Get(squad)
	if head == nil {
		return
	}
	s.cancelChain(head.First)
	head.First = ecs.Entity{}
	if mp := s.macroPathMap.Get(squad); mp != nil {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
	}
	s.clearTacticalOverrides(squad)
}

// clearTacticalOverrides removes TacticalOverride from every live member
// and drops a Scrambling squad back to Engaged. Without the squad-state
// reset, a player order during scramble would clear overrides only to have
// them re-acquired on the next ScatterProtocol tick (effective threshold = 0
// while scrambling).
func (s *SquadService) clearTacticalOverrides(squad ecs.Entity) {
	if s.tacticalOverrideMap == nil {
		return
	}
	if state := s.squadStateMap.Get(squad); state != nil && state.Code == components.SquadStateScrambling {
		state.Code = components.SquadStateEngaged
		state.LowDeltaSince = 0
	}
	roster := s.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.world.Alive(mem) {
			continue
		}
		if s.tacticalOverrideMap.Has(mem) {
			s.tacticalOverrideMap.Remove(mem)
		}
	}
}

// clearIndividualPositions removes IndividualPosition from every live
// member so AttackMove resets hand-placed positions.
func (s *SquadService) clearIndividualPositions(squad ecs.Entity) {
	if s.individualPosMap == nil {
		return
	}
	roster := s.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.world.Alive(mem) {
			continue
		}
		if s.individualPosMap.Has(mem) {
			s.individualPosMap.Remove(mem)
		}
	}
}

// cancelChain walks Next pointers from `start`, removes each order. Keeps
// the "no dangling orders after CancelAllOrders" invariant.
func (s *SquadService) cancelChain(start ecs.Entity) {
	cur := start
	for cur != (ecs.Entity{}) {
		if !s.world.Alive(cur) {
			break
		}
		next := ecs.Entity{}
		if ch := s.orderChainMap.Get(cur); ch != nil {
			next = ch.Next
		}
		s.world.RemoveEntity(cur)
		cur = next
	}
}

// OrderMoveTo is a compat wrapper around IssueOrder.
func (s *SquadService) OrderMoveTo(squad ecs.Entity, goal components.WorldPos) {
	s.IssueOrder(squad, components.OrderKindMoveTo, goal, ecs.Entity{}, false, OrderParams{})
}

// Stop cancels the full chain.
func (s *SquadService) Stop(squad ecs.Entity) {
	s.CancelAllOrders(squad)
}
