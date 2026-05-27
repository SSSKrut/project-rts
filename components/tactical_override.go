package components

import "github.com/mlange-42/ark/ecs"

// TacticalOverrideReason names the AI driver that pulled the unit out of its
// formation slot.
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
// will clear the override.
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
// by SurvivalInstinctSystem when a unit's Threat.Suppression crosses the
// squad's BehaviorRules.SuppressionThreshold; cleared when threat passes, the
// player issues a new order, or the safety Until timer expires.
//
// LowSuppSince is the session-time the unit's Threat.Suppression first
// dropped below the clear threshold; 0 while still above. The clear path
// waits for a sustained low-suppression window before removing the marker -
// hysteresis against jitter.
//
// AssignedSlot is the cover slot the unit moves toward. Stored so the next
// pass can recognise the unit's claim (occupancy penalty).
type TacticalOverride struct {
	Reason       TacticalOverrideReason
	Until        float32
	LowSuppSince float32
	AssignedSlot ecs.Entity
}
