package systems

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// bakeLevelNavPass is Pass 3: per Level entity (Without[LevelNavBaked]),
// build a LevelNavGrid sized from Level.AABB. Open-door openings carve a
// passable strip; windows and closed doors keep Cost=0. Grid origin =
// AABB.Min{X,Z} in chunk-local coords of the Level's root chunk;
// multi-chunk buildings translate each wall's Local to the anchor chunk.
func (sys *SpatialBakeSystem) bakeLevelNavPass(ctx core.UpdateContext) {
	type levelRec struct {
		ent ecs.Entity
		pos components.WorldPos
		lvl components.Level
	}
	childIdx := sys.buildingIndexRes.Get()
	var levelTodo []levelRec
	qL := sys.levelFilter.Query()
	for qL.Next() {
		lvl, pos := qL.Get()
		// Levels are AlwaysActive from boot, but their building's wall
		// children spawn with the chunk. Baking before the children exist
		// produced an EMPTY grid (walls=0) stamped LevelNavBaked forever —
		// every interior A* then pathed straight through partitions and
		// exterior walls, leaving colWall physics to catch the lie.
		if bm := sys.buildingMemberMap.Get(qL.Entity()); bm != nil {
			if childIdx == nil || len(childIdx.Loaded[bm.Building]) == 0 {
				continue
			}
		}
		levelTodo = append(levelTodo, levelRec{ent: qL.Entity(), pos: *pos, lvl: *lvl})
	}
	if debugLog && len(levelTodo) > 0 && !bakeDebugReported {
		total := 0
		qLAll := sys.levelFilterAll.Query()
		for qLAll.Next() {
			total++
		}
		fmt.Printf("[spatial_bake] Pass3 entry: unbaked-levels=%d total-levels=%d\n",
			len(levelTodo), total)
	}

	type wallSnap struct {
		ent             ecs.Entity
		pos             components.WorldPos
		w               components.WallSegment
		openingPassable bool
		levelMember     ecs.Entity
	}
	var wallSnaps []wallSnap
	type stairSnapL struct {
		pos      components.WorldPos
		s        components.Stairs
		from, to ecs.Entity
	}
	var stairSnapsL []stairSnapL
	if len(levelTodo) > 0 {
		qW := sys.wallFilter.Query()
		for qW.Next() {
			pos, w := qW.Get()
			e := qW.Entity()
			passable := false
			if w.OpeningKind == components.OpeningDoor {
				if d := sys.doorMap.Get(e); d != nil && d.State == components.DoorOpen {
					passable = true
				}
			}
			var lev ecs.Entity
			if lm := sys.levelMemberMap.Get(e); lm != nil {
				lev = lm.Level
			}
			wallSnaps = append(wallSnaps, wallSnap{
				ent: e, pos: *pos, w: *w, openingPassable: passable, levelMember: lev,
			})
		}
		qS := sys.stairsFilter.Query()
		for qS.Next() {
			pos, s := qS.Get()
			var from, to ecs.Entity
			if sl := sys.stairLevelsMap.Get(qS.Entity()); sl != nil {
				from = sl.From
				to = sl.To
			}
			stairSnapsL = append(stairSnapsL, stairSnapL{pos: *pos, s: *s, from: from, to: to})
		}
	}

	for _, lr := range levelTodo {
		sx := lr.lvl.AABB.SizeX()
		sz := lr.lvl.AABB.SizeZ()
		if sx <= 0 || sz <= 0 {
			continue
		}
		szi := uint8(math.Ceil(float64(sx)))
		szj := uint8(math.Ceil(float64(sz)))
		if szi > components.MaxLevelSide {
			szi = components.MaxLevelSide
		}
		if szj > components.MaxLevelSide {
			szj = components.MaxLevelSide
		}

		chunkBaseX := float32(lr.pos.Chunk.X) * components.ChunkSize
		chunkBaseZ := float32(lr.pos.Chunk.Z) * components.ChunkSize
		originX := lr.lvl.AABB.MinX - chunkBaseX
		originZ := lr.lvl.AABB.MinZ - chunkBaseZ
		var grid components.LevelNavGrid
		grid.SizeX = szi
		grid.SizeZ = szj
		grid.Origin = components.Vec3{X: originX, Y: lr.lvl.AABB.MinY, Z: originZ}

		// Seed cells open with NavInBuilding flag.
		for cj := uint8(0); cj < szj; cj++ {
			for ci := uint8(0); ci < szi; ci++ {
				grid.Cells[int(cj)*components.MaxLevelSide+int(ci)] = components.NavCell{
					Cost:  navCostOpen,
					Flags: components.NavInBuilding,
				}
			}
		}

		// Walls can live in any chunk crossed by a multi-chunk building;
		// translate each wall's Local to the anchor chunk before rasterising.
		wallsRasterized := 0
		for _, ws := range wallSnaps {
			if ws.levelMember != lr.ent {
				continue
			}
			wallChunkBaseX := float32(ws.pos.Chunk.X) * components.ChunkSize
			wallChunkBaseZ := float32(ws.pos.Chunk.Z) * components.ChunkSize
			adjusted := rl.Vector3{
				X: wallChunkBaseX + ws.pos.Local.X - chunkBaseX,
				Y: ws.pos.Local.Y,
				Z: wallChunkBaseZ + ws.pos.Local.Z - chunkBaseZ,
			}
			rasterizeFloorWall(&grid, adjusted, ws.w, ws.openingPassable, originX, originZ)
			wallsRasterized++
		}

		// Transition-endpoint carve: Pass 4 wires stair edges into the cells
		// under the stair anchors; a stair standing 0.5 m off a wall lands
		// its cell inside the wall's inflate band, and a blocked FROM cell
		// makes the whole storey unreachable (ai_main_m1 0/8). The anchor
		// cells are walkable by construction — force them open.
		carve := func(worldX, worldZ float32) {
			lx := worldX - (chunkBaseX + originX)
			lz := worldZ - (chunkBaseZ + originZ)
			ci := int(math.Floor(float64(lx)))
			cj := int(math.Floor(float64(lz)))
			if ci < 0 || cj < 0 || ci >= int(szi) || cj >= int(szj) {
				return
			}
			cell := &grid.Cells[cj*components.MaxLevelSide+ci]
			if cell.Cost == 0 {
				cell.Cost = navCostOpen
			}
		}
		for _, st := range stairSnapsL {
			if st.s.Length <= 0 {
				continue
			}
			baseX := float32(st.pos.Chunk.X) * components.ChunkSize
			baseZ := float32(st.pos.Chunk.Z) * components.ChunkSize
			bottomX := baseX + st.pos.Local.X
			bottomZ := baseZ + st.pos.Local.Z
			sa := float32(math.Sin(float64(st.s.Yaw)))
			ca := float32(math.Cos(float64(st.s.Yaw)))
			if st.from == lr.ent {
				carve(bottomX, bottomZ)
			}
			if st.to == lr.ent && st.to != st.from {
				carve(bottomX+sa*st.s.Length, bottomZ+ca*st.s.Length)
			}
		}
		if debugLog {
			blocked := 0
			for cj := uint8(0); cj < szj; cj++ {
				for ci := uint8(0); ci < szi; ci++ {
					if grid.Cells[int(cj)*components.MaxLevelSide+int(ci)].Cost == 0 {
						blocked++
					}
				}
			}
			fmt.Printf("[spatial_bake] Pass3 level=%v minY=%.1f walls=%d blocked=%d/%d\n",
				lr.ent, lr.lvl.AABB.MinY, wallsRasterized, blocked, int(szi)*int(szj))
		}

		if existing := sys.floorNavMap.Get(lr.ent); existing != nil {
			*existing = grid
		} else {
			sys.floorNavMap.Add(lr.ent, &grid)
		}
		if !sys.floorNavBakedMap.Has(lr.ent) {
			sys.floorNavBakedMap.Add(lr.ent, &components.LevelNavBaked{})
		}
	}
	_ = ctx
}

// rasterizeFloorWall marks cells of a LevelNavGrid as Cost=0 inside the
// wall's oriented rectangle. Mirrors rasterizeWall but uses a (sizeX, sizeZ,
// origin) sub-block. Open-door openings carve a gap; closed doors / windows
// stay Cost=0.
func rasterizeFloorWall(grid *components.LevelNavGrid, wallLocal rl.Vector3,
	w components.WallSegment, openingPassable bool,
	originX, originZ float32) {
	// Deliberately the OPPOSITE of the surface pass, which must not inflate
	// (it over-blocked tangential approaches outside footprints). Interior
	// walls partition rooms on a 1 m grid and the level grid has no footprint
	// flag to fall back on, so half a cell of inflate is what makes a
	// partition actually separate two rooms.
	const cellInflate float32 = 0.5
	yaw := w.Yaw
	length := w.Length
	halfT := w.Thickness*0.5 + cellInflate
	if length <= 0 || halfT <= 0 {
		return
	}

	sa := float32(math.Sin(float64(yaw)))
	ca := float32(math.Cos(float64(yaw)))

	fromX := wallLocal.X - originX
	fromZ := wallLocal.Z - originZ

	ex := length * sa
	ez := length * ca
	px := halfT * ca
	pz := halfT * (-sa)
	corners := [4][2]float32{
		{fromX - px, fromZ - pz},
		{fromX + px, fromZ + pz},
		{fromX + ex - px, fromZ + ez - pz},
		{fromX + ex + px, fromZ + ez + pz},
	}
	minX, maxX := corners[0][0], corners[0][0]
	minZ, maxZ := corners[0][1], corners[0][1]
	for i := 1; i < 4; i++ {
		if corners[i][0] < minX {
			minX = corners[i][0]
		}
		if corners[i][0] > maxX {
			maxX = corners[i][0]
		}
		if corners[i][1] < minZ {
			minZ = corners[i][1]
		}
		if corners[i][1] > maxZ {
			maxZ = corners[i][1]
		}
	}
	iMin := int(math.Floor(float64(minX)))
	iMax := int(math.Ceil(float64(maxX)))
	jMin := int(math.Floor(float64(minZ)))
	jMax := int(math.Ceil(float64(maxZ)))
	if iMin < 0 {
		iMin = 0
	}
	if jMin < 0 {
		jMin = 0
	}
	if iMax > int(grid.SizeX) {
		iMax = int(grid.SizeX)
	}
	if jMax > int(grid.SizeZ) {
		jMax = int(grid.SizeZ)
	}
	if iMin >= iMax || jMin >= jMax {
		return
	}

	openCenter := w.OpeningCenterT * length
	openStart := openCenter - w.OpeningWidth*0.5
	openEnd := openCenter + w.OpeningWidth*0.5

	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			cx := float32(ci) + 0.5
			cz := float32(cj) + 0.5
			dx := cx - fromX
			dz := cz - fromZ
			t := dx*sa + dz*ca
			n := dx*ca - dz*sa
			if t < -cellInflate || t > length+cellInflate || n < -halfT || n > halfT {
				continue
			}
			if openingPassable && w.OpeningWidth > 0 {
				if t >= openStart && t <= openEnd {
					continue
				}
			}
			grid.Cells[cj*components.MaxLevelSide+ci].Cost = 0
		}
	}
}
