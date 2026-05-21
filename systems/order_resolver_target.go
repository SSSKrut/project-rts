package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// resolveTargetPos refreshes OrderTarget.Pos when the target is an entity
// (Garrison -> Building, OccupyTrench -> TrenchRoot). For Pos-only kinds it's
// a no-op.
func (sys *OrderResolverSystem) resolveTargetPos(kind components.OrderKindCode, target *components.OrderTarget) {
	if target.Entity == (ecs.Entity{}) {
		return
	}
	// Phase 14.6 M14.6.0 (Issue #11): refuse Map.Get on a dead entity id.
	// Building / TrenchRoot don't die in Phase 14.6 (root entities carry
	// AlwaysActive), but the guard is cheap and protects against future
	// targets that do.
	if !sys.squadService.world.Alive(target.Entity) {
		return
	}
	switch kind {
	case components.OrderKindGarrison:
		if b := sys.buildingMap.Get(target.Entity); b != nil {
			// Phase 14.6 followup - prefer the ground-floor (lowest Level)
			// child's WorldPos. NavService.resolveNode matches it as a
			// NodeLevel, so A* routes through a Door TransitionEdge instead
			// of dead-ending at a NavInBuilding surface cell.
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

// updateProgress writes a rough 0..1 progress value for the head order.
// Currently 1 - dist/initialDist, computed against OrderIssuedAt as anchor
// (no extra component needed for "initial" position; we use squad center at
// issuance via the issued-at clock + a per-tick recompute would drift).
// Approximation: clamp(1 - cur/100m, 0, 1) - coarse but enough for Inspector
// progress bars. Real progress accounting comes with Phase 13's Pace param.
func (sys *OrderResolverSystem) updateProgress(squad, ord ecs.Entity, target *components.OrderTarget) {
	pr := sys.orderProgressMap.Get(ord)
	if pr == nil {
		return
	}
	// Phase 14.6 M14.6.2: Garrison writes inside/alive into Progress.Value
	// directly from evaluateCompletion. Don't clobber it with a distance-
	// from-center fraction here.
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

// applyArrivedFacing reads the optional OrderParamFacing on a freshly-
// completed order and snaps every roster member's Motion.Yaw to the requested
// yaw. Phase 13.6 M13.6.4: instant rotation - no easing. Phase 25 polish may
// interpolate; the writer side is the same, just the reader (UnitMovement)
// becomes lerp-aware.
//
// No-op when the order has no facing param, the squad has no roster, or a
// member lacks a Motion component (defensive - Phase 7 spawns guarantee it).
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
// `building`. Used by resolveTargetPos to anchor a Garrison goal on a Floor
// NavNode (reachable through Door TransitionEdges) rather than a surface
// NavInBuilding cell that A* would refuse. (_, false) when the building has
// no live Floor children (chunk evicted, or layout has not yet generated).
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
// the building's Footprint AABB AND on a Floor entity (any storey). Returns
// (alive, inside). Phase 14.6 M14.6.2 - the Garrison
// CompletionEveryMemberOnFloor arm uses this to gate Done / Failed
// transitions and to write a per-member progress fraction into
// OrderProgress.Value.
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

// memberOnFloor returns true when `pos` sits over the horizontal extent of
// any live Floor plate AND its Y is within +/-1.5 m of the floor surface.
// The Y proximity catches both surface stories (member Y ~ floor Y) and
// bunkers (member Y dropped into the sunken floor). Cheap: 1-3 buildings x
// 1-3 floors per scene = handful of plate checks per call.
func (sys *OrderResolverSystem) memberOnFloor(pos *components.WorldPos) bool {
	mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	q := sys.floorFilter.Query()
	for q.Next() {
		fpos, floor := q.Get()
		fx := float32(fpos.Chunk.X)*components.ChunkSize + fpos.Local.X
		fz := float32(fpos.Chunk.Z)*components.ChunkSize + fpos.Local.Z
		if mx < fx || mx > fx+floor.SizeX || mz < fz || mz > fz+floor.SizeZ {
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

// pointNearPolyline returns true when p is within `radius` of any segment of
// the polyline. Reused by trench arrival and the hit-test resolver.
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
