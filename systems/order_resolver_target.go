package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// resolveTargetPos refreshes OrderTarget.Pos when the target is an entity
// (Garrison → Building, OccupyTrench → TrenchRoot). No-op for Pos-only kinds.
func (sys *OrderResolverSystem) resolveTargetPos(kind components.OrderKindCode, target *components.OrderTarget) {
	if target.Entity == (ecs.Entity{}) {
		return
	}
	if !sys.squadService.world.Alive(target.Entity) {
		return
	}
	switch kind {
	case components.OrderKindGarrison, components.OrderKindOccupyBuilding, components.OrderKindClearBuilding:
		// Anchor on the lowest Floor NavNode (NodeLevel) so A* routes
		// through a Door TransitionEdge instead of dead-ending at a
		// NavInBuilding surface cell.
		if b := sys.buildingMap.Get(target.Entity); b != nil {
			if fp, ok := sys.firstFloorPos(target.Entity); ok {
				target.Pos = fp
			} else {
				cx := b.Footprint.CenterX()
				cz := b.Footprint.CenterZ()
				target.Pos = (components.WorldPos{}).Add(rlVec3XZ(cx, cz))
			}
		}
	case components.OrderKindOccupyTrench:
		if root := sys.trenchRootMap.Get(target.Entity); root != nil {
			tn := sys.trenchResource.Get()
			if tn != nil && root.Index >= 0 && root.Index < len(tn.Lines) {
				line := &tn.Lines[root.Index]
				if len(line.Points) > 0 {
					target.Pos = line.Points[len(line.Points)/2]
				}
			}
		}
	}
}

// updateProgress writes a rough 0..1 progress value: clamp(1 - dist/100m, 0, 1).
// Garrison writes inside/alive directly in evaluateCompletion and is skipped.
func (sys *OrderResolverSystem) updateProgress(squad, ord ecs.Entity, target *components.OrderTarget) {
	pr := sys.orderProgressMap.Get(ord)
	if pr == nil {
		return
	}
	if kind := sys.orderKindMap.Get(ord); kind != nil &&
		components.SpecForOrderKind(kind.Code).Completion == components.CompletionEveryMemberOnFloor {
		return
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	center, ok := SquadCenter(sys.squadService.world, roster, sys.posMap)
	if !ok {
		return
	}
	d := math.Sqrt(float64(centerXZDistSq(center, target.Pos)))
	const fullRange = 100.0
	v := float32(1 - d/fullRange)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	pr.Value = v
}

// applyArrivedFacing snaps every roster member's Motion.Yaw to the
// OrderParamFacing yaw on completion. Instant rotation, no easing.
func (sys *OrderResolverSystem) applyArrivedFacing(squad, ord ecs.Entity) {
	if sys.orderFacingMap == nil || sys.motionMap == nil {
		return
	}
	facing := sys.orderFacingMap.Get(ord)
	if facing == nil {
		return
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		if m := sys.motionMap.Get(mem); m != nil {
			m.Yaw = facing.YawRad
		}
	}
}

// firstFloorPos returns the WorldPos of the lowest-Level Floor child of
// `building` — anchors the Garrison goal on a Floor NavNode reachable
// through Door TransitionEdges. (_, false) when no live Floor children.
func (sys *OrderResolverSystem) firstFloorPos(building ecs.Entity) (components.WorldPos, bool) {
	idx := sys.buildingChildIndex.Get()
	if idx == nil {
		return components.WorldPos{}, false
	}
	children, ok := idx.Loaded[building]
	if !ok || len(children) == 0 {
		return components.WorldPos{}, false
	}
	var bestPos components.WorldPos
	bestLevel := uint8(255)
	found := false
	for _, c := range children {
		floor := sys.floorComponentMap.Get(c)
		if floor == nil {
			continue
		}
		pos := sys.posMap.Get(c)
		if pos == nil {
			continue
		}
		if !found || floor.Level < bestLevel {
			bestPos = *pos
			bestLevel = floor.Level
			found = true
		}
	}
	return bestPos, found
}

// countInsideBuilding tallies how many of `roster`'s live members sit inside
// the building's Footprint AABB AND on a Floor entity (any storey).
func (sys *OrderResolverSystem) countInsideBuilding(
	roster *components.CommandRoster, footprint components.AABB2D,
) (alive, inside uint8) {
	if roster == nil {
		return 0, 0
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.squadService.world.Alive(mem) {
			continue
		}
		pos := sys.posMap.Get(mem)
		if pos == nil {
			continue
		}
		alive++
		mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		if mx < footprint.MinX || mx > footprint.MaxX || mz < footprint.MinZ || mz > footprint.MaxZ {
			continue
		}
		if sys.memberOnFloor(pos) {
			inside++
		}
	}
	return alive, inside
}

// memberOnFloor returns true when `pos` sits over a live Floor plate AND its
// Y is within ±1.5 m of the floor surface. Floor WorldPos is the plate
// centre, so the extent is [centre ± Size/2] (same convention as
// GroundStick). Treating the WorldPos as a corner shifts the match by
// +Size/2 and breaks OccupyBuilding completion for units in the lower-X /
// lower-Z half of the plate.
func (sys *OrderResolverSystem) memberOnFloor(pos *components.WorldPos) bool {
	mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	q := sys.floorFilter.Query()
	for q.Next() {
		fpos, floor := q.Get()
		fx := float32(fpos.Chunk.X)*components.ChunkSize + fpos.Local.X
		fz := float32(fpos.Chunk.Z)*components.ChunkSize + fpos.Local.Z
		halfX := floor.SizeX * 0.5
		halfZ := floor.SizeZ * 0.5
		if mx < fx-halfX || mx > fx+halfX || mz < fz-halfZ || mz > fz+halfZ {
			continue
		}
		dy := pos.Local.Y - fpos.Local.Y
		if dy > -1.5 && dy < 1.5 {
			q.Close()
			return true
		}
	}
	return false
}

// countHostilesInBuilding tallies live Units whose Faction differs from
// `ownFaction` and whose XZ position lies inside `footprint`. Walks the unit
// filter (every live unit, not just the issuing squad's roster).
func (sys *OrderResolverSystem) countHostilesInBuilding(
	footprint components.AABB2D, ownFaction uint8,
) uint8 {
	q := sys.unitFilter.Query()
	var hostiles uint8
	for q.Next() {
		_, pos := q.Get()
		ent := q.Entity()
		f := sys.factionMap.Get(ent)
		if f == nil || f.ID == ownFaction {
			continue
		}
		mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		if mx < footprint.MinX || mx > footprint.MaxX || mz < footprint.MinZ || mz > footprint.MaxZ {
			continue
		}
		hostiles++
		if hostiles == 255 {
			break // saturate; the completion only cares about == 0
		}
	}
	return hostiles
}

// pointNearPolyline returns true when p is within `radius` of any segment.
func pointNearPolyline(p components.WorldPos, points []components.WorldPos, radius float32) bool {
	if len(points) < 2 {
		return false
	}
	rSq := radius * radius
	px := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
	pz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
	for i := 1; i < len(points); i++ {
		ax := float32(points[i-1].Chunk.X)*components.ChunkSize + points[i-1].Local.X
		az := float32(points[i-1].Chunk.Z)*components.ChunkSize + points[i-1].Local.Z
		bx := float32(points[i].Chunk.X)*components.ChunkSize + points[i].Local.X
		bz := float32(points[i].Chunk.Z)*components.ChunkSize + points[i].Local.Z
		dx := bx - ax
		dz := bz - az
		lenSq := dx*dx + dz*dz
		if lenSq < 1e-6 {
			continue
		}
		t := ((px-ax)*dx + (pz-az)*dz) / lenSq
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		cx := ax + dx*t
		cz := az + dz*t
		if (px-cx)*(px-cx)+(pz-cz)*(pz-cz) < rSq {
			return true
		}
	}
	return false
}
