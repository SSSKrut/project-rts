package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// rlVec3XZ packs an XZ pair into a Vector3 with Y = 0. Kept private because
// it's only useful where world-coord -> WorldPos requires Vector3 inputs.
func rlVec3XZ(x, z float32) rl.Vector3 { return rl.Vector3{X: x, Y: 0, Z: z} }

// OrderResolverSystem owns the Order lifecycle (PHASE-11.md P3 + P4):
//
//   - Issued -> InProgress (after the first pass; tells SquadMacroPathSystem
//     to do an immediate replan via MacroPath.ReplanAt = 0)
//   - InProgress -> Completed when per-kind completion fires
//   - Cleanup of finished orders (Completed/Cancelled/Failed) - entity
//     removed, OrderQueueHead.First advances to OrderChain.Next
//
// Per-kind completion (P4.5) lives in order_resolver_completion.go and is
// dispatched through components.OrderKindSpec.Completion. Target resolution
// (Garrison footprint center / Floor anchor, OccupyTrench nearest polyline
// point) lives in order_resolver_target.go.
type OrderResolverSystem struct {
	// Filters / maps reusable across ticks.
	squadFilter      *ecs.Filter2[components.Squad, components.OrderQueueHead]
	rosterMap        *ecs.Map[components.CommandRoster]
	posMap           *ecs.Map[components.WorldPos]
	formationDataMap *ecs.Map[components.FormationData]
	macroPathMap     *ecs.Map[components.MacroPath]

	// Order-side maps.
	orderKindMap     *ecs.Map[components.OrderKind]
	orderStateMap    *ecs.Map[components.OrderState]
	orderTargetMap   *ecs.Map[components.OrderTarget]
	orderProgressMap *ecs.Map[components.OrderProgress]
	orderChainMap    *ecs.Map[components.OrderChain]
	orderPatrolMap   *ecs.Map[components.OrderParamPatrol]
	orderIssuedAtMap *ecs.Map[components.OrderIssuedAt]
	orderOwnerMap    *ecs.Map[components.OrderOwner]
	orderFacingMap   *ecs.Map[components.OrderParamFacing]
	// Phase 14 M14.4 - SuppressFire timer reader.
	orderSuppressMap *ecs.Map[components.OrderParamSuppress]
	// Phase 14.5 M14.5.0 - Issue #10 fix: per-order out-of-range timer.
	orderOutOfRangeMap *ecs.Map[components.OrderOutOfRangeTracker]
	// Equipment / Weapon - read to compute the squad's effective weapon range
	// for the out-of-range check. Use the maximum range across roster members.
	equipmentMap *ecs.Map[components.Equipment]
	weaponMap    *ecs.Map[components.Weapon]

	// Phase 13.6 M13.6.4: read on MoveTo / DefendPosition completion to apply
	// arrived-facing to every roster member's Motion.Yaw. Instant snap; Phase
	// 25 polish may smooth the rotation.
	motionMap *ecs.Map[components.Motion]

	// Building / trench target resolution.
	buildingMap    *ecs.Map[components.Building]
	trenchRootMap  *ecs.Map[components.TrenchRoot]
	trenchResource ecs.Resource[components.TrenchNetwork]

	// Phase 14.6 M14.6.2 - Garrison CompletionEveryMemberOnFloor reads Floor
	// plates to confirm each roster member is on a floor cell (not just
	// inside the footprint AABB).
	floorFilter *ecs.Filter2[components.WorldPos, components.Floor]

	// Phase 17.6 M17.6.5 - ClearBuilding completion counts hostile Units
	// inside the footprint AABB. unitFilter walks every live unit; factionMap
	// resolves friend/foe relative to the issuing squad's Faction.
	unitFilter *ecs.Filter2[components.Unit, components.WorldPos]
	factionMap *ecs.Map[components.Faction]
	// Phase 17.8 follow-up — interior-intent orders mark the target
	// Building entity as an LODAnchor so its host chunks (and any chunk
	// the footprint spans) stay loaded by TerrainStreamingSystem. Without
	// this, doors of a building outside the player anchor's streaming
	// radius don't exist as entities — NavService has no TransitionEdge
	// for them and pathfinding into the building fails until the squad
	// physically walks close enough for the chunks to activate.
	lodAnchorMap *ecs.Map[components.LODAnchor]

	// Phase 14.6 followup - Garrison target.Pos points at a Floor entity's
	// WorldPos (NodeLevel in the multi-graph A*) so NavService.FindPath can
	// route through a Door TransitionEdge. Surface-inside-footprint cells are
	// NavInBuilding (M14.6.1) and refuse expansion - without a floor goal
	// the resolver path would terminate at the wall.
	buildingChildIndex ecs.Resource[BuildingChildIndex]
	floorComponentMap  *ecs.Map[components.Floor]

	// Phase 15 M15.C.2 - EventLog resource. Push OrderCompleted /
	// OrderFailed on lifecycle transitions.
	eventLogRes ecs.Resource[components.EventLog]

	squadService *SquadService
}

// NewOrderResolverSystem wires the system. SquadService is the dependency
// that mutates orders (re-issue Patrol) - keeps mutation logic in one place.
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

// OrderResolverSystem runs every tick. Phase 11.5 M11.5.3: tier-gating
// dropped - units / commanders no longer carry LOD markers, so all squads
// get completion checks each frame regardless of where the player is
// looking. Serial; archetype mutations (Completed -> cleanup, head advance)
// don't parallelise cleanly.
func (OrderResolverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// Order-arrival radii - Phase 14.5 M14.5.0: now driven by OrderKindSpec.
// arrivalRadiusFor reads Spec.ArrivalRadius; falls back to MoveTo default
// for kinds whose spec didn't fill the field (none in Phase 14.5, but
// defensive).
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
		squad   ecs.Entity
		newHead ecs.Entity
		oldHead ecs.Entity
		// reissueAsPatrol stamps a fresh Patrol order at the same target
		// (Loop repeat); newHead is then the new order's entity.
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
			// Stale pointer (entity destroyed elsewhere); drop the head.
			advances = append(advances, advance{squad: squad, oldHead: ord})
			continue
		}
		state := sys.orderStateMap.Get(ord)
		kind := sys.orderKindMap.Get(ord)
		target := sys.orderTargetMap.Get(ord)
		if state == nil || kind == nil || target == nil {
			continue
		}

		// Refresh entity-based target Pos every tick so a building / trench
		// that may shift (none do in Phase 11, but Phase 24 will load DEM
		// content that does) stays current.
		sys.resolveTargetPos(kind.Code, target)

		// Lifecycle.
		switch state.Code {
		case components.OrderStateIssued:
			// First pass - transition + tell macro path to replan now.
			state.Code = components.OrderStateInProgress
			if mp := sys.macroPathMap.Get(squad); mp != nil {
				mp.ReplanAt = 0
			}
			// Phase 17.8 follow-up — interior intent: pin LODAnchor on
			// target Building. Mutation deferred — archetype changes
			// during live query are forbidden by Ark. Collect into
			// `pendingLODAnchors` and apply after the query closes.
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
			// Advancement happens after the query loop (see below).
			adv := advance{squad: squad, oldHead: ord}
			if ch := sys.orderChainMap.Get(ord); ch != nil {
				adv.newHead = ch.Next
			}
			// Patrol Loop re-issue: completed Patrol with Loop=true and no
			// Next -> spawn a fresh Patrol at the same target. The chain
			// advancement then points to the new order rather than zero.
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
			// Phase 17.6 M17.6.5: ClearBuilding auto-chains an OccupyBuilding
			// on Done so the squad rests inside the cleared building. Same
			// "reissue at head" mechanism as Patrol Loop — newHead must be
			// empty (no explicit queued order), otherwise the player's chain
			// wins.
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

	// Phase 17.8 follow-up — apply LODAnchor adds after the squad query
	// closes (Ark archetype-change rule).
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
		// Drop the dead order entity. SquadService.cancelChain handles the
		// already-cancelled case; here it's an explicit single-entity removal.
		if a.oldHead != (ecs.Entity{}) && ctx.World.Alive(a.oldHead) {
			ctx.World.RemoveEntity(a.oldHead)
		}
		head := getOrderHead(sys.squadService, a.squad)
		if head == nil {
			continue
		}
		if a.reissue {
			// Re-issue at the tail of the (empty) queue. SquadService.IssueOrder
			// handles the head-empty branch. Per-kind params: Patrol carries
			// PatrolLoop, ClearBuilding auto-chains a plain OccupyBuilding.
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
		// Tell macro path to replan against the new head (or stop if zero).
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

// getOrderHead exposes the head pointer through SquadService's typed map. We
// don't store the map handle on OrderResolverSystem to avoid duplicating
// ownership - SquadService is the canonical mutator of the queue head.
func getOrderHead(svc *SquadService, squad ecs.Entity) *components.OrderQueueHead {
	if svc == nil || squad == (ecs.Entity{}) || !svc.world.Alive(squad) {
		return nil
	}
	return svc.orderQueueMap.Get(squad)
}
