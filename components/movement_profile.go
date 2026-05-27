package components

// Pace is a movement-tempo dial that multiplies the base stance-speed table.
// Walk is the default; Sprint costs Stamina.
type Pace uint8

const (
	PaceWalk Pace = iota
	PaceRun
	PaceSprint
)

// PaceSpeedMul is the Pace -> speed multiplier. Applied after Stance:
// final = base * Stance * Pace.
var PaceSpeedMul = [...]float32{
	PaceWalk:   1.0,
	PaceRun:    1.6,
	PaceSprint: 2.2,
}

// PaceStaminaDrain is the per-second Stamina cost at each Pace.
var PaceStaminaDrain = [...]float32{
	PaceWalk:   0.0,
	PaceRun:    0.10,
	PaceSprint: 0.50,
}

// Posture is the audio / visual signature lever. Quiet reduces detection
// once the audio/vision modifier system reads it.
type Posture uint8

const (
	PostureStandard Posture = iota
	PostureQuiet
)

// PathStyle steers NavService.FindPath cell-cost modifiers. CoverSeek reads
// NavCell.CoverDistance for cover-proximity bias.
type PathStyle uint8

const (
	PathStyleDirect PathStyle = iota
	PathStyleRoadPrefer
	PathStyleRoadAvoid
	PathStyleCoverSeek
)

// MovementProfile is the per-Squad standing rule covering all four movement
// axes. UnitMovementSystem reads it via SquadMember back-pointer. Override
// per-order via OrderParamMovementProfile.
//
// Stance is the resting posture to return to once any ActionStance from
// ActionQueue completes.
type MovementProfile struct {
	Pace      Pace
	Stance    StanceCode
	Posture   Posture
	PathStyle PathStyle
}

type MovementPreset uint8

const (
	PresetDefault MovementPreset = iota
	PresetCautious
	PresetRush
	PresetSprint
	PresetStealth
	PresetProneCrawl
)

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
