package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SquadService is the only authorised mutator of Squad / CommandRoster /
// SquadMember. Keeps the bidirectional invariant from PHASE-9.md P2: for
// every SquadMember{s,i} on a unit, CommandRoster(s).Members[i] == that unit.
//
// Pattern mirrors Stamper / NavService - pre-built handles in
// NewSquadService, methods called from outside ECS queries (main.go input
// handlers; systems after closing their filter loops). Archetype mutations
// (Add/Remove of SquadMember) on member units may happen at any time, so
// callers must finish iterating before invoking Leave / CreateFromUnits.
//
// The implementation is split across sibling files in this package:
//
//	squad_service_membership.go - CreateFromUnits / Join / Leave / Despawn
//	squad_service_template.go   - CreateFromTemplate + applyTemplateStandingRules
//	squad_service_orders.go     - OrderParams, IssueOrder, CancelAllOrders,
//	                              clearTacticalOverrides / clearIndividualPositions,
//	                              cancelChain, OrderMoveTo / Stop
type SquadService struct {
	world           *ecs.World
	squadMap        *ecs.Map[components.Squad]
	rosterMap       *ecs.Map[components.CommandRoster]
	formationMap    *ecs.Map[components.FormationData]
	macroPathMap    *ecs.Map[components.MacroPath]
	radioMap        *ecs.Map[components.RadioNetwork]
	memberMap       *ecs.Map[components.SquadMember]
	alwaysActiveMap *ecs.Map[components.AlwaysActive]
	// Phase 11 order maps. SquadService is the only path that issues / cancels
	// orders, mirroring the "service owns its archetype" pattern from Phase 9.
	orderQueueMap    *ecs.Map[components.OrderQueueHead]
	orderMap         *ecs.Map[components.Order]
	orderKindMap     *ecs.Map[components.OrderKind]
	orderStateMap    *ecs.Map[components.OrderState]
	orderOwnerMap    *ecs.Map[components.OrderOwner]
	orderTargetMap   *ecs.Map[components.OrderTarget]
	orderIssuedAtMap *ecs.Map[components.OrderIssuedAt]
	orderProgressMap *ecs.Map[components.OrderProgress]
	orderChainMap    *ecs.Map[components.OrderChain]
	orderFacingMap   *ecs.Map[components.OrderParamFacing]
	orderPatrolMap   *ecs.Map[components.OrderParamPatrol]
	// Phase 12 handles. hasRadiomanInRoster walks Equipment.Secondary on every
	// member and checks the Radio marker, so SquadService needs both maps live.
	equipmentMap *ecs.Map[components.Equipment]
	radioGearMap *ecs.Map[components.Radio]
	// Phase 13 standing-rule handles. CreateFromTemplate aggregates per-role
	// defaults from the leader and template-specific overrides into these
	// squad-level components. Idempotent: not overwritten if already set (P8
	// - protects player-edited values across role changes).
	movementProfileMap *ecs.Map[components.MovementProfile]
	engagementRulesMap *ecs.Map[components.EngagementRules]
	behaviorRulesMap   *ecs.Map[components.BehaviorRules]
	// OrderParamMovementProfile handle for issuing orders with movement
	// overrides (M13.5: Ctrl+RMB Stealth, Double-RMB Sprint).
	orderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	// OrderParamAttackMove handle for Alt+RMB AttackMove scaffold (M13.5).
	// Phase 14 WeaponSystem is the canonical reader.
	orderAttackMoveMap *ecs.Map[components.OrderParamAttackMove]
	// Phase 14 M14.4: optional Suppress params on SuppressFire orders.
	orderSuppressMap *ecs.Map[components.OrderParamSuppress]
	// Phase 14.5 M14.5.0: Issue #10 out-of-range tracker for AttackTarget.
	orderOutOfRangeMap *ecs.Map[components.OrderOutOfRangeTracker]
	// Phase 14 M14.1: Faction handle. CreateFromTemplate stamps each spawned
	// unit so WeaponSystem can gate firing on hostility.
	factionMap *ecs.Map[components.Faction]
	// Phase 15 M15.A.0: TacticalOverride handle. IssueOrder / CancelAllOrders
	// clear any AI-driven cover assignment on the squad's members so an
	// explicit player order regains control.
	tacticalOverrideMap *ecs.Map[components.TacticalOverride]
	// Phase 15 M15.A.1: SquadState handle. Player intent also resets a
	// Scrambling squad back to Engaged so the next tick doesn't immediately
	// re-acquire overrides via scramble's zero-threshold path.
	squadStateMap *ecs.Map[components.SquadState]
	// Phase 15 M15.B.2: IndividualPosition handle. Alt+RMB orders carry the
	// AttackMove flag; when the flag is set, IssueOrder also wipes every
	// member's IndividualPosition so the squad falls back into formation
	// before charging.
	individualPosMap *ecs.Map[components.IndividualPosition]
	// Session-time clock for OrderIssuedAt. Advanced by SetClock (called from
	// main.go each frame before input handlers run).
	clock float32
}

func NewSquadService(w *ecs.World) *SquadService {
	return &SquadService{
		world:                    w,
		squadMap:                 ecs.NewMap[components.Squad](w),
		rosterMap:                ecs.NewMap[components.CommandRoster](w),
		formationMap:             ecs.NewMap[components.FormationData](w),
		macroPathMap:             ecs.NewMap[components.MacroPath](w),
		radioMap:                 ecs.NewMap[components.RadioNetwork](w),
		memberMap:                ecs.NewMap[components.SquadMember](w),
		alwaysActiveMap:          ecs.NewMap[components.AlwaysActive](w),
		orderQueueMap:            ecs.NewMap[components.OrderQueueHead](w),
		orderMap:                 ecs.NewMap[components.Order](w),
		orderKindMap:             ecs.NewMap[components.OrderKind](w),
		orderStateMap:            ecs.NewMap[components.OrderState](w),
		orderOwnerMap:            ecs.NewMap[components.OrderOwner](w),
		orderTargetMap:           ecs.NewMap[components.OrderTarget](w),
		orderIssuedAtMap:         ecs.NewMap[components.OrderIssuedAt](w),
		orderProgressMap:         ecs.NewMap[components.OrderProgress](w),
		orderChainMap:            ecs.NewMap[components.OrderChain](w),
		orderFacingMap:           ecs.NewMap[components.OrderParamFacing](w),
		orderPatrolMap:           ecs.NewMap[components.OrderParamPatrol](w),
		equipmentMap:             ecs.NewMap[components.Equipment](w),
		radioGearMap:             ecs.NewMap[components.Radio](w),
		movementProfileMap:       ecs.NewMap[components.MovementProfile](w),
		engagementRulesMap:       ecs.NewMap[components.EngagementRules](w),
		behaviorRulesMap:         ecs.NewMap[components.BehaviorRules](w),
		orderMovementOverrideMap: ecs.NewMap[components.OrderParamMovementProfile](w),
		orderAttackMoveMap:       ecs.NewMap[components.OrderParamAttackMove](w),
		orderSuppressMap:         ecs.NewMap[components.OrderParamSuppress](w),
		orderOutOfRangeMap:       ecs.NewMap[components.OrderOutOfRangeTracker](w),
		factionMap:               ecs.NewMap[components.Faction](w),
		tacticalOverrideMap:      ecs.NewMap[components.TacticalOverride](w),
		squadStateMap:            ecs.NewMap[components.SquadState](w),
		individualPosMap:         ecs.NewMap[components.IndividualPosition](w),
	}
}

// SetClock updates the session-time used for OrderIssuedAt. main.go calls
// this once per frame before issuing any orders. Separate from app.elapsed
// because SquadService doesn't import core.
func (s *SquadService) SetClock(t float32) { s.clock = t }

// Clock returns the current session clock used for order timestamps. Used by
// OrderResolverSystem to compute progress / timeouts without re-importing
// elapsed.
func (s *SquadService) Clock() float32 { return s.clock }

// MovementProfileMap / EngagementRulesMap / BehaviorRulesMap expose the
// Phase 13 standing-rule handles so external readers (UnitMovementSystem,
// SquadMacroPathSystem, Inspector) can fetch components without instantiating
// their own ecs.Map. Returned pointers must not be retained across world
// archetype changes - caller uses them inline.
func (s *SquadService) MovementProfileMap() *ecs.Map[components.MovementProfile] {
	return s.movementProfileMap
}
func (s *SquadService) EngagementRulesMap() *ecs.Map[components.EngagementRules] {
	return s.engagementRulesMap
}
func (s *SquadService) BehaviorRulesMap() *ecs.Map[components.BehaviorRules] {
	return s.behaviorRulesMap
}

// OrderMovementOverrideMap returns the OrderParamMovementProfile handle used
// by IssueOrderWithMovementOverride (M13.5).
func (s *SquadService) OrderMovementOverrideMap() *ecs.Map[components.OrderParamMovementProfile] {
	return s.orderMovementOverrideMap
}

// FormationSpacing returns the default spacing (metres) for each formation
// kind. F1-F4 hotkeys use this table; Phase 14 may swap it for a slider.
func FormationSpacing(k components.FormationKind) float32 {
	switch k {
	case components.FormationLine:
		return 2.0
	case components.FormationColumn:
		return 2.0
	case components.FormationWedge:
		return 3.0
	case components.FormationLoose:
		return 4.0
	}
	return 2.0
}
