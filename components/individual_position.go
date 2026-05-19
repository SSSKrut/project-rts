package components

import rl "github.com/gen2brain/raylib-go/raylib"

// IndividualPositionMode picks how IndividualPosition.Target is resolved each
// formation tick.
type IndividualPositionMode uint8

const (
	// IndividualPosAbsolute - goal is the literal world coordinate stored in
	// AbsolutePos. Survives squad center motion; player commit per-unit when
	// they want a specific spot (behind that tree).
	IndividualPosAbsolute IndividualPositionMode = iota
	// IndividualPosRelative - goal is squadCenter + RelativeOffset every tick.
	// Same intent as a formation slot but with a player-defined offset rather
	// than the FormationKind grid.
	IndividualPosRelative
)

// IndividualPosition replaces the formation slot for one squad member. Absent
// component = use FormationSystem's per-kind offset (default Phase 9 path).
//
// FormationSystem checks the marker per member: TacticalOverride wins (AI
// safety), then IndividualPosition, then formation slot. SquadService clears
// the marker via Alt+RMB explicit reset (Phase 15.B.2); otherwise it persists
// across new orders so a unit placed behind cover stays there when the squad
// gets a new MoveTo.
type IndividualPosition struct {
	Mode           IndividualPositionMode
	AbsolutePos    WorldPos
	RelativeOffset rl.Vector2
	AcquiredAt     float32
}
