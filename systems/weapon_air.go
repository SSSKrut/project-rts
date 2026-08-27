package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Air shots resolve analytically (Phase 20 M2, P6). The raycast pipeline is
// the wrong tool twice over: its chunk window tops out at weaponMaxRange and
// its target pool is ground-only, while a lead-computing gun needs no ray at
// all — the hit is a probability falling with range and angular rate, rolled
// from the same deterministic hash the dispersion uses. Runs SERIALLY inside
// the gunner snapshot (AA barrels are few), so worker-0 buffers are safe —
// the parallel pass has not started yet.
const (
	airShotBaseP    = 0.65
	airShotRangeExp = 0.6
	airShotOmegaMul = 5.0
	airShotMinP     = 0.02
	airShotMaxP     = 0.85
	// Near-miss danger goes straight to the target's DangerBuffer:
	// propagateSuppression walks the ground hashes and an airframe is in
	// neither (P10).
	airMissDanger = 0.25
	airHitDanger  = 0.5
)

type airDangerEvent struct {
	target   ecs.Entity
	muzzle   components.WorldPos
	strength float32
}

// airShotBlocked gates the shot BEFORE ammo is committed: NOE masking must
// hold at fire time, not one Awareness tick ago. Walls only matter when the
// pair is not vertically separated, same rule as detection; the wall set is
// the shooter's 3x3 window — an approximation that covers the courtyard case.
func (sys *WeaponSystem) airShotBlocked(muzzle rl.Vector3, targetPos components.WorldPos) bool {
	tx := worldXYZ(targetPos, 0)
	if terrainBlocksLOS(sys.heightmaps, muzzle.X, muzzle.Z, muzzle.Y, tx.X, tx.Z, tx.Y) {
		return true
	}
	dy := tx.Y - muzzle.Y
	if dy < 0 {
		dy = -dy
	}
	if dy <= airWallClearM {
		walls := localWalls(sys.wallsByChunk, worldChunkOf(muzzle))
		if _, hit := segmentToWallsT(walls, muzzle.X, muzzle.Z, tx.X, tx.Z); hit {
			return true
		}
	}
	return false
}

func worldChunkOf(p rl.Vector3) components.ChunkCoord {
	return components.ChunkCoord{
		X: int32(math.Floor(float64(p.X) / float64(components.ChunkSize))),
		Z: int32(math.Floor(float64(p.Z) / float64(components.ChunkSize))),
	}
}

func targetVelXZ(m *components.Motion) (float32, float32) {
	if m == nil {
		return 0, 0
	}
	return float32(math.Sin(float64(m.Yaw))) * m.Speed,
		float32(math.Cos(float64(m.Yaw))) * m.Speed
}

// resolveAirShotSerial rolls one burst against an airborne target and pushes
// the outcome into the worker-0 scratch buffers.
func (sys *WeaponSystem) resolveAirShotSerial(muzzle rl.Vector3,
	muzzlePos components.WorldPos, target ecs.Entity, targetPos components.WorldPos,
	velX, velZ float32, weapon *components.Weapon, wspec *components.WeaponSpec,
	now float32, seed uint64) {

	tx := worldXYZ(targetPos, 0)
	dx, dy, dz := tx.X-muzzle.X, tx.Y-muzzle.Y, tx.Z-muzzle.Z
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if dist < 1e-3 || dist >= weapon.RangeM {
		return
	}

	// Lead point — one iteration of t = d/v is plenty at these speeds. The
	// tracer flies there either way; that is what "firing ahead" looks like.
	t := dist / wspec.ProjSpeedM
	lead := rl.Vector3{X: tx.X + velX*t, Y: tx.Y, Z: tx.Z + velZ*t}

	// Angular rate across the line of sight — the one quantity both the
	// gun's probability and a missile's chase geometry respect (M2).
	speedSq := velX*velX + velZ*velZ
	radial := (velX*dx + velZ*dz) / dist
	perpSq := speedSq - radial*radial
	if perpSq < 0 {
		perpSq = 0
	}
	omega := float32(math.Sqrt(float64(perpSq))) / dist

	rangeF := 1 - dist/weapon.RangeM
	p := airShotBaseP * float32(math.Pow(float64(rangeF), airShotRangeExp)) /
		(1 + airShotOmegaMul*omega)
	if p < airShotMinP {
		p = airShotMinP
	} else if p > airShotMaxP {
		p = airShotMaxP
	}

	rng := seed
	hit := float32(splitmix(&rng))/float32(0x80000000) < p
	end := lead
	danger := float32(airMissDanger)
	if hit {
		danger = airHitDanger
		sys.workerDamage[0] = append(sys.workerDamage[0], damageEvent{
			target: target, amount: float32(weapon.Damage) * wspec.VsAir,
		})
		sys.workerImpact[0] = append(sys.workerImpact[0], impactSpec{
			Pos: end, Color: wspec.TracerColor, SpawnTime: now,
			TTL: weaponImpactTTL, Hit: hitKindUnitFlag,
		})
	} else {
		// Deterministic bracket so the burst visibly straddles the airframe.
		end.X += (float32(splitmix(&rng))/float32(0x40000000) - 1) * 7
		end.Y += (float32(splitmix(&rng))/float32(0x40000000) - 1) * 5
		end.Z += (float32(splitmix(&rng))/float32(0x40000000) - 1) * 7
	}
	sys.workerTracer[0] = append(sys.workerTracer[0], tracerSpec{
		From: muzzle, To: end, Color: wspec.TracerColor,
		SpawnTime: now, TTL: weaponTracerTTL,
	})
	sys.airDangerBuf = append(sys.airDangerBuf, airDangerEvent{
		target: target, muzzle: muzzlePos, strength: danger,
	})
}

// pendingMissile is a launch queued during the (query-locked) snapshot;
// the entity is created in the serial post-pass.
type pendingMissile struct {
	origin components.WorldPos
	muzzle rl.Vector3
	m      components.Missile
}

// SetMissileMap wires the launcher to MissileSystem's write handle (built in
// the registration tail — see MissileSystem.MissileMap).
func (sys *WeaponSystem) SetMissileMap(m *ecs.Map[components.Missile]) { sys.missileMap = m }

// SetAirEngageMap hands the gunner the AirEngagement handle, built in the same
// tail for the same reason.
func (sys *WeaponSystem) SetAirEngageMap(m *ecs.Map[components.AirEngagement]) {
	sys.airEngageMap = m
}

// missileDamageMul resolves the class multiplier at LAUNCH — the target class
// is fixed the moment the round leaves. Sector armour is not: that depends on
// the approach, and a missile knows its own at impact (MissileSystem).
func (sys *WeaponSystem) missileDamageMul(target ecs.Entity, wspec *components.WeaponSpec) float32 {
	// Zero-safe like every other target read since block G: an ordered aim
	// point carries no body, and this was the one raw dereference left after
	// that sweep — reached the day a guided barrel was pointed at bare ground.
	if sys.targetIsAircraft(target) {
		return wspec.VsAir
	}
	class := components.ArmorClassSoft
	if tv := sys.targetVehicleOf(target); tv != nil {
		class = components.SpecForVehicle(tv.Kind).Class
	}
	return components.VsClassMul(wspec, class)
}

func (sys *WeaponSystem) queueMissile(target ecs.Entity, shooterPos components.WorldPos,
	muzzle rl.Vector3, targetPos components.WorldPos, wspec *components.WeaponSpec) {
	tx := worldXYZ(targetPos, 0)
	dx, dy, dz := tx.X-muzzle.X, tx.Y-muzzle.Y, tx.Z-muzzle.Z
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if dist < 1e-3 {
		return
	}
	origin := shooterPos
	origin.Local.Y += weaponEyeHeight
	sys.missilePending = append(sys.missilePending, pendingMissile{
		origin: origin,
		muzzle: muzzle,
		m: components.Missile{
			Target:  target,
			VelX:    dx / dist * wspec.MissileSpeedM,
			VelY:    dy / dist * wspec.MissileSpeedM,
			VelZ:    dz / dist * wspec.MissileSpeedM,
			Speed:   wspec.MissileSpeedM,
			TurnRad: wspec.MissileTurnDps * float32(math.Pi) / 180,
			Fuel:    wspec.MissileFuelS,
			Damage:  float32(wspec.Damage) * sys.missileDamageMul(target, wspec),
			AimX:    tx.X, AimY: tx.Y, AimZ: tx.Z,
		},
	})
}

// spawnPendingMissiles runs in the serial post-pass, after every query closed.
func (sys *WeaponSystem) spawnPendingMissiles(now float32) {
	if sys.missileMap == nil {
		return
	}
	for i := range sys.missilePending {
		p := &sys.missilePending[i]
		ent := sys.worldRef.NewEntity()
		origin := p.origin
		sys.posMap.Add(ent, &origin)
		m := p.m
		sys.missileMap.Add(ent, &m)
		if sys.particles != nil {
			sys.particles.SpawnMuzzleFlash(p.muzzle, rl.Color{R: 255, G: 240, B: 200, A: 255}, now)
		}
	}
}
