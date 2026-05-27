package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// ThreatState is the discretised band of Threat.Total used by reactive AI
// (cover/stance choice, perception factor, reaction delay).
type ThreatState uint8

const (
	ThreatSafe       ThreatState = iota // Total < ThreatVigilantThreshold
	ThreatVigilant                      // [Vigilant, Alerted)
	ThreatAlerted                       // [Alerted, Threatened)
	ThreatThreatened                    // >= Threatened
)

// Threshold table indexed by destination state. ThreatSystem reads these to
// classify Total each tick.
const (
	ThreatVigilantThreshold   float32 = 0.05
	ThreatAlertedThreshold    float32 = 0.33
	ThreatThreatenedThreshold float32 = 0.66
)

// Threat is the per-unit aggregate of incoming danger signals. Suppression
// survives as one of the four contributing channels, alongside ShotsFired
// (far gunfire), Endangered (someone aiming at me) and Injury (HP/bleeding).
// Total is recomputed each tick by ThreatSystem from those channels.
//
// ThreatDir is the unit-length world XZ vector pointing from the dominant
// threat source toward this unit. Reactive AI reads it for cover selection
// and combat-move facing.
//
// Writers: producers push typed DangerEvent into the unit's DangerBuffer;
// ThreatSystem drains the buffer each tick, accumulates per-channel
// contribs (with decay), aggregates ThreatDir as a recency-weighted
// average, recomputes Total + State.
type Threat struct {
	// Total is the [0,1] aggregate. ThreatSystem sets it.
	Total float32

	// Suppression - direct-fire pressure (bullets landing close).
	Suppression float32

	// ShotsFired - far/indirect gunfire heard but not impacting.
	ShotsFired float32

	// Endangered - someone is currently aiming at me.
	Endangered float32

	// Injury - bleeding / wounded contribution.
	Injury float32

	// ThreatDir - unit-length world XZ vector from the dominant threat
	// source toward the unit.
	ThreatDir rl.Vector3

	State ThreatState
}

// ClassifyThreat maps a Total in [0,1] to a ThreatState band.
func ClassifyThreat(total float32) ThreatState {
	switch {
	case total >= ThreatThreatenedThreshold:
		return ThreatThreatened
	case total >= ThreatAlertedThreshold:
		return ThreatAlerted
	case total >= ThreatVigilantThreshold:
		return ThreatVigilant
	}
	return ThreatSafe
}

type DangerKind uint8

const (
	DangerGunshot       DangerKind = iota // distant gunfire heard
	DangerBulletImpact                    // bullet landed nearby
	DangerExplosion                       // boom in radius
	DangerGrenadeLanding                  // grenade visible on ground
	DangerUnknownFire                     // shots audible, source unknown
	DangerMeleeHit                        // knife / bayonet hit
	DangerDamageTaken                     // HP loss inflicted on me
	DangerBleeding                        // active bleeding tick
	DangerVehicleHorn                     // vehicle horn nearby
	DangerUnsafeArea                      // marked zone (artillery)
	dangerKindCount
)

// DangerEvent is one typed signal pushed into a unit's DangerBuffer.
// ThreatSystem drains the buffer each tick.
type DangerEvent struct {
	Kind     DangerKind
	Source   ecs.Entity
	Pos      WorldPos
	Strength float32 // amplitude: per-kind, e.g. hitMul*falloff for impacts
	Time     float32
}

// DangerBufferSize - ring capacity. Budgets 8 events per tick per unit;
// bursts above that overwrite oldest (FIFO eviction).
const DangerBufferSize = 8

// DangerBuffer is a per-unit fixed-size ring of recently observed
// DangerEvents. ThreatSystem drains the buffer every tick (Count=0 after
// read), so the "ring" really only matters when multiple producers push in
// the same serial post-pass.
type DangerBuffer struct {
	Events [DangerBufferSize]DangerEvent
	Head   uint8
	Count  uint8
}

// PushDanger appends an event to the buffer. When full, overwrites the oldest
// entry (Head wraps; Count stays at the cap).
func PushDanger(buf *DangerBuffer, ev DangerEvent) {
	buf.Events[buf.Head] = ev
	buf.Head = (buf.Head + 1) % DangerBufferSize
	if buf.Count < DangerBufferSize {
		buf.Count++
	}
}

// ThreatChannel names which Threat float a DangerEvent contributes to.
// Decay is per-channel; event routing is per-DangerKind.
type ThreatChannel uint8

const (
	ThreatChannelNone ThreatChannel = iota
	ThreatChannelSuppression
	ThreatChannelShotsFired
	ThreatChannelEndangered
	ThreatChannelInjury
	threatChannelCount
)

type dangerSpec struct {
	Channel ThreatChannel
}

var dangerSpecs = [dangerKindCount]dangerSpec{
	DangerGunshot:        {Channel: ThreatChannelShotsFired},
	DangerBulletImpact:   {Channel: ThreatChannelSuppression},
	DangerExplosion:      {Channel: ThreatChannelSuppression},
	DangerGrenadeLanding: {Channel: ThreatChannelEndangered},
	DangerUnknownFire:    {Channel: ThreatChannelShotsFired},
	DangerMeleeHit:       {Channel: ThreatChannelSuppression},
	DangerDamageTaken:    {Channel: ThreatChannelInjury},
	DangerBleeding:       {Channel: ThreatChannelInjury},
	DangerVehicleHorn:    {Channel: ThreatChannelShotsFired},
	DangerUnsafeArea:     {Channel: ThreatChannelEndangered},
}

// ChannelDecayRates - per-second linear drop applied to each channel by
// ThreatSystem.
var ChannelDecayRates = [threatChannelCount]float32{
	ThreatChannelNone:        0,
	ThreatChannelSuppression: 0.10,
	ThreatChannelShotsFired:  0.11,
	ThreatChannelEndangered:  0.20,
	ThreatChannelInjury:      0.05,
}

// ChannelForDanger returns the Threat channel that absorbs `kind`. Out-of-range
// values fall through to ThreatChannelNone so ThreatSystem can skip them.
func ChannelForDanger(kind DangerKind) ThreatChannel {
	if int(kind) >= int(dangerKindCount) {
		return ThreatChannelNone
	}
	return dangerSpecs[kind].Channel
}

// ThreatSource is a short-lived ECS entity spawned at the muzzle of each
// shot. SurvivalInstinctSystem reads the field cluster to pick a cover slot:
// the weighted average of nearby ThreatSource origins gives a "where is the
// danger coming from" vector that stays stable across one threat pulse even
// as bullets fly out.
//
// Entity layout: ThreatSource + WorldPos. Severity grows with weapon damage
// so an MG burst pushes more weight than a single rifle round.
type ThreatSource struct {
	Severity  float32 // 0..1 fire intensity / round weight
	SpawnTime float32
	TTL       float32
}
