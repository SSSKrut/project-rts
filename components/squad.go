package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// Squad is a marker on an abstract squad entity (PHASE-9.md P1). The squad
// lives without a WorldPos — its center is computed on the fly from member
// positions by FormationSystem and SquadMacroPathSystem. Squad-entity
// archetype: Squad + CommandRoster + FormationData + MacroPath + RadioNetwork
// + AlwaysActive.
type Squad struct{}

// SquadRosterSize is the maximum members per squad. 8 fits Cold War squad
// sizes (Soviet 6-8, NATO fireteam pair 2×4). Widening is a one-line array
// change, no architectural shift.
const SquadRosterSize = 8

// CommandRoster — fixed-size unit list, compacted (Leave shifts the tail).
// Members[0..Count-1] is always the live set; Members[i] for i >= Count is
// zero. Slot 0 is the commander; the slot index also keys the formation
// offset, so commander always sits at offset(0).
type CommandRoster struct {
	Members [SquadRosterSize]ecs.Entity
	Count   uint8
}

// FormationKind picks the offset function applied by FormationSystem.
type FormationKind uint8

const (
	FormationLine   FormationKind = iota // perpendicular shoulder-to-shoulder
	FormationColumn                      // single file along Forward
	FormationWedge                       // V, commander at the tip
	FormationLoose                       // hashed scatter inside a radius
)

// FormationData — current formation and pacing. Forward is the unit XZ-vector
// of the squad's facing, written by SquadMacroPathSystem from the segment
// (prev → next waypoint). Spacing is metres between adjacent slots; default
// per kind is in systems.formationSpacing.
type FormationData struct {
	Type    FormationKind
	Forward rl.Vector3
	Spacing float32
}

// SquadMacroPathSize bounds the waypoint count of one macro path. After
// decimation (PHASE-9.md P5) a chunk-wide order (~64 m / 6-8 m steps) lands
// around 8 points; if the path is longer the squad will replan after the last
// waypoint.
const SquadMacroPathSize = 8

// MacroPath — current macro route of the squad center. Waypoints[Head] is the
// next target; Head < Count. HasGoal=false ⇒ idle (FormationSystem stops
// writing ActionQueue). ReplanAt is session-time seconds; setting it to 0
// forces a replan on the next SquadMacroPathSystem pass — the way
// OrderMoveTo triggers an immediate plan.
type MacroPath struct {
	Waypoints   [SquadMacroPathSize]WorldPos
	Head        uint8
	Count       uint8
	Goal        WorldPos
	HasGoal     bool
	ReplanAt    float32
	LastPlanned float32
}

// RadioNetwork — structural scaffold for Phase 12 (PHASE-9.md M9.6). No
// Phase 9 system reads it; CreateFromUnits populates it once with placeholder
// data (HasRadioman is always false until Radio sub-entities exist).
type RadioNetwork struct {
	Frequency   uint8
	HasRadioman bool
	HQReachable bool
}

// SquadMember — back-reference on each rostered unit (PHASE-9.md P2). Mirror
// of BuildingMember. Invariant: for every SquadMember{Squad: s, SlotIndex: i}
// on entity u, world.Get(s, CommandRoster).Members[i] == u. Maintained by
// SquadService — never edit by hand.
type SquadMember struct {
	Squad     ecs.Entity
	SlotIndex uint8
}
