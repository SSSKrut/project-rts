package components

import "github.com/mlange-42/ark/ecs"

// TacticalOverrideReason names the AI driver that pulled the unit out of its
// formation slot. UnderFire is the only one a Phase 15 writer sets; the rest
// are placeholders for systems that land later (LostLOS in Phase 17 Vision
// extensions, Reloading / OutOfAmmo in Phase 18 weapon rework, NoPath /
// BlockedByContact in Phase 16 buildings + Phase 18 traffic).
//
// Inspector reads ReasonLabel / ReasonDetail / ReasonResume off the active
// reason to render the State / Override / Reason / Resume block.
type TacticalOverrideReason uint8

const (
	TacticalOverrideNone TacticalOverrideReason = iota
	TacticalOverrideUnderFire
	TacticalOverrideLostLOS
	TacticalOverrideNoPath
	TacticalOverrideReloading
	TacticalOverrideOutOfAmmo
	TacticalOverrideBlockedByContact
)

// ReasonLabel returns the short headline shown on the Inspector's Override
// row, e.g. "Taking cover under fire".
func ReasonLabel(r TacticalOverrideReason) string {
	switch r {
	case TacticalOverrideUnderFire:
		return "Taking cover under fire"
	case TacticalOverrideLostLOS:
		return "Lost line of sight"
	case TacticalOverrideNoPath:
		return "No path to objective"
	case TacticalOverrideReloading:
		return "Reloading"
	case TacticalOverrideOutOfAmmo:
		return "Out of ammo"
	case TacticalOverrideBlockedByContact:
		return "Blocked by contact"
	}
	return ""
}

// ReasonResume returns the human-readable template describing what condition
// will clear the override. Phase 15 keeps these as static strings; future
// passes can interpolate live thresholds.
func ReasonResume(r TacticalOverrideReason) string {
	switch r {
	case TacticalOverrideUnderFire:
		return "Suppression sustained below clear threshold for 3 s"
	case TacticalOverrideLostLOS:
		return "Target reacquired or order cancelled"
	case TacticalOverrideNoPath:
		return "Replan succeeds or order cancelled"
	case TacticalOverrideReloading:
		return "Reload completes"
	case TacticalOverrideOutOfAmmo:
		return "Resupply or weapon swap"
	case TacticalOverrideBlockedByContact:
		return "Blocking unit moves clear"
	}
	return ""
}

// TacticalOverride is the marker that says "this unit is AI-driven right now -
// FormationSystem leaves it alone, an explicit player order clears it." Set
// by SurvivalInstinctSystem when a unit's Suppression crosses the squad's
// BehaviorRules.SuppressionThreshold; cleared when threat passes, the player
// issues a new order, or the safety Until timer expires.
//
// LowSuppSince is the session-time the unit's Suppression first dropped below
// the clear threshold; 0 while still above. Resets to 0 whenever Suppression
// climbs back up. The clear path waits for a sustained low-suppression window
// (clearLowDuration) before removing the marker - hysteresis against jitter.
//
// AssignedSlot is the cover slot the unit moves toward. Stored so the next
// pass can recognise the unit's claim (occupancy penalty) and a future Phase
// 17 reader can label "going to that bush" in the Inspector.
type TacticalOverride struct {
	Reason       TacticalOverrideReason
	Until        float32
	LowSuppSince float32
	AssignedSlot ecs.Entity
}
