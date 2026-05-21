package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// spawnDustBurst emits N small dust spheres around `pos` with downward +
// slight outward velocity. Phase 14.5 M14.5.5 - terrain hit visual cue.
// Deterministic-per-shot RNG would be ideal but the visual jitter is purely
// cosmetic; we use the hash address as a cheap seed.
func (sys *WeaponSystem) spawnDustBurst(pos rl.Vector3, now float32, n int, baseColor rl.Color) {
	if sys.particles == nil {
		return
	}
	seed := uint64(now * 10000.0)
	dustColor := rl.Color{R: 180, G: 160, B: 130, A: 200}
	_ = baseColor
	for i := 0; i < n; i++ {
		seed++
		rx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rz := float32(splitmix(&seed))/float32(0x40000000) - 1
		vel := rl.Vector3{X: rx * 1.0, Y: 0.5, Z: rz * 1.0}
		sys.particles.SpawnDust(pos, dustColor, vel, now)
	}
}

// spawnDebrisBurst emits N small debris cubes scattering from `pos`. Used
// for wall hits (small N=4) and splash impacts (N=10). Phase 14.5 M14.5.5.
func (sys *WeaponSystem) spawnDebrisBurst(pos rl.Vector3, now float32, n int) {
	if sys.particles == nil {
		return
	}
	seed := uint64(now * 10000.0)
	color := rl.Color{R: 120, G: 110, B: 100, A: 235}
	for i := 0; i < n; i++ {
		seed++
		rx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rz := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		ry := float32(splitmix(&seed)) / float32(0x40000000)
		vel := rl.Vector3{X: rx * 3.5, Y: 1.5 + ry*3.0, Z: rz * 3.5}
		sys.particles.SpawnDebris(pos, color, vel, now)
	}
}

// applySplashDamage walks the spatial hash inside the splash radius and
// submits per-target damage to DamageService. Damage falls off as
// `(1 - dSq/radiusSq)^falloff` so a unit at the edge takes a fraction of the
// baseline. Friendly fire enabled per Phase 14 design.
func (sys *WeaponSystem) applySplashDamage(ev splashEvent, hash *core.SpatialHash) {
	rSq := ev.radius * ev.radius
	hash.ForEachInRadius(ev.pos.X, ev.pos.Z, ev.radius, func(ent ecs.Entity, dSq float32) {
		if ent == ev.excluded {
			return // direct-hit target already damaged.
		}
		if !sys.worldRef.Alive(ent) {
			return
		}
		// Only damage Units (Threat component is on Units; we read HP
		// through DamageService).
		if sys.threatMap.Get(ent) == nil {
			return
		}
		t := float32(1) - dSq/rSq
		if t <= 0 {
			return
		}
		// Falloff exponent: 1 = linear, 2 = quadratic. Phase 14.5 ships with
		// these two only; math.Pow only kicks in for arbitrary exponents
		// (none currently configured) - most splash weapons match exactly.
		mul := t
		switch {
		case ev.falloff >= 2:
			mul = t * t
		case ev.falloff <= 1:
			// linear fallthrough
		default:
			// 1 < falloff < 2 - lerp between linear and quadratic.
			mul = t*t*(ev.falloff-1) + t*(2-ev.falloff)
		}
		amt := ev.damage * mul
		if amt > 0 {
			sys.damage.Apply(ent, amt)
		}
	})
}

// propagateSuppression pushes a DangerBulletImpact event into the
// DangerBuffer of every unit within suppressionRadius of `ev.impact`.
// Strength = hitMul * (1 - d/radius), matching the Phase 14 falloff curve.
// ThreatSystem drains the buffer next tick and converts Strength +
// impact-to-unit direction into Threat.Suppression + ThreatDir
// (recency-weighted average across all events in the same tick).
//
// hitMul:
//   - suppressionHitMul (0.5) - direct hit on a unit.
//   - suppressionMissMul (0.2) - near miss, scaled linearly by distance.
func (sys *WeaponSystem) propagateSuppression(ev suppressionEvent, now float32) {
	hash := sys.spatialHash.Get()
	if hash == nil {
		return
	}
	impactPos := components.Normalize(components.WorldPos{
		Local: rl.Vector3{X: ev.impact.X, Y: ev.impact.Y, Z: ev.impact.Z},
	})
	hash.ForEachInRadius(ev.impact.X, ev.impact.Z, suppressionRadius, func(ent ecs.Entity, dSq float32) {
		if !sys.worldRef.Alive(ent) {
			return
		}
		buf := sys.dangerBufMap.Get(ent)
		if buf == nil {
			return // not a Unit (no DangerBuffer component).
		}
		pos := sys.posMap.Get(ent)
		if pos == nil {
			return
		}
		ux := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		uz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		dx := ux - ev.impact.X
		dz := uz - ev.impact.Z
		_ = dSq
		d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		falloff := float32(1) - d/suppressionRadius
		if falloff < 0 {
			falloff = 0
		}
		components.PushDanger(buf, components.DangerEvent{
			Kind:     components.DangerBulletImpact,
			Source:   ev.shooter,
			Pos:      impactPos,
			Strength: ev.hitMul * falloff,
			Time:     now,
		})
	})
}
