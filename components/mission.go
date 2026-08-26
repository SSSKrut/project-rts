package components

// Mission is the scenario as data (block D P7): sides, points, forces, and one
// victory condition. There is no scripting — anything conditional is the bot's
// job, and triggers in JSON become a programming language by the third mission.
type Mission struct {
	Name       string
	TimeSec    float32
	HoldPoints uint8
	OfTotal    uint8
	ForSec     float32
	Loaded     bool
}

type MissionOutcome uint8

const (
	OutcomeNone MissionOutcome = iota
	OutcomePlayerWin
	OutcomePlayerLoss
	OutcomeDraw
)

var missionOutcomeLabel = [...]string{"running", "VICTORY", "DEFEAT", "DRAW"}

func (o MissionOutcome) String() string {
	if int(o) < len(missionOutcomeLabel) {
		return missionOutcomeLabel[o]
	}
	return "?"
}

// MissionState is the live scoreboard. It is deliberately all NUMBERS (P4):
// "why did I lose" has to be answerable from the same row the player was
// watching, and LinkedSec is the block-A join — a mission scores how well the
// net was kept, which is what makes comms a resource instead of a nuisance.
type MissionState struct {
	Elapsed   float32
	HoldSince [FactionCount]float32
	Held      [FactionCount]uint8
	Losses    [FactionCount]uint16
	LinkedSec float32
	Outcome   MissionOutcome
	EndedAt   float32
}

// Over reports that the mission has been decided. Nothing may advance the world
// after this (P5): a result you can keep playing is not a result.
func (s *MissionState) Over() bool { return s.Outcome != OutcomeNone }

// ForceKind is what a scheduled arrival puts on the ground.
type ForceKind uint8

const (
	ForceSquad ForceKind = iota
	ForceVehicle
)

// ForceArrival is the ground twin of AirArrival (P2): one mechanism for
// "something shows up at time T". Starting forces are not a separate concept —
// they are At = 0.
type ForceArrival struct {
	At       float32
	Side     uint8
	Ctrl     uint8
	Kind     ForceKind
	Template uint8
	Vehicle  VehicleKind
	Pos      WorldPos
}
