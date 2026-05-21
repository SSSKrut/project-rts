package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// resolveShot is the parallel work unit. Applies dispersion, walks the LOS,
// finds the closest unit-vs-ray hit, and emits damage / tracer / impact /
// threat / suppression records into per-worker buffers.
func resolveShot(
	s *shotWork, targets []targetSnap, targetsByChunk map[components.ChunkCoord][]int32,
	wallsByChunk map[components.ChunkCoord][]losWall, now float32,
	dmgBuf *[]damageEvent, tracerBuf *[]tracerSpec, impactBuf *[]impactSpec,
	threatBuf *[]threatEvent, suppBuf *[]suppressionEvent, splashBuf *[]splashEvent,
) {
	// Apply lateral dispersion to the aim point. dispersion is small-angle
	// radians; lateral deflection ~ dispersion * range. Sample one uniform
	// per axis from the seeded RNG so the same shot always lands the same way
	// (debuggable replay).
	dx := s.aim.X - s.muzzle.X
	dz := s.aim.Z - s.muzzle.Z
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if dist > s.rangeMax {
		// Out-of-range guard (should have been filtered in pickTarget but
		// targets move between picks). Drop the shot silently.
		return
	}

	rng := s.rngSeed
	rx := float32(splitmix(&rng))/float32(0x40000000) - 1 // [-1, 1)
	ry := float32(splitmix(&rng))/float32(0x40000000) - 1
	lateral := s.dispersion * dist
	// Perpendicular-to-LOS XZ basis (rotate (dx,dz) by 90 deg). Adds
	// horizontal scatter; ry adds a small vertical jitter so the impact
	// sphere doesn't always sit on the ground plane.
	invD := float32(1)
	if dist > 1e-4 {
		invD = 1 / dist
	}
	perpX := -dz * invD
	perpZ := dx * invD
	aim := s.aim
	aim.X += perpX * lateral * rx
	aim.Z += perpZ * lateral * rx
	aim.Y += lateral * ry * 0.5

	// Wall LOS - collect walls in the 3x3 chunk window around the shooter.
	walls := localWalls(wallsByChunk, s.shooterChunk)
	wallT, wallBlocks := segmentToWallsT(walls, s.muzzle.X, s.muzzle.Z, aim.X, aim.Z)

	// Unit-vs-ray - walk targets in the 3x3 chunk window. Pick the unit
	// closest to the shooter (along ray) that's within its hit cylinder.
	bestHitT := float32(1.5) // > 1 means no hit yet
	var bestHit ecs.Entity
	var bestPos components.WorldPos
	var bestRadius float32
	for dcZ := int32(-1); dcZ <= 1; dcZ++ {
		for dcX := int32(-1); dcX <= 1; dcX++ {
			cc := components.ChunkCoord{X: s.shooterChunk.X + dcX, Z: s.shooterChunk.Z + dcZ}
			for _, idx := range targetsByChunk[cc] {
				t := &targets[idx]
				if t.ent == s.shooter {
					continue
				}
				tx := float32(t.pos.Chunk.X)*components.ChunkSize + t.pos.Local.X
				tz := float32(t.pos.Chunk.Z)*components.ChunkSize + t.pos.Local.Z
				hitT, miss := segmentPointHit(s.muzzle.X, s.muzzle.Z, aim.X, aim.Z, tx, tz, t.radius)
				if miss {
					continue
				}
				if hitT < bestHitT {
					bestHitT = hitT
					bestHit = t.ent
					bestPos = t.pos
					bestRadius = t.radius
				}
			}
		}
	}

	// Final hit decision: nearest event between unit and wall.
	hitWall := wallBlocks && wallT < bestHitT
	hitUnit := bestHit != (ecs.Entity{}) && !hitWall

	// Compute terminal impact point + classify the hit kind for per-kind
	// particle dispatch in the serial post-pass.
	var impact rl.Vector3
	var hk hitKind
	switch {
	case hitWall:
		impact = lerpVec3(s.muzzle, aim, wallT)
		hk = hitKindWall
	case hitUnit:
		impact = rl.Vector3{
			X: float32(bestPos.Chunk.X)*components.ChunkSize + bestPos.Local.X,
			Y: aim.Y, // approximate impact at aim's torso height
			Z: float32(bestPos.Chunk.Z)*components.ChunkSize + bestPos.Local.Z,
		}
		_ = bestRadius // reserved for cover model in Phase 24
		hk = hitKindUnitFlag
	default:
		impact = aim
		hk = hitKindTerrain
	}

	*tracerBuf = append(*tracerBuf, tracerSpec{
		From: s.muzzle, To: impact,
		Color: s.tracerColor, SpawnTime: now, TTL: weaponTracerTTL,
	})

	impactColor := rl.Color{R: 230, G: 200, B: 80, A: 255}
	if hitUnit {
		impactColor = rl.Color{R: 230, G: 70, B: 70, A: 255}
	}
	*impactBuf = append(*impactBuf, impactSpec{
		Pos: impact, Color: impactColor, SpawnTime: now, TTL: weaponImpactTTL,
		Hit: hk,
	})

	// Phase 14.5 M14.5.5: splash event for AoE weapons. Direct-hit target
	// already lands in dmgBuf; the splash applies to everyone else in radius
	// (friendly fire enabled, matches Phase 14 design).
	if s.splashRadius > 0 {
		*splashBuf = append(*splashBuf, splashEvent{
			pos: impact, radius: s.splashRadius, falloff: s.splashFalloff,
			damage: s.damage, excluded: bestHit,
		})
	}

	if hitUnit {
		// Stance multiplier - smaller silhouette -> less damage transferred.
		// Re-read stance from snapshot (best-effort; stance change between
		// snapshot and apply is rare and acceptable).
		mul := float32(1)
		for _, idx := range targetsByChunk[bestPos.Chunk] {
			if targets[idx].ent == bestHit {
				mul = components.SpecForStance(targets[idx].stance).DamageMultiplier
				break
			}
		}
		*dmgBuf = append(*dmgBuf, damageEvent{
			target: bestHit,
			amount: s.damage * mul,
		})
	}

	// Phase 14 M14.5: every shot spawns one ThreatSource at the muzzle.
	// Severity grows with weapon damage so MG bursts press harder than
	// single-shot rifle fire - clamped to 1.0.
	severity := s.damage / 100
	if severity > 1 {
		severity = 1
	}
	*threatBuf = append(*threatBuf, threatEvent{
		origin:   s.muzzlePos,
		severity: severity,
	})

	// Suppression propagation: every shot (hit or miss) writes one event at
	// the terminal impact point. Hit -> bigger nominal weight; miss ->
	// smaller distance-scaled push. resolveShot can't walk units (workers
	// don't share state) - the serial post-pass does the radius query.
	hitMul := suppressionMissMul
	if hitUnit {
		hitMul = suppressionHitMul
	}
	*suppBuf = append(*suppBuf, suppressionEvent{
		impact:  impact,
		hitMul:  hitMul,
		shooter: s.shooter,
	})
}

// localWalls returns the concatenated wall slice for the 3x3 chunk window
// around `home`. Returns a fresh slice each call (worker-local - no shared
// mutation), so the parallel pass is race-safe.
func localWalls(wallsByChunk map[components.ChunkCoord][]losWall, home components.ChunkCoord) []losWall {
	var total int
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			total += len(wallsByChunk[components.ChunkCoord{X: home.X + dx, Z: home.Z + dz}])
		}
	}
	if total == 0 {
		return nil
	}
	out := make([]losWall, 0, total)
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			out = append(out, wallsByChunk[components.ChunkCoord{X: home.X + dx, Z: home.Z + dz}]...)
		}
	}
	return out
}

// segmentToWallsT returns the smallest t in [0,1] along (ax,az)->(bx,bz) at
// which a non-transparent wall is hit, plus a "blocked" flag. Mirrors
// anyLosWallBlocks but reports the parametric distance so we can render the
// tracer up to the wall.
func segmentToWallsT(walls []losWall, ax, az, bx, bz float32) (float32, bool) {
	bestT := float32(2)
	blocked := false
	for i := range walls {
		w := &walls[i]
		toX := w.fromX + w.sa*w.length
		toZ := w.fromZ + w.ca*w.length
		t1, t2, ok := segmentSegmentIntersect2D(ax, az, bx, bz, w.fromX, w.fromZ, toX, toZ)
		if !ok || t1 < 0 || t1 > 1 || t2 < 0 || t2 > 1 {
			continue
		}
		if w.openingPresent {
			wallT := t2 * w.length
			if wallT >= w.openStart && wallT <= w.openEnd {
				if w.openingTransparent {
					continue
				}
			}
		}
		if t1 < bestT {
			bestT = t1
			blocked = true
		}
	}
	return bestT, blocked
}

// segmentPointHit returns (t, miss) - t is the parametric position on the
// (ax,az)->(bx,bz) segment closest to (px,pz). miss=true if that closest
// approach distance exceeds `radius` (no hit) or if the closest point lies
// outside [0,1] (beyond the segment endpoints).
func segmentPointHit(ax, az, bx, bz, px, pz, radius float32) (float32, bool) {
	dx := bx - ax
	dz := bz - az
	lenSq := dx*dx + dz*dz
	if lenSq < 1e-6 {
		return 0, true
	}
	t := ((px-ax)*dx + (pz-az)*dz) / lenSq
	if t < 0 || t > 1 {
		return t, true
	}
	cx := ax + dx*t
	cz := az + dz*t
	d2 := (px-cx)*(px-cx) + (pz-cz)*(pz-cz)
	if d2 > radius*radius {
		return t, true
	}
	return t, false
}

// worldXYZ converts a WorldPos to absolute rl.Vector3 in world coords (chunk
// base + local + extra Y offset).
func worldXYZ(p components.WorldPos, yOff float32) rl.Vector3 {
	return rl.Vector3{
		X: float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
		Y: p.Local.Y + yOff,
		Z: float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z,
	}
}

// worldDistSq returns squared world XZ distance between two WorldPos values.
// Y is ignored - combat range is read on the horizontal plane.
func worldDistSq(a, b components.WorldPos) float32 {
	ax := float32(a.Chunk.X)*components.ChunkSize + a.Local.X
	az := float32(a.Chunk.Z)*components.ChunkSize + a.Local.Z
	bx := float32(b.Chunk.X)*components.ChunkSize + b.Local.X
	bz := float32(b.Chunk.Z)*components.ChunkSize + b.Local.Z
	dx := bx - ax
	dz := bz - az
	return dx*dx + dz*dz
}

// lerpVec3 is plain linear interpolation between two rl.Vector3.
func lerpVec3(a, b rl.Vector3, t float32) rl.Vector3 {
	return rl.Vector3{
		X: a.X + (b.X-a.X)*t,
		Y: a.Y + (b.Y-a.Y)*t,
		Z: a.Z + (b.Z-a.Z)*t,
	}
}

// clampWeaponRange enforces the system-wide max so the LOS window stays
// bounded.
func clampWeaponRange(r float32) float32 {
	if r <= 0 {
		return 0
	}
	if r > weaponMaxRange {
		return weaponMaxRange
	}
	return r
}

// tracerColorFor reads tracer hue from components.WeaponSpecs. Phase 14.5
// M14.5.1 - palette switch replaced with spec table.
func tracerColorFor(k components.WeaponKind) rl.Color {
	return components.SpecForWeapon(k).TracerColor
}

// splitmix is a tiny stateful SplitMix64 used to pick deterministic
// dispersion jitter inside resolveShot. Reading *seed and writing back so
// successive calls advance the stream. Output is uint32 in [0, 2^31); caller
// divides by 2^30 then subtracts 1 to recentre to [-1, 1). Pulling math/rand
// would allocate a *rand.Rand per worker for thread safety, which we avoid
// here.
func splitmix(seed *uint64) uint32 {
	*seed += 0x9e3779b97f4a7c15
	z := *seed
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return uint32((z >> 33) & 0x7fffffff)
}
