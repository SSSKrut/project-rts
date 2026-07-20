package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

// pickUnitFromMouse - single-click selection. Cast a ray through the panel-
// local cursor (panel-local, NOT screen-space; raylib's GetScreenToWorldRayEx
// derives the projection matrix from viewport size), intersect the horizontal
// plane at the anchor's surface height, then snap to the nearest unit within
// 1 m XZ or vehicle within its ColliderR (nearest across both pools wins).
func pickUnitFromMouse(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	vehFilter *ecs.Filter2[components.WorldPos, components.Vehicle],
	anchor components.WorldPos, localCursor rl.Vector2, viewW, viewH int32) (ecs.Entity, bool) {
	target, ok := mouseTargetWorldPos(systems.CurrentCamera,
		anchor.ToRenderSpace(systems.CurrentOriginChunk), localCursor, viewW, viewH)
	if !ok {
		return ecs.Entity{}, false
	}
	var best ecs.Entity
	bestDSq := float32(math.MaxFloat32)
	q := filter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		diff := pos.Sub(target)
		dSq := diff.X*diff.X + diff.Z*diff.Z
		if dSq < 1.0 && dSq < bestDSq {
			bestDSq = dSq
			best = q.Entity()
		}
	}
	if vehFilter != nil {
		qv := vehFilter.Query()
		for qv.Next() {
			pos, veh := qv.Get()
			r := components.SpecForVehicle(veh.Kind).ColliderR
			diff := pos.Sub(target)
			dSq := diff.X*diff.X + diff.Z*diff.Z
			if dSq < r*r && dSq < bestDSq {
				bestDSq = dSq
				best = qv.Entity()
			}
		}
	}
	return best, best != (ecs.Entity{})
}

// hoverUnitFromMouse is the silent twin of pickUnitFromMouse - no click.
func hoverUnitFromMouse(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	vehFilter *ecs.Filter2[components.WorldPos, components.Vehicle],
	anchor components.WorldPos, localCursor rl.Vector2, viewW, viewH int32) (ecs.Entity, bool) {
	return pickUnitFromMouse(filter, vehFilter, anchor, localCursor, viewW, viewH)
}

// collectUnitsInRect - marquee selection over units + vehicles. minX/Y/maxX/Y
// are panel-local coords.
func collectUnitsInRect(filter *ecs.Filter3[components.WorldPos, components.Unit, components.Stance],
	vehFilter *ecs.Filter2[components.WorldPos, components.Vehicle],
	minX, maxX, minY, maxY float32, viewW, viewH int32) []ecs.Entity {
	var out []ecs.Entity
	inRect := func(pos *components.WorldPos) bool {
		screen := rl.GetWorldToScreenEx(pos.ToRenderSpace(systems.CurrentOriginChunk),
			systems.CurrentCamera, viewW, viewH)
		return screen.X >= minX && screen.X <= maxX && screen.Y >= minY && screen.Y <= maxY
	}
	q := filter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		if inRect(pos) {
			out = append(out, q.Entity())
		}
	}
	if vehFilter != nil {
		qv := vehFilter.Query()
		for qv.Next() {
			pos, _ := qv.Get()
			if inRect(pos) {
				out = append(out, qv.Entity())
			}
		}
	}
	return out
}

// mouseTargetWorldPos raycasts the panel-local cursor against a horizontal
// plane at the anchor's surface height. viewW/viewH must be the 3D panel's
// dimensions (raylib's GetScreenToWorldRayEx uses them to derive the
// projection matrix). Returns (_, false) on parallel / backward-facing ray.
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

// compactAlive filters dead entities out of a selection-like list in place.
// Units die inside App.Advance while selected (and bind snapshots go stale);
// Ark's Map.Get PANICS on a dead entity, so every list that survives a tick
// must be scrubbed before component derefs.
func compactAlive(world *ecs.World, list []ecs.Entity) []ecs.Entity {
	out := list[:0]
	for _, e := range list {
		if e != (ecs.Entity{}) && world.Alive(e) {
			out = append(out, e)
		}
	}
	return out
}

// bindEntry stores one slot of the Ctrl+1..5 / 1..5 selection-recall ring.
// If Squad is non-zero, recall expands to whoever is currently in the roster
// (binding a squad and then losing members still works). Squad == zero ⇒
// Units is the ad-hoc snapshot taken at bind time.
type bindEntry struct {
	Squad ecs.Entity
	Units []ecs.Entity
}

// SelectionGroups splits a mixed selection into the unique squads to issue
// orders against and the soloists (units with no SquadMember).
type SelectionGroups struct {
	SquadsToOrder []ecs.Entity
	Soloists      []ecs.Entity
}

// groupSelectionByOwner walks `selected`, collects the distinct squads any
// member belongs to, and aggregates the rest as soloists.
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

// detectSubsetOfSquad returns (squad, true) when every entity in `selected`
// belongs to the same squad AND the selection is a *proper* subset of that
// squad's roster. Empty / soloist / multi-squad / whole-squad selections
// return ecs.Entity{}, false.
func detectSubsetOfSquad(
	selected []ecs.Entity,
	squadMemberMap *ecs.Map[components.SquadMember],
	rosterMap *ecs.Map[components.CommandRoster],
) (ecs.Entity, bool) {
	if len(selected) == 0 {
		return ecs.Entity{}, false
	}
	var commonSquad ecs.Entity
	for i, e := range selected {
		sm := squadMemberMap.Get(e)
		if sm == nil || sm.Squad == (ecs.Entity{}) {
			return ecs.Entity{}, false
		}
		if i == 0 {
			commonSquad = sm.Squad
			continue
		}
		if commonSquad != sm.Squad {
			return ecs.Entity{}, false
		}
	}
	roster := rosterMap.Get(commonSquad)
	if roster == nil || uint8(len(selected)) >= roster.Count {
		return ecs.Entity{}, false
	}
	return commonSquad, true
}

// placeIndividualPositions stamps IndividualPosition{Absolute, target} on
// every live unit in `selected`. Existing markers are overwritten.
func placeIndividualPositions(
	world *ecs.World,
	selected []ecs.Entity,
	target components.WorldPos,
	individualPosMap *ecs.Map[components.IndividualPosition],
	now float32,
) {
	for _, e := range selected {
		if e == (ecs.Entity{}) || !world.Alive(e) {
			continue
		}
		ip := components.IndividualPosition{
			Mode:        components.IndividualPosAbsolute,
			AbsolutePos: target,
			AcquiredAt:  now,
		}
		if individualPosMap.Has(e) {
			*individualPosMap.Get(e) = ip
		} else {
			individualPosMap.Add(e, &ip)
		}
	}
}

// groupSelected returns (commonSquad, true) when every selected unit belongs
// to the same squad. commonSquad is zero when every unit is a soloist (still
// homogeneous — caller checks the zero value before routing through SquadService).
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

// stepAlongPath moves the anchor toward path[0] by up to step metres,
// popping waypoints when reached. Y is lerped toward the waypoint's Y so the
// anchor can climb stairs / enter sunken bunkers; GroundStick clamps next tick.
func stepAlongPath(anchorPos *components.WorldPos, path []components.WorldPos, step float32) []components.WorldPos {
	for len(path) > 0 && step > 0 {
		target := path[0]
		diff := target.Sub(*anchorPos)
		dist := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
		if dist <= 0.5 {
			anchorPos.Local.Y = target.Local.Y
			path = path[1:]
			continue
		}
		take := step
		if take > dist {
			take = dist
		}
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
