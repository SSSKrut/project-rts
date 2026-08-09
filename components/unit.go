package components

import (
	"github.com/mlange-42/ark/ecs"
)

// Unit marks an infantry entity. All state lives in adjacent components
// (Stance, Motion, Sensors, ActionQueue, ...) so different systems read
// disjoint subsets.
type Unit struct{}

type StanceCode uint8

const (
	StanceStand StanceCode = iota
	StanceCrouch
	StanceProne
)

// Stance carries the unit's current posture + an animation lock window.
// LockUntil is session-time (matches squadService.Clock); writers set it to
// "now + animLock" so the next autonomous flipper can't snap-spin between
// bands.
type Stance struct {
	Code      StanceCode
	LockUntil float32
}

// StanceOverride - player-set stance lock. While Until is in the future,
// StanceControllerSystem skips the unit (the player meant a manual stance,
// leave it alone).
type StanceOverride struct {
	Until float32
}

// Motion - current facing & speed. Yaw is the facing-yaw (radians around +Y),
// what renderers and the contact detect pass read. Speed is |velocity| in m/s.
//
// VelocityYaw is the direction of motion this frame, computed from the move
// delta in UnitMovementSystem. Separated from Yaw so combat-move can decouple
// body facing from path direction - under fire a unit looks at the threat
// while sidestepping along VelocityYaw.
//
// Both yaws: 0 = +Z (north), increasing clockwise around +Y.
type Motion struct {
	Yaw          float32 // facing yaw - drives render + sensor cone
	VelocityYaw  float32 // direction of last frame's motion
	Speed        float32
}

// Collider - XZ radius for separation steering and selection raycasts.
// Height comes from the Stance table at read time.
type Collider struct {
	Radius float32
}

const AwarenessSlots = 8

// AwareDirect: the unit saw/heard the target itself; clear = a squadmate
// shared it. Weapons fire at Direct entries; Shared needs an own-LOS confirm.
const AwareDirect uint8 = 1

// AwarenessEntry - one sighting record. Time == 0 means the slot is empty.
type AwarenessEntry struct {
	Target ecs.Entity
	Pos    WorldPos
	Time   float32
	Flags  uint8
}

// Awareness - FIFO of recent sightings. Fixed array, no slice (DOD).
type Awareness struct {
	LastSeen [AwarenessSlots]AwarenessEntry
}

// LocalBlackboard — per-unit scratchpad for tactical AI.
//
// Layout choices:
//   - Reason is a fixed-size byte buffer (32 bytes) so SetReason doesn't
//     allocate per tick. ReasonLen tracks how many bytes are valid.
//   - PrefVelocityX/Z carry the velocity MicroPath / FormationSystem want
//     for this tick; ORCA reads them as input, writes the adjusted velocity
//     back into Motion (the blackboard stays advisory).
//   - AssignedCover / AssignedTarget link to cover-slot / enemy-unit
//     entities chosen by SurvivalInstinct / WeaponSystem.
//   - GoalSlot is the formation-anchored target the unit should pathfind to.
//
// Mode transitions are gated by hysteresis — UtilityEvaluator checks
// `now - LastModeSwitch >= MinModeDuration` AND `newScore >
// currentScore + Delta` before flipping CurrentMode.
type LocalBlackboard struct {
	CurrentMode    ActionMode
	LastModeSwitch float32 // session-time seconds
	Reason         [32]byte
	ReasonLen      uint8

	AssignedCover  ecs.Entity
	AssignedTarget ecs.Entity

	GoalSlot  WorldPos
	GoalLevel ecs.Entity // non-zero when GoalSlot lies on a Level entity (interior pathing)
	// CatchUp: the member lags its slot; UnitMovement raises effective Pace
	// one tier until the slot is regained (FormationSystem hysteresis).
	CatchUp bool

	PrefVelocityX float32 // pre-ORCA preferred velocity (XZ plane)
	PrefVelocityZ float32

	// Counters for replan triggers.
	OvercrowdedSince float32 // accumulates while ORCA reports no feasible solution
	StuckSince       float32 // accumulates while velocity is low but pref non-zero

	// YieldPressure: seconds a standing man has been pressed by another body.
	// Past the threshold he makes way — a man parked in the only doorway
	// blocks the storey outright, since nav cannot see bodies and there is no
	// route around a 1.2 m opening.
	YieldPressure float32

	// HeldSlot: the interior slot this man currently owns, stored as index+1
	// so the zero value reads as "none" without any factory init. Assignment
	// is recomputed every formation pass; the hold bonus keyed off this field
	// is what stops a squad reshuffling every position when one man dies.
	HeldSlot uint8
}

// HeldSlotIndex returns the owned slot index and whether one is held.
func (b *LocalBlackboard) HeldSlotIndex() (uint8, bool) {
	if b.HeldSlot == 0 {
		return 0, false
	}
	return b.HeldSlot - 1, true
}

func (b *LocalBlackboard) SetHeldSlot(i uint8) { b.HeldSlot = i + 1 }

func (b *LocalBlackboard) ClearHeldSlot() { b.HeldSlot = 0 }

// SetReason copies `text` into Blackboard.Reason without allocating. Up to
// 32 bytes; longer text is truncated.
func (b *LocalBlackboard) SetReason(text string) {
	n := len(text)
	if n > len(b.Reason) {
		n = len(b.Reason)
	}
	copy(b.Reason[:n], text)
	b.ReasonLen = uint8(n)
}

// ReasonString returns the valid portion of the Reason buffer as a string.
// Allocates only when called (UI side, not hot path).
func (b *LocalBlackboard) ReasonString() string {
	return string(b.Reason[:b.ReasonLen])
}

// ActionMode is the Utility evaluator's per-unit decision. UtilityEvaluatorSystem
// picks the highest-scoring mode every 0.5 s with hysteresis; executor
// systems (MicroPath, StanceController, WeaponSystem) read CurrentMode to
// pick mode-appropriate behavior.
type ActionMode uint8

const (
	// ModeFollowing — default: pathfind to formation slot / squad waypoint.
	// No active threat or threat too low to override movement intent.
	ModeFollowing ActionMode = iota
	// ModeEngaging — visible hostile in range, RoE permits fire.
	// Movement constrained, weapon active.
	ModeEngaging
	// ModeTakingCover — under fire or high threat; seek and hold cover slot.
	// Planned response (SurvivalInstinct cover-pick), distinct from Suppressed.
	ModeTakingCover
	// ModeRepositioning — relocate to alternative cover slot after firing /
	// cover lost. Brief mode, transitions back into Engaging or TakingCover.
	ModeRepositioning
	// ModeReloading — out of ammo or low; stay in cover, no fire.
	ModeReloading
	// ModeSuppressed — reactive collapse under heavy fire; forced Prone, no
	// movement, no fire. Distinct from TakingCover (planned) — Suppressed is
	// the AI losing initiative.
	ModeSuppressed
	ModeTreating
	ModeMounted
	ModeCount
)

// UtilitySpec is the per-mode metadata row.
type UtilitySpec struct {
	Mode    ActionMode
	Name    string
	Enabled bool
}

var UtilitySpecs = [ModeCount]UtilitySpec{
	ModeFollowing:     {Mode: ModeFollowing, Name: "Following", Enabled: true},
	ModeEngaging:      {Mode: ModeEngaging, Name: "Engaging", Enabled: true},
	ModeTakingCover:   {Mode: ModeTakingCover, Name: "Taking cover", Enabled: true},
	ModeRepositioning: {Mode: ModeRepositioning, Name: "Repositioning", Enabled: true},
	ModeReloading:     {Mode: ModeReloading, Name: "Reloading", Enabled: true},
	ModeSuppressed:    {Mode: ModeSuppressed, Name: "Suppressed", Enabled: true},
	// Treating / Mounted: declared so the table is exhaustive but Enabled=false
	// — UtilityEvaluator skips disabled modes when scoring.
	ModeTreating: {Mode: ModeTreating, Name: "Treating", Enabled: false},
	ModeMounted:  {Mode: ModeMounted, Name: "Mounted", Enabled: false},
}

// Compile-time guard: forgotten enum value = build break here.
var _ = [ModeCount]UtilitySpec(UtilitySpecs)

// UtilityBucketCount — distribute Utility evaluation across N frames so the
// per-tick cost is ~1/N of the all-units case. At 60 fps with N=30, every
// unit re-evaluates once per 0.5 s on average.
const UtilityBucketCount uint32 = 30

// ModeSwitchScoreDelta — hysteresis threshold. New mode must beat current
// mode's score by this much before switching. Prevents per-tick flapping
// between nearly-tied modes.
const ModeSwitchScoreDelta float32 = 0.15

// MinModeDurationSec — anti-flap floor. Once a mode is entered, the unit
// stays in it at least this long before a switch is allowed (except when
// the new score wildly exceeds the old — see UtilityEmergencyDelta below).
const MinModeDurationSec float32 = 1.0

// UtilityEmergencyDelta — if the new score beats the current by more than
// this, MinModeDurationSec is bypassed. Lets a unit slam into Suppressed /
// TakingCover the instant a heavy danger event hits without waiting out
// the dwell timer.
const UtilityEmergencyDelta float32 = 0.6

// ModeName returns the human-readable mode label. Falls back to "?" if the
// code drifts out of bounds.
func ModeName(m ActionMode) string {
	if int(m) >= len(UtilitySpecs) {
		return "?"
	}
	return UtilitySpecs[m].Name
}

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
