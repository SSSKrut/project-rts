package systems

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// math.Min / Max are float64; these are the float32 helpers.
func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// bakeTransitionsPass is Pass 4: rebuild TransitionRegistry from the live
// set of Doors / Stairs / bunker entrances. Connects surface↔level and
// level↔level NavNodes. Full rebuild every tick that any chunk bakes —
// edge counts are small (<~20) so it beats per-owner invalidation.
func (sys *SpatialBakeSystem) bakeTransitionsPass(ctx core.UpdateContext) {
	_ = ctx
	registry := sys.transitionRes.Get()
	if registry == nil {
		return
	}

	for k := range registry.Out {
		delete(registry.Out, k)
	}

	// Levels are AlwaysActive; filter only by "has LevelNavGrid".
	type levelSnapshot struct {
		ent     ecs.Entity
		chunk   components.ChunkCoord
		aabb    components.AABB3D
		originX float32
		originZ float32
		sizeX   uint8
		sizeZ   uint8
	}
	var levels []levelSnapshot
	qL2 := sys.levelFilterAll.Query()
	for qL2.Next() {
		lvl, pos := qL2.Get()
		grid := sys.floorNavMap.Get(qL2.Entity())
		if grid == nil {
			continue
		}
		chunkBaseX := float32(pos.Chunk.X) * components.ChunkSize
		chunkBaseZ := float32(pos.Chunk.Z) * components.ChunkSize
		levels = append(levels, levelSnapshot{
			ent:     qL2.Entity(),
			chunk:   pos.Chunk,
			aabb:    lvl.AABB,
			originX: lvl.AABB.MinX - chunkBaseX,
			originZ: lvl.AABB.MinZ - chunkBaseZ,
			sizeX:   grid.SizeX,
			sizeZ:   grid.SizeZ,
		})
	}
	if len(levels) == 0 {
		return
	}

	// Nearest-MinY pick, same rule as NavService.resolveNode: stacked storeys
	// share a boundary plane, so first-match binds a storey-1 partition door's
	// samples to L0 and bakes a phantom L0↔L1 "door" through the ceiling
	// (ISSUES #21).
	findLevel := func(worldX, worldZ, y float32) *levelSnapshot {
		const yPad float32 = 0.6
		var best *levelSnapshot
		bestDY := float32(math.MaxFloat32)
		for i := range levels {
			lv := &levels[i]
			if !lv.aabb.ContainsXZ(worldX, worldZ) {
				continue
			}
			if y < lv.aabb.MinY-yPad || y > lv.aabb.MaxY+yPad {
				continue
			}
			if dy := absF(y - lv.aabb.MinY); dy < bestDY {
				bestDY = dy
				best = lv
			}
		}
		return best
	}
	levelCell := func(lv *levelSnapshot, worldX, worldZ float32) (int16, int16, bool) {
		chunkBaseX := float32(lv.chunk.X) * components.ChunkSize
		chunkBaseZ := float32(lv.chunk.Z) * components.ChunkSize
		lx := worldX - (chunkBaseX + lv.originX)
		lz := worldZ - (chunkBaseZ + lv.originZ)
		i := int16(math.Floor(float64(lx)))
		j := int16(math.Floor(float64(lz)))
		if i < 0 || j < 0 || i >= int16(lv.sizeX) || j >= int16(lv.sizeZ) {
			return 0, 0, false
		}
		return i, j, true
	}

	type doorSnap struct {
		ent     ecs.Entity
		pos     components.WorldPos
		w       components.WallSegment
		outward rl.Vector3
		level   ecs.Entity
	}
	var doors []doorSnap
	qDW := sys.wallFilter.Query()
	for qDW.Next() {
		pos, w := qDW.Get()
		if w.OpeningKind != components.OpeningDoor {
			continue
		}
		e := qDW.Entity()
		var outward rl.Vector3
		if cd := sys.coverDirMap.Get(e); cd != nil {
			outward = cd.Dir
		}
		var lev ecs.Entity
		if lm := sys.levelMemberMap.Get(e); lm != nil {
			lev = lm.Level
		}
		doors = append(doors, doorSnap{ent: e, pos: *pos, w: *w, outward: outward, level: lev})
	}

	type stairsSnap struct {
		ent     ecs.Entity
		pos     components.WorldPos
		s       components.Stairs
		fromLev ecs.Entity
		toLev   ecs.Entity
	}
	var stairs []stairsSnap
	qS := sys.stairsFilter.Query()
	for qS.Next() {
		pos, s := qS.Get()
		e := qS.Entity()
		var from, to ecs.Entity
		if sl := sys.stairLevelsMap.Get(e); sl != nil {
			from = sl.From
			to = sl.To
		}
		stairs = append(stairs, stairsSnap{ent: e, pos: *pos, s: *s, fromLev: from, toLev: to})
	}

	edgeCounts := struct{ surfLevel, levelLevel int }{}
	addEdge := func(from, to components.NavNode, cost uint8, owner ecs.Entity) {
		registry.Out[from] = append(registry.Out[from], components.TransitionEdge{
			From: from, To: to, Cost: cost, Owner: owner,
		})
		if from.Kind == components.NodeSurface || to.Kind == components.NodeSurface {
			edgeCounts.surfLevel++
		} else {
			edgeCounts.levelLevel++
		}
	}

	levelByEntity := func(ent ecs.Entity) *levelSnapshot {
		for i := range levels {
			if levels[i].ent == ent {
				return &levels[i]
			}
		}
		return nil
	}

	// 1 bidirectional edge per door between surface cell outside and level
	// cell inside.
	for _, d := range doors {
		sa := float32(math.Sin(float64(d.w.Yaw)))
		ca := float32(math.Cos(float64(d.w.Yaw)))
		centreT := d.w.OpeningCenterT * d.w.Length
		baseX := float32(d.pos.Chunk.X) * components.ChunkSize
		baseZ := float32(d.pos.Chunk.Z) * components.ChunkSize
		cx := baseX + d.pos.Local.X + sa*centreT
		cz := baseZ + d.pos.Local.Z + ca*centreT

		// Prefer LevelMember; fall back to containing-AABB scan.
		var lv *levelSnapshot
		if d.level != (ecs.Entity{}) {
			lv = levelByEntity(d.level)
		}
		if lv == nil {
			lv = findLevel(cx, cz, d.pos.Local.Y)
		}
		if lv == nil {
			continue
		}
		insideX := cx - d.outward.X*0.7
		insideZ := cz - d.outward.Z*0.7
		fi, fj, ok := levelCell(lv, insideX, insideZ)
		if !ok {
			continue
		}
		levelNode := components.NavNode{Kind: components.NodeLevel, Level: lv.ent, I: fi, J: fj}

		outsideX := cx + d.outward.X*0.7
		outsideZ := cz + d.outward.Z*0.7

		var cost uint8 = 3
		if dc := sys.doorMap.Get(d.ent); dc != nil && dc.State == components.DoorClosed {
			cost = 0
		}

		// If the door opens into another Level volume (compound wing or
		// multi-section interior), wire a level<->level transition at the
		// opening instead of surface<->level. For internal partitions within
		// the same Level, skip transitions entirely — LevelNavGrid already
		// models the opening.
		if otherLv := findLevel(outsideX, outsideZ, d.pos.Local.Y); otherLv != nil {
			if otherLv.ent == lv.ent {
				continue
			}
			oi, oj, ok := levelCell(otherLv, outsideX, outsideZ)
			if ok {
				otherNode := components.NavNode{Kind: components.NodeLevel, Level: otherLv.ent, I: oi, J: oj}
				addEdge(levelNode, otherNode, cost, d.ent)
				addEdge(otherNode, levelNode, cost, d.ent)
				continue
			}
		}

		sgi := int32(math.Floor(float64(outsideX)))
		sgj := int32(math.Floor(float64(outsideZ)))
		surfChunk := components.ChunkCoord{X: sgi >> 6, Z: sgj >> 6}
		surfNode := components.NavNode{
			Kind:  components.NodeSurface,
			Chunk: surfChunk,
			I:     int16(sgi & 63),
			J:     int16(sgj & 63),
		}
		addEdge(surfNode, levelNode, cost, d.ent)
		addEdge(levelNode, surfNode, cost, d.ent)
	}

	// Stairs: level↔level OR level↔surface (bunker entrance, where
	// StairLevels.From == StairLevels.To = ground floor).
	for _, st := range stairs {
		if st.fromLev == (ecs.Entity{}) {
			continue
		}
		fromLv := levelByEntity(st.fromLev)
		if fromLv == nil {
			continue
		}

		sa := float32(math.Sin(float64(st.s.Yaw)))
		ca := float32(math.Cos(float64(st.s.Yaw)))
		baseX := float32(st.pos.Chunk.X) * components.ChunkSize
		baseZ := float32(st.pos.Chunk.Z) * components.ChunkSize
		bottomX := baseX + st.pos.Local.X
		bottomZ := baseZ + st.pos.Local.Z
		topX := bottomX + sa*st.s.Length
		topZ := bottomZ + ca*st.s.Length

		fI, fJ, ok := levelCell(fromLv, bottomX, bottomZ)
		if !ok {
			continue
		}
		fromNode := components.NavNode{Kind: components.NodeLevel, Level: fromLv.ent, I: fI, J: fJ}

		// From==To: stair anchors to one level (ground floor) and exits to
		// the exterior surface.
		isBunker := st.fromLev == st.toLev
		if !isBunker && st.toLev != (ecs.Entity{}) {
			toLv := levelByEntity(st.toLev)
			if toLv == nil {
				continue
			}
			tI, tJ, ok2 := levelCell(toLv, topX, topZ)
			if !ok2 {
				continue
			}
			toNode := components.NavNode{Kind: components.NodeLevel, Level: toLv.ent, I: tI, J: tJ}
			addEdge(fromNode, toNode, 4, st.ent)
			addEdge(toNode, fromNode, 4, st.ent)
			continue
		}

		tgi := int32(math.Floor(float64(topX)))
		tgj := int32(math.Floor(float64(topZ)))
		surfChunk := components.ChunkCoord{X: tgi >> 6, Z: tgj >> 6}
		toNode := components.NavNode{
			Kind:  components.NodeSurface,
			Chunk: surfChunk,
			I:     int16(tgi & 63),
			J:     int16(tgj & 63),
		}
		addEdge(fromNode, toNode, 4, st.ent)
		addEdge(toNode, fromNode, 4, st.ent)
	}

	// Same-storey Level↔Level junction edges for multi-section buildings
	// (Compound / Office wings). Without these, units can't path between
	// wings through internal open junctions even when AABBs touch.
	const (
		levelJunctionMaxYDiff float32 = 0.5
		levelJunctionMaxXZGap float32 = 1.5
		// Sample this far inside each level to avoid degenerate boundary cells.
		levelJunctionInset float32 = 1.0
		// Cost=10 keeps junctions as a fallback for wings without doors to
		// each other but stops them beating a real door route (Cost=3) —
		// otherwise the unit lines up with the junction sample point rather
		// than the door opening and gets stuck on the wall slide.
		levelJunctionCost uint8 = 10
	)
	clampF := func(v, lo, hi float32) float32 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	for i := 0; i < len(levels); i++ {
		for j := i + 1; j < len(levels); j++ {
			lvA := &levels[i]
			lvB := &levels[j]
			if math.Abs(float64(lvA.aabb.MinY-lvB.aabb.MinY)) > float64(levelJunctionMaxYDiff) {
				continue
			}
			// AABBs must overlap or be within MaxXZGap on each axis.
			gapX := maxF(lvA.aabb.MinX, lvB.aabb.MinX) - minF(lvA.aabb.MaxX, lvB.aabb.MaxX)
			gapZ := maxF(lvA.aabb.MinZ, lvB.aabb.MinZ) - minF(lvA.aabb.MaxZ, lvB.aabb.MaxZ)
			if gapX > levelJunctionMaxXZGap || gapZ > levelJunctionMaxXZGap {
				continue
			}
			// Sample points INSIDE each level (inset from junction toward
			// centre) so levelCell returns a valid cell when AABBs are
			// flush-touching (zero-width overlap).
			centreA := orcaVec2{X: lvA.aabb.CenterX(), Z: lvA.aabb.CenterZ()}
			centreB := orcaVec2{X: lvB.aabb.CenterX(), Z: lvB.aabb.CenterZ()}
			junctionX := (centreA.X + centreB.X) * 0.5
			junctionZ := (centreA.Z + centreB.Z) * 0.5
			dxA := centreA.X - junctionX
			dzA := centreA.Z - junctionZ
			magA := float32(math.Sqrt(float64(dxA*dxA + dzA*dzA)))
			var sampleAX, sampleAZ float32
			if magA > 1e-3 {
				sampleAX = junctionX + dxA/magA*levelJunctionInset
				sampleAZ = junctionZ + dzA/magA*levelJunctionInset
			} else {
				sampleAX = centreA.X
				sampleAZ = centreA.Z
			}
			sampleAX = clampF(sampleAX, lvA.aabb.MinX+0.5, lvA.aabb.MaxX-0.5)
			sampleAZ = clampF(sampleAZ, lvA.aabb.MinZ+0.5, lvA.aabb.MaxZ-0.5)
			dxB := centreB.X - junctionX
			dzB := centreB.Z - junctionZ
			magB := float32(math.Sqrt(float64(dxB*dxB + dzB*dzB)))
			var sampleBX, sampleBZ float32
			if magB > 1e-3 {
				sampleBX = junctionX + dxB/magB*levelJunctionInset
				sampleBZ = junctionZ + dzB/magB*levelJunctionInset
			} else {
				sampleBX = centreB.X
				sampleBZ = centreB.Z
			}
			sampleBX = clampF(sampleBX, lvB.aabb.MinX+0.5, lvB.aabb.MaxX-0.5)
			sampleBZ = clampF(sampleBZ, lvB.aabb.MinZ+0.5, lvB.aabb.MaxZ-0.5)

			aI, aJ, aOK := levelCell(lvA, sampleAX, sampleAZ)
			if !aOK {
				continue
			}
			bI, bJ, bOK := levelCell(lvB, sampleBX, sampleBZ)
			if !bOK {
				continue
			}
			nodeA := components.NavNode{Kind: components.NodeLevel, Level: lvA.ent, I: aI, J: aJ}
			nodeB := components.NavNode{Kind: components.NodeLevel, Level: lvB.ent, I: bI, J: bJ}
			addEdge(nodeA, nodeB, levelJunctionCost, ecs.Entity{})
			addEdge(nodeB, nodeA, levelJunctionCost, ecs.Entity{})
			if debugLog && !bakeDebugReported {
				fmt.Printf("[spatial_bake] level-junction edge ent=%v <-> ent=%v at (%.1f,%.1f) <-> (%.1f,%.1f)\n",
					lvA.ent, lvB.ent, sampleAX, sampleAZ, sampleBX, sampleBZ)
			}
		}
	}

	if debugLog && !bakeDebugReported && (len(doors) > 0 || len(stairs) > 0) {
		for i, d := range doors {
			fmt.Printf("[spatial_bake] door[%d] ent=%v levelMember=%v wallChunk=%v wallLocal=(%.1f,%.1f)\n",
				i, d.ent, d.level, d.pos.Chunk, d.pos.Local.X, d.pos.Local.Z)
		}
		for i, lv := range levels {
			fmt.Printf("[spatial_bake] level[%d] ent=%v chunk=%v aabb X[%.1f..%.1f] Y[%.1f..%.1f] Z[%.1f..%.1f]\n",
				i, lv.ent, lv.chunk, lv.aabb.MinX, lv.aabb.MaxX, lv.aabb.MinY, lv.aabb.MaxY, lv.aabb.MinZ, lv.aabb.MaxZ)
		}
		// One-shot stdout dump so a regression in Pass 4 is visible without
		// overlays. Missing LevelMember / StairLevels is the common failure.
		doorsWithLevel := 0
		for _, d := range doors {
			if d.level != (ecs.Entity{}) {
				doorsWithLevel++
			}
		}
		stairsWithFrom := 0
		stairsWithBoth := 0
		for _, st := range stairs {
			if st.fromLev != (ecs.Entity{}) {
				stairsWithFrom++
			}
			if st.fromLev != (ecs.Entity{}) && st.toLev != (ecs.Entity{}) {
				stairsWithBoth++
			}
		}
		fmt.Printf("[spatial_bake] Pass4 bake: levels=%d doors=%d (with LevelMember=%d) stairs=%d (with From=%d both=%d) edges: surf<->lvl=%d lvl<->lvl=%d\n",
			len(levels), len(doors), doorsWithLevel,
			len(stairs), stairsWithFrom, stairsWithBoth,
			edgeCounts.surfLevel, edgeCounts.levelLevel)
		bakeDebugReported = true
	}
}
