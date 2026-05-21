package systems

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// bakeTransitionsPass is Pass 4: rebuild TransitionRegistry from the live set
// of Doors / Stairs / bunker entrances. They connect surface<->level and
// level<->level NavNodes. We wipe the registry and re-emit on every tick that
// any chunk bakes; on the placeholder scene the total edge count is small
// (<~20), so a full rebuild is cheaper than per-owner invalidation.
func (sys *SpatialBakeSystem) bakeTransitionsPass(ctx core.UpdateContext) {
	_ = ctx
	registry := sys.transitionRes.Get()
	if registry == nil {
		return
	}

	for k := range registry.Out {
		delete(registry.Out, k)
	}

	// Snapshot every Level entity that already has a baked nav grid. Level
	// entities are AlwaysActive so they're always reachable - we filter only
	// by "has LevelNavGrid".
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

	// Find the level whose AABB contains (worldX, worldZ) and whose Y range
	// covers `y` with a small pad. Returns nil if no match.
	findLevel := func(worldX, worldZ, y float32) *levelSnapshot {
		const yPad float32 = 0.6
		for i := range levels {
			lv := &levels[i]
			if !lv.aabb.ContainsXZ(worldX, worldZ) {
				continue
			}
			if y < lv.aabb.MinY-yPad || y > lv.aabb.MaxY+yPad {
				continue
			}
			return lv
		}
		return nil
	}
	// Map a world XZ inside a level to a (I, J) cell index on its grid.
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

	// Snapshot door walls.
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

	// Snapshot stairs (carry their resolved level endpoints).
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

	// Doors -> 1 bidirectional edge between surface cell outside and level
	// cell inside.
	for _, d := range doors {
		sa := float32(math.Sin(float64(d.w.Yaw)))
		ca := float32(math.Cos(float64(d.w.Yaw)))
		centreT := d.w.OpeningCenterT * d.w.Length
		baseX := float32(d.pos.Chunk.X) * components.ChunkSize
		baseZ := float32(d.pos.Chunk.Z) * components.ChunkSize
		cx := baseX + d.pos.Local.X + sa*centreT
		cz := baseZ + d.pos.Local.Z + ca*centreT

		// Inside level: prefer LevelMember; fall back to containing-AABB scan
		// for resilience.
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
		sgi := int32(math.Floor(float64(outsideX)))
		sgj := int32(math.Floor(float64(outsideZ)))
		surfChunk := components.ChunkCoord{X: sgi >> 6, Z: sgj >> 6}
		surfNode := components.NavNode{
			Kind:  components.NodeSurface,
			Chunk: surfChunk,
			I:     int16(sgi & 63),
			J:     int16(sgj & 63),
		}

		var cost uint8 = 3
		if dc := sys.doorMap.Get(d.ent); dc != nil && dc.State == components.DoorClosed {
			cost = 0
		}
		addEdge(surfNode, levelNode, cost, d.ent)
		addEdge(levelNode, surfNode, cost, d.ent)
	}

	// Stairs -> level<->level OR level<->surface (bunker entrance, where
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

		// Bunker entrance: From == To means the stair only anchors to one
		// level (ground floor) and exits to the exterior surface.
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

		// Surface side at top of bunker entrance.
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

	if !bakeDebugReported && (len(doors) > 0 || len(stairs) > 0) {
		for i, d := range doors {
			fmt.Printf("[spatial_bake] door[%d] ent=%v levelMember=%v wallChunk=%v wallLocal=(%.1f,%.1f)\n",
				i, d.ent, d.level, d.pos.Chunk, d.pos.Local.X, d.pos.Local.Z)
		}
		for i, lv := range levels {
			fmt.Printf("[spatial_bake] level[%d] ent=%v chunk=%v aabb X[%.1f..%.1f] Y[%.1f..%.1f] Z[%.1f..%.1f]\n",
				i, lv.ent, lv.chunk, lv.aabb.MinX, lv.aabb.MaxX, lv.aabb.MinY, lv.aabb.MaxY, lv.aabb.MinZ, lv.aabb.MaxZ)
		}
		// One-shot stdout dump so a regression in Pass 4 is visible without
		// adding overlays. Walls / doors / stairs missing a LevelMember /
		// StairLevels are the common failure mode after Phase 16.B.1.b.
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
