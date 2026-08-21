package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ParticleSystem manages ECS-entity-backed transient visual particles
// (tracers, impacts, muzzle flashes, smoke, dust, debris).
//
// Pipeline: (1) age each particle, integrate Vel + gravity, collect expired;
// (2) soft-cap eviction by SpawnTime when count > ParticleSoftCap;
// (3) RemoveEntity sweep. Serial — particle counts are low (<2000).
//
// Render lives in render_world.go (drawParticles); ParticleSystem doesn't
// touch raylib drawing.
type ParticleSystem struct {
	filter     *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual]
	vehFilter  *ecs.Filter3[components.Vehicle, components.WorldPos, components.Motion]
	posMap     *ecs.Map[components.WorldPos]
	visualMap  *ecs.Map[components.ParticleVisual]
	velMap     *ecs.Map[components.ParticleVel]
	endMap     *ecs.Map[components.ParticleEnd]
	particle   *ecs.Map[components.Particle]
	atmRes     ecs.Resource[components.Atmosphere]
	world      *ecs.World
	removeBuf  []ecs.Entity
	sortBuf    []particleAge
	exhaustBuf []exhaustSpawn
}

type exhaustSpawn struct {
	pos   rl.Vector3
	vel   rl.Vector3
	color rl.Color
	size  float32
}

type particleAge struct {
	ent       ecs.Entity
	spawnTime float32
}

func NewParticleSystem() *ParticleSystem {
	return &ParticleSystem{
		removeBuf: make([]ecs.Entity, 0, 64),
		sortBuf:   make([]particleAge, 0, 128),
	}
}

func (sys *ParticleSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](w)
	sys.vehFilter = ecs.NewFilter3[components.Vehicle, components.WorldPos, components.Motion](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.visualMap = ecs.NewMap[components.ParticleVisual](w)
	sys.velMap = ecs.NewMap[components.ParticleVel](w)
	sys.endMap = ecs.NewMap[components.ParticleEnd](w)
	sys.particle = ecs.NewMap[components.Particle](w)
	sys.atmRes = ecs.NewResource[components.Atmosphere](w)
	sys.world = w
}

func (ParticleSystem) Name() string { return "particle" }

func (ParticleSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// kindGravity gives per-kind vertical acceleration (m/s², +Y up).
var kindGravity = [...]float32{
	components.ParticleTracer:      0,
	components.ParticleImpact:      0,
	components.ParticleMuzzleFlash: 0,
	components.ParticleSmoke:       +0.5,
	components.ParticleDust:        -1.0,
	components.ParticleDebris:      -9.8,
}

// kindWindGrip: per-second relaxation of horizontal velocity toward the local
// wind. Smoke surrenders to it, dust partially, ballistic debris barely.
var kindWindGrip = [...]float32{
	components.ParticleTracer:      0,
	components.ParticleImpact:      0,
	components.ParticleMuzzleFlash: 0,
	components.ParticleSmoke:       1.4,
	components.ParticleDust:        0.6,
	components.ParticleDebris:      0.12,
}

func windGripFor(kind components.ParticleKind) float32 {
	if int(kind) < len(kindWindGrip) {
		return kindWindGrip[kind]
	}
	return 0
}

func (sys *ParticleSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	if dt < 0 {
		dt = 0
	}
	now := float32(ctx.SimNow)

	sys.removeBuf = sys.removeBuf[:0]
	sys.sortBuf = sys.sortBuf[:0]

	q := sys.filter.Query()
	for q.Next() {
		_, pos, vis := q.Get()
		ent := q.Entity()
		age := now - vis.SpawnTime
		if age > vis.TTL {
			sys.removeBuf = append(sys.removeBuf, ent)
			continue
		}
		if vel := sys.velMap.Get(ent); vel != nil {
			if int(vis.Kind) < len(kindGravity) {
				vel.Vel.Y += kindGravity[vis.Kind] * dt
			}
			if grip := windGripFor(vis.Kind); grip > 0 {
				// Particle positions are absolute (chunk 0 convention).
				wxv, wzv := WindAt(sys.atmRes.Get(), pos.Local.X, pos.Local.Z, ctx.SimNow)
				k := grip * dt
				if k > 1 {
					k = 1
				}
				vel.Vel.X += (wxv - vel.Vel.X) * k
				vel.Vel.Z += (wzv - vel.Vel.Z) * k
			}
			pos.Local.X += vel.Vel.X * dt
			pos.Local.Y += vel.Vel.Y * dt
			pos.Local.Z += vel.Vel.Z * dt
		}
		sys.sortBuf = append(sys.sortBuf, particleAge{
			ent: ent, spawnTime: vis.SpawnTime,
		})
	}

	// Soft cap: evict the oldest when alive count exceeds the cap.
	if overflow := len(sys.sortBuf) - components.ParticleSoftCap; overflow > 0 {
		for i := 1; i < len(sys.sortBuf); i++ {
			for j := i; j > 0 && sys.sortBuf[j-1].spawnTime > sys.sortBuf[j].spawnTime; j-- {
				sys.sortBuf[j-1], sys.sortBuf[j] = sys.sortBuf[j], sys.sortBuf[j-1]
			}
		}
		for i := 0; i < overflow; i++ {
			sys.removeBuf = append(sys.removeBuf, sys.sortBuf[i].ent)
		}
	}

	for _, ent := range sys.removeBuf {
		if sys.world.Alive(ent) {
			sys.world.RemoveEntity(ent)
		}
	}

	sys.emitExhaust(ctx, now)
}

// emitExhaust drops engine-smoke puffs behind every live vehicle on a
// deterministic tick cadence (TickIndex + entity ID — no private state, no
// RNG). Sim-side so headless runs stay bit-identical with watched ones.
func (sys *ParticleSystem) emitExhaust(ctx core.UpdateContext, now float32) {
	sys.exhaustBuf = sys.exhaustBuf[:0]
	q := sys.vehFilter.Query()
	for q.Next() {
		veh, pos, mot := q.Get()
		moving := mot.Speed > 0.5 || mot.Speed < -0.5
		period := uint64(36)
		if moving {
			period = 10
		}
		if (ctx.TickIndex+uint64(q.Entity().ID()))%period != 0 {
			continue
		}
		spec := components.SpecForVehicle(veh.Kind)
		sinY := float32(math.Sin(float64(mot.Yaw)))
		cosY := float32(math.Cos(float64(mot.Yaw)))
		absX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		absZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		rear := spec.BoxLen * 0.42
		p := rl.Vector3{
			X: absX - sinY*rear,
			Y: pos.Local.Y + spec.BoxHgt*0.55,
			Z: absZ - cosY*rear,
		}
		color := rl.Color{R: 96, G: 96, B: 100, A: 150}
		if spec.Locomotion == components.LocomotionTracked {
			color = rl.Color{R: 62, G: 62, B: 64, A: 185}
		}
		size := float32(0.30)
		if moving {
			size = 0.45
		}
		sys.exhaustBuf = append(sys.exhaustBuf, exhaustSpawn{
			pos:   p,
			vel:   rl.Vector3{X: -sinY * 0.3, Y: 0.7, Z: -cosY * 0.3},
			color: color,
			size:  size,
		})
	}
	for i := range sys.exhaustBuf {
		e := sys.exhaustBuf[i]
		ent := sys.world.NewEntity()
		sys.particle.Add(ent, &components.Particle{})
		posCopy := components.WorldPos{Local: e.pos}
		sys.posMap.Add(ent, &posCopy)
		sys.visualMap.Add(ent, &components.ParticleVisual{
			Kind: components.ParticleSmoke, Color: e.color, Size: e.size,
			SpawnTime: now, TTL: 1.6,
		})
		sys.velMap.Add(ent, &components.ParticleVel{Vel: e.vel})
	}
}

// SpawnParticleHandles bundles the maps a writer (WeaponSystem) needs to
// spawn a particle entity in the serial post-pass.
type SpawnParticleHandles struct {
	world    *ecs.World
	particle *ecs.Map[components.Particle]
	pos      *ecs.Map[components.WorldPos]
	visual   *ecs.Map[components.ParticleVisual]
	vel      *ecs.Map[components.ParticleVel]
	end      *ecs.Map[components.ParticleEnd]
}

func NewSpawnHandles(w *ecs.World) *SpawnParticleHandles {
	return &SpawnParticleHandles{
		world:    w,
		particle: ecs.NewMap[components.Particle](w),
		pos:      ecs.NewMap[components.WorldPos](w),
		visual:   ecs.NewMap[components.ParticleVisual](w),
		vel:      ecs.NewMap[components.ParticleVel](w),
		end:      ecs.NewMap[components.ParticleEnd](w),
	}
}

// SpawnTracer spawns a line-particle from `from` to `to` (world-space).
func (h *SpawnParticleHandles) SpawnTracer(from, to rl.Vector3, color rl.Color, now, ttl float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: from} // chunk=0; render uses absolute coords
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleTracer, Color: color, Size: 0, SpawnTime: now, TTL: ttl,
	})
	h.end.Add(ent, &components.ParticleEnd{To: to})
}

func (h *SpawnParticleHandles) SpawnImpact(pos rl.Vector3, color rl.Color, now, ttl float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: pos}
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleImpact, Color: color, Size: 0.1, SpawnTime: now, TTL: ttl,
	})
}

func (h *SpawnParticleHandles) SpawnMuzzleFlash(pos rl.Vector3, color rl.Color, now float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: pos}
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleMuzzleFlash, Color: color, Size: 0.18, SpawnTime: now, TTL: 0.08,
	})
}

func (h *SpawnParticleHandles) SpawnSmoke(pos rl.Vector3, color rl.Color, now float32) {
	h.SpawnPuff(pos, color, rl.Vector3{Y: 0.2}, 0.6, 3.0, now)
}

// SpawnPuff — general smoke puff with caller-chosen size / TTL / velocity.
// Trails, explosion clouds and exhaust all come through here.
func (h *SpawnParticleHandles) SpawnPuff(pos rl.Vector3, color rl.Color,
	vel rl.Vector3, size, ttl, now float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: pos}
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleSmoke, Color: color, Size: size, SpawnTime: now, TTL: ttl,
	})
	h.vel.Add(ent, &components.ParticleVel{Vel: vel})
}

func (h *SpawnParticleHandles) SpawnDust(pos rl.Vector3, color rl.Color, vel rl.Vector3, now float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: pos}
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleDust, Color: color, Size: 0.15, SpawnTime: now, TTL: 0.6,
	})
	h.vel.Add(ent, &components.ParticleVel{Vel: vel})
}

func (h *SpawnParticleHandles) SpawnDebris(pos rl.Vector3, color rl.Color, vel rl.Vector3, now float32) {
	ent := h.world.NewEntity()
	h.particle.Add(ent, &components.Particle{})
	posCopy := components.WorldPos{Local: pos}
	h.pos.Add(ent, &posCopy)
	h.visual.Add(ent, &components.ParticleVisual{
		Kind: components.ParticleDebris, Color: color, Size: 0.18, SpawnTime: now, TTL: 1.5,
	})
	h.vel.Add(ent, &components.ParticleVel{Vel: vel})
}
