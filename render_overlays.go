package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// orderMarkerCtx bundles the per-frame handles drawOrderMarkers3D needs.
// Built once in main.go (alongside ghostCtx) and reused per frame.
type orderMarkerCtx struct {
	world          *ecs.World
	posMap         *ecs.Map[components.WorldPos]
	rosterMap      *ecs.Map[components.CommandRoster]
	squadMemberMap *ecs.Map[components.SquadMember]
	orderQueueMap  *ecs.Map[components.OrderQueueHead]
	orderKindMap   *ecs.Map[components.OrderKind]
	orderTargetMap *ecs.Map[components.OrderTarget]
	orderChainMap  *ecs.Map[components.OrderChain]
	orderFacingMap *ecs.Map[components.OrderParamFacing]
	squadColor     func(ecs.Entity) rl.Color
}

// drawOrderMarkers3D paints a cube + connector line for every order in the
// queue of each selected squad. Phase 17.6 M17.6.7: depth-test is disabled so
// markers are visible through walls / hills — standard RTS convention.
//
// Marker shape:
//   - Active (head) order: 0.5 m solid cube + wire outline at +1.0 alpha
//   - Queued: 0.35 m cube at +0.5 alpha
//   - DefendPosition: extra 90° sector arc oriented by OrderParamFacing
//   - Connector line: squad center → head marker; previous → next for chain
//
// Caller must invoke between BeginMode3D and EndMode3D, after the scene's
// real geometry, so marker depth-disable doesn't bleed into terrain depth.
func drawOrderMarkers3D(ctx orderMarkerCtx, selected []ecs.Entity) {
	if len(selected) == 0 || ctx.orderQueueMap == nil {
		return
	}
	// Collect unique squads touched by selection.
	seen := make(map[ecs.Entity]struct{}, len(selected))
	// Disable depth test so markers are always visible — standard RTS
	// convention for command icons (Wargame / Combat Mission). Re-enable
	// at function exit so subsequent passes (particles, etc.) keep depth.
	rl.DisableDepthTest()
	defer rl.EnableDepthTest()
	for _, e := range selected {
		sm := ctx.squadMemberMap.Get(e)
		if sm == nil || sm.Squad == (ecs.Entity{}) {
			continue
		}
		squad := sm.Squad
		if _, dup := seen[squad]; dup {
			continue
		}
		seen[squad] = struct{}{}
		if !ctx.world.Alive(squad) {
			continue
		}
		head := ctx.orderQueueMap.Get(squad)
		if head == nil || head.First == (ecs.Entity{}) {
			continue
		}
		roster := ctx.rosterMap.Get(squad)
		if roster == nil {
			continue
		}
		center, ok := systems.SquadCenter(ctx.world, roster, ctx.posMap)
		if !ok {
			continue
		}
		startRender := center.ToRenderSpace(systems.CurrentOriginChunk)
		startRender.Y += 0.5
		col := rl.Color{R: 200, G: 220, B: 240, A: 230}
		if ctx.squadColor != nil {
			col = ctx.squadColor(squad)
		}
		drawOrderChainMarkers(ctx, head.First, startRender, col)
	}
}

// drawOrderChainMarkers walks the OrderChain starting at `ord`, drawing each
// marker + connector. Head marker (the first call) is the "active" style;
// every subsequent marker is the "queued" style.
func drawOrderChainMarkers(ctx orderMarkerCtx, ord ecs.Entity, prevRender rl.Vector3, col rl.Color) {
	active := true
	cur := ord
	guard := 0
	for cur != (ecs.Entity{}) && guard < 16 {
		guard++
		if !ctx.world.Alive(cur) {
			return
		}
		target := ctx.orderTargetMap.Get(cur)
		if target == nil {
			return
		}
		markerRender := target.Pos.ToRenderSpace(systems.CurrentOriginChunk)
		markerRender.Y += 0.6

		// Connector line previous → current marker. Slight Y offset keeps
		// the line above ground without intersecting the cube.
		rl.DrawLine3D(prevRender, markerRender, rl.Color{R: col.R, G: col.G, B: col.B, A: 180})

		// Marker cube + outline. Active style is bigger + brighter.
		size := float32(0.35)
		alpha := uint8(140)
		if active {
			size = 0.5
			alpha = 220
		}
		cubeCol := rl.Color{R: col.R, G: col.G, B: col.B, A: alpha}
		rl.DrawCubeV(markerRender, rl.Vector3{X: size, Y: size, Z: size}, cubeCol)
		rl.DrawCubeWiresV(markerRender, rl.Vector3{X: size, Y: size, Z: size},
			rl.Color{R: col.R, G: col.G, B: col.B, A: 240})

		// DefendPosition arc: orient via OrderParamFacing if present.
		if kind := ctx.orderKindMap.Get(cur); kind != nil && kind.Code == components.OrderKindDefendPosition {
			yaw := float32(0)
			if facing := ctx.orderFacingMap.Get(cur); facing != nil {
				yaw = facing.YawRad
			}
			arcCol := rl.Color{R: col.R, G: col.G, B: col.B, A: 80}
			drawGhostArc(markerRender, yaw, math.Pi/4, 8, arcCol)
		}

		prevRender = markerRender
		active = false
		if ch := ctx.orderChainMap.Get(cur); ch != nil {
			cur = ch.Next
		} else {
			cur = ecs.Entity{}
		}
	}
}

// aabbToRenderBox converts a world-space AABB3D into a render-space
// rl.BoundingBox using the current origin chunk. Phase 17.6 M17.6.8:
// shared by ray-vs-floor pick + per-level outline draw so both use the
// same coordinate frame.
func aabbToRenderBox(aabb components.AABB3D) rl.BoundingBox {
	offX := float32(systems.CurrentOriginChunk.X) * components.ChunkSize
	offZ := float32(systems.CurrentOriginChunk.Z) * components.ChunkSize
	return rl.BoundingBox{
		Min: rl.Vector3{X: aabb.MinX - offX, Y: aabb.MinY, Z: aabb.MinZ - offZ},
		Max: rl.Vector3{X: aabb.MaxX - offX, Y: aabb.MaxY, Z: aabb.MaxZ - offZ},
	}
}

// drawLevelOutline paints a wire-box around `lvl.AABB` in `color`. Phase
// 17.6 M17.6.8: per-floor highlight when the cursor's ray hits a specific
// storey of a building. Caller invokes inside BeginMode3D.
func drawLevelOutline(lvl *components.Level, color rl.Color) {
	if lvl == nil {
		return
	}
	const pad float32 = 0.10
	box := aabbToRenderBox(lvl.AABB)
	cx := (box.Min.X + box.Max.X) * 0.5
	cy := (box.Min.Y + box.Max.Y) * 0.5
	cz := (box.Min.Z + box.Max.Z) * 0.5
	sx := (box.Max.X - box.Min.X) + 2*pad
	sy := (box.Max.Y - box.Min.Y) + 2*pad
	sz := (box.Max.Z - box.Min.Z) + 2*pad
	rl.DrawCubeWires(rl.Vector3{X: cx, Y: cy, Z: cz}, sx, sy, sz, color)
}

// pickLevelUnderRay finds the topmost Level entity of `building` that the
// ray hits — "topmost" by closest hit distance (ray nearest-first). Returns
// (entity, true) on hit; (zero, false) if the building has no levels or
// the ray misses all level boxes. Phase 17.6 M17.6.8: routes the floor
// outline + future popup defaults at the level the cursor is visually on.
func pickLevelUnderRay(
	ray rl.Ray,
	building ecs.Entity,
	planIndex *systems.BuildingPlanIndex,
	levelMap *ecs.Map[components.Level],
) (ecs.Entity, bool) {
	if planIndex == nil || levelMap == nil || building == (ecs.Entity{}) {
		return ecs.Entity{}, false
	}
	levels := planIndex.Levels[building]
	if len(levels) == 0 {
		return ecs.Entity{}, false
	}
	var best ecs.Entity
	bestDist := float32(math.MaxFloat32)
	for _, l := range levels {
		lvl := levelMap.Get(l)
		if lvl == nil {
			continue
		}
		box := aabbToRenderBox(lvl.AABB)
		col := rl.GetRayCollisionBox(ray, box)
		if !col.Hit {
			continue
		}
		if col.Distance < bestDist {
			bestDist = col.Distance
			best = l
		}
	}
	return best, best != (ecs.Entity{})
}

// unitPathRenderCtx bundles the handles drawUnitPaths needs. Built once
// per frame in main.go; the filters are reused across ticks.
type unitPathRenderCtx struct {
	filter         *ecs.Filter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember]
	soloFilter     *ecs.Filter3[components.Unit, components.WorldPos, components.MicroPath]
	squadMemberMap *ecs.Map[components.SquadMember]
	// selectedSquad narrows the draw to one squad's members. Zero =
	// every unit gets a path (used when the player hasn't selected anyone).
	selectedSquad ecs.Entity
}

// unitPathColour hashes the entity ID into a stable bright hue. 8 squad
// members visibly diverge.
func unitPathColour(e ecs.Entity) rl.Color {
	h := uint32(e.ID()) * 2654435761
	r := uint8(80 + (h>>0)&0x7F)
	g := uint8(80 + (h>>8)&0x7F)
	b := uint8(80 + (h>>16)&0x7F)
	return rl.Color{R: r, G: g, B: b, A: 230}
}

// drawUnitPaths renders each eligible unit's MicroPath as a line strip from
// the unit's current position through Waypoints[Head..Count). A small cube
// marks the goal cell. Colour is per-unit hashed so overlapping paths in a
// squad of 8 stay distinguishable.
//
// Selection-aware: if `ctx.selectedSquad` is set we draw only that squad's
// members; otherwise every unit gets a path. This keeps the overlay readable
// when the player is examining one squad's stragglers.
func drawUnitPaths(ctx unitPathRenderCtx) {
	originChunk := systems.CurrentOriginChunk
	q := ctx.filter.Query()
	for q.Next() {
		_, pos, mp, member := q.Get()
		ent := q.Entity()
		if ctx.selectedSquad != (ecs.Entity{}) && member.Squad != ctx.selectedSquad {
			continue
		}
		if mp.Count == 0 || mp.Head >= mp.Count {
			continue
		}
		drawUnitPathLineStrip(ent, *pos, mp, originChunk)
	}
	// Soloists (units with no SquadMember). Skipped when a specific squad
	// is selected — soloists by definition aren't in it.
	if ctx.selectedSquad == (ecs.Entity{}) {
		qs := ctx.soloFilter.Query()
		for qs.Next() {
			_, pos, mp := qs.Get()
			ent := qs.Entity()
			if ctx.squadMemberMap.Has(ent) {
				continue
			}
			if mp.Count == 0 || mp.Head >= mp.Count {
				continue
			}
			drawUnitPathLineStrip(ent, *pos, mp, originChunk)
		}
	}
}

// drawUnitPathLineStrip emits one polyline from unit pos through every live
// waypoint plus a small goal-marker cube at the last waypoint.
func drawUnitPathLineStrip(ent ecs.Entity, pos components.WorldPos,
	mp *components.MicroPath, originChunk components.ChunkCoord) {
	col := unitPathColour(ent)
	prev := pos.ToRenderSpace(originChunk)
	prev.Y += 0.3
	for i := mp.Head; i < mp.Count; i++ {
		p := mp.Waypoints[i].ToRenderSpace(originChunk)
		p.Y += 0.3
		rl.DrawLine3D(prev, p, col)
		prev = p
	}
	rl.DrawCubeV(prev, rl.Vector3{X: 0.3, Y: 0.3, Z: 0.3}, col)
}

// drawNavPath renders the path from the anchor through every remaining
// waypoint. Empty/nil paths render nothing. Lifted slightly so the line is
// visible against the terrain.
func drawNavPath(path []components.WorldPos, anchorPos components.WorldPos) {
	if len(path) == 0 {
		return
	}
	prev := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)
	prev.Y += 0.4
	for i := range path {
		p := path[i].ToRenderSpace(systems.CurrentOriginChunk)
		p.Y += 0.4
		rl.DrawLine3D(prev, p, rl.Magenta)
		prev = p
	}
	target := path[0].ToRenderSpace(systems.CurrentOriginChunk)
	target.Y += 0.4
	rl.DrawCircle3D(target, 0.6, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Magenta)
}

// drawNavGridOverlay draws a flat coloured plate per NavCell at the cell's
// terrain height + 5 cm. Cell size is 1 m; plates are sized to 0.9 m so
// neighbouring cells visibly separate. Colour LUT:
//
//	Cost = 0           -> black (impassable)
//	Cost = navCostRoad -> light blue (OnRoad - M6.4)
//	Cost = navCostOpen -> green (open field)
//	Cost = navCostRough -> yellow-orange (rough)
//	Cost = navCostTrench -> orange (trench - M6.4)
//	other              -> magenta (unknown)
func drawNavGridOverlay(chunkPos components.WorldPos, cc components.ChunkCoord,
	grid *components.NavGrid, hm *components.Heightmap) {
	chunkRender := chunkPos.ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.9
	const lift float32 = 0.05
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			cell := grid.Cells[cj*components.NavGridSide+ci]
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			centerY := (h00 + h10 + h01 + h11) * 0.25

			col := navCellColor(cell)
			c := rl.Vector3{
				X: chunkRender.X + float32(ci) + 0.5,
				Y: centerY + lift,
				Z: chunkRender.Z + float32(cj) + 0.5,
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate}, col)
		}
	}
	_ = cc
}

// drawCoverMapOverlay - same shape as drawNavGridOverlay but with a single
// translucent blue plate per cell whose alpha tracks BaseCover. 0 cover ->
// fully transparent (cell skipped); 8 covered directions -> 90% blue.
func drawCoverMapOverlay(chunkPos components.WorldPos, cc components.ChunkCoord,
	cov *components.CoverMap, hm *components.Heightmap) {
	chunkRender := chunkPos.ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.9
	const lift float32 = 0.06
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			cell := cov.Cells[cj*components.NavGridSide+ci]
			if cell.BaseCover == 0 {
				continue
			}
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			centerY := (h00 + h10 + h01 + h11) * 0.25

			alpha := uint8(int(cell.BaseCover))
			c := rl.Vector3{
				X: chunkRender.X + float32(ci) + 0.5,
				Y: centerY + lift,
				Z: chunkRender.Z + float32(cj) + 0.5,
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate},
				rl.Color{R: 60, G: 110, B: 230, A: alpha})
		}
	}
	_ = cc
}

// drawFloorNavOverlay renders one LevelNavGrid as a layer of coloured plates
// 5 cm above the floor surface. Black = Cost=0 (wall / blocked); green = open.
func drawFloorNavOverlay(floorPos components.WorldPos, grid *components.LevelNavGrid) {
	chunkBase := (components.WorldPos{Chunk: floorPos.Chunk}).ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.85
	const lift float32 = 0.05
	for cj := uint8(0); cj < grid.SizeZ; cj++ {
		for ci := uint8(0); ci < grid.SizeX; ci++ {
			cell := grid.Cells[int(cj)*components.MaxLevelSide+int(ci)]
			c := rl.Vector3{
				X: chunkBase.X + grid.Origin.X + float32(ci) + 0.5,
				Y: grid.Origin.Y + lift,
				Z: chunkBase.Z + grid.Origin.Z + float32(cj) + 0.5,
			}
			col := rl.Color{R: 80, G: 200, B: 80, A: 150}
			if cell.Cost == 0 {
				col = rl.Color{R: 20, G: 20, B: 20, A: 220}
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate}, col)
		}
	}
}

func navCellColor(cell components.NavCell) rl.Color {
	if cell.Cost == 0 {
		return rl.Color{R: 0, G: 0, B: 0, A: 200}
	}
	if cell.Flags&components.NavInTrench != 0 {
		return rl.Color{R: 230, G: 130, B: 30, A: 170}
	}
	if cell.Flags&components.NavOnRoad != 0 {
		return rl.Color{R: 120, G: 180, B: 230, A: 170}
	}
	switch cell.Cost {
	case 4:
		return rl.Color{R: 80, G: 200, B: 80, A: 150}
	case 8:
		return rl.Color{R: 230, G: 200, B: 60, A: 170}
	default:
		return rl.Color{R: 220, G: 60, B: 220, A: 200}
	}
}

// drawRoadGraphDebug overlays the road graph in 3D - one coloured line per
// edge, plus a small marker cube at each node. Colour by RoadKind: white
// Highway, blue Local, brown DirtTrack, yellow Bridge.
func drawRoadGraphDebug(g *components.RoadGraph) {
	for i := range g.Edges {
		e := &g.Edges[i]
		from := g.Nodes[e.From].Pos
		to := g.Nodes[e.To].Pos
		fr := from.ToRenderSpace(systems.CurrentOriginChunk)
		tr := to.ToRenderSpace(systems.CurrentOriginChunk)
		// Lift slightly so the line isn't buried in the road surface.
		fr.Y += 0.5
		tr.Y += 0.5
		var col rl.Color
		switch e.Kind {
		case components.RoadHighway:
			col = rl.White
		case components.RoadLocal:
			col = rl.Blue
		case components.RoadDirtTrack:
			col = rl.Brown
		case components.RoadBridge:
			col = rl.Yellow
		default:
			col = rl.Magenta
		}
		rl.DrawLine3D(fr, tr, col)
	}
	for i := range g.Nodes {
		p := g.Nodes[i].Pos.ToRenderSpace(systems.CurrentOriginChunk)
		p.Y += 0.5
		rl.DrawCube(p, 0.6, 0.6, 0.6, rl.Black)
	}
}
