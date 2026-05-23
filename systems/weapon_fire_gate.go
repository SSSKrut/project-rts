package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// weaponMovingSpeedThreshold - Motion.Speed above this (m/s) counts as
// "moving" for the firing-while-moving gate. PHASE-14.md notes: matches the
// dispersion movingFactor threshold (1.0 m/s) so the two flags toggle on the
// same boundary instead of needing two separate empirical fits.
const weaponMovingSpeedThreshold float32 = 1.0

// shouldFire is the RoE + AttackMove + Sector gate. Returns true when the
// unit is permitted to take the resolved shot.
//
// Decision tree (PHASE-14.md M14.3 + Q2 lock-in: HoldFire wins over
// AttackMove). Phase 14.5 M14.5.0 (Issue #9 fix): if the active order's spec
// declares `OverridesHoldFire`, the HoldFire silence is bypassed for that
// order's duration. AttackTarget and SuppressFire carry this flag; AttackMove
// does NOT (HoldFire still wins per Q2 lock).
//
//   - No squad (soloist) -> default FreeFire-on-Inf. Fires.
//   - Mode=HoldFire -> never, UNLESS the active order has OverridesHoldFire.
//   - Mode=ReturnFire -> always (any awareness hostile counts as "threat in
//     awareness" per P9). Stricter "only after being shot at" deferred.
//   - Mode=FreeFire -> always.
//   - FireOnInf gate - Phase 14 has only Inf targets, so this acts on
//     AT-team-style rules that have FireOnInf=false. Bypassed by the same
//     OverridesHoldFire override.
//   - Fire-while-moving (Motion.Speed > threshold) requires the squad's
//     active order to carry the AttackMove flag.
//   - Phase 15 M15.A.4: Sector cone. When SectorHalfDot > 0 the squad defends
//     only a cone centred on SectorYaw; targets outside silently fail the
//     gate. SectorHalfDot is interpreted as cos(half-angle), so 1.0 = zero
//     cone (disabled when 0).
//
// `motionSpeed` + `shooterPos` + `targetPos` are passed in by the caller so
// the seer-loop snapshot avoids re-resolving pointers.
func (sys *WeaponSystem) shouldFire(shooter ecs.Entity, motionSpeed float32,
	shooterPos *components.WorldPos, targetPos components.WorldPos) bool {
	rules := components.EngagementRules{
		Mode: components.FreeFire, FireOnInf: true, FireOnArm: true,
	}
	attackMoveOn := false
	overridesHoldFire := false

	if sm := sys.squadMemberMap.Get(shooter); sm != nil && sm.Squad != (ecs.Entity{}) {
		if r := sys.engagementRulesMap.Get(sm.Squad); r != nil {
			rules = *r
		}
		if head := sys.orderQueueMap.Get(sm.Squad); head != nil && head.First != (ecs.Entity{}) {
			if sys.orderAttackMoveMap.Has(head.First) {
				attackMoveOn = true
			}
			if kind := sys.orderKindMap.Get(head.First); kind != nil {
				if components.SpecForOrderKind(kind.Code).OverridesHoldFire {
					overridesHoldFire = true
				}
			}
			// Phase 17.6 M17.6.6 - per-order EngagementMode override
			// (Hidden position preset). Swaps Mode only; FireOn* / Sector
			// remain the standing rules.
			if override := sys.orderEngagementOverrideMap.Get(head.First); override != nil {
				rules.Mode = override.Mode
			}
		}
	}

	switch rules.Mode {
	case components.HoldFire:
		// Phase 14.5 M14.5.0 (Issue #9): AttackTarget / SuppressFire - the
		// player's explicit fire orders - win over the standing HoldFire.
		if !overridesHoldFire {
			return false
		}
	case components.ReturnFire, components.FreeFire:
		// Allowed; target-type and movement gates apply below.
	}

	// Phase 14 target-type gate: every target is Inf for now (vehicles in
	// Phase 16). FireOnInf=false (AT team default) silences the squad - unless
	// an explicit AttackTarget/SuppressFire is overriding.
	if !rules.FireOnInf && !overridesHoldFire {
		return false
	}

	// Fire-while-moving: stationary always fires; moving needs AttackMove or
	// an explicit fire-order override (you can sprint-shoot an AttackTarget).
	if motionSpeed > weaponMovingSpeedThreshold && !attackMoveOn && !overridesHoldFire {
		return false
	}

	// Phase 15 M15.A.4 - Sector gate. SectorHalfDot is cos(half-angle); when
	// positive (i.e. the squad has set a cone) any target outside the cone is
	// filtered out. OverridesHoldFire bypasses the gate so an explicit
	// AttackTarget on an out-of-sector enemy still fires.
	if rules.SectorHalfDot > 0 && !overridesHoldFire {
		dx := targetPos.Local.X - shooterPos.Local.X +
			float32(targetPos.Chunk.X-shooterPos.Chunk.X)*components.ChunkSize
		dz := targetPos.Local.Z - shooterPos.Local.Z +
			float32(targetPos.Chunk.Z-shooterPos.Chunk.Z)*components.ChunkSize
		mag := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if mag > 0 {
			dx /= mag
			dz /= mag
			fx := float32(math.Sin(float64(rules.SectorYaw)))
			fz := float32(math.Cos(float64(rules.SectorYaw)))
			if dx*fx+dz*fz < rules.SectorHalfDot {
				return false
			}
		}
	}
	return true
}

// pickTarget walks the seer's Awareness FIFO and returns the most recent
// hostile sighting that's still alive, still in range, and within the
// awareness max-age. (ent, pos, true) on hit; (_, _, false) on miss.
func (sys *WeaponSystem) pickTarget(
	self ecs.Entity, ownFaction uint8, selfPos *components.WorldPos,
	aware *components.Awareness, weapon *components.Weapon, now float32,
) (ecs.Entity, components.WorldPos, bool) {
	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	bestTime := float32(0)
	rngSq := weapon.RangeM * weapon.RangeM
	for i := range aware.LastSeen {
		e := aware.LastSeen[i]
		if e.Time == 0 || e.Target == (ecs.Entity{}) || e.Target == self {
			continue
		}
		// Phase 14.6 M14.6.0 (Issue #11): alive-check BEFORE any Map.Get on
		// e.Target. Vision tick can lag Death by up to one cadence, so the
		// FIFO may hold a recycled slot; touching factionMap on a dead id
		// crashes via Ark's slot reuse path.
		if !sys.worldRef.Alive(e.Target) {
			continue
		}
		if now-e.Time > weaponAwarenessMaxAge {
			continue
		}
		f := sys.factionMap.Get(e.Target)
		if f == nil || f.ID == ownFaction {
			continue
		}
		// Use the candidate's live position if the entity is still alive
		// (Awareness.Pos is stale by up to one Vision tick).
		pos := e.Pos
		if live := sys.posMap.Get(e.Target); live != nil {
			pos = *live
		} else {
			// Entity vanished (despawn) - skip.
			continue
		}
		dSq := worldDistSq(*selfPos, pos)
		if dSq > rngSq {
			continue
		}
		if e.Time > bestTime {
			bestTime = e.Time
			bestEnt = e.Target
			bestPos = pos
		}
	}
	if bestEnt == (ecs.Entity{}) {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	return bestEnt, bestPos, true
}
