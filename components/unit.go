package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// Unit marks an infantry entity. Filter-target only — all gameplay-relevant
// state lives in adjacent components (Stance, Motion, Vision, ActionQueue, …).
// Composition over a monolithic Unit struct: DOD (different systems read
// different subsets), and Phase 10 / 11 modifiers slot in without touching
// existing readers.
type Unit struct{}

// StanceCode is a typed posture identifier. Three values now (Stand, Crouch,
// Prone); when Phase 10 wires modifiers (cover, suppressed, aiming) the field
// becomes Stance.Posture with a sibling Modifiers bitmask — rename only, no
// architectural change. See PHASE-7.md P5.
type StanceCode uint8

const (
	StanceStand StanceCode = iota
	StanceCrouch
	StanceProne
)

// Stance — current posture. Read by UnitMovementSystem (speed table) and
// renderer (cube height).
type Stance struct {
	Code StanceCode
}

// Motion — current facing & speed. Yaw is the unit's forward radians around
// +Y, written by UnitMovementSystem each tick. Speed is |velocity| in m/s.
type Motion struct {
	Yaw   float32
	Speed float32
}

// Collider — XZ radius for separation steering and selection raycasts. Height
// comes from the Stance table at read time.
type Collider struct {
	Radius float32
}

// Vision — sight cone. RangeM is hard-capped at chunkSize (64 m) in Phase 7;
// AngleDot = cos(half-FOV) for cheap dot-product compares.
type Vision struct {
	RangeM   float32
	AngleDot float32
}

// Suppression — incoming-fire stress. ThreatDir points toward the source of
// pressure (last bullet impact). Written by Phase 11; read by Phase 10
// SurvivalInstinct. Phase 7 leaves both at zero.
type Suppression struct {
	Level     float32
	ThreatDir rl.Vector3
}

// AwarenessSlots is the fixed-size memory of recently-seen targets per unit.
// 8 is enough for Phase 7 visualisation; Phase 10 may expand once tactical AI
// actually consumes the buffer.
const AwarenessSlots = 8

// AwarenessEntry — one sighting record. Time = session-time seconds at last
// sighting; Time == 0 means the slot is empty.
type AwarenessEntry struct {
	Target ecs.Entity
	Pos    WorldPos
	Time   float32
}

// Awareness — FIFO of recent sightings. Fixed array, no slice (DOD).
type Awareness struct {
	LastSeen [AwarenessSlots]AwarenessEntry
}

// LocalBlackboard — scratchpad for Phase 10 tactical AI. Empty in Phase 7;
// the component exists so the unit archetype is stable from spawn.
type LocalBlackboard struct {
	_ struct{}
}

// ActionKind discriminates ActionQueue entries.
type ActionKind uint8

const (
	ActionNone ActionKind = iota
	ActionMoveTo
	ActionStop
	ActionStance
)

// Action — one queued micro-order. Target re-used by Kind: MoveTo uses XZ;
// Stance encodes the new StanceCode in Target.Y.
type Action struct {
	Kind   ActionKind
	Target WorldPos
}

// ActionQueueSize — max queued actions per unit. 4 is plenty for Phase 7
// (RMB clear+push or chained Shift+RMB waypoints); Phase 12 rewrites the
// queue around Order entities anyway.
const ActionQueueSize = 4

// ActionQueue — fixed-size ring buffer. Head == Tail ⇒ empty. Wrap on
// (Head+1)%ActionQueueSize. When full, callers drop the oldest entry by
// advancing Head (or refuse the push, depending on intent).
type ActionQueue struct {
	Actions [ActionQueueSize]Action
	Head    uint8
	Tail    uint8
	Count   uint8
	// StopUntil — session-time (seconds) the current Stop action holds the
	// unit in place until. Used only when Actions[Head].Kind == ActionStop.
	StopUntil float32
}

// Equipment — ID references to weapon / grenade / radio entities, never
// arrays inside the component (DOD). 0 = empty slot.
type Equipment struct {
	Primary   ecs.Entity
	Secondary ecs.Entity
	Active    ecs.Entity
}

// OwnedBy — pointer back to the owning entity. Lives on weapon / grenade /
// radio sub-entities. Phase 11 ballistic resolver reads this to attribute
// kills.
type OwnedBy struct {
	Owner ecs.Entity
}
