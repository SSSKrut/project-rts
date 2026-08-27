package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MissileSystem flies every live round (Phase 20 M2, P6). Runs before
// `contact` so a hit this tick is part of this tick's picture. Guidance is
// velocity-steering under a turn limit; the proximity fuse tests the swept
// segment, not the endpoint, so a fast crosser cannot step over the kill
// radius in one tick.
type MissileSystem struct {
	filter      *ecs.Filter2[components.Missile, components.WorldPos]
	posMap      *ecs.Map[components.WorldPos]
	missileMap  *ecs.Map[components.Missile]
	overrideMap *ecs.Map[components.AircraftOverride]
	vehicleMap  *ecs.Map[components.Vehicle]
	motionMap   *ecs.Map[components.Motion]
	sampler     *HeightSampler
	damage      *DamageService
	worldRef    *ecs.World

	kills []missileKill
	dead  []ecs.Entity
}

type missileKill struct {
	target ecs.Entity
	amount float32
}

const (
	missileKillR  float32 = 7.0
	missileDecoyR float32 = 260.0
	missileDecoyP float32 = 0.65
)

func NewMissileSystem(damage *DamageService) *MissileSystem {
	return &MissileSystem{damage: damage}
}

func (sys *MissileSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter2[components.Missile, components.WorldPos](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.missileMap = ecs.NewMap[components.Missile](w)
	sys.overrideMap = ecs.NewMap[components.AircraftOverride](w)
	sys.vehicleMap = ecs.NewMap[components.Vehicle](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.sampler = NewHeightSampler(w)
	sys.worldRef = w
}

// MissileMap hands the launcher (WeaponSystem) its write handle — built here
// so the Missile component registers with this system, in the tail of
// registerSystems, never mid-order.
func (sys *MissileSystem) MissileMap() *ecs.Map[components.Missile] { return sys.missileMap }

func (MissileSystem) Name() string { return "missile" }

func (MissileSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MissileSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	now := float32(ctx.SimNow)
	sys.kills = sys.kills[:0]
	sys.dead = sys.dead[:0]

	q := sys.filter.Query()
	for q.Next() {
		m, pos := q.Get()
		ent := q.Entity()
		m.Fuel -= dt
		if m.Fuel <= 0 {
			sys.dead = append(sys.dead, ent)
			continue
		}

		px := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		pz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		py := pos.Local.Y

		chasing := !m.Decoyed && m.Target != (ecs.Entity{}) && sys.worldRef.Alive(m.Target)
		if chasing {
			if tp := sys.posMap.Get(m.Target); tp != nil {
				m.AimX = float32(tp.Chunk.X)*components.ChunkSize + tp.Local.X
				m.AimZ = float32(tp.Chunk.Z)*components.ChunkSize + tp.Local.Z
				// A hull's WorldPos is its footprint; aiming there flies the
				// round along the dirt for the last hundred metres of a shallow
				// approach and buries it short.
				m.AimY = tp.Local.Y + sys.aimOffsetY(m.Target)
			} else {
				chasing = false
			}
		}
		dx, dy, dz := m.AimX-px, m.AimY-py, m.AimZ-pz
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))

		// The one roll (P6): inside the terminal window, against a target
		// with flares out. Per-tick re-rolls would converge to certainty.
		if chasing && !m.Rolled && dist < missileDecoyR {
			m.Rolled = true
			if ov := sys.overrideMap.Get(m.Target); ov != nil &&
				ov.Kind == components.AirReflexFlare && now < ov.Until {
				seed := uint64(ent.ID())<<32 ^ uint64(ctx.TickIndex)
				if float32(splitmix(&seed))/float32(0x80000000) < missileDecoyP {
					m.Decoyed = true // aim freezes where the flares bloomed
					chasing = false
				}
			}
		}

		steerMissile(m, dx, dy, dz, dist, dt)

		stepX := m.VelX * dt
		stepY := m.VelY * dt
		stepZ := m.VelZ * dt
		if chasing &&
			segPointDistSq(px, py, pz, stepX, stepY, stepZ, m.AimX, m.AimY, m.AimZ) <
				missileKillR*missileKillR {
			sys.kills = append(sys.kills, missileKill{
				target: m.Target,
				amount: m.Damage * sys.sectorMul(m.Target, m.VelX, m.VelZ),
			})
			sys.dead = append(sys.dead, ent)
			continue
		}
		*pos = pos.Add(rl.Vector3{X: stepX, Y: stepY, Z: stepZ})
		if pos.Local.Y <= sys.sampler.Sample(px+stepX, pz+stepZ) {
			sys.dead = append(sys.dead, ent) // into the dirt
		}
	}

	for _, k := range sys.kills {
		sys.damage.Apply(k.target, k.amount)
	}
	for _, ent := range sys.dead {
		if sys.worldRef.Alive(ent) {
			sys.worldRef.RemoveEntity(ent)
		}
	}
}

// aimOffsetY lifts the aim point to hull centre for a vehicle; anything else
// is aimed at where it stands.
func (sys *MissileSystem) aimOffsetY(target ecs.Entity) float32 {
	if v := sys.liveVehicle(target); v != nil {
		return components.SpecForVehicle(v.Kind).BoxHgt * 0.5
	}
	return 0
}

// liveVehicle guards the two target reads that run at the FUSE rather than
// under the chase test: a round outlives its target routinely — someone else
// kills it mid-flight — and a dead id is a panic, not a nil.
func (sys *MissileSystem) liveVehicle(target ecs.Entity) *components.Vehicle {
	if target == (ecs.Entity{}) || !sys.worldRef.Alive(target) {
		return nil
	}
	return sys.vehicleMap.Get(target)
}

// sectorMul is the armour face the round actually struck. Unlike a hitscan
// shot, which has to reconstruct the direction from the muzzle, a missile
// carries its approach in its own velocity.
func (sys *MissileSystem) sectorMul(target ecs.Entity, velX, velZ float32) float32 {
	v := sys.liveVehicle(target)
	if v == nil {
		return 1
	}
	spec := components.SpecForVehicle(v.Kind)
	yaw := float32(0)
	if m := sys.motionMap.Get(target); m != nil {
		yaw = m.Yaw
	}
	l := float32(math.Sqrt(float64(velX*velX + velZ*velZ)))
	if l <= 0 {
		return spec.ArmorSide
	}
	fx := float32(math.Sin(float64(yaw)))
	fz := float32(math.Cos(float64(yaw)))
	switch c := (velX*fx + velZ*fz) / l; {
	case c < -0.5:
		return spec.ArmorFront
	case c > 0.5:
		return spec.ArmorRear
	}
	return spec.ArmorSide
}

// steerMissile bends the velocity toward the aim under the turn limit.
func steerMissile(m *components.Missile, dx, dy, dz, dist, dt float32) {
	if dist < 1e-3 {
		return
	}
	wx, wy, wz := dx/dist, dy/dist, dz/dist
	sp := float32(math.Sqrt(float64(m.VelX*m.VelX + m.VelY*m.VelY + m.VelZ*m.VelZ)))
	if sp < 1e-3 {
		m.VelX, m.VelY, m.VelZ = wx*m.Speed, wy*m.Speed, wz*m.Speed
		return
	}
	cx, cy, cz := m.VelX/sp, m.VelY/sp, m.VelZ/sp
	dot := cx*wx + cy*wy + cz*wz
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	ang := float32(math.Acos(float64(dot)))
	maxAng := m.TurnRad * dt
	var nx, ny, nz float32
	if ang <= maxAng || ang < 1e-4 {
		nx, ny, nz = wx, wy, wz
	} else {
		// Slerp between current and wanted headings, clamped at maxAng.
		t := maxAng / ang
		sinA := float32(math.Sin(float64(ang)))
		a := float32(math.Sin(float64((1-t)*ang))) / sinA
		b := float32(math.Sin(float64(t*ang))) / sinA
		nx, ny, nz = a*cx+b*wx, a*cy+b*wy, a*cz+b*wz
		n := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
		if n > 1e-4 {
			nx, ny, nz = nx/n, ny/n, nz/n
		}
	}
	m.VelX, m.VelY, m.VelZ = nx*m.Speed, ny*m.Speed, nz*m.Speed
}

// segPointDistSq — squared distance from point (tx,ty,tz) to the segment
// from (px,py,pz) along (sx,sy,sz).
func segPointDistSq(px, py, pz, sx, sy, sz, tx, ty, tz float32) float32 {
	wx, wy, wz := tx-px, ty-py, tz-pz
	segSq := sx*sx + sy*sy + sz*sz
	t := float32(0)
	if segSq > 1e-6 {
		t = (wx*sx + wy*sy + wz*sz) / segSq
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	ex, ey, ez := wx-sx*t, wy-sy*t, wz-sz*t
	return ex*ex + ey*ey + ez*ez
}
