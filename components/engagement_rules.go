package components

// EngagementMode is the standing fire-decision for a Squad. Phase 13 writes
// it; Phase 14 (Combat) wires WeaponSystem to actually gate firing on it.
// See COMMAND-MODEL.md §4 for the per-role default mapping.
type EngagementMode uint8

const (
	// HoldFire — no engagement under any condition unless overridden by an
	// explicit AttackTarget order.
	HoldFire EngagementMode = iota
	// ReturnFire — only engage entities that have fired on us first.
	ReturnFire
	// FreeFire — engage every valid target in sight within the target-type
	// allow list below.
	FreeFire
)

// StandoffPolicy is the preferred engagement range bucket. Phase 13 reserves
// the field as a scaffold; Phase 14 TargetPriority will read it to bias
// target selection. Tactical UI (Hold-at-Long for snipers, Close for AT)
// surfaces through the Inspector once Phase 14 has weapon range data.
type StandoffPolicy uint8

const (
	StandoffAny StandoffPolicy = iota
	StandoffClose
	StandoffMedium
	StandoffLong
)

// EngagementRules is the per-Squad standing rule covering fire decisions.
// Phase 13 fills Mode + the four per-type toggles; Standoff / SectorYaw /
// SectorHalfDot are reserved for Phase 14 and left zero for now.
//
// COMMAND-MODEL.md §4 P-decision: lives on Squad, per-Unit override deferred
// to Phase 21. Phase 14 WeaponSystem reads this; Phase 13 has no reader
// beyond the Inspector quick-bar (M13.6).
type EngagementRules struct {
	Mode         EngagementMode
	FireOnInf    bool
	FireOnArm    bool
	FireOnAir    bool
	FireOnStruct bool

	// Phase 14+ — left zero in Phase 13.
	Standoff      StandoffPolicy
	SectorYaw     float32 // optional cone centre yaw (radians)
	SectorHalfDot float32 // optional cone half-angle as dot threshold
}

// EngagementModeName / StandoffName return human labels surfaced by the
// Inspector quick-bar (M13.6).
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
