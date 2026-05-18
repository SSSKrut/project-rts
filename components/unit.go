package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// Unit marks an infantry entity. All state lives in adjacent components
// (Stance, Motion, Vision, ActionQueue, ...) so different systems read
// disjoint subsets.
type Unit struct{}

// StanceCode is a typed posture identifier. Modifiers (cover, suppressed,
// aiming) will land as a sibling bitmask later.
type StanceCode uint8

const (
	StanceStand StanceCode = iota
	StanceCrouch
	StanceProne
)

type Stance struct {
	Code StanceCode
}

// Motion - current facing & speed. Yaw is forward radians around +Y. Speed
// is |velocity| in m/s.
type Motion struct {
	Yaw   float32
	Speed float32
}

// Collider - XZ radius for separation steering and selection raycasts.
// Height comes from the Stance table at read time.
type Collider struct {
	Radius float32
}

// Vision - sight cone. AngleDot = cos(half-FOV) for cheap dot-product compares.
type Vision struct {
	RangeM   float32
	AngleDot float32
}

// Suppression - incoming-fire stress. ThreatDir points toward the source of
// pressure (last bullet impact).
type Suppression struct {
	Level     float32
	ThreatDir rl.Vector3
}

// AwarenessSlots is the fixed-size memory of recently-seen targets per unit.
const AwarenessSlots = 8

// AwarenessEntry - one sighting record. Time == 0 means the slot is empty.
type AwarenessEntry struct {
	Target ecs.Entity
	Pos    WorldPos
	Time   float32
}

// Awareness - FIFO of recent sightings. Fixed array, no slice (DOD).
type Awareness struct {
	LastSeen [AwarenessSlots]AwarenessEntry
}

// LocalBlackboard - scratchpad for tactical AI. Empty marker; the component
// exists so the unit archetype stays stable from spawn.
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

// Action - one queued micro-order. Target re-used by Kind: MoveTo uses XZ;
// Stance encodes the new StanceCode in Target.Y.
type Action struct {
	Kind   ActionKind
	Target WorldPos
}

const ActionQueueSize = 4

// ActionQueue - fixed-size ring buffer. Head == Tail => empty. When full,
// callers drop the oldest entry by advancing Head.
type ActionQueue struct {
	Actions [ActionQueueSize]Action
	Head    uint8
	Tail    uint8
	Count   uint8
	// StopUntil - session-time the current Stop action holds the unit in
	// place until. Used only when Actions[Head].Kind == ActionStop.
	StopUntil float32
}

// Equipment - ID references to weapon / grenade / radio entities. No arrays
// inside (DOD). 0 = empty slot.
type Equipment struct {
	Primary   ecs.Entity
	Secondary ecs.Entity
	Active    ecs.Entity
}

// OwnedBy - pointer back to the owning entity. Lives on weapon / grenade /
// radio sub-entities. Read by the ballistic resolver to attribute kills.
type OwnedBy struct {
	Owner ecs.Entity
}
