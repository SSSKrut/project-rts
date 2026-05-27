package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// rlVec3XZ packs an XZ pair into a Vector3 with Y = 0.
func rlVec3XZ(x, z float32) rl.Vector3 { return rl.Vector3{X: x, Y: 0, Z: z} }

// OrderResolverSystem owns the Order lifecycle:
//
//   - Issued → InProgress (first pass; tells SquadMacroPathSystem to replan
//     via MacroPath.ReplanAt = 0)
//   - InProgress → Completed when per-kind completion fires
//   - Cleanup of finished orders — entity removed, OrderQueueHead.First
//     advances to OrderChain.Next
//
// Per-kind completion dispatches through components.OrderKindSpec.Completion
// (see order_resolver_completion.go). Target resolution (Garrison footprint
// center / Floor anchor, OccupyTrench nearest polyline point) lives in
// order_resolver_target.go.
type OrderResolverSystem struct {
	squadFilter      *ecs.Filter2[components.Squad, components.OrderQueueHead]
	rosterMap        *ecs.Map[components.CommandRoster]
	posMap           *ecs.Map[components.WorldPos]
	formationDataMap *ecs.Map[components.FormationData]
	macroPathMap     *ecs.Map[components.MacroPath]

	orderKindMap       *ecs.Map[components.OrderKind]
	orderStateMap      *ecs.Map[components.OrderState]
	orderTargetMap     *ecs.Map[components.OrderTarget]
	orderProgressMap   *ecs.Map[components.OrderProgress]
	orderChainMap      *ecs.Map[components.OrderChain]
	orderPatrolMap     *ecs.Map[components.OrderParamPatrol]
	orderIssuedAtMap   *ecs.Map[components.OrderIssuedAt]
	orderOwnerMap      *ecs.Map[components.OrderOwner]
	orderFacingMap     *ecs.Map[components.OrderParamFacing]
	orderSuppressMap   *ecs.Map[components.OrderParamSuppress]
	orderOutOfRangeMap *ecs.Map[components.OrderOutOfRangeTracker]
	// Used to compute the squad's effective weapon range (max across roster).
	equipmentMap *ecs.Map[components.Equipment]
	weaponMap    *ecs.Map[components.Weapon]

	// Read on MoveTo / DefendPosition completion to apply arrived-facing
	// (instant snap) to every roster member's Motion.Yaw.
	motionMap *ecs.Map[components.Motion]

	buildingMap    *ecs.Map[components.Building]
	trenchRootMap  *ecs.Map[components.TrenchRoot]
	trenchResource ecs.Resource[components.TrenchNetwork]

	// Garrison CompletionEveryMemberOnFloor reads Floor plates to confirm
	// each roster member sits on a floor cell (not just inside the AABB).
	floorFilter *ecs.Filter2[components.WorldPos, components.Floor]

	// ClearBuilding counts hostile Units inside the footprint AABB.
	unitFilter *ecs.Filter2[components.Unit, components.WorldPos]
	factionMap *ecs.Map[components.Faction]
	// Interior-intent orders mark the target Building as LODAnchor so its
	// host chunks stay loaded — otherwise doors outside the player's
	// streaming radius don't exist as entities, NavService has no
	// TransitionEdge for them, and pathfinding into the building fails
	// until the squad walks close enough to activate the chunks.
	lodAnchorMap *ecs.Map[components.LODAnchor]

	// Garrison target.Pos points at a Floor entity's WorldPos (NodeLevel)
	// so NavService.FindPath can route through a Door TransitionEdge —
	// surface-inside-footprint cells are NavInBuilding and refuse expansion.
	buildingChildIndex ecs.Resource[BuildingChildIndex]
	floorComponentMap  *ecs.Map[components.Floor]

	eventLogRes ecs.Resource[components.EventLog]

	squadService *SquadService
}

// NewOrderResolverSystem wires the system. SquadService owns the mutation
// path for re-issuing orders (Patrol Loop, ClearBuilding → OccupyBuilding).
func NewOrderResolverSystem(svc *SquadService) *OrderResolverSystem {
	return &OrderResolverSystem{squadService: svc}
}

func (sys *OrderResolverSystem) InitUI(w *ecs.World) {
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.OrderQueueHead](w)
	sys.rosterMap = ecs.NewMap[components.CommandRoster](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.formationDataMap = ecs.NewMap[components.FormationData](w)
	sys.macroPathMap = ecs.NewMap[components.MacroPath](w)

	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderStateMap = ecs.NewMap[components.OrderState](w)
	sys.orderTargetMap = ecs.NewMap[components.OrderTarget](w)
	sys.orderProgressMap = ecs.NewMap[components.OrderProgress](w)
	sys.orderChainMap = ecs.NewMap[components.OrderChain](w)
	sys.orderPatrolMap = ecs.NewMap[components.OrderParamPatrol](w)
	sys.orderIssuedAtMap = ecs.NewMap[components.OrderIssuedAt](w)
	sys.orderOwnerMap = ecs.NewMap[components.OrderOwner](w)
	sys.orderFacingMap = ecs.NewMap[components.OrderParamFacing](w)
	sys.orderSuppressMap = ecs.NewMap[components.OrderParamSuppress](w)
	sys.orderOutOfRangeMap = ecs.NewMap[components.OrderOutOfRangeTracker](w)
	sys.equipmentMap = ecs.NewMap[components.Equipment](w)
	sys.weaponMap = ecs.NewMap[components.Weapon](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)

	sys.buildingMap = ecs.NewMap[components.Building](w)
	sys.trenchRootMap = ecs.NewMap[components.TrenchRoot](w)
	sys.trenchResource = ecs.NewResource[components.TrenchNetwork](w)
	sys.floorFilter = ecs.NewFilter2[components.WorldPos, components.Floor](w)
	sys.buildingChildIndex = ecs.NewResource[BuildingChildIndex](w)
	sys.floorComponentMap = ecs.NewMap[components.Floor](w)
	sys.eventLogRes = ecs.NewResource[components.EventLog](w)
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.lodAnchorMap = ecs.NewMap[components.LODAnchor](w)
}

// pushOrderEvent records an order-lifecycle event into the global log.
func (sys *OrderResolverSystem) pushOrderEvent(kind components.EventKind, squad ecs.Entity, target components.WorldPos) {
	log := sys.eventLogRes.Get()
	if log == nil {
		return
	}
	text := "Order completed"
	if kind == components.EventOrderFailed {
		text = "Order failed"
	}
	log.Push(components.EventEntry{
		Kind:  kind,
		At:    sys.squadService.Clock(),
		Pos:   target,
		Squad: squad,
		Text:  text,
	})
}

func (OrderResolverSystem) Name() string { return "order_resolver" }

// Runs every tick; serial (archetype mutations on head advance / cleanup
// don't parallelise cleanly).
func (OrderResolverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// arrivalRadiusFor reads Spec.ArrivalRadius; falls back to MoveTo default
// for kinds whose spec didn't fill the field.
func arrivalRadiusFor(kind components.OrderKindCode) float32 {
	spec := components.SpecForOrderKind(kind)
	if spec.ArrivalRadius > 0 {
		return spec.ArrivalRadius
	}
	return 2.5
}

func (sys *OrderResolverSystem) Update(ctx core.UpdateContext) {
	// Collect lifecycle mutations during iteration; apply after closing the
	// query (head reassignment / chain re-spawn would mutate archetypes of
	// queued entities and break a live filter).
	type advance struct {
		squad         ecs.Entity
		newHead       ecs.Entity
		oldHead       ecs.Entity
		reissueKind   components.OrderKindCode
		reissueTarget components.WorldPos
		reissueEntity ecs.Entity
		reissue       bool
	}
	var advances []advance
	var pendingLODAnchors []ecs.Entity

	q := sys.squadFilter.Query()
	for q.Next() {
		_, head := q.Get()
		squad := q.Entity()
		if head.First == (ecs.Entity{}) {
			continue
		}
		ord := head.First
		if !ctx.World.Alive(ord) {
			advances = append(advances, advance{squad: squad, oldHead: ord})
			continue
		}
		state := sys.orderStateMap.Get(ord)
		kind := sys.orderKindMap.Get(ord)
		target := sys.orderTargetMap.Get(ord)
		if state == nil || kind == nil || target == nil {
			continue
		}

		// Refresh entity-based target Pos every tick.
		sys.resolveTargetPos(kind.Code, target)

		switch state.Code {
		case components.OrderStateIssued:
			state.Code = components.OrderStateInProgress
			if mp := sys.macroPathMap.Get(squad); mp != nil {
				mp.ReplanAt = 0
			}
			// Pin LODAnchor on target Building for interior-intent orders.
			// Mutation deferred — Ark forbids archetype changes inside a
			// live query.
			if target.Entity != (ecs.Entity{}) && ctx.World.Alive(target.Entity) {
				spec := components.SpecForOrderKind(kind.Code)
				if spec.Completion == components.CompletionEveryMemberOnFloor ||
					spec.Completion == components.CompletionClearBuilding {
					pendingLODAnchors = append(pendingLODAnchors, target.Entity)
				}
			}

		case components.OrderStateInProgress:
			spec := components.SpecForOrderKind(kind.Code)
			outcome := sys.evaluateCompletion(squad, ord, spec, target, float32(ctx.Delta.Seconds()))
			switch outcome {
			case completionDone:
				state.Code = components.OrderStateCompleted
				if pr := sys.orderProgressMap.Get(ord); pr != nil {
					pr.Value = 1
				}
				sys.applyArrivedFacing(squad, ord)
				sys.pushOrderEvent(components.EventOrderCompleted, squad, target.Pos)
			case completionFailed:
				state.Code = components.OrderStateFailed
				sys.pushOrderEvent(components.EventOrderFailed, squad, target.Pos)
			case completionPending:
				sys.updateProgress(squad, ord, target)
			}

		case components.OrderStateCompleted,
			components.OrderStateCancelled,
			components.OrderStateFailed:
			adv := advance{squad: squad, oldHead: ord}
			if ch := sys.orderChainMap.Get(ord); ch != nil {
				adv.newHead = ch.Next
			}
			// Patrol Loop re-issue: completed Patrol with Loop=true and
			// no queued Next → spawn a fresh Patrol at the same target.
			if state.Code == components.OrderStateCompleted &&
				kind.Code == components.OrderKindPatrol &&
				adv.newHead == (ecs.Entity{}) {
				if patrol := sys.orderPatrolMap.Get(ord); patrol != nil && patrol.Loop {
					adv.reissue = true
					adv.reissueKind = components.OrderKindPatrol
					adv.reissueTarget = target.Pos
					adv.reissueEntity = target.Entity
				}
			}
			// ClearBuilding auto-chains OccupyBuilding on Done so the
			// squad rests inside. Player's explicit chain wins.
			if state.Code == components.OrderStateCompleted &&
				kind.Code == components.OrderKindClearBuilding &&
				adv.newHead == (ecs.Entity{}) {
				adv.reissue = true
				adv.reissueKind = components.OrderKindOccupyBuilding
				adv.reissueTarget = target.Pos
				adv.reissueEntity = target.Entity
			}
			advances = append(advances, adv)
		}
	}

	// Apply LODAnchor adds after the query closes.
	if sys.lodAnchorMap != nil {
		for _, ent := range pendingLODAnchors {
			if ent == (ecs.Entity{}) || !ctx.World.Alive(ent) {
				continue
			}
			if !sys.lodAnchorMap.Has(ent) {
				sys.lodAnchorMap.Add(ent, &components.LODAnchor{})
			}
		}
	}

	for _, a := range advances {
		if a.oldHead != (ecs.Entity{}) && ctx.World.Alive(a.oldHead) {
			ctx.World.RemoveEntity(a.oldHead)
		}
		head := getOrderHead(sys.squadService, a.squad)
		if head == nil {
			continue
		}
		if a.reissue {
			head.First = ecs.Entity{}
			params := OrderParams{}
			if a.reissueKind == components.OrderKindPatrol {
				params.PatrolLoop = true
			}
			sys.squadService.IssueOrder(a.squad, a.reissueKind, a.reissueTarget, a.reissueEntity,
				false, params)
			continue
		}
		head.First = a.newHead
		// Replan against new head (or stop if zero).
		if mp := sys.macroPathMap.Get(a.squad); mp != nil {
			if head.First == (ecs.Entity{}) {
				mp.HasGoal = false
				mp.Head = 0
				mp.Count = 0
			} else {
				mp.ReplanAt = 0
			}
		}
	}
}

// getOrderHead exposes the head pointer through SquadService's typed map.
// SquadService is the canonical mutator of the queue head.
func getOrderHead(svc *SquadService, squad ecs.Entity) *components.OrderQueueHead {
	if svc == nil || squad == (ecs.Entity{}) || !svc.world.Alive(squad) {
		return nil
	}
	return svc.orderQueueMap.Get(squad)
}
