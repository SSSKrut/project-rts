package systems

import (
	"math"

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
//   - Issued -> InProgress (after the first pass; tells SquadMacroPathSystem to
//     do an immediate replan via MacroPath.ReplanAt = 0)
//   - InProgress -> Completed when per-kind completion fires
//   - Cleanup of finished orders (Completed/Cancelled/Failed) - entity removed,
//     OrderQueueHead.First advances to OrderChain.Next
//
// Per-kind completion (P4.5):
//
//   - MoveTo / Garrison / OccupyTrench: squad center within an arrival radius
//     of the resolved target Pos
//   - DefendPosition: never auto-completes (only via CancelAllOrders)
//   - Patrol: reached final waypoint -> Completed; Loop=true re-issues the
//     order at the same Pos (handled at advancement, simpler than cycling
//     OrderChain.Next pointers per PHASE-11.md notes)
//
// Order-aware target resolution (Garrison footprint center, OccupyTrench
// nearest polyline point) is *also* this system's job - runs once per tick to
// refresh OrderTarget.Pos when the entity target moved. Cheap; 5 orders
// max active in Phase 11 scenes.
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

	// Phase 14.6 followup - Garrison target.Pos points at a Floor entity's
	// WorldPos (NodeLevel in the multi-graph A*) so NavService.FindPath can
	// route through a Door TransitionEdge. Surface-inside-footprint cells
	// are NavInBuilding (M14.6.1) and refuse expansion - without a floor
	// goal the resolver path would terminate at the wall.
	buildingChildIndex ecs.Resource[BuildingChildIndex]
	floorComponentMap  *ecs.Map[components.Floor]

	// Phase 15 M15.C.2 - EventLog resource. Push OrderCompleted / OrderFailed
	// on lifecycle transitions.
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
// dropped - units / commanders no longer carry LOD markers, so all squads get
// completion checks each frame regardless of where the player is looking.
// Serial; archetype mutations (Completed -> cleanup, head advance) don't
// parallelise cleanly.
func (OrderResolverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// Order-arrival radii - Phase 14.5 M14.5.0: now driven by OrderKindSpec.
// arrivalRadiusFor reads Spec.ArrivalRadius; falls back to MoveTo default for
// kinds whose spec didn't fill the field (none in Phase 14.5, but defensive).
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
		// reissueAsPatrol stamps a fresh Patrol order at the same target (Loop
		// repeat); newHead is then the new order's entity.
		reissueKind   components.OrderKindCode
		reissueTarget components.WorldPos
		reissueEntity ecs.Entity
		reissue       bool
	}
	var advances []advance

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
			advances = append(advances, adv)
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
			// handles the head-empty branch.
			head.First = ecs.Entity{}
			sys.squadService.IssueOrder(a.squad, a.reissueKind, a.reissueTarget, a.reissueEntity,
				false, OrderParams{PatrolLoop: true})
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

// resolveTargetPos refreshes OrderTarget.Pos when the target is an entity
// (Garrison -> Building, OccupyTrench -> TrenchRoot). For Pos-only kinds it's a
// no-op.
func (sys *OrderResolverSystem) resolveTargetPos(kind components.OrderKindCode, target *components.OrderTarget) {
	if target.Entity == (ecs.Entity{}) {
		return
	}
	// Phase 14.6 M14.6.0 (Issue #11): refuse Map.Get on a dead entity id.
	// Building / TrenchRoot don't die in Phase 14.6 (root entities carry
	// AlwaysActive), but the guard is cheap and protects against future
	// targets that do.
	if !sys.squadService.world.Alive(target.Entity) {
		return
	}
	switch kind {
	case components.OrderKindGarrison:
		if b := sys.buildingMap.Get(target.Entity); b != nil {
			// Phase 14.6 followup - prefer the ground-floor (lowest Level)
			// child's WorldPos. NavService.resolveNode matches it as a
			// NodeLevel, so A* routes through a Door TransitionEdge instead
			// of dead-ending at a NavInBuilding surface cell.
			if fp, ok := sys.firstFloorPos(target.Entity); ok {
				target.Pos = fp
			} else {
				cx := b.Footprint.CenterX()
				cz := b.Footprint.CenterZ()
				target.Pos = (components.WorldPos{}).Add(rlVec3XZ(cx, cz))
			}
		}
	case components.OrderKindOccupyTrench:
		if root := sys.trenchRootMap.Get(target.Entity); root != nil {
			tn := sys.trenchResource.Get()
			if tn != nil && root.Index >= 0 && root.Index < len(tn.Lines) {
				line := &tn.Lines[root.Index]
				if len(line.Points) > 0 {
					target.Pos = line.Points[len(line.Points)/2]
				}
			}
		}
	}
}

// completionOutcome carries the per-tick decision for an in-progress order:
// pending (keep going), done (transition to Completed), or failed (transition
// to Failed - Issue #10 for AttackTarget out-of-range).
type completionOutcome uint8

const (
	completionPending completionOutcome = iota
	completionDone
	completionFailed
)

// evaluateCompletion is Phase 14.5 M14.5.0's replacement for the old
// per-OrderKindCode switch. It dispatches by `Spec.Completion`, which keeps
// the resolver oblivious to enum identities: a new completion rule is one
// `case` here, plus the spec row pointing at it.
//
// `dt` is the current tick's delta (passed through Update). Required by
// CompletionTargetDeath to advance the out-of-range tracker.
func (sys *OrderResolverSystem) evaluateCompletion(
	squad, ord ecs.Entity,
	spec *components.OrderKindSpec,
	target *components.OrderTarget,
	dt float32,
) completionOutcome {
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return completionPending
	}
	center, ok := SquadCenter(sys.squadService.world, roster, sys.posMap)
	if !ok {
		return completionPending
	}

	switch spec.Completion {
	case components.CompletionNever:
		// DefendPosition - only Cancelled by the player.
		return completionPending

	case components.CompletionTargetDeath:
		// AttackTarget: order ends when target dies. Issue #10 extension:
		// transition to Failed when out-of-range too long.
		if target.Entity == (ecs.Entity{}) || !sys.squadService.world.Alive(target.Entity) {
			return completionDone
		}
		if spec.MaxOutOfRangeSeconds > 0 {
			if sys.advanceOutOfRange(squad, ord, target, dt) > spec.MaxOutOfRangeSeconds {
				return completionFailed
			}
		}
		return completionPending

	case components.CompletionTimer:
		// SuppressFire (and any future timer-gated kind). Spec.DurationSeconds
		// is the per-kind cap; OrderParamSuppress.StartTime overrides the
		// timing anchor when present (so a player Issue-then-pause sequence
		// uses the original StartTime, not a refreshed one).
		startTime := float32(0)
		if sp := sys.orderSuppressMap.Get(ord); sp != nil {
			startTime = sp.StartTime
		} else if ia := sys.orderIssuedAtMap.Get(ord); ia != nil {
			startTime = ia.Time
		}
		if sys.squadService.Clock()-startTime > spec.DurationSeconds {
			return completionDone
		}
		return completionPending

	case components.CompletionEveryMemberOnFloor:
		// Phase 14.6 M14.6.2 - Garrison. Completes when every live roster
		// member sits inside the building's Footprint AABB AND stands on a
		// Floor entity (any storey). Wipeout (no live members) -> Failed.
		// OrderProgress.Value carries inside/alive so Inspector's progress
		// bar reflects partial entry without a dedicated component.
		if target.Entity == (ecs.Entity{}) || !sys.squadService.world.Alive(target.Entity) {
			return completionPending
		}
		bld := sys.buildingMap.Get(target.Entity)
		if bld == nil {
			// Defensive - Garrison target wasn't a Building entity. Fall back
			// to the arrival-radius gate the old Phase 11 logic used.
			r := spec.ArrivalRadius
			if r <= 0 {
				r = 2.5
			}
			if centerXZDistSq(center, target.Pos) < r*r {
				return completionDone
			}
			return completionPending
		}
		alive, inside := sys.countInsideBuilding(roster, bld.Footprint)
		if pr := sys.orderProgressMap.Get(ord); pr != nil {
			if alive == 0 {
				pr.Value = 0
			} else {
				pr.Value = float32(inside) / float32(alive)
			}
		}
		if alive == 0 {
			return completionFailed
		}
		if inside == alive {
			return completionDone
		}
		return completionPending

	case components.CompletionArrivalRadius:
		// MoveTo / Patrol / OccupyTrench. Garrison moved to a dedicated arm
		// (CompletionEveryMemberOnFloor); the AABB short-circuit there is the
		// authoritative gate. Per-kind shape-aware short-circuits
		// (OccupyTrench polyline) run first; the radius is the universal
		// fallback.
		switch spec.Code {
		case components.OrderKindOccupyTrench:
			if root := sys.trenchRootMap.Get(target.Entity); root != nil {
				tn := sys.trenchResource.Get()
				if tn != nil && root.Index >= 0 && root.Index < len(tn.Lines) {
					line := &tn.Lines[root.Index]
					if pointNearPolyline(center, line.Points, spec.ArrivalRadius) {
						return completionDone
					}
				}
			}
		}
		r := spec.ArrivalRadius
		if r <= 0 {
			r = 2.5
		}
		if centerXZDistSq(center, target.Pos) < r*r {
			return completionDone
		}
		return completionPending
	}
	return completionPending
}

// advanceOutOfRange updates the out-of-range tracker on `ord` and returns the
// accumulated elapsed seconds. Resets to zero whenever at least one alive
// roster member sits within the squad's max weapon range of the target. Used
// by the CompletionTargetDeath arm to enforce Spec.MaxOutOfRangeSeconds.
func (sys *OrderResolverSystem) advanceOutOfRange(
	squad, ord ecs.Entity,
	target *components.OrderTarget,
	dt float32,
) float32 {
	tracker := sys.orderOutOfRangeMap.Get(ord)
	if tracker == nil {
		// Defensive - IssueOrder installs the tracker for any kind whose
		// spec has MaxOutOfRangeSeconds > 0. If we somehow got here without
		// one, install lazily so the next tick has a place to write.
		sys.orderOutOfRangeMap.Add(ord, &components.OrderOutOfRangeTracker{})
		tracker = sys.orderOutOfRangeMap.Get(ord)
		if tracker == nil {
			return 0
		}
	}
	// Phase 14.6 M14.6.0 (Issue #11): if target died this tick, reset the
	// tracker and let the parent CompletionTargetDeath arm transition to
	// Done - keep us off any Map.Get against the dead id.
	if target.Entity != (ecs.Entity{}) && !sys.squadService.world.Alive(target.Entity) {
		tracker.Elapsed = 0
		return 0
	}
	// Compute max weapon range across roster.
	maxRange := sys.maxSquadWeaponRange(squad)
	if maxRange <= 0 {
		// No weapons or all dead - treat as in-range so we don't insta-fail.
		// (Squad with no working weapons can't complete AttackTarget anyway;
		// Phase 15 SurvivalInstinct will surface this differently.)
		tracker.Elapsed = 0
		return 0
	}
	targetPos := target.Pos
	if live := sys.posMap.Get(target.Entity); live != nil {
		targetPos = *live
	}
	roster := sys.rosterMap.Get(squad)
	anyInRange := false
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		mp := sys.posMap.Get(mem)
		if mp == nil {
			continue
		}
		if centerXZDistSq(*mp, targetPos) <= maxRange*maxRange {
			anyInRange = true
			break
		}
	}
	if anyInRange {
		tracker.Elapsed = 0
		return 0
	}
	tracker.Elapsed += dt
	return tracker.Elapsed
}

// maxSquadWeaponRange returns the largest Weapon.RangeM across all live
// roster members' Equipment.Primary. Zero if no weapons.
func (sys *OrderResolverSystem) maxSquadWeaponRange(squad ecs.Entity) float32 {
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return 0
	}
	var best float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		eq := sys.equipmentMap.Get(mem)
		if eq == nil || eq.Primary == (ecs.Entity{}) {
			continue
		}
		w := sys.weaponMap.Get(eq.Primary)
		if w == nil {
			continue
		}
		if w.RangeM > best {
			best = w.RangeM
		}
	}
	return best
}

// updateProgress writes a rough 0..1 progress value for the head order.
// Currently 1 - dist/initialDist, computed against OrderIssuedAt as anchor
// (no extra component needed for "initial" position; we use squad center at
// issuance via the issued-at clock + a per-tick recompute would drift).
// Approximation: clamp(1 - cur/100m, 0, 1) - coarse but enough for Inspector
// progress bars. Real progress accounting comes with Phase 13's Pace param.
func (sys *OrderResolverSystem) updateProgress(squad, ord ecs.Entity, target *components.OrderTarget) {
	pr := sys.orderProgressMap.Get(ord)
	if pr == nil {
		return
	}
	// Phase 14.6 M14.6.2: Garrison writes inside/alive into Progress.Value
	// directly from evaluateCompletion. Don't clobber it with a
	// distance-from-center fraction here.
	if kind := sys.orderKindMap.Get(ord); kind != nil &&
		components.SpecForOrderKind(kind.Code).Completion == components.CompletionEveryMemberOnFloor {
		return
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	center, ok := SquadCenter(sys.squadService.world, roster, sys.posMap)
	if !ok {
		return
	}
	d := math.Sqrt(float64(centerXZDistSq(center, target.Pos)))
	const fullRange = 100.0
	v := float32(1 - d/fullRange)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	pr.Value = v
}

// applyArrivedFacing reads the optional OrderParamFacing on a freshly-
// completed order and snaps every roster member's Motion.Yaw to the requested
// yaw. Phase 13.6 M13.6.4: instant rotation - no easing. Phase 25 polish may
// interpolate; the writer side is the same, just the reader (UnitMovement)
// becomes lerp-aware.
//
// No-op when the order has no facing param, the squad has no roster, or a
// member lacks a Motion component (defensive - Phase 7 spawns guarantee it).
func (sys *OrderResolverSystem) applyArrivedFacing(squad, ord ecs.Entity) {
	if sys.orderFacingMap == nil || sys.motionMap == nil {
		return
	}
	facing := sys.orderFacingMap.Get(ord)
	if facing == nil {
		return
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		if m := sys.motionMap.Get(mem); m != nil {
			m.Yaw = facing.YawRad
		}
	}
}

// firstFloorPos returns the WorldPos of the lowest-Level Floor child of
// `building`. Used by resolveTargetPos to anchor a Garrison goal on a Floor
// NavNode (reachable through Door TransitionEdges) rather than a surface
// NavInBuilding cell that A* would refuse. (_, false) when the building has
// no live Floor children (chunk evicted, or layout has not yet generated).
func (sys *OrderResolverSystem) firstFloorPos(building ecs.Entity) (components.WorldPos, bool) {
	idx := sys.buildingChildIndex.Get()
	if idx == nil {
		return components.WorldPos{}, false
	}
	children, ok := idx.Loaded[building]
	if !ok || len(children) == 0 {
		return components.WorldPos{}, false
	}
	var bestPos components.WorldPos
	bestLevel := uint8(255)
	found := false
	for _, c := range children {
		floor := sys.floorComponentMap.Get(c)
		if floor == nil {
			continue
		}
		pos := sys.posMap.Get(c)
		if pos == nil {
			continue
		}
		if !found || floor.Level < bestLevel {
			bestPos = *pos
			bestLevel = floor.Level
			found = true
		}
	}
	return bestPos, found
}

// countInsideBuilding tallies how many of `roster`'s live members sit inside
// the building's Footprint AABB AND on a Floor entity (any storey). Returns
// (alive, inside). Phase 14.6 M14.6.2 - the Garrison CompletionEveryMemberOnFloor
// arm uses this to gate Done / Failed transitions and to write a per-member
// progress fraction into OrderProgress.Value.
func (sys *OrderResolverSystem) countInsideBuilding(
	roster *components.CommandRoster, footprint components.AABB2D,
) (alive, inside uint8) {
	if roster == nil {
		return 0, 0
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		pos := sys.posMap.Get(mem)
		if pos == nil {
			continue
		}
		alive++
		mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		if mx < footprint.MinX || mx > footprint.MaxX || mz < footprint.MinZ || mz > footprint.MaxZ {
			continue
		}
		if sys.memberOnFloor(pos) {
			inside++
		}
	}
	return alive, inside
}

// memberOnFloor returns true when `pos` sits over the horizontal extent of any
// live Floor plate AND its Y is within +/-1.5 m of the floor surface. The Y
// proximity catches both surface stories (member Y ~ floor Y) and bunkers
// (member Y dropped into the sunken floor). Cheap: 1-3 buildings x 1-3 floors
// per scene = handful of plate checks per call.
func (sys *OrderResolverSystem) memberOnFloor(pos *components.WorldPos) bool {
	mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	q := sys.floorFilter.Query()
	for q.Next() {
		fpos, floor := q.Get()
		fx := float32(fpos.Chunk.X)*components.ChunkSize + fpos.Local.X
		fz := float32(fpos.Chunk.Z)*components.ChunkSize + fpos.Local.Z
		if mx < fx || mx > fx+floor.SizeX || mz < fz || mz > fz+floor.SizeZ {
			continue
		}
		dy := pos.Local.Y - fpos.Local.Y
		if dy > -1.5 && dy < 1.5 {
			q.Close()
			return true
		}
	}
	return false
}

// pointNearPolyline returns true when p is within `radius` of any segment of
// the polyline. Reused by trench arrival and the hit-test resolver.
func pointNearPolyline(p components.WorldPos, points []components.WorldPos, radius float32) bool {
	if len(points) < 2 {
		return false
	}
	rSq := radius * radius
	px := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
	pz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
	for i := 1; i < len(points); i++ {
		ax := float32(points[i-1].Chunk.X)*components.ChunkSize + points[i-1].Local.X
		az := float32(points[i-1].Chunk.Z)*components.ChunkSize + points[i-1].Local.Z
		bx := float32(points[i].Chunk.X)*components.ChunkSize + points[i].Local.X
		bz := float32(points[i].Chunk.Z)*components.ChunkSize + points[i].Local.Z
		dx := bx - ax
		dz := bz - az
		lenSq := dx*dx + dz*dz
		if lenSq < 1e-6 {
			continue
		}
		t := ((px-ax)*dx + (pz-az)*dz) / lenSq
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		cx := ax + dx*t
		cz := az + dz*t
		if (px-cx)*(px-cx)+(pz-cz)*(pz-cz) < rSq {
			return true
		}
	}
	return false
}
