package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SquadService is the only authorised mutator of Squad / CommandRoster /
// SquadMember. Invariant: for every SquadMember{s,i} on a unit,
// CommandRoster(s).Members[i] == that unit.
//
// Methods are called from outside ECS queries. SquadMember Add/Remove on
// member units may happen at any time, so callers must finish iterating
// before invoking Leave / CreateFromUnits.
//
// Implementation split across sibling files:
//
//	squad_service_membership.go - CreateFromUnits / Join / Leave / Despawn
//	squad_service_template.go   - CreateFromTemplate
//	squad_service_orders.go     - IssueOrder / CancelAllOrders / etc.
type SquadService struct {
	world           *ecs.World
	squadMap        *ecs.Map[components.Squad]
	rosterMap       *ecs.Map[components.CommandRoster]
	formationMap    *ecs.Map[components.FormationData]
	macroPathMap    *ecs.Map[components.MacroPath]
	radioMap        *ecs.Map[components.RadioNetwork]
	memberMap       *ecs.Map[components.SquadMember]
	alwaysActiveMap *ecs.Map[components.AlwaysActive]
	// SquadService is the only path that issues / cancels orders.
	orderQueueMap              *ecs.Map[components.OrderQueueHead]
	orderMap                   *ecs.Map[components.Order]
	orderKindMap               *ecs.Map[components.OrderKind]
	orderStateMap              *ecs.Map[components.OrderState]
	orderOwnerMap              *ecs.Map[components.OrderOwner]
	orderTargetMap             *ecs.Map[components.OrderTarget]
	orderIssuedAtMap           *ecs.Map[components.OrderIssuedAt]
	orderProgressMap           *ecs.Map[components.OrderProgress]
	orderChainMap              *ecs.Map[components.OrderChain]
	orderFacingMap             *ecs.Map[components.OrderParamFacing]
	orderPatrolMap             *ecs.Map[components.OrderParamPatrol]
	equipmentMap               *ecs.Map[components.Equipment]
	radioGearMap               *ecs.Map[components.Radio]
	movementProfileMap         *ecs.Map[components.MovementProfile]
	engagementRulesMap         *ecs.Map[components.EngagementRules]
	behaviorRulesMap           *ecs.Map[components.BehaviorRules]
	orderMovementOverrideMap   *ecs.Map[components.OrderParamMovementProfile]
	orderAttackMoveMap         *ecs.Map[components.OrderParamAttackMove]
	orderSuppressMap           *ecs.Map[components.OrderParamSuppress]
	orderEngagementOverrideMap *ecs.Map[components.OrderParamEngagementOverride]
	orderOutOfRangeMap         *ecs.Map[components.OrderOutOfRangeTracker]
	factionMap                 *ecs.Map[components.Faction]
	// IssueOrder / CancelAllOrders clear AI-driven cover assignment so an
	// explicit player order regains control.
	tacticalOverrideMap *ecs.Map[components.TacticalOverride]
	// Player intent also resets a Scrambling squad back to Engaged.
	squadStateMap *ecs.Map[components.SquadState]
	// AttackMove orders wipe every member's IndividualPosition so the squad
	// falls back into formation before charging.
	individualPosMap *ecs.Map[components.IndividualPosition]
	clock            float32
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
		orderSuppressMap:           ecs.NewMap[components.OrderParamSuppress](w),
		orderEngagementOverrideMap: ecs.NewMap[components.OrderParamEngagementOverride](w),
		orderOutOfRangeMap:       ecs.NewMap[components.OrderOutOfRangeTracker](w),
		factionMap:               ecs.NewMap[components.Faction](w),
		tacticalOverrideMap:      ecs.NewMap[components.TacticalOverride](w),
		squadStateMap:            ecs.NewMap[components.SquadState](w),
		individualPosMap:         ecs.NewMap[components.IndividualPosition](w),
	}
}

// SetClock updates the session-time used for OrderIssuedAt. Separate from
// app.elapsed because SquadService doesn't import core.
func (s *SquadService) SetClock(t float32) { s.clock = t }

func (s *SquadService) Clock() float32 { return s.clock }

// MovementProfileMap / EngagementRulesMap / BehaviorRulesMap expose the
// standing-rule handles to read-only callers. Returned pointers must not be
// retained across world archetype changes.
func (s *SquadService) MovementProfileMap() *ecs.Map[components.MovementProfile] {
	return s.movementProfileMap
}
func (s *SquadService) EngagementRulesMap() *ecs.Map[components.EngagementRules] {
	return s.engagementRulesMap
}
func (s *SquadService) BehaviorRulesMap() *ecs.Map[components.BehaviorRules] {
	return s.behaviorRulesMap
}

func (s *SquadService) OrderMovementOverrideMap() *ecs.Map[components.OrderParamMovementProfile] {
	return s.orderMovementOverrideMap
}

// FormationSpacing returns the default spacing (metres) per formation kind.
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
