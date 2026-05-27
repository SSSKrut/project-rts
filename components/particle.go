package components

import rl "github.com/gen2brain/raylib-go/raylib"

// Particle is an ECS-entity-backed transient visual: each tracer, impact,
// muzzle flash, smoke puff, dust kick, or debris fragment is its own
// entity with `Particle + WorldPos + ParticleVisual` (+ optionally
// `ParticleVel` for moving particles, `ParticleEnd` for line particles).
//
// Why ECS entities? ParticleSystem ages every particle each tick, applies
// velocity + gravity, removes expired ones via the same code path. Capacity
// is honest: the cap is enforced by despawn, not by silently overwriting.
type Particle struct{}

type ParticleKind uint8

const (
	ParticleTracer ParticleKind = iota
	ParticleImpact
	ParticleMuzzleFlash
	ParticleSmoke
	ParticleDust
	ParticleDebris
)

// ParticleVisual - render-time state. SpawnTime + TTL gate the fade in
// alpha; Size means radius for sphere kinds and "length" hint for line
// kinds (tracer end-point is in ParticleEnd).
type ParticleVisual struct {
	Kind      ParticleKind
	Color     rl.Color
	Size      float32
	SpawnTime float32
	TTL       float32
}

// ParticleVel - current 3D velocity (m/s). Optional component; absence
// means "static". Gravity is applied per-kind by ParticleSystem.
type ParticleVel struct {
	Vel rl.Vector3
}

// ParticleEnd - end-point for line particles (tracers). WorldPos vs the
// entity's `WorldPos` lets ParticleSystem render the segment in render-
// space coords.
type ParticleEnd struct {
	To rl.Vector3
}

// ParticleSoftCap - soft cap on live particle entity count. Beyond this,
// ParticleSystem evicts the oldest particles to make room.
const ParticleSoftCap = 2000
