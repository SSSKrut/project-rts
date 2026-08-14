package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

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

// spawnRocketTrail lays a line of puffs along the flight path. The rocket is
// hitscan, so the whole trail appears at once; wind advection and the erosion
// fade sell the aftermath.
func (sys *WeaponSystem) spawnRocketTrail(from, to rl.Vector3, now float32) {
	if sys.particles == nil {
		return
	}
	dx := to.X - from.X
	dy := to.Y - from.Y
	dz := to.Z - from.Z
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if dist < 4 {
		return
	}
	const spacing float32 = 7.0
	n := int(dist / spacing)
	if n > 30 {
		n = 30
	}
	seed := uint64(now*10000.0) ^ uint64(dist*100)
	color := rl.Color{R: 172, G: 172, B: 175, A: 190}
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n+1)
		seed++
		jx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		jy := float32(splitmix(&seed))/float32(0x40000000) - 1
		p := rl.Vector3{
			X: from.X + dx*t + jx*0.4,
			Y: from.Y + dy*t + jy*0.3,
			Z: from.Z + dz*t + jx*0.3,
		}
		sys.particles.SpawnPuff(p, color, rl.Vector3{Y: 0.15}, 0.42, 3.2, now)
	}
}

// spawnExplosionSmoke — the blast cloud: a few large dark puffs rising from
// the crater, scaled by splash radius.
func (sys *WeaponSystem) spawnExplosionSmoke(pos rl.Vector3, radius, now float32) {
	if sys.particles == nil {
		return
	}
	if radius < 1 {
		radius = 1
	}
	n := 4 + int(radius*0.5)
	if n > 8 {
		n = 8
	}
	seed := uint64(now*10000.0) ^ uint64(radius*77)
	for i := 0; i < n; i++ {
		seed++
		rx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rz := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rr := float32(splitmix(&seed)) / float32(0x40000000) * 0.5
		p := rl.Vector3{
			X: pos.X + rx*radius*0.35,
			Y: pos.Y + rr*radius*0.3,
			Z: pos.Z + rz*radius*0.35,
		}
		shade := uint8(58 + i*9)
		color := rl.Color{R: shade, G: shade, B: shade, A: 210}
		vel := rl.Vector3{X: rx * 0.7, Y: 1.1 + rr, Z: rz * 0.7}
		sys.particles.SpawnPuff(p, color, vel, radius*(0.28+rr*0.3), 3.5+rr*2, now)
	}
}

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

// Damage falls off as `(1 - dSq/radiusSq)^falloff`; friendly fire enabled.
func (sys *WeaponSystem) applySplashDamage(ev splashEvent, hash *core.SpatialHash) {
	rSq := ev.radius * ev.radius
	hash.ForEachInRadius(ev.pos.X, ev.pos.Z, ev.radius, func(ent ecs.Entity, dSq float32) {
		if ent == ev.excluded {
			return
		}
		if !sys.worldRef.Alive(ent) {
			return
		}
		if sys.threatMap.Get(ent) == nil {
			return
		}
		t := float32(1) - dSq/rSq
		if t <= 0 {
			return
		}
		amt := ev.damage * splashFalloffMul(t, ev.falloff) * ev.vsSoft
		if amt > 0 {
			sys.damage.Apply(ent, amt)
		}
	})
}

func splashFalloffMul(t, falloff float32) float32 {
	switch {
	case falloff >= 2:
		return t * t
	case falloff <= 1:
		return t
	}
	return t*t*(falloff-1) + t*(2-falloff)
}

// Splash against vehicles: class multiplier × side armor (no sector
// resolution for blast waves).
func (sys *WeaponSystem) applySplashToVehicles(ev splashEvent, hash *core.SpatialHash) {
	rSq := ev.radius * ev.radius
	hash.ForEachInRadius(ev.pos.X, ev.pos.Z, ev.radius, func(ent ecs.Entity, dSq float32) {
		if ent == ev.excluded || !sys.worldRef.Alive(ent) {
			return
		}
		veh := sys.vehicleMap.Get(ent)
		if veh == nil {
			return
		}
		t := float32(1) - dSq/rSq
		if t <= 0 {
			return
		}
		spec := components.SpecForVehicle(veh.Kind)
		classMul := vsClassOf(ev.vsSoft, ev.vsLight, ev.vsHeavy, spec.Class)
		amt := ev.damage * splashFalloffMul(t, ev.falloff) * classMul * spec.ArmorSide
		if amt > 0 {
			sys.damage.Apply(ent, amt)
		}
	})
}

// propagateBlast pushes DangerExplosion to everyone inside the blast's felt
// radius and drops a BlastMark for the unsafe-area aggregator. Felt radius is
// wider than the lethal one — being missed by a shell is still a reason to
// leave.
func (sys *WeaponSystem) propagateBlast(ev splashEvent, now float32) {
	feltR := ev.radius * blastFeltMul
	severity := ev.damage * 0.01
	if severity > 1 {
		severity = 1
	}
	centre := components.WorldPos{}.Add(rl.Vector3{X: ev.pos.X, Z: ev.pos.Z})
	push := func(ent ecs.Entity, dSq float32) {
		if !sys.worldRef.Alive(ent) {
			return
		}
		buf := sys.dangerBufMap.Get(ent)
		if buf == nil {
			return
		}
		falloff := 1 - float32(math.Sqrt(float64(dSq)))/feltR
		if falloff <= 0 {
			return
		}
		components.PushDanger(buf, components.DangerEvent{
			Kind:     components.DangerExplosion,
			Pos:      centre,
			Strength: severity * falloff,
			Time:     now,
		})
	}
	if hash := sys.spatialHash.Get(); hash != nil {
		hash.ForEachInRadius(ev.pos.X, ev.pos.Z, feltR, push)
	}
	if vh := sys.vehHash.Get(); vh != nil {
		vh.SpatialHash.ForEachInRadius(ev.pos.X, ev.pos.Z, feltR, push)
	}
	SpawnBlastMark(sys.worldRef, sys.posMap, sys.blastMap, centre, ev.radius, severity, now)
}

// SpawnBlastMark records one explosion for the unsafe-area aggregator. Shared
// with test harnesses that stage synthetic shelling.
func SpawnBlastMark(world *ecs.World, posMap *ecs.Map[components.WorldPos],
	blastMap *ecs.Map[components.BlastMark], pos components.WorldPos,
	radius, severity, now float32) {
	e := world.NewEntity()
	p := pos
	posMap.Add(e, &p)
	blastMap.Add(e, &components.BlastMark{
		Radius:    radius,
		Severity:  severity,
		ExpiresAt: float64(now) + float64(components.BlastMarkTTL),
	})
}

// Pushes DangerBulletImpact into each nearby unit's DangerBuffer with
// Strength = hitMul * (1 - d/radius). ThreatSystem drains the buffer next
// tick to update Threat.Suppression / ThreatDir. Event Pos = the MUZZLE:
// ThreatDir votes must point away from the shooter — impact positions
// scatter around the target and randomise the cover side.
func (sys *WeaponSystem) propagateSuppression(ev suppressionEvent, now float32) {
	dangerPos := ev.muzzle
	push := func(ent ecs.Entity, _ float32) {
		if !sys.worldRef.Alive(ent) {
			return
		}
		buf := sys.dangerBufMap.Get(ent)
		if buf == nil {
			return
		}
		pos := sys.posMap.Get(ent)
		if pos == nil {
			return
		}
		ux := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		uz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		dx := ux - ev.impact.X
		dz := uz - ev.impact.Z
		d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		falloff := float32(1) - d/suppressionRadius
		if falloff < 0 {
			falloff = 0
		}
		components.PushDanger(buf, components.DangerEvent{
			Kind:     components.DangerBulletImpact,
			Source:   ev.shooter,
			Pos:      dangerPos,
			Strength: ev.hitMul * falloff,
			Time:     now,
		})
	}
	if hash := sys.spatialHash.Get(); hash != nil {
		hash.ForEachInRadius(ev.impact.X, ev.impact.Z, suppressionRadius, push)
	}
	if vh := sys.vehHash.Get(); vh != nil {
		vh.SpatialHash.ForEachInRadius(ev.impact.X, ev.impact.Z, suppressionRadius, push)
	}
}
