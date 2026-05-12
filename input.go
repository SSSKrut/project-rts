package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

// pickUnitFromMouse — single-click selection. Cast a ray through the mouse
// pointer, intersect with the horizontal plane at the anchor's surface
// height, then snap to the nearest unit within 1 m XZ. Returns the entity ID
// when something is hit, or (0, false) otherwise.
func pickUnitFromMouse(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	anchor components.WorldPos) (ecs.Entity, bool) {
	target, ok := mouseTargetWorldPos(systems.CurrentCamera,
		anchor.ToRenderSpace(systems.CurrentOriginChunk))
	if !ok {
		return ecs.Entity{}, false
	}
	var best ecs.Entity
	bestDSq := float32(1.0 * 1.0)
	hasHit := false
	q := filter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		diff := pos.Sub(target)
		dSq := diff.X*diff.X + diff.Z*diff.Z
		if dSq < bestDSq {
			bestDSq = dSq
			best = q.Entity()
			hasHit = true
		}
	}
	return best, hasHit
}

// collectUnitsInRect — marquee selection. For every Unit, project its
// WorldPos into screen space and keep those whose XY lies in the rect.
func collectUnitsInRect(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	minX, maxX, minY, maxY float32) []ecs.Entity {
	var out []ecs.Entity
	q := filter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		screen := rl.GetWorldToScreen(pos.ToRenderSpace(systems.CurrentOriginChunk), systems.CurrentCamera)
		if screen.X < minX || screen.X > maxX || screen.Y < minY || screen.Y > maxY {
			continue
		}
		out = append(out, q.Entity())
	}
	return out
}

// mouseTargetWorldPos converts the current cursor position into a WorldPos by
// raycasting against a horizontal plane at the anchor's surface height. Returns
// (_, false) if the ray is parallel to the plane or points away from it
// (mouse hovering above the horizon, etc.) — the caller should leave navPath
// untouched in that case.
func mouseTargetWorldPos(cam rl.Camera3D, anchorRender rl.Vector3) (components.WorldPos, bool) {
	ray := rl.GetMouseRay(rl.GetMousePosition(), cam)
	planeY := anchorRender.Y - systems.AnchorEyeHeight
	if math.Abs(float64(ray.Direction.Y)) < 1e-4 {
		return components.WorldPos{}, false
	}
	t := (planeY - ray.Position.Y) / ray.Direction.Y
	if t < 0 {
		return components.WorldPos{}, false
	}
	hit := rl.Vector3{
		X: ray.Position.X + t*ray.Direction.X,
		Y: planeY,
		Z: ray.Position.Z + t*ray.Direction.Z,
	}
	return (components.WorldPos{Chunk: systems.CurrentOriginChunk}).Add(hit), true
}

// stepAlongPath moves the anchor toward path[0] by up to step metres, popping
// the waypoint when the agent enters its arrival radius. Y is lerped toward
// the waypoint's Y so the anchor can climb stairs / enter sunken bunkers —
// the chunk GroundStick clamps it back to either the surface or the covering
// floor depending on which is closer next tick.
func stepAlongPath(anchorPos *components.WorldPos, path []components.WorldPos, step float32) []components.WorldPos {
	for len(path) > 0 && step > 0 {
		target := path[0]
		diff := target.Sub(*anchorPos)
		dist := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
		if dist <= 0.5 {
			// Snap Y to the waypoint when we pop — this is where multi-floor
			// transitions take effect, so GroundStick picks the new floor.
			anchorPos.Local.Y = target.Local.Y
			path = path[1:]
			continue
		}
		take := step
		if take > dist {
			take = dist
		}
		// Vertical climb: lerp Y proportionally so a path crossing a stairs
		// transition slides instead of teleporting.
		dy := target.Local.Y - anchorPos.Local.Y
		yStep := dy * (take / dist)
		*anchorPos = anchorPos.Add(rl.Vector3{
			X: diff.X / dist * take,
			Y: yStep,
			Z: diff.Z / dist * take,
		})
		step -= take
		if take >= dist {
			anchorPos.Local.Y = target.Local.Y
			path = path[1:]
		} else {
			break
		}
	}
	return path
}
