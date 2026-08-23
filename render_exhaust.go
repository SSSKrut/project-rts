package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// Engine smoke used to be spawned inside the tick as ECS entities. It is pure
// cosmetics, and living in the sim cost more than it looked: every puff took an
// entity id, and ids decide gameplay (which hull holds course in a crossing,
// which side a pressed man steps to). It also made the emitter unable to read
// the model — a headless gate has no GL and could not load a .glb, so the
// exhaust point had to be synthesised from the hull box.
//
// Now it lives here: a plain ring buffer in the render half, emitting from the
// asset's exhaust mount when there is a model and from the box when there is
// not. Nothing enters the world, save_spec or a hash.
const exhaustCap = 512

type exhaustPuff struct {
	pos   rl.Vector3 // absolute world space; the render origin shifts, puffs must not
	vel   rl.Vector3
	born  float32
	ttl   float32
	size  float32
	tint  rl.Color
	seed  uint32
	alive bool
}

type exhaustField struct {
	puffs [exhaustCap]exhaustPuff
	head  int
	clock float32
	next  map[ecs.Entity]float32
	seen  map[ecs.Entity]float32
}

func newExhaustField() *exhaustField {
	return &exhaustField{
		next: make(map[ecs.Entity]float32),
		seen: make(map[ecs.Entity]float32),
	}
}

// beginFrame ages the field and drops the timers of hulls that stopped being
// drawn, so a despawned vehicle cannot keep a slot in the map.
func (e *exhaustField) beginFrame(dt float32, wind rl.Vector2) {
	e.clock += dt
	for i := range e.puffs {
		p := &e.puffs[i]
		if !p.alive {
			continue
		}
		if e.clock-p.born > p.ttl {
			p.alive = false
			continue
		}
		p.vel.X += wind.X * 0.35 * dt
		p.vel.Z += wind.Y * 0.35 * dt
		p.vel.Y += 0.45 * dt // exhaust is hot: it climbs instead of falling
		p.pos.X += p.vel.X * dt
		p.pos.Y += p.vel.Y * dt
		p.pos.Z += p.vel.Z * dt
	}
	e.next, e.seen = e.seen, e.next
	clear(e.seen)
}

// emit drops one puff per period per hull. The cadence is wall time, not ticks:
// this runs in the render half now, and a paused sim should still smoke.
func (e *exhaustField) emit(ent ecs.Entity, at rl.Vector3, dir rl.Vector3,
	spec *components.VehicleSpec, moving bool) {
	period := float32(0.55)
	size := float32(0.30)
	if moving {
		period, size = 0.16, 0.45
	}
	due, known := e.next[ent]
	if !known {
		// Stagger by id so a parked column does not puff in lockstep.
		due = e.clock + float32(ent.ID()%29)/29*period
	}
	if e.clock < due {
		e.seen[ent] = due
		return
	}
	e.seen[ent] = e.clock + period

	tint := rl.Color{R: 96, G: 96, B: 100, A: 150}
	if spec.Locomotion == components.LocomotionTracked {
		tint = rl.Color{R: 62, G: 62, B: 64, A: 185}
	}
	p := &e.puffs[e.head]
	e.head = (e.head + 1) % exhaustCap
	*p = exhaustPuff{
		pos:   at,
		vel:   rl.Vector3{X: dir.X * 0.6, Y: dir.Y*0.6 + 0.5, Z: dir.Z * 0.6},
		born:  e.clock,
		ttl:   1.6,
		size:  size,
		tint:  tint,
		seed:  uint32(ent.ID())*2654435761 + uint32(e.head),
		alive: true,
	}
}

// draw feeds the live puffs to the shared billboard renderer, converting to the
// current render origin on the way out.
func (e *exhaustField) draw(puffs *particleRenderer, originBaseX, originBaseZ float32) {
	if puffs == nil || !puffs.ok {
		return
	}
	for i := range e.puffs {
		p := &e.puffs[i]
		if !p.alive {
			continue
		}
		frac := (e.clock - p.born) / p.ttl
		puffs.add(puffDraw{
			pos:     rl.Vector3{X: p.pos.X - originBaseX, Y: p.pos.Y, Z: p.pos.Z - originBaseZ},
			size:    p.size * (1 + 1.3*frac),
			tint:    p.tint,
			fade:    smokeEase(frac) * float32(p.tint.A) / 255,
			seed:    p.seed,
			groundY: systems.GroundHeight(p.pos.X, p.pos.Z),
		})
	}
}

// emitVehicleExhaust resolves the emission point for one hull. With a model it
// is the exhaust mount, which rides the part it is bolted to; without one it
// falls back to the old box-relative guess so unmodelled classes keep smoking.
func (g *Game) emitVehicleExhaust(ent ecs.Entity, pos components.WorldPos,
	kind components.VehicleKind, side components.AssetSide, yaw, turretYaw float32) {
	spec := components.SpecForVehicle(kind)
	speed := float32(0)
	if mo := g.Svc.UnitFactory.MotionMap.Get(ent); mo != nil {
		speed = mo.Speed
	}
	abs := rl.Vector3{
		X: float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
		Y: pos.Local.Y,
		Z: float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
	}
	at, dir, ok := g.vehicleMountWorld(ent, kind, side, "exhaust.0", abs, yaw, turretYaw)
	if !ok {
		sinY := float32(math.Sin(float64(yaw)))
		cosY := float32(math.Cos(float64(yaw)))
		rear := spec.BoxLen * 0.42
		at = rl.Vector3{X: abs.X - sinY*rear, Y: abs.Y + spec.BoxHgt*0.55, Z: abs.Z - cosY*rear}
		dir = rl.Vector3{X: -sinY * 0.5, Y: 1, Z: -cosY * 0.5}
	}
	g.Ctx.Exhaust.emit(ent, at, dir, spec, speed > 0.5 || speed < -0.5)
}

// drawVehicleLights puts a warm dot on each headlight mount after dark. Only a
// glow, not a projector — the point is that a column is visible at night and
// that the mounts the bake exports are actually load-bearing.
func (g *Game) drawVehicleLights(ent ecs.Entity, renderPos rl.Vector3,
	kind components.VehicleKind, side components.AssetSide,
	yaw, turretYaw float32, night bool) {
	if !night || !g.Ctx.Models.has(kind, side) {
		return
	}
	for _, id := range [...]string{"light.head.left", "light.head.right"} {
		if at, _, ok := g.vehicleMountWorld(ent, kind, side, id, renderPos, yaw, turretYaw); ok {
			rl.DrawSphere(at, 0.16, rl.Color{R: 255, G: 240, B: 190, A: 220})
		}
	}
	for _, id := range [...]string{"light.tail.left", "light.tail.right"} {
		if at, _, ok := g.vehicleMountWorld(ent, kind, side, id, renderPos, yaw, turretYaw); ok {
			rl.DrawSphere(at, 0.10, rl.Color{R: 230, G: 60, B: 40, A: 200})
		}
	}
}
