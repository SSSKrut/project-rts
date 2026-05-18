package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

// pickUnitFromMouse - single-click selection. Cast a ray through the panel-
// local cursor (in viewW×viewH viewport space, NOT screen space), intersect
// with the horizontal plane at the anchor's surface height, then snap to the
// nearest unit within 1 m XZ. Returns the entity ID when something is hit, or
// (0, false) otherwise. Phase 10 (M10.3): callers must pass panel-local
// cursor + the 3D panel's bounds, because raylib's GetScreenToWorldRayEx
// derives the projection matrix from the supplied viewport size.
func pickUnitFromMouse(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	anchor components.WorldPos, localCursor rl.Vector2, viewW, viewH int32) (ecs.Entity, bool) {
	target, ok := mouseTargetWorldPos(systems.CurrentCamera,
		anchor.ToRenderSpace(systems.CurrentOriginChunk), localCursor, viewW, viewH)
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

// hoverUnitFromMouse is the silent twin of pickUnitFromMouse - no click, just
// a closest-within-1 m lookup. Used per-frame to refresh the global `hovered`
// state so the inspector / map can highlight whoever the cursor is over.
func hoverUnitFromMouse(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	anchor components.WorldPos, localCursor rl.Vector2, viewW, viewH int32) (ecs.Entity, bool) {
	return pickUnitFromMouse(filter, anchor, localCursor, viewW, viewH)
}

// collectUnitsInRect - marquee selection. Projects every Unit into the panel-
// local viewport, keeps those whose XY lies inside the rect. minX/Y/maxX/Y
// are in panel-local coords (cursor.x - panel.Bounds.X, etc.).
func collectUnitsInRect(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	minX, maxX, minY, maxY float32, viewW, viewH int32) []ecs.Entity {
	var out []ecs.Entity
	q := filter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		screen := rl.GetWorldToScreenEx(pos.ToRenderSpace(systems.CurrentOriginChunk),
			systems.CurrentCamera, viewW, viewH)
		if screen.X < minX || screen.X > maxX || screen.Y < minY || screen.Y > maxY {
			continue
		}
		out = append(out, q.Entity())
	}
	return out
}

// mouseTargetWorldPos converts the panel-local cursor into a WorldPos by
// raycasting against a horizontal plane at the anchor's surface height. viewW
// / viewH are the 3D panel's dimensions - GetScreenToWorldRayEx uses them to
// derive the projection matrix, so the ray matches what the player sees in
// the panel even when the panel is smaller than the OS window. Returns
// (_, false) if the ray is parallel to the plane or points away from it.
func mouseTargetWorldPos(cam rl.Camera3D, anchorRender rl.Vector3,
	localCursor rl.Vector2, viewW, viewH int32) (components.WorldPos, bool) {
	ray := rl.GetScreenToWorldRayEx(localCursor, cam, viewW, viewH)
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

// bindEntry stores one slot of the Ctrl+1..5 / 1..5 selection-recall ring.
// If Squad is non-zero, the slot is tied to a live squad - recall expands it
// to whoever is currently in the roster, so binding a squad and then losing
// members still works. If Squad is zero, Units is the ad-hoc snapshot taken at
// bind time (Phase 7-style selection memory).
type bindEntry struct {
	Squad ecs.Entity
	Units []ecs.Entity
}

// SelectionGroups splits a mixed selection into the unique squads to issue
// orders against and the soloists (units with no SquadMember). PHASE-11.md P9
// - replaces the homogeneous-squad-only routing from Phase 9 / 10 and
// prevents the Leave() chain that dissolved multi-squad selections (ISSUES #3).
type SelectionGroups struct {
	SquadsToOrder []ecs.Entity
	Soloists      []ecs.Entity
}

// groupSelectionByOwner walks `selected`, collects the distinct squads any
// member belongs to, and aggregates the rest as soloists. Allocation: O(N)
// for the temporary set; Phase 11 selections fit in a handful of entities,
// so the cost is negligible.
func groupSelectionByOwner(selected []ecs.Entity,
	squadMemberMap *ecs.Map[components.SquadMember]) SelectionGroups {
	var groups SelectionGroups
	seen := make(map[ecs.Entity]struct{}, len(selected))
	for _, e := range selected {
		sm := squadMemberMap.Get(e)
		if sm == nil || sm.Squad == (ecs.Entity{}) {
			groups.Soloists = append(groups.Soloists, e)
			continue
		}
		if _, dup := seen[sm.Squad]; dup {
			continue
		}
		seen[sm.Squad] = struct{}{}
		groups.SquadsToOrder = append(groups.SquadsToOrder, sm.Squad)
	}
	return groups
}

// groupSelected inspects the SquadMember of every entity in `selected` and
// returns (commonSquad, true) when every selected unit belongs to the *same*
// squad. The common Squad entity is zero when every selected unit is a
// soloist (which still counts as homogeneous - the caller checks the zero
// value before routing the order through SquadService).
func groupSelected(selected []ecs.Entity,
	squadMemberMap *ecs.Map[components.SquadMember]) (ecs.Entity, bool) {
	if len(selected) == 0 {
		return ecs.Entity{}, false
	}
	var common ecs.Entity
	first := true
	for _, e := range selected {
		var s ecs.Entity
		if m := squadMemberMap.Get(e); m != nil {
			s = m.Squad
		}
		if first {
			common = s
			first = false
			continue
		}
		if common != s {
			return ecs.Entity{}, false
		}
	}
	return common, true
}

// stepAlongPath moves the anchor toward path[0] by up to step metres, popping
// the waypoint when the agent enters its arrival radius. Y is lerped toward
// the waypoint's Y so the anchor can climb stairs / enter sunken bunkers -
// the chunk GroundStick clamps it back to either the surface or the covering
// floor depending on which is closer next tick.
func stepAlongPath(anchorPos *components.WorldPos, path []components.WorldPos, step float32) []components.WorldPos {
	for len(path) > 0 && step > 0 {
		target := path[0]
		diff := target.Sub(*anchorPos)
		dist := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
		if dist <= 0.5 {
			// Snap Y to the waypoint when we pop - this is where multi-floor
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
