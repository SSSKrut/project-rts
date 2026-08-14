package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// completionOutcome carries the per-tick decision for an in-progress order.
type completionOutcome uint8

const (
	completionPending completionOutcome = iota
	completionDone
	completionFailed
)

// evaluateCompletion dispatches by Spec.Completion so the resolver stays
// oblivious to OrderKindCode enum identities — a new completion rule is one
// `case` here plus the spec row pointing at it.
//
// `dt` is the current tick's delta, required by CompletionTargetDeath to
// advance the out-of-range tracker.
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
		// DefendPosition — only Cancelled by the player.
		return completionPending

	case components.CompletionTargetDeath:
		// AttackTarget: order ends when target dies; Failed when out-of-
		// range exceeds MaxOutOfRangeSeconds.
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
		// OrderParamSuppress.StartTime overrides the timing anchor when
		// present (Issue-then-pause uses the original StartTime, not a
		// refreshed one).
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
		// Garrison: completes when every live member sits inside the
		// Footprint AABB AND stands on a Floor entity (any storey).
		// Wipeout (no live members) → Failed. OrderProgress.Value
		// carries inside/alive for the Inspector progress bar.
		if target.Entity == (ecs.Entity{}) || !sys.squadService.world.Alive(target.Entity) {
			return completionPending
		}
		bld := sys.buildingMap.Get(target.Entity)
		if bld == nil {
			// Defensive — target wasn't a Building; fall back to arrival
			// radius.
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
			// Garrison additionally demands the window plan manned: member i
			// parked at window slot i (the FormationSystem assignment).
			// Completing on mere entry dropped squad management the moment
			// the roster crossed the wall line — nobody ever reached the
			// windows the popup promised.
			if spec.Code == components.OrderKindGarrison {
				manned, expected := sys.windowSlotsManned(ord, target.Entity, roster)
				if manned < expected {
					if pr := sys.orderProgressMap.Get(ord); pr != nil && expected > 0 {
						pr.Value = float32(manned) / float32(expected)
					}
					return completionPending
				}
			}
			return completionDone
		}
		return completionPending

	case components.CompletionClearBuilding:
		// ClearBuilding: completes when no hostile sits inside the
		// footprint AND ≥1 friendly is inside. Wipeout → Failed.
		// Auto-chain to OccupyBuilding lives in Update's advance loop.
		if target.Entity == (ecs.Entity{}) || !sys.squadService.world.Alive(target.Entity) {
			return completionPending
		}
		bld := sys.buildingMap.Get(target.Entity)
		if bld == nil {
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
		if alive == 0 {
			return completionFailed
		}
		var ownFaction uint8
		if f := sys.factionMap.Get(squad); f != nil {
			ownFaction = f.ID
		}
		hostiles := sys.countHostilesInBuilding(bld.Footprint, ownFaction)
		if pr := sys.orderProgressMap.Get(ord); pr != nil {
			pr.Value = float32(inside) / float32(alive)
		}
		if hostiles == 0 && inside >= 1 {
			return completionDone
		}
		return completionPending

	case components.CompletionArrivalRadius:
		// MoveTo / Patrol / OccupyTrench. Per-kind shape-aware short-
		// circuits (OccupyTrench polyline) run first; radius is the
		// universal fallback.
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
		// Arrival is judged from the SAME reference point and radius the
		// macro path clears at: the squad ANCHOR (leader), within
		// SquadArrivalCoeff × Spacing. Any mismatch deadlocks: SquadMacroPath
		// drops HasGoal, FormationSystem stops driving slots, and whatever
		// the completion still measures freezes just outside its ring (the
		// old member-centroid test needed depth-widening crutches for
		// columns/vehicles and still froze the compound standoff MoveTo once
		// the wake forward bias stopped overshooting centroids).
		if fd := sys.formationDataMap.Get(squad); fd != nil {
			if ma := SquadArrivalCoeff * fd.Spacing; ma > r {
				r = ma
			}
		}
		// A vehicle anchor parks rampPopRadius short of the point (M7).
		if reach := SquadWaypointReach(sys.squadService.world, roster, sys.vehicleMap); reach > r {
			r = reach
		}
		anchor, okA := SquadAnchorPos(sys.squadService.world, roster, sys.posMap)
		if !okA {
			return completionPending
		}
		if centerXZDistSq(anchor, target.Pos) < r*r {
			// Storey-target MoveTo ("Occupy L<n>"): the squad centre passes
			// the goal XZ on the way to the stairs while still a floor below
			// — require the centre's Y to match the target storey as well.
			if target.Entity != (ecs.Entity{}) && sys.levelMap.Has(target.Entity) {
				dy := center.Local.Y - target.Pos.Local.Y
				if dy < -1.5 || dy > 1.5 {
					return completionPending
				}
			}
			return completionDone
		}
		return completionPending
	}
	return completionPending
}

// garrisonWindowSlots recomputes the Garrison window plan — the same planner
// call FormationSystem executes.
func (sys *OrderResolverSystem) garrisonWindowSlots(
	ord, building ecs.Entity, roster *components.CommandRoster,
) []BuildingSlot {
	if sys.slotPlanner == nil {
		return nil
	}
	hasFacing := false
	var yaw float32
	if f := sys.orderFacingMap.Get(ord); f != nil {
		hasFacing = true
		yaw = f.YawRad
	}
	return sys.slotPlanner.PlanSlots(building, SlotWindows, int(roster.Count), hasFacing, yaw)
}

// windowSlotsManned counts assigned window slots and how many hold their
// member. WHO holds WHICH slot comes from the matcher's live assignment
// (LocalBlackboard.HeldSlot) — the old member-i↔slot-i pairing diverged from
// FormationSystem the moment the slot-major matcher landed (19.8): completion
// fired on coincidental proximity and windows the matcher dropped (scarce
// men, overrides) blocked it. Nil plan (floors not streamed) = 0/0.
func (sys *OrderResolverSystem) windowSlotsManned(
	ord, building ecs.Entity, roster *components.CommandRoster,
) (manned, expected int) {
	slots := sys.garrisonWindowSlots(ord, building, roster)
	if len(slots) == 0 {
		return 0, 0
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		bb := sys.blackboardMap.Get(mem)
		if bb == nil {
			continue
		}
		si, held := bb.HeldSlotIndex()
		if !held || int(si) >= len(slots) || !slots[si].Window {
			continue
		}
		expected++
		pos := sys.posMap.Get(mem)
		if pos == nil {
			continue
		}
		d := pos.Sub(slots[si].Pos)
		r := SlotParkRadius + 0.2
		if d.X*d.X+d.Z*d.Z <= r*r && d.Y >= -0.8 && d.Y <= 0.8 {
			manned++
		}
	}
	if expected == 0 {
		// Windows exist but nobody holds one yet (matcher hasn't run / every
		// man overridden) — completing on entry is exactly the bug slot-
		// completion exists to prevent.
		for i := range slots {
			if slots[i].Window {
				return 0, 1
			}
		}
	}
	return manned, expected
}

// applyGarrisonFacing snaps every member parked on its HELD window slot to
// the opening's outward yaw at completion time. Same assignment source as
// windowSlotsManned — the formation's own park-radius snap (0.8) misses a
// man completing at 0.9, and after completion nothing else writes his yaw.
func (sys *OrderResolverSystem) applyGarrisonFacing(squad, ord, building ecs.Entity) {
	if sys.motionMap == nil {
		return
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	slots := sys.garrisonWindowSlots(ord, building, roster)
	if len(slots) == 0 {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		bb := sys.blackboardMap.Get(mem)
		if bb == nil {
			continue
		}
		si, held := bb.HeldSlotIndex()
		if !held || int(si) >= len(slots) || !slots[si].Window {
			continue
		}
		pos := sys.posMap.Get(mem)
		if pos == nil {
			continue
		}
		d := pos.Sub(slots[si].Pos)
		r := SlotParkRadius + 0.2
		if d.X*d.X+d.Z*d.Z > r*r || d.Y < -0.8 || d.Y > 0.8 {
			continue
		}
		if m := sys.motionMap.Get(mem); m != nil {
			m.Yaw = slots[si].Yaw
		}
	}
}

// advanceOutOfRange updates the out-of-range tracker and returns accumulated
// elapsed seconds. Resets when any alive member sits within the squad's max
// weapon range of the target.
func (sys *OrderResolverSystem) advanceOutOfRange(
	squad, ord ecs.Entity,
	target *components.OrderTarget,
	dt float32,
) float32 {
	tracker := sys.orderOutOfRangeMap.Get(ord)
	if tracker == nil {
		sys.orderOutOfRangeMap.Add(ord, &components.OrderOutOfRangeTracker{})
		tracker = sys.orderOutOfRangeMap.Get(ord)
		if tracker == nil {
			return 0
		}
	}
	// Target died: reset and let the parent arm transition to Done — keep
	// us off any Map.Get against the dead id.
	if target.Entity != (ecs.Entity{}) && !sys.squadService.world.Alive(target.Entity) {
		tracker.Elapsed = 0
		return 0
	}
	maxRange := sys.maxSquadWeaponRange(squad)
	if maxRange <= 0 {
		// No working weapons → treat as in-range so we don't insta-fail.
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

// maxSquadWeaponRange returns the largest Weapon.RangeM across live
// members' Equipment.Primary. Zero if no weapons.
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
