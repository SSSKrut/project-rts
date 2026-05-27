package components

// SquadStateCode names the squad's tactical posture as decided by
// SurvivalInstinct's ScatterProtocol pass.
type SquadStateCode uint8

const (
	SquadStateIdle SquadStateCode = iota
	// SquadStateEngaged - the squad is taking or returning fire but has not
	// scrambled.
	SquadStateEngaged
	// SquadStateScrambling - a recent suppression spike has triggered cover
	// search across every live member; FormationSystem yields all slots until
	// the recovery window elapses.
	SquadStateScrambling
)

// SquadSuppressionWindow - number of samples held in SuppHistory. With a 250
// ms ScatterProtocol cadence this covers a 3 s rolling window.
const SquadSuppressionWindow = 12

// SquadState carries the suppression-history ring buffer and the current
// state-machine code.
//
// SuppHistory records the squad-average Suppression.Level captured at each
// ScatterProtocol tick. HistHead is the next write slot; HistCount grows up
// to SquadSuppressionWindow then stays clamped.
//
// LowDeltaSince is the session-time the squad's delta first dropped below
// the recovery threshold (0 while still above). ScramblingSince records when
// the marker was first set, used for the safety timeout.
type SquadState struct {
	Code            SquadStateCode
	SuppHistory     [SquadSuppressionWindow]float32
	HistHead        uint8
	HistCount       uint8
	ScramblingSince float32
	LowDeltaSince   float32
}
