package components

import rl "github.com/gen2brain/raylib-go/raylib"

type IndividualPositionMode uint8

const (
	// IndividualPosAbsolute - goal is the literal world coordinate stored in
	// AbsolutePos. Survives squad center motion.
	IndividualPosAbsolute IndividualPositionMode = iota
	// IndividualPosRelative - goal is squadCenter + RelativeOffset every tick.
	// Same intent as a formation slot but with a player-defined offset.
	IndividualPosRelative
)

// IndividualPosition replaces the formation slot for one squad member. Absent
// component = use FormationSystem's per-kind offset.
//
// FormationSystem checks per member: TacticalOverride wins (AI safety), then
// IndividualPosition, then formation slot. SquadService clears the marker via
// Alt+RMB explicit reset; otherwise it persists across new orders.
type IndividualPosition struct {
	Mode           IndividualPositionMode
	AbsolutePos    WorldPos
	RelativeOffset rl.Vector2
	AcquiredAt     float32
}
