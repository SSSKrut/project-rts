package components

// EngagementMode is the standing fire-decision for a Squad. WeaponSystem
// gates firing on it. See COMMAND-MODEL.md S4 for the per-role default.
type EngagementMode uint8

const (
	// HoldFire - no engagement unless overridden by an explicit AttackTarget
	// or SuppressFire order (OrderKindSpec.OverridesHoldFire).
	HoldFire EngagementMode = iota
	// ReturnFire - only engage entities that have fired on us first.
	ReturnFire
	// FreeFire - engage every valid target in sight within the target-type
	// allow list below.
	FreeFire
)

// StandoffPolicy is the preferred engagement range bucket. TargetPriority
// uses it to bias target selection.
type StandoffPolicy uint8

const (
	StandoffAny StandoffPolicy = iota
	StandoffClose
	StandoffMedium
	StandoffLong
)

// EngagementRules is the per-Squad standing rule covering fire decisions.
// Lives on Squad (per-Unit override deferred). WeaponSystem is the reader.
type EngagementRules struct {
	Mode         EngagementMode
	FireOnInf    bool
	FireOnArm    bool
	FireOnAir    bool
	FireOnStruct bool

	Standoff      StandoffPolicy
	SectorYaw     float32 // optional cone centre yaw (radians)
	SectorHalfDot float32 // optional cone half-angle as dot threshold
}

func EngagementModeName(m EngagementMode) string {
	switch m {
	case ReturnFire:
		return "Return"
	case FreeFire:
		return "Free"
	default:
		return "Hold"
	}
}

func StandoffName(s StandoffPolicy) string {
	switch s {
	case StandoffClose:
		return "Close"
	case StandoffMedium:
		return "Medium"
	case StandoffLong:
		return "Long"
	default:
		return "Any"
	}
}
