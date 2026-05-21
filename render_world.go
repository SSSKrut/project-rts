package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// This file used to hold ~820 lines of mixed render helpers. The bodies have
// moved to per-domain sibling files:
//
//	render_overlays.go   debug overlays (nav grid / cover / floor nav / road graph) + drawNavPath
//	render_buildings.go  drawBuildingFloor / drawBuildingWall / drawBuildingStairs
//	render_units.go      unit cubes, role labels, stamina / HP bars, ghost cube + arc, squad palettes
//
// What stays here:
//
//	drawProp        - prop primitives (cube / sphere / cylinder / cone / plane / tree)
//	ParticleRenderCtx + drawParticles - particle pass (read-only ECS walk)

// drawProp renders one placeholder prop primitive. Yaw radians around +Y;
// scale uniform. Position is the prop's *foot* (ground contact), so
// primitives lift themselves to sit on top.
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
		// Trunk + canopy composite. 6-sided is enough at silhouette resolution.
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

// ParticleRenderCtx bundles ECS handles needed to walk every live particle.
// main.go builds one and passes to drawParticles each frame. Phase 14.5
// M14.5.4.
type ParticleRenderCtx struct {
	Filter *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual]
	EndMap *ecs.Map[components.ParticleEnd]
}

// drawParticles walks every live Particle entity and dispatches per-kind
// draw calls. Replaces drawTracers + drawImpacts; the new kinds (smoke,
// dust, debris, muzzle flash) share the same pipeline.
//
// Phase 14.5 M14.5.4: raylib's DrawLine3D / DrawSphere / DrawCube are not
// instanced - each particle is its own draw call. Acceptable at the cap of
// 2000 live entries; Phase 16 may revisit if firefight density grows.
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
		// Particle WorldPos.Local is the absolute world-space position
		// (writers set Chunk=0 to keep this simple); subtract the render
		// origin to get camera-relative coords. This mirrors the old
		// VisualEvents code path exactly.
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
