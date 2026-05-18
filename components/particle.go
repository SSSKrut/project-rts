package components

import rl "github.com/gen2brain/raylib-go/raylib"

// Phase 14.5 M14.5.4 — Particle is an ECS-entity-backed transient visual.
// Replaces the slice-based `VisualEvents` resource: each tracer, impact,
// muzzle flash, smoke puff, dust kick, or debris fragment is its own
// entity with `Particle + WorldPos + ParticleVisual` (+ optionally
// `ParticleVel` for moving particles, `ParticleEnd` for line particles).
//
// Why ECS entities? Three reasons:
//
//  1. Uniform lifecycle. `ParticleSystem` ages every particle each tick,
//     applies velocity + gravity, removes expired ones via the same code
//     path. The old VisualEvents.Decay treated tracers and impacts as
//     separate slices — adding smoke / debris would have meant doubling
//     the wrap-around bookkeeping.
//
//  2. Capacity is honest. With entities the cap is enforced by despawn,
//     not by silently overwriting; the soft-cap eviction (oldest first)
//     plugs into the same `RemoveEntity` path as natural expiry.
//
//  3. Future readers can correlate. A smoke cloud spawned by an RPG
//     impact can carry an `OwnedBy{Shooter}` if Phase 15 wants to
//     attribute the visual to the action — trivial extension without
//     restructuring the storage.

// Particle is the marker component. Filter-target only.
type Particle struct{}

// ParticleKind discriminates per-render dispatch.
type ParticleKind uint8

const (
	ParticleTracer ParticleKind = iota
	ParticleImpact
	ParticleMuzzleFlash
	ParticleSmoke
	ParticleDust
	ParticleDebris
)

// ParticleVisual — render-time state. SpawnTime + TTL gate the fade in
// alpha; Size means radius for sphere kinds and "length" hint for line
// kinds (tracer end-point is in ParticleEnd).
type ParticleVisual struct {
	Kind      ParticleKind
	Color     rl.Color
	Size      float32
	SpawnTime float32
	TTL       float32
}

// ParticleVel — current 3D velocity (m/s). Optional component; absence
// means "static". Gravity is applied per-kind by ParticleSystem (see
// `kindGravity` table inside the system file).
type ParticleVel struct {
	Vel rl.Vector3
}

// ParticleEnd — end-point for line particles (tracers). WorldPos vs the
// entity's `WorldPos` lets ParticleSystem render the segment in render-
// space coords. Phase 14.5: tracer endpoints are static (muzzle->impact);
// future ricochet effects may animate them.
type ParticleEnd struct {
	To rl.Vector3
}

// ParticleSoftCap — soft cap on live particle entity count. Beyond this,
// `ParticleSystem` evicts the oldest particles to make room. Tuned for the
// Phase 14.5 reference scene (4-vs-8 firefight with RPG/GP25 splash =
// ~200-300 live at peak); 2000 leaves ample headroom for Phase 15 / 25
// firefight density. PHASE-14.5.md P12.
const ParticleSoftCap = 2000
