package components

import (
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

// Stance carries the unit's current posture + an animation lock window.
// LockUntil is session-time (matches squadService.Clock); writers (player
// command, StanceControllerSystem) set it to "now + animLock" so the next
// autonomous flipper can't snap-spin between bands.
type Stance struct {
	Code      StanceCode
	LockUntil float32
}

// StanceOverride - player-set stance lock. While Until is in the future,
// StanceControllerSystem skips the unit (the player meant Z/X/C / pie-menu
// stance, leave it alone). Phase 21 hot-keys will write this marker; Phase 17
// only defines the type so the controller's gate is in place.
type StanceOverride struct {
	Until float32
}

// Motion - current facing & speed. Yaw is the facing-yaw (radians around +Y),
// what renderers and Vision read. Speed is |velocity| in m/s.
//
// Phase 17 M17.B.4: VelocityYaw is the direction of motion this frame (the
// "where I'm walking"), computed from the move delta in UnitMovementSystem.
// It's separated from Yaw so combat-move can decouple body facing from path
// direction - under fire a unit looks at the threat while sidestepping along
// VelocityYaw, instead of snapping the body to whatever the path tangent is.
//
// Both yaws follow the same convention as Motion.Yaw did pre-split:
// 0 = +Z (north), increasing clockwise around +Y.
type Motion struct {
	Yaw          float32 // facing yaw - drives render + Vision cone
	VelocityYaw  float32 // direction of last frame's motion
	Speed        float32
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

// LocalBlackboard — per-unit scratchpad for tactical AI. Phase 17.8 M17.8.1
// gave it real fields after Phase 7 left it as a placeholder.
//
// Layout choices:
//   - Reason is a fixed-size byte buffer (32 bytes) so SetReason doesn't
//     allocate per tick. ReasonLen tracks how many bytes are valid.
//   - PrefVelocityX/Z carry the velocity MicroPath / FormationSystem want
//     for this tick; ORCA reads them as input, writes the adjusted velocity
//     back into Motion (the blackboard stays advisory).
//   - AssignedCover / AssignedTarget link to cover-slot / enemy-unit
//     entities chosen by SurvivalInstinct / WeaponSystem. UtilityEvaluator
//     reads them to score mode candidates.
//   - GoalSlot is the formation-anchored target the unit should pathfind
//     to. M17.8.4: written by FormationSystem (slot-aware MicroPath path).
//
// Mode transitions are gated by hysteresis — UtilityEvaluator (M17.8.2)
// checks `now - LastModeSwitch >= MinModeDuration` AND `newScore >
// currentScore + Delta` before flipping CurrentMode.
type LocalBlackboard struct {
	CurrentMode    ActionMode
	LastModeSwitch float32 // session-time seconds
	Reason         [32]byte
	ReasonLen      uint8

	AssignedCover  ecs.Entity
	AssignedTarget ecs.Entity

	GoalSlot   WorldPos
	GoalLevel  ecs.Entity // non-zero when GoalSlot lies on a Level entity (interior pathing)

	PrefVelocityX float32 // pre-ORCA preferred velocity (XZ plane)
	PrefVelocityZ float32

	// Counters for replan triggers (M17.8.6).
	OvercrowdedSince float32 // accumulates while ORCA reports no feasible solution
	StuckSince       float32 // accumulates while velocity is low but pref non-zero
}

// SetReason copies `text` into Blackboard.Reason without allocating. Up to
// 32 bytes; longer text is truncated. Phase 17.8 M17.8.1 — Inspector
// reason row reads this slice.
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
// (M17.8.2) picks the highest-scoring mode every 0.5 s with hysteresis;
// executor systems (MicroPath, StanceController, WeaponSystem) read
// CurrentMode to pick mode-appropriate behavior.
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
	// ModeTreating, ModeMounted — placeholders for Phase 19 / 21 expansion.
	ModeTreating
	ModeMounted
	// ModeCount — compile-time bound for the spec table.
	ModeCount
)

// UtilitySpec is the per-mode metadata row. Phase 17.8 M17.8.1 ships
// placeholder rows with Name only; Score functions land in M17.8.2.
// Spec-table pattern matches OrderKindSpecs (Phase 14.5).
type UtilitySpec struct {
	Mode    ActionMode
	Name    string
	Enabled bool
}

// UtilitySpecs — canonical metadata table. Index by ActionMode. Adding a
// new mode = bump ModeCount + append a row. Compile-time guard below.
var UtilitySpecs = [ModeCount]UtilitySpec{
	ModeFollowing:     {Mode: ModeFollowing, Name: "Following", Enabled: true},
	ModeEngaging:      {Mode: ModeEngaging, Name: "Engaging", Enabled: true},
	ModeTakingCover:   {Mode: ModeTakingCover, Name: "Taking cover", Enabled: true},
	ModeRepositioning: {Mode: ModeRepositioning, Name: "Repositioning", Enabled: true},
	ModeReloading:     {Mode: ModeReloading, Name: "Reloading", Enabled: true},
	ModeSuppressed:    {Mode: ModeSuppressed, Name: "Suppressed", Enabled: true},
	// Treating / Mounted: declared so the table is exhaustive but Enabled=false
	// — UtilityEvaluator skips disabled modes when scoring. Phase 19 / 21 flip
	// them on with real Score functions + executor wiring.
	ModeTreating: {Mode: ModeTreating, Name: "Treating", Enabled: false},
	ModeMounted:  {Mode: ModeMounted, Name: "Mounted", Enabled: false},
}

// Compile-time guard: forgotten enum value = build break here.
var _ = [ModeCount]UtilitySpec(UtilitySpecs)

// UtilityBucketCount — distribute Utility evaluation across N frames so the
// per-tick cost is ~1/N of the all-units case. At 60 fps with N=30, every
// unit re-evaluates once per 0.5 s on average. Match UtilityEvaluatorSystem
// bucket gate in systems/utility_evaluator.go.
const UtilityBucketCount uint32 = 30

// ModeSwitchScoreDelta — hysteresis threshold. New mode must beat current
// mode's score by this much before switching. Prevents per-tick flapping
// between nearly-tied modes (e.g. Following vs TakingCover at threat=0.4).
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

// ModeName returns the human-readable mode label for Inspector / debug
// overlays. Falls back to "?" if the code drifts out of bounds.
func ModeName(m ActionMode) string {
	if int(m) >= len(UtilitySpecs) {
		return "?"
	}
	return UtilitySpecs[m].Name
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
