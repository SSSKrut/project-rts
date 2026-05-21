package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

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
		// OrderProgress.Value carries inside/alive so Inspector's progress bar
		// reflects partial entry without a dedicated component.
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
		// Defensive - IssueOrder installs the tracker for any kind whose spec
		// has MaxOutOfRangeSeconds > 0. If we somehow got here without one,
		// install lazily so the next tick has a place to write.
		sys.orderOutOfRangeMap.Add(ord, &components.OrderOutOfRangeTracker{})
		tracker = sys.orderOutOfRangeMap.Get(ord)
		if tracker == nil {
			return 0
		}
	}
	// Phase 14.6 M14.6.0 (Issue #11): if target died this tick, reset the
	// tracker and let the parent CompletionTargetDeath arm transition to Done
	// - keep us off any Map.Get against the dead id.
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
