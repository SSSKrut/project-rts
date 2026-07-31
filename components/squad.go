package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// Squad is a marker on an abstract squad entity. The squad lives without a
// WorldPos - its center is computed on the fly from member positions.
// Archetype: Squad + CommandRoster + FormationData + MacroPath +
// RadioNetwork + AlwaysActive.
type Squad struct{}

// SquadRosterSize is the maximum members per squad. 8 fits Cold War squad
// sizes (Soviet 6-8, NATO fireteam pair 2x4).
const SquadRosterSize = 8

// CommandRoster - fixed-size unit list, compacted (Leave shifts the tail).
// Members[0..Count-1] is always the live set; Members[i] for i >= Count is
// zero. Slot 0 is the commander; the slot index also keys the formation
// offset.
type CommandRoster struct {
	Members [SquadRosterSize]ecs.Entity
	Count   uint8
}

type FormationKind uint8

const (
	FormationLine   FormationKind = iota // perpendicular shoulder-to-shoulder
	FormationColumn                      // single file along Forward
	FormationWedge                       // V, commander at the tip
	FormationLoose                       // hashed scatter inside a radius
)

// FormationData - current formation and pacing. Forward is the unit XZ-vector
// of the squad's facing, written by SquadMacroPathSystem from the segment
// (prev -> next waypoint). Spacing is metres between adjacent slots.
type FormationData struct {
	Type    FormationKind
	Forward rl.Vector3
	Spacing float32
	// ReformPending: the player edited the layout (editor slot drag / kind /
	// preset). An idle squad re-forms in place around the leader; a marching
	// squad picks the new slots up on the fly and clears the flag.
	ReformPending bool
}

// SquadMacroPathSize bounds the waypoint count of one macro path. After
// decimation a chunk-wide order (~64 m / 6-8 m steps) lands around 8 points;
// longer paths replan after the last waypoint.
const SquadMacroPathSize = 8

// MacroPath - current macro route of the squad center. Waypoints[Head] is
// the next target. HasGoal=false => idle. Setting ReplanAt to 0 forces a
// replan on the next SquadMacroPathSystem pass.
//
// WaitingForStragglers / StragglerCaughtUp / StragglerTotal track the
// wait-for-stragglers gate: when the roster spread > 2*Spacing,
// FormationSystem freezes Head advance and SquadMacroPath honours the flag.
type MacroPath struct {
	Waypoints            [SquadMacroPathSize]WorldPos
	Head                 uint8
	Count                uint8
	Goal                 WorldPos
	HasGoal              bool
	ReplanAt             float32
	LastPlanned          float32
	WaitingForStragglers bool
	StragglerCaughtUp    uint8
	StragglerTotal       uint8
}

// RadioNetwork - structural scaffold. Real readers land in the Comms phase.
type RadioNetwork struct {
	Frequency   uint8
	HasRadioman bool
	HQReachable bool
}

// SquadMember - back-reference on each rostered unit. Invariant: for every
// SquadMember{Squad: s, SlotIndex: i} on entity u,
// world.Get(s, CommandRoster).Members[i] == u. Maintained by SquadService.
type SquadMember struct {
	Squad     ecs.Entity
	SlotIndex uint8
}

// FormationOrientationMode picks how a formation aligns to the world.
//   - OrientMovement (default): forward = squad's MacroPath heading, so the
//     formation rotates with motion (line keeps perpendicular to march).
//   - OrientNorth: forward locked to +Z, so slots stay relative to compass
//     north regardless of where the squad is heading.
type FormationOrientationMode uint8

const (
	OrientMovement FormationOrientationMode = iota
	OrientNorth
)

// FormationOrientation lives on a Squad. Absence == OrientMovement (default).
type FormationOrientation struct {
	Mode FormationOrientationMode
}

// FormationPreset is one named layout the user has saved from the
// formation editor: a Count-prefixed snapshot of CustomSlots that can be
// applied to any other squad. Storage convention matches CustomSlots —
// X = right of forward, Y = along forward, metres.
type FormationPreset struct {
	Name  string
	Count uint8
	Slots [SquadRosterSize]rl.Vector2
}

// FormationPresets is the singleton ECS resource holding every saved
// preset. Created at startup; appended to by the editor's "Save current"
// button; consulted by the editor's dropdown.
type FormationPresets struct {
	List []FormationPreset
}

// FormationCustomSlots holds per-slot offsets in the squad's local frame:
// X axis = right of forward (positive = right), Y axis = forward
// (positive = ahead of commander, negative = behind). When present on a
// Squad, FormationSystem reads slot offsets from here instead of the
// kind-based FormationOffset table.
//
// Slot 0 (commander) always sits at (0,0).
type FormationCustomSlots struct {
	Slots [SquadRosterSize]rl.Vector2
}
