package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// suppressDefaultRadius - default OrderParamSuppress.Radius (metres) when the
// caller doesn't override. PHASE-14.md M14.4 picks 8 m to roughly cover one
// cover-cluster (sandbag stack or 2-3 trench segments).
const suppressDefaultRadius float32 = 8.0

// suppressDuration - Phase 14 simple completion timer (seconds) for
// OrderKindSuppressFire when no AmmoCap reader exists. PHASE-14.md M14.4:
// 30 s engagement window.
const suppressDuration float32 = 30.0

// OrderParams bundles the optional per-kind fields IssueOrder accepts. Zero
// values are fine for kinds that don't read them.
//
// Phase 13 M13.5 additions:
//   - MovementOverride: when non-nil, attaches an OrderParamMovementProfile
//     to the spawned order. The unit movement / macro path systems use this
//     in place of the squad's standing MovementProfile until the order leaves
//     InProgress.
//   - AttackMove: when true, attaches an OrderParamAttackMove marker. Phase
//     14 WeaponSystem reads it to permit fire-without-cancel-of-move.
type OrderParams struct {
	FacingYawRad     float32
	HasFacing        bool
	PatrolLoop       bool
	MovementOverride *components.MovementProfile
	AttackMove       bool
	// Phase 14 M14.4: when non-nil, attach an OrderParamSuppress to the
	// spawned SuppressFire order. Other kinds ignore the field.
	Suppress *components.OrderParamSuppress
}

// IssueOrder spawns a new Order entity owned by `squad`. If append=false the
// existing chain (head + tails) is cancelled first; if append=true the new
// order is appended at the tail (Shift+RMB semantics, PHASE-11.md P9).
//
// kind / target / entityTarget describe what the order *means*; per-kind
// completion lives in OrderResolverSystem. Returns the new Order entity, or
// zero on a no-op (dead squad / missing OrderQueueHead).
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

	// Cancel mode - wipe the current chain so the new order is the sole head.
	// The wipe deletes order entities and walks the chain via OrderChain.Next.
	if !appendToQueue {
		s.cancelChain(head.First)
		head.First = ecs.Entity{}
	}

	// Phase 15 M15.A.0 - any explicit player order is "I have control again",
	// so AI-driven cover assignments on squad members are cleared. Members
	// fall back to formation slot on the next FormationSystem tick.
	s.clearTacticalOverrides(squad)

	// Spawn the new order entity.
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
	// Phase 13 M13.5: optional movement / attack-move overrides.
	if params.MovementOverride != nil {
		s.orderMovementOverrideMap.Add(ord, &components.OrderParamMovementProfile{
			Profile: *params.MovementOverride,
		})
	}
	if params.AttackMove {
		s.orderAttackMoveMap.Add(ord, &components.OrderParamAttackMove{})
		// Phase 15 M15.B.2 - AttackMove is "go aggressive, fall back into
		// formation". Wipe any hand-placed positions so the squad's macro path
		// doesn't drag stragglers parked behind cover.
		s.clearIndividualPositions(squad)
	}
	// Phase 14.5 M14.5.0 (Issue #10): orders whose spec carries a max
	// out-of-range window get the tracker installed up-front. Resolver reads
	// the spec and decides per-tick if the squad is in range.
	if spec := components.SpecForOrderKind(kind); spec.MaxOutOfRangeSeconds > 0 {
		s.orderOutOfRangeMap.Add(ord, &components.OrderOutOfRangeTracker{})
	}
	// Phase 14 M14.4: only SuppressFire reads OrderParamSuppress. Default the
	// StartTime to the current clock if the caller didn't set it.
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

	// Wire into the chain. The squad's MacroPath also gets ReplanAt=0 so
	// SquadMacroPathSystem replans immediately rather than waiting for the 1 s
	// throttle - this is the "fresh order -> don't dawdle" guarantee from
	// PHASE-11.md notes.
	if head.First == (ecs.Entity{}) {
		head.First = ord
		if mp := s.macroPathMap.Get(squad); mp != nil {
			mp.ReplanAt = 0
		}
	} else {
		// Append at tail.
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

// CancelAllOrders aborts the squad's full chain (head + every Next). Used by
// H (Stop) hotkey and by the Phase 11 non-append RMB path.
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

// clearTacticalOverrides removes the TacticalOverride marker (if any) from
// every live member of `squad` and drops a Scrambling squad back to Engaged.
// Called by IssueOrder / CancelAllOrders so any explicit player intent regains
// control from SurvivalInstinct's AI-driven cover assignment. Without the
// squad-state reset a player order during scramble would clear overrides only
// to have them re-acquired on the next ScatterProtocol tick (effective
// threshold is 0 while scrambling).
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

// clearIndividualPositions removes the IndividualPosition marker (if any)
// from every live member of `squad`. Called by IssueOrder when the player
// taps Alt+RMB (AttackMove flag) - aggressive intent resets hand-placed
// positions so the squad re-forms behind the commander.
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

// cancelChain walks Next pointers starting at `start`, marks each order
// Cancelled, and removes the entity. OrderResolverSystem would catch
// Cancelled and remove anyway, but doing it here keeps a clear "no dangling
// orders after CancelAllOrders" invariant.
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

// OrderMoveTo is the Phase 9-compat wrapper. New callers should use
// IssueOrder; this stays so existing test code / hotkeys keep working.
func (s *SquadService) OrderMoveTo(squad ecs.Entity, goal components.WorldPos) {
	s.IssueOrder(squad, components.OrderKindMoveTo, goal, ecs.Entity{}, false, OrderParams{})
}

// Stop is the Phase 9-compat wrapper. Cancels the full chain.
func (s *SquadService) Stop(squad ecs.Entity) {
	s.CancelAllOrders(squad)
}
