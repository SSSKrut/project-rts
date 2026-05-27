package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Matches the dispersion movingFactor threshold so the moving/dispersion
// flags toggle on the same boundary.
const weaponMovingSpeedThreshold float32 = 1.0

// shouldFire is the RoE + AttackMove + Sector gate. HoldFire wins over
// AttackMove, but an active order whose spec sets OverridesHoldFire bypasses
// the HoldFire silence (AttackTarget / SuppressFire carry this flag;
// AttackMove does not).
func (sys *WeaponSystem) shouldFire(shooter ecs.Entity, motionSpeed float32,
	shooterPos *components.WorldPos, targetPos components.WorldPos) bool {
	// Reloading / Suppressed silence the unit unconditionally — animation /
	// shock state forbids firing even with FreeFire or AttackTarget override.
	if b := sys.blackboardMap.Get(shooter); b != nil {
		switch b.CurrentMode {
		case components.ModeReloading, components.ModeSuppressed:
			return false
		}
	}

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
			// Per-order EngagementMode override (e.g. Hidden position preset).
			// Swaps Mode only; FireOn* / Sector remain the standing rules.
			if override := sys.orderEngagementOverrideMap.Get(head.First); override != nil {
				rules.Mode = override.Mode
			}
		}
	}

	switch rules.Mode {
	case components.HoldFire:
		if !overridesHoldFire {
			return false
		}
	case components.ReturnFire, components.FreeFire:
	}

	if !rules.FireOnInf && !overridesHoldFire {
		return false
	}

	if motionSpeed > weaponMovingSpeedThreshold && !attackMoveOn && !overridesHoldFire {
		return false
	}

	// SectorHalfDot is cos(half-angle); 0 disables the cone.
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
// hostile sighting still alive, in range, and within the awareness max-age.
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
		// Alive-check BEFORE any Map.Get: Vision tick can lag Death by one
		// cadence, so the FIFO may hold a recycled slot; touching a dead id
		// crashes via Ark's slot-reuse path.
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
		// Awareness.Pos is stale by up to one Vision tick; prefer live.
		pos := e.Pos
		if live := sys.posMap.Get(e.Target); live != nil {
			pos = *live
		} else {
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
