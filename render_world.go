package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// Prop draw distances (ISSUES #15): full detail near, trees-as-cones +
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
}

func drawParticles(ctx ParticleRenderCtx, now float32) {
	if ctx.Filter == nil {
		return
	}
	originChunk := systems.CurrentOriginChunk
	originBaseX := float32(originChunk.X) * components.ChunkSize
	originBaseZ := float32(originChunk.Z) * components.ChunkSize
	q := ctx.Filter.Query()
	for q.Next() {
		_, pos, vis := q.Get()
		age := now - vis.SpawnTime
		if age < 0 || age > vis.TTL {
			continue
		}
		fade := 1 - age/vis.TTL
		col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
			A: uint8(float32(vis.Color.A) * fade)}
		// Particle WorldPos.Local is absolute world-space (writers set Chunk=0);
		// subtract the render origin to get camera-relative coords.
		from := rl.Vector3{
			X: pos.Local.X - originBaseX,
			Y: pos.Local.Y,
			Z: pos.Local.Z - originBaseZ,
		}
		switch vis.Kind {
		case components.ParticleTracer:
			if end := ctx.EndMap.Get(q.Entity()); end != nil {
				to := rl.Vector3{
					X: end.To.X - originBaseX,
					Y: end.To.Y,
					Z: end.To.Z - originBaseZ,
				}
				rl.DrawLine3D(from, to, col)
			}
		case components.ParticleImpact, components.ParticleMuzzleFlash,
			components.ParticleSmoke, components.ParticleDust:
			r := vis.Size
			if r <= 0 {
				r = 0.1
			}
			rl.DrawSphere(from, r, col)
		case components.ParticleDebris:
			s := vis.Size
			if s <= 0 {
				s = 0.15
			}
			rl.DrawCube(from, s, s, s, col)
		}
	}
}
