package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

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
	commsMap       *ecs.Map[components.CommsState]
	squadColor     func(ecs.Entity) rl.Color
}

// drawOrderMarkers3D paints a cube + connector line for every order in the
// queue of each selected squad. Active head = larger/brighter cube; queued =
// smaller/dim; DefendPosition adds a 90° sector arc.
//
// Caller must invoke between BeginMode3D and EndMode3D, after the scene's
// real geometry — depth test is disabled here so markers always show.
func drawOrderMarkers3D(ctx orderMarkerCtx, selected []ecs.Entity) {
	if len(selected) == 0 || ctx.orderQueueMap == nil {
		return
	}
	seen := make(map[ecs.Entity]struct{}, len(selected))
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
		waiting := undeliveredOrder(ctx, squad)
		if (head == nil || head.First == (ecs.Entity{})) && waiting == (ecs.Entity{}) {
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
		if head != nil && head.First != (ecs.Entity{}) {
			drawOrderChainMarkers(ctx, head.First, startRender, col)
		}
		// A plan the net has not carried yet still draws — the player made it
		// and can cancel it. Washed out, because nothing is executing it.
		if waiting != (ecs.Entity{}) {
			pale := col
			pale.A = 90
			drawOrderChainMarkers(ctx, waiting, startRender, pale)
		}
	}
}

func undeliveredOrder(ctx orderMarkerCtx, squad ecs.Entity) ecs.Entity {
	if ctx.commsMap == nil {
		return ecs.Entity{}
	}
	cs := ctx.commsMap.Get(squad)
	if cs == nil || cs.Undelivered == (ecs.Entity{}) || !ctx.world.Alive(cs.Undelivered) {
		return ecs.Entity{}
	}
	return cs.Undelivered
}

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

		rl.DrawLine3D(prevRender, markerRender, rl.Color{R: col.R, G: col.G, B: col.B, A: 180})

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
// rl.BoundingBox using the current origin chunk.
func aabbToRenderBox(aabb components.AABB3D) rl.BoundingBox {
	offX := float32(systems.CurrentOriginChunk.X) * components.ChunkSize
	offZ := float32(systems.CurrentOriginChunk.Z) * components.ChunkSize
	return rl.BoundingBox{
		Min: rl.Vector3{X: aabb.MinX - offX, Y: aabb.MinY, Z: aabb.MinZ - offZ},
		Max: rl.Vector3{X: aabb.MaxX - offX, Y: aabb.MaxY, Z: aabb.MaxZ - offZ},
	}
}

// drawLevelOutline paints a wire-box around `lvl.AABB` in `color`. Caller
// invokes inside BeginMode3D.
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
// ray hits — closest by hit distance. Returns (zero, false) on miss.
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

type unitPathRenderCtx struct {
	filter         *ecs.Filter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember]
	soloFilter     *ecs.Filter3[components.Unit, components.WorldPos, components.MicroPath]
	squadMemberMap *ecs.Map[components.SquadMember]
	// selectedSquad narrows the draw to one squad's members. Zero =
	// every unit gets a path.
	selectedSquad ecs.Entity
}

func unitPathColour(e ecs.Entity) rl.Color {
	h := uint32(e.ID()) * 2654435761
	r := uint8(80 + (h>>0)&0x7F)
	g := uint8(80 + (h>>8)&0x7F)
	b := uint8(80 + (h>>16)&0x7F)
	return rl.Color{R: r, G: g, B: b, A: 230}
}

// drawUnitPaths renders each eligible unit's MicroPath as a line strip from
// the unit's current position through Waypoints[Head..Count). A small cube
// marks the goal cell.
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
// terrain height + 5 cm. Colour LUT lives in navCellColor.
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

// drawCoverMapOverlay draws a translucent blue plate per cell with alpha
// tracking BaseCover. Cells with 0 cover are skipped.
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
// 5 cm above the floor surface. Black = blocked; green = open.
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

// drawLOSPreview renders the hold-V visibility fan: sector quads per visible
// run (alpha = channel falloff at run midpoint), sensor-range ring, weapon
// range rings.
func drawLOSPreview(lp *losPreviewState) {
	if !lp.active || !lp.haveSweep {
		return
	}
	base := lp.origin.ToRenderSpace(systems.CurrentOriginChunk)
	wx := float32(lp.origin.Chunk.X)*components.ChunkSize + lp.origin.Local.X
	wz := float32(lp.origin.Chunk.Z)*components.ChunkSize + lp.origin.Local.Z
	drawVisFan(lp.sampler, wx, wz, base, lp.runs, lp.sensorR, lp.falloff, 30, 90)
	drawTerrainRing(lp.sampler, wx, wz, base, lp.sensorR, rl.Color{R: 240, G: 220, B: 80, A: 200}, false)
	for _, wr := range lp.weaponRs {
		drawTerrainRing(lp.sampler, wx, wz, base, wr, rl.Color{R: 240, G: 120, B: 80, A: 170}, false)
	}
}

// drawSelectionCoverage renders the persistent reach of the current selection
// (Phase 20.7 L1). Dashed = detection, solid red = how far the group is
// heard, solid orange = weapon reach. The fan is dimmer than hold-V's so the
// planning tool and the state indicator stay tellable apart.
func drawSelectionCoverage(cs *coverageState) {
	v := &cs.View
	if !v.Active {
		return
	}
	base := v.Origin.ToRenderSpace(systems.CurrentOriginChunk)
	wx := float32(v.Origin.Chunk.X)*components.ChunkSize + v.Origin.Local.X
	wz := float32(v.Origin.Chunk.Z)*components.ChunkSize + v.Origin.Local.Z
	if len(v.Runs) > 0 {
		drawVisFan(cs.sampler, wx, wz, base, v.Runs, v.VisR, v.Falloff, 14, 52)
	}
	if v.EmitR > 0 {
		eb := v.EmitOrigin.ToRenderSpace(systems.CurrentOriginChunk)
		ex := float32(v.EmitOrigin.Chunk.X)*components.ChunkSize + v.EmitOrigin.Local.X
		ez := float32(v.EmitOrigin.Chunk.Z)*components.ChunkSize + v.EmitOrigin.Local.Z
		col := ui.CoverageEmitColor
		col.A = 200
		drawTerrainRing(cs.sampler, ex, ez, eb, v.EmitR, col, false)
	}
	for _, ring := range v.Rings {
		col := ui.SensorRingColor(ring.Kind)
		col.A = 170
		drawTerrainRing(cs.sampler, wx, wz, base, ring.RadiusM, col, true)
	}
	for _, wr := range v.WeaponRs {
		drawTerrainRing(cs.sampler, wx, wz, base, wr, rl.Color{R: 240, G: 120, B: 80, A: 150}, false)
	}
}

// A run is a radial STRIP, not a quad: only its corners sit on the ground, so
// one quad spanning 60 m dives under every ridge between them. Subdivide to
// the terrain's own resolution and lift clear of z-fighting.
const (
	fanStepM float32 = 4.0
	fanLiftM float32 = 0.3
)

// drawVisFan draws terrain-following sector strips per visible run. Culling is
// off for the pass so one winding reads from any camera side (the batch is
// flushed around the state change — rlgl applies it to pending geometry too).
func drawVisFan(sampler *systems.HeightSampler, wx, wz float32, base rl.Vector3,
	allRuns [][]components.VisRun, rangeM float32, falloff components.FalloffKind,
	aBase, aSpan float32) {
	rays := len(allRuns)
	if rays == 0 {
		return
	}
	yAt := func(dx, dz float32) float32 { return sampler.Sample(wx+dx, wz+dz) + fanLiftM }
	halfStep := math.Pi / float64(rays)
	fill := rl.Color{R: 70, G: 210, B: 130}
	rl.DrawRenderBatchActive()
	rl.DisableBackfaceCulling()
	for r, runs := range allRuns {
		angC := float64(r) * (2 * math.Pi / float64(rays))
		s0 := float32(math.Sin(angC - halfStep))
		c0 := float32(math.Cos(angC - halfStep))
		s1 := float32(math.Sin(angC + halfStep))
		c1 := float32(math.Cos(angC + halfStep))
		corner := func(s, c, d float32) rl.Vector3 {
			return rl.Vector3{X: base.X + s*d, Y: yAt(s*d, c*d), Z: base.Z + c*d}
		}
		for _, run := range runs {
			t0 := run.T0
			if t0 < 0.6 {
				t0 = 0.6
			}
			if run.T1 <= t0 {
				continue
			}
			t1 := run.T1
			a := components.Falloff(falloff, (t0+t1)*0.5, rangeM)
			col := fill
			col.A = uint8(aBase + aSpan*a)
			segs := int((t1-t0)/fanStepM) + 1
			v00, v01 := corner(s0, c0, t0), corner(s1, c1, t0)
			for s := 1; s <= segs; s++ {
				d := t0 + (t1-t0)*float32(s)/float32(segs)
				v10, v11 := corner(s0, c0, d), corner(s1, c1, d)
				rl.DrawTriangle3D(v00, v01, v11, col)
				rl.DrawTriangle3D(v00, v11, v10, col)
				v00, v01 = v10, v11
			}
		}
	}
	rl.DrawRenderBatchActive()
	rl.EnableBackfaceCulling()
}

// drawTerrainRing draws a terrain-following circle; segment count is set by ARC
// length, not radius, so a 700 m ESM ring hugs the ground as closely as a 40 m
// optical one. Dashed = detection semantics.
func drawTerrainRing(sampler *systems.HeightSampler, wx, wz float32, base rl.Vector3,
	radius float32, col rl.Color, dashed bool) {
	segs := int(2 * math.Pi * float64(radius) / float64(fanStepM))
	if segs < 96 {
		segs = 96
	}
	if segs > 1024 {
		segs = 1024
	}
	segs &^= 1
	pt := func(i int) rl.Vector3 {
		ang := float64(i) * (2 * math.Pi / float64(segs))
		s := float32(math.Sin(ang))
		c := float32(math.Cos(ang))
		return rl.Vector3{
			X: base.X + s*radius,
			Y: sampler.Sample(wx+s*radius, wz+c*radius) + 0.2,
			Z: base.Z + c*radius,
		}
	}
	for i := 0; i < segs; i++ {
		if dashed && i%2 == 1 {
			continue
		}
		rl.DrawLine3D(pt(i), pt(i+1), col)
	}
}
