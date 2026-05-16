package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// rlVec3XZ packs an XZ pair into a Vector3 with Y = 0. Kept private because
// it's only useful where world-coord → WorldPos requires Vector3 inputs.
func rlVec3XZ(x, z float32) rl.Vector3 { return rl.Vector3{X: x, Y: 0, Z: z} }

// OrderResolverSystem owns the Order lifecycle (PHASE-11.md P3 + P4):
//
//   - Issued → InProgress (after the first pass; tells SquadMacroPathSystem to
//     do an immediate replan via MacroPath.ReplanAt = 0)
//   - InProgress → Completed when per-kind completion fires
//   - Cleanup of finished orders (Completed/Cancelled/Failed) — entity removed,
//     OrderQueueHead.First advances to OrderChain.Next
//
// Per-kind completion (P4.5):
//
//   - MoveTo / Garrison / OccupyTrench: squad center within an arrival radius
//     of the resolved target Pos
//   - DefendPosition: never auto-completes (only via CancelAllOrders)
//   - Patrol: reached final waypoint → Completed; Loop=true re-issues the
//     order at the same Pos (handled at advancement, simpler than cycling
//     OrderChain.Next pointers per PHASE-11.md notes)
//
// Order-aware target resolution (Garrison footprint center, OccupyTrench
// nearest polyline point) is *also* this system's job — runs once per tick to
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

	// Phase 13.6 M13.6.4: read on MoveTo / DefendPosition completion to apply
	// arrived-facing to every roster member's Motion.Yaw. Instant snap; Phase
	// 25 polish may smooth the rotation.
	motionMap *ecs.Map[components.Motion]

	// Building / trench target resolution.
	buildingMap    *ecs.Map[components.Building]
	trenchRootMap  *ecs.Map[components.TrenchRoot]
	trenchResource ecs.Resource[components.TrenchNetwork]

	squadService *SquadService
}

// NewOrderResolverSystem wires the system. SquadService is the dependency
// that mutates orders (re-issue Patrol) — keeps mutation logic in one place.
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
	sys.motionMap = ecs.NewMap[components.Motion](w)

	sys.buildingMap = ecs.NewMap[components.Building](w)
	sys.trenchRootMap = ecs.NewMap[components.TrenchRoot](w)
	sys.trenchResource = ecs.NewResource[components.TrenchNetwork](w)
}

func (OrderResolverSystem) Name() string { return "order_resolver" }

// OrderResolverSystem runs every tick. Phase 11.5 M11.5.3: tier-gating
// dropped — units / commanders no longer carry LOD markers, so all squads get
// completion checks each frame regardless of where the player is looking.
// Serial; archetype mutations (Completed → cleanup, head advance) don't
// parallelise cleanly.
func (OrderResolverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// Order-arrival radii. Garrison / OccupyTrench tolerate larger squads since
// the "in footprint" / "on polyline" check is already coarse.
const (
	orderArrivalMoveTo       float32 = 2.5
	orderArrivalGarrison     float32 = 4.0
	orderArrivalOccupyTrench float32 = 3.0
)

// arrivalsByKind is a small lookup so per-kind constants stay readable.
func arrivalRadiusFor(kind components.OrderKindCode) float32 {
	switch kind {
	case components.OrderKindGarrison:
		return orderArrivalGarrison
	case components.OrderKindOccupyTrench:
		return orderArrivalOccupyTrench
	default:
		return orderArrivalMoveTo
	}
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
			// First pass — transition + tell macro path to replan now.
			state.Code = components.OrderStateInProgress
			if mp := sys.macroPathMap.Get(squad); mp != nil {
				mp.ReplanAt = 0
			}

		case components.OrderStateInProgress:
			done := sys.checkCompletion(squad, ord, kind.Code, target)
			if done {
				state.Code = components.OrderStateCompleted
				if pr := sys.orderProgressMap.Get(ord); pr != nil {
					pr.Value = 1
				}
				// Phase 13.6 M13.6.4: arrived-facing — if the order carries an
				// OrderParamFacing param, snap every roster member's Motion.Yaw
				// to the target yaw. MoveTo and DefendPosition both benefit
				// (Defend never reaches Completed via the InProgress→done path,
				// but the snapshot here is harmless for any other kind that
				// gets a facing param).
				sys.applyArrivedFacing(squad, ord)
			} else {
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
			// Next → spawn a fresh Patrol at the same target. The chain
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
// ownership — SquadService is the canonical mutator of the queue head.
func getOrderHead(svc *SquadService, squad ecs.Entity) *components.OrderQueueHead {
	if svc == nil || squad == (ecs.Entity{}) || !svc.world.Alive(squad) {
		return nil
	}
	return svc.orderQueueMap.Get(squad)
}

// resolveTargetPos refreshes OrderTarget.Pos when the target is an entity
// (Garrison → Building, OccupyTrench → TrenchRoot). For Pos-only kinds it's a
// no-op.
func (sys *OrderResolverSystem) resolveTargetPos(kind components.OrderKindCode, target *components.OrderTarget) {
	if target.Entity == (ecs.Entity{}) {
		return
	}
	switch kind {
	case components.OrderKindGarrison:
		if b := sys.buildingMap.Get(target.Entity); b != nil {
			cx := b.Footprint.CenterX()
			cz := b.Footprint.CenterZ()
			target.Pos = (components.WorldPos{}).Add(rlVec3XZ(cx, cz))
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

// checkCompletion runs the per-kind arrival test. Returns true when the
// order should transition to Completed.
func (sys *OrderResolverSystem) checkCompletion(
	squad, ord ecs.Entity,
	kind components.OrderKindCode,
	target *components.OrderTarget,
) bool {
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return false
	}
	center, ok := SquadCenter(sys.squadService.world, roster, sys.posMap)
	if !ok {
		return false
	}

	switch kind {
	case components.OrderKindDefendPosition:
		// Never auto-completes; only Cancelled by the player.
		return false

	case components.OrderKindGarrison:
		// "Squad center inside the building footprint" is the MVP completion
		// gate. Phase 15 will replace with "every roster member has window
		// cover-slot assigned".
		if b := sys.buildingMap.Get(target.Entity); b != nil {
			cx := float32(center.Chunk.X)*components.ChunkSize + center.Local.X
			cz := float32(center.Chunk.Z)*components.ChunkSize + center.Local.Z
			fp := b.Footprint
			if cx >= fp.MinX && cx <= fp.MaxX && cz >= fp.MinZ && cz <= fp.MaxZ {
				return true
			}
		}
		// Fallback: standard arrival radius against target.Pos.
		r := arrivalRadiusFor(kind)
		return centerXZDistSq(center, target.Pos) < r*r

	case components.OrderKindOccupyTrench:
		if root := sys.trenchRootMap.Get(target.Entity); root != nil {
			tn := sys.trenchResource.Get()
			if tn != nil && root.Index >= 0 && root.Index < len(tn.Lines) {
				line := &tn.Lines[root.Index]
				r := arrivalRadiusFor(kind)
				if pointNearPolyline(center, line.Points, r) {
					return true
				}
			}
		}
		// Fallback to Pos arrival.
		r := arrivalRadiusFor(kind)
		return centerXZDistSq(center, target.Pos) < r*r

	default:
		r := arrivalRadiusFor(kind)
		return centerXZDistSq(center, target.Pos) < r*r
	}
}

// updateProgress writes a rough 0..1 progress value for the head order.
// Currently 1 - dist/initialDist, computed against OrderIssuedAt as anchor
// (no extra component needed for "initial" position; we use squad center at
// issuance via the issued-at clock + a per-tick recompute would drift).
// Approximation: clamp(1 - cur/100m, 0, 1) — coarse but enough for Inspector
// progress bars. Real progress accounting comes with Phase 13's Pace param.
func (sys *OrderResolverSystem) updateProgress(squad, ord ecs.Entity, target *components.OrderTarget) {
	pr := sys.orderProgressMap.Get(ord)
	if pr == nil {
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
// yaw. Phase 13.6 M13.6.4: instant rotation — no easing. Phase 25 polish may
// interpolate; the writer side is the same, just the reader (UnitMovement)
// becomes lerp-aware.
//
// No-op when the order has no facing param, the squad has no roster, or a
// member lacks a Motion component (defensive — Phase 7 spawns guarantee it).
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
