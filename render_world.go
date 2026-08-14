package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// Prop draw distances: full detail near, trees-as-cones +
// planes only in the far band, nothing beyond cull. Behind-camera props are
// skipped outside the near ring (orbit swings keep the surroundings).
const (
	propFullDetailDistSq float32 = 120 * 120
	propCullDistSq       float32 = 260 * 260
	propNearKeepDistSq   float32 = 40 * 40
)

// drawPropFar — reduced far-band LOD: a tree collapses to one 4-segment
// canopy cone. Caller culls small props (bushes/rocks) in the far band.
func drawPropFar(meta components.PropMeta, pos rl.Vector3, scale float32) {
	trunkH := meta.Size.Y * scale
	canopyR := meta.Size.Z * scale
	tip := rl.Vector3{X: pos.X, Y: pos.Y + trunkH*2.5, Z: pos.Z}
	rl.DrawCylinderEx(pos, tip, canopyR, 0, 4, meta.Color)
}

func drawProp(meta components.PropMeta, pos rl.Vector3, yaw, scale float32) {
	switch meta.Primitive {
	case components.PrimitiveCube:
		sx := meta.Size.X * scale
		sy := meta.Size.Y * scale
		sz := meta.Size.Z * scale
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y+sy*0.5, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawCubeV(rl.Vector3{}, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
			rl.PopMatrix()
		} else {
			c := rl.Vector3{X: pos.X, Y: pos.Y + sy*0.5, Z: pos.Z}
			rl.DrawCubeV(c, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
		}

	case components.PrimitiveSphere:
		r := meta.Size.X * scale
		c := rl.Vector3{X: pos.X, Y: pos.Y + r*0.5, Z: pos.Z}
		rl.DrawSphere(c, r, meta.Color)

	case components.PrimitiveCylinder:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, r, 8, meta.Color)

	case components.PrimitiveCone:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, 0, 8, meta.Color)

	case components.PrimitivePlane:
		size := rl.Vector2{X: meta.Size.X * scale, Y: meta.Size.Z * scale}
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawPlane(rl.Vector3{}, size, meta.Color)
			rl.PopMatrix()
		} else {
			rl.DrawPlane(pos, size, meta.Color)
		}

	case components.PrimitiveTree:
		trunkR := meta.Size.X * scale
		trunkH := meta.Size.Y * scale
		canopyR := meta.Size.Z * scale
		canopyH := trunkH * 1.5
		trunkBase := pos
		trunkTop := rl.Vector3{X: pos.X, Y: pos.Y + trunkH, Z: pos.Z}
		canopyTip := rl.Vector3{X: pos.X, Y: pos.Y + trunkH + canopyH, Z: pos.Z}
		rl.DrawCylinderEx(trunkBase, trunkTop, trunkR, trunkR, 6, meta.TrunkColor)
		rl.DrawCylinderEx(trunkTop, canopyTip, canopyR, 0, 6, meta.Color)
	}
}

type ParticleRenderCtx struct {
	Filter *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual]
	EndMap *ecs.Map[components.ParticleEnd]
	Smoke  *ecs.Filter2[components.WorldPos, components.SmokeField]
	Puffs  *particleRenderer
}

func drawParticles(ctx ParticleRenderCtx, now float32) {
	if ctx.Filter == nil {
		return
	}
	originChunk := systems.CurrentOriginChunk
	originBaseX := float32(originChunk.X) * components.ChunkSize
	originBaseZ := float32(originChunk.Z) * components.ChunkSize
	billboards := ctx.Puffs != nil && ctx.Puffs.ok
	q := ctx.Filter.Query()
	for q.Next() {
		_, pos, vis := q.Get()
		age := now - vis.SpawnTime
		if age < 0 || age > vis.TTL {
			continue
		}
		ageFrac := age / vis.TTL
		fade := 1 - ageFrac
		// Particle WorldPos.Local is absolute world-space (writers set Chunk=0);
		// subtract the render origin to get camera-relative coords.
		from := rl.Vector3{
			X: pos.Local.X - originBaseX,
			Y: pos.Local.Y,
			Z: pos.Local.Z - originBaseZ,
		}
		switch vis.Kind {
		case components.ParticleTracer:
			col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
				A: uint8(float32(vis.Color.A) * fade)}
			if end := ctx.EndMap.Get(q.Entity()); end != nil {
				to := rl.Vector3{
					X: end.To.X - originBaseX,
					Y: end.To.Y,
					Z: end.To.Z - originBaseZ,
				}
				rl.DrawLine3D(from, to, col)
			}
		case components.ParticleSmoke, components.ParticleDust:
			if billboards {
				grow := float32(1.3)
				if vis.Kind == components.ParticleDust {
					grow = 0.9
				}
				ctx.Puffs.add(puffDraw{
					pos:     from,
					size:    vis.Size * (1 + grow*ageFrac),
					tint:    vis.Color,
					fade:    smokeEase(ageFrac) * float32(vis.Color.A) / 255,
					seed:    puffSeed(q.Entity().ID(), 0),
					groundY: systems.GroundHeight(pos.Local.X, pos.Local.Z),
				})
			} else {
				col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
					A: uint8(float32(vis.Color.A) * fade)}
				r := vis.Size
				if r <= 0 {
					r = 0.1
				}
				rl.DrawSphere(from, r, col)
			}
		case components.ParticleImpact, components.ParticleMuzzleFlash:
			col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
				A: uint8(float32(vis.Color.A) * fade)}
			r := vis.Size
			if r <= 0 {
				r = 0.1
			}
			rl.DrawSphere(from, r, col)
		case components.ParticleDebris:
			col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
				A: uint8(float32(vis.Color.A) * fade)}
			s := vis.Size
			if s <= 0 {
				s = 0.15
			}
			rl.DrawCube(from, s, s, s, col)
		}
	}

	drawSmokeFields(ctx, now, originBaseX, originBaseZ, billboards)
	if billboards {
		ctx.Puffs.flush()
	}
}

// drawSmokeFields renders each concealment volume as a deterministic cluster
// of slowly-churning puffs (hash on entity ID) instead of one translucent
// sphere. Sim state stays two fields; spawn time = ExpiresAt - SmokeFieldTTL.
func drawSmokeFields(ctx ParticleRenderCtx, now, originBaseX, originBaseZ float32, billboards bool) {
	if ctx.Smoke == nil {
		return
	}
	const puffsPerField = 12
	tint := rl.Color{R: 182, G: 183, B: 186, A: 255}
	qs := ctx.Smoke.Query()
	for qs.Next() {
		sp, sf := qs.Get()
		absX := float32(sp.Chunk.X)*components.ChunkSize + sp.Local.X
		absZ := float32(sp.Chunk.Z)*components.ChunkSize + sp.Local.Z
		if !billboards {
			c := sp.ToRenderSpace(systems.CurrentOriginChunk)
			c.Y += sf.Radius * 0.5
			rl.DrawSphere(c, sf.Radius, rl.Color{R: 150, G: 150, B: 155, A: 70})
			continue
		}
		age := now - (float32(sf.ExpiresAt) - components.SmokeFieldTTL)
		frac := age / components.SmokeFieldTTL
		if frac < 0 || frac > 1 {
			continue
		}
		life := smokeEase(frac)
		id := qs.Entity().ID()
		for i := uint32(0); i < puffsPerField; i++ {
			s1 := puffSeed(id, i*3+1)
			s2 := puffSeed(id, i*3+2)
			s3 := puffSeed(id, i*3+3)
			ang := hash01(s1)*6.2831 + now*(0.05+hash01(s2)*0.08)
			r := sf.Radius * (0.12 + 0.78*hash01(s2))
			sn, cs := sinCos(ang)
			px := absX + sn*r
			pz := absZ + cs*r
			h := hash01(s3)
			py := sp.Local.Y + sf.Radius*(0.06+0.42*h) + age*0.1
			ctx.Puffs.add(puffDraw{
				pos:     rl.Vector3{X: px - originBaseX, Y: py, Z: pz - originBaseZ},
				size:    sf.Radius * (0.42 + 0.26*h) * (0.8 + 0.3*frac),
				tint:    tint,
				fade:    life * 0.88,
				seed:    s1,
				groundY: systems.GroundHeight(px, pz),
			})
		}
	}
}
