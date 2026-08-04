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
	// source toward the unit. DERIVED from Clusters[0] since P3: readers that
	// only need "where is the main threat" keep working unchanged.
	ThreatDir rl.Vector3

	// Clusters holds the distinct threat bearings this unit is under, sorted
	// by Weight (dominant first). One averaged direction is a lie in a
	// crossfire — it points between two shooters, i.e. at cover that stops
	// neither. Empty slots have Weight == 0.
	Clusters [ThreatClusterCount]ThreatCluster

	State ThreatState
}

// ThreatCluster is one direction danger arrives from. Dir points FROM the
// source TOWARD the unit (same convention as ThreatDir), Dist is the metric
// distance to the source, Weight the decayed accumulated strength, LastAt the
// sim time of the last contribution.
type ThreatCluster struct {
	Dir    rl.Vector3
	Dist   float32
	Weight float32
	LastAt float32
}

const (
	// How many distinct bearings a unit tracks. Three covers "front + flank +
	// the one behind me"; a fourth adds noise, not decisions.
	ThreatClusterCount = 3
	// Merge threshold: events within ~60° of a cluster join it.
	ThreatClusterMergeCos float32 = 0.5
	// Per-second weight decay. Slower than any channel: a bearing stays
	// actionable a little past the pressure that established it.
	ThreatClusterDecay float32 = 0.08
)

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
// ThreatSystem drains the buffer each tick. Pos is where the danger
// EMANATES FROM (bullet impacts carry the shooter's muzzle, not the crater —
// ThreatDir votes derive from Pos and cover must face the shooter).
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

// BlastMark is one recent explosion, spawned with a WorldPos and reaped by
// ThreatDecaySystem. It exists as an ENTITY rather than a ring on some system
// so the "was there shelling here recently" memory rides save/load like
// everything else — private cross-tick state would re-arm zones at different
// ticks after a load and break replay continuity.
type BlastMark struct {
	Radius    float32
	Severity  float32
	ExpiresAt float64
}

// UnsafeArea is a volume the AI must vacate — spawned when several blasts
// land close together in time and space (artillery walking over a position),
// reaped on TTL. Entity layout mirrors SmokeField: UnsafeArea + WorldPos.
type UnsafeArea struct {
	Radius    float32
	Severity  float32
	ExpiresAt float64
}

const (
	// Blast memory window (seconds) and the radius within which separate
	// blasts count as the same barrage.
	BlastMarkTTL       float32 = 6.0
	UnsafeClusterR     float32 = 18.0
	UnsafeMinBlasts            = 2
	UnsafeAreaTTL      float32 = 8.0
	UnsafeAreaMinR     float32 = 12.0
	UnsafeDangerPerSec float32 = 0.55
)
