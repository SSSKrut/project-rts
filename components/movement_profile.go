package components

// Pace is a Phase 13 movement-tempo dial that multiplies the base
// stance-speed table. Walk is the default; Sprint costs Stamina.
//
// See COMMAND-MODEL.md §4 and PHASE-13.md P3 for the rate table.
type Pace uint8

const (
	PaceWalk Pace = iota
	PaceRun
	PaceSprint
)

// PaceSpeedMul is the Pace → speed multiplier table. Applied after
// the Stance multiplier in UnitMovementSystem: final = base × Stance × Pace.
// 1.0 / 1.6 / 2.2 matches GAMEDESIGN §5 (Movement profile).
var PaceSpeedMul = [...]float32{
	PaceWalk:   1.0,
	PaceRun:    1.6,
	PaceSprint: 2.2,
}

// PaceStaminaDrain is the per-second Stamina cost at each Pace. Walk is free,
// Sprint depletes fast. Phase 13 only — Phase 15 may rebalance per doctrine.
var PaceStaminaDrain = [...]float32{
	PaceWalk:   0.0,
	PaceRun:    0.10,
	PaceSprint: 0.50,
}

// Posture is the audio / visual signature lever. Standard is the default;
// Quiet reduces detection signature once Phase 15 wires audio. In Phase 13
// the field is written/read by Inspector + presets but has no behavioural
// effect — pure scaffold for the audio/vision modifier system later.
type Posture uint8

const (
	PostureStandard Posture = iota
	PostureQuiet
)

// PathStyle steers NavService.FindPath cell-cost modifiers. See
// PHASE-13.md P4 for the modifier table; CoverSeek reads NavCell.CoverDistance
// (added in M13.4) for cover-proximity bias.
type PathStyle uint8

const (
	PathStyleDirect PathStyle = iota
	PathStyleRoadPrefer
	PathStyleRoadAvoid
	PathStyleCoverSeek
)

// MovementProfile is the per-Squad standing rule covering all four movement
// axes. Lives on the Squad entity (not the Unit) per PHASE-13.md P1;
// UnitMovementSystem reads its squad's profile via SquadMember back-pointer.
// Override at order-level via OrderParamMovementProfile (M13.5).
//
// Stance here is the "default to return to" — the squad's resting posture
// once it has no ActionStance override. Action{Kind=ActionStance} from
// ActionQueue still wins for the duration of that action.
type MovementProfile struct {
	Pace      Pace
	Stance    StanceCode
	Posture   Posture
	PathStyle PathStyle
}

// MovementPreset enumerates the six built-in preset chips surfaced in the
// Inspector quick-bar (M13.6) and cycled via the `[` / `]` hotkeys (M13.7).
// Hotkey grammar Ctrl+RMB → PresetStealth applied as OrderParamMovementProfile
// override on the new MoveTo order.
type MovementPreset uint8

const (
	PresetDefault MovementPreset = iota
	PresetCautious
	PresetRush
	PresetSprint
	PresetStealth
	PresetProneCrawl
)

// ApplyPreset returns the MovementProfile for a given preset. Table mirrors
// the GAMEDESIGN §5 / COMMAND-MODEL §4 standard preset list. Phase 13 callers
// (Inspector chip click, Ctrl+RMB resolver) treat the returned value as a
// whole-profile replacement.
func ApplyPreset(p MovementPreset) MovementProfile {
	switch p {
	case PresetCautious:
		return MovementProfile{Pace: PaceWalk, Stance: StanceCrouch, Posture: PostureStandard, PathStyle: PathStyleCoverSeek}
	case PresetRush:
		return MovementProfile{Pace: PaceRun, Stance: StanceStand, Posture: PostureStandard, PathStyle: PathStyleDirect}
	case PresetSprint:
		return MovementProfile{Pace: PaceSprint, Stance: StanceStand, Posture: PostureStandard, PathStyle: PathStyleDirect}
	case PresetStealth:
		return MovementProfile{Pace: PaceWalk, Stance: StanceCrouch, Posture: PostureQuiet, PathStyle: PathStyleRoadAvoid}
	case PresetProneCrawl:
		return MovementProfile{Pace: PaceWalk, Stance: StanceProne, Posture: PostureQuiet, PathStyle: PathStyleCoverSeek}
	default:
		return MovementProfile{Pace: PaceWalk, Stance: StanceStand, Posture: PostureStandard, PathStyle: PathStyleDirect}
	}
}

// PresetName returns the human label used by the Inspector chip row.
func PresetName(p MovementPreset) string {
	switch p {
	case PresetCautious:
		return "Cautious"
	case PresetRush:
		return "Rush"
	case PresetSprint:
		return "Sprint"
	case PresetStealth:
		return "Stealth"
	case PresetProneCrawl:
		return "Crawl"
	default:
		return "Default"
	}
}

// PaceName / PostureName / PathStyleName / StanceName return the human labels
// surfaced by the Inspector dropdowns (M13.6).
func PaceName(p Pace) string {
	switch p {
	case PaceRun:
		return "Run"
	case PaceSprint:
		return "Sprint"
	default:
		return "Walk"
	}
}

func PostureName(p Posture) string {
	if p == PostureQuiet {
		return "Quiet"
	}
	return "Standard"
}

func PathStyleName(s PathStyle) string {
	switch s {
	case PathStyleRoadPrefer:
		return "RoadPrefer"
	case PathStyleRoadAvoid:
		return "RoadAvoid"
	case PathStyleCoverSeek:
		return "CoverSeek"
	default:
		return "Direct"
	}
}

func StanceName(s StanceCode) string {
	switch s {
	case StanceCrouch:
		return "Crouch"
	case StanceProne:
		return "Prone"
	default:
		return "Stand"
	}
}
