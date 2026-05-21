package systems

import (
	"math"

	"rts-go/components"
	"rts-go/core"
)

// Slope thresholds for NavCell.Cost (foot locomotion, Phase 6 P5):
//
//	slope < 0.30  (~17 deg)  ->  Cost = navCostOpen
//	0.30 <= slope < 0.60   ->  Cost = navCostRough
//	slope >= 0.60  (~31 deg)  ->  Cost = 0    (impassable)
//
// Slope = max pairwise corner-height difference per 1 m cell (ChunkResolution
// step is exactly 1 m). Diagonals are not divided by sqrt2 - the bake takes the
// raw max so a sharp ridge-on-diagonal still flags as steep.
const (
	navSlopeOpen  float32 = 0.30
	navSlopeRough float32 = 0.60

	navCostOpen   uint8 = 4
	navCostRough  uint8 = 8
	navCostRoad   uint8 = 2
	navCostTrench uint8 = 16
)

// bakeNavPass is Pass 1: walks navFilter (chunks without NavBaked) and emits
// one NavGrid per chunk. Bake order is calibrated for the cost-priority
// hierarchy in P5:
//
//	slope  <  OnRoad  <  InTrench  <  NearWater  <  Walls/Props
//
// Apply lower priority first; later passes overwrite. Two intentional twists:
//
//   - River-block pass leaves Cost untouched on cells that are already OnRoad
//     - this is how a bridge keeps Cost=2 over the river bed instead of
//     falling back to "water = impassable".
//   - Walls/props always win at the end with hard Cost=0; flags from earlier
//     passes are kept so AI can see "this blocked cell is also on a road".
func (sys *SpatialBakeSystem) bakeNavPass(ctx core.UpdateContext) []spatialBakeChunkRec {
	var navTodo []spatialBakeChunkRec
	qN := sys.navFilter.Query()
	for qN.Next() {
		cc, _, _ := qN.Get()
		navTodo = append(navTodo, spatialBakeChunkRec{id: qN.Entity(), cc: *cc})
	}
	if len(navTodo) == 0 {
		return navTodo
	}

	// Bucket walls by host chunk in one pass - avoids quadratic re-scanning
	// when several pristine chunks bake in the same tick.
	wallsByChunk := map[components.ChunkCoord][]wallEntry{}
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		passable := false
		if w.OpeningKind == components.OpeningDoor {
			if d := sys.doorMap.Get(qW.Entity()); d != nil && d.State == components.DoorOpen {
				passable = true
			}
		}
		wallsByChunk[pos.Chunk] = append(wallsByChunk[pos.Chunk], wallEntry{
			local:           pos.Local,
			w:               *w,
			openingPassable: passable,
		})
	}

	propIdx := sys.propIndexRes.Get()
	registry := sys.registryRes.Get()
	graph := sys.roadGraphRes.Get()
	trenches := sys.trenchRes.Get()
	rivers := sys.riversRes.Get()

	// Phase 14.6 M14.6.1 - snapshot every Building's Footprint once for the
	// NavInBuilding stamp pass. Building roots carry AlwaysActive, so a
	// single filter sweep covers the whole world.
	var footprints []components.AABB2D
	qB := sys.buildingFilter.Query()
	for qB.Next() {
		b := qB.Get()
		footprints = append(footprints, b.Footprint)
	}

	for _, rec := range navTodo {
		hm := sys.heightmapMap.Get(rec.id)
		if hm == nil {
			continue
		}
		var grid components.NavGrid
		bakeNavSlope(&grid, hm)

		applyNavRoads(&grid, rec.cc, graph)
		applyNavTrenches(&grid, rec.cc, trenches)
		applyNavRiverBlock(&grid, rec.cc, rivers)

		for i := range wallsByChunk[rec.cc] {
			rasterizeWall(&grid, wallsByChunk[rec.cc][i])
		}
		if propIdx != nil && registry != nil {
			for _, propEnt := range propIdx.Loaded[rec.cc] {
				prop := sys.propMap.Get(propEnt)
				propPos := sys.posMap.Get(propEnt)
				if prop == nil || propPos == nil {
					continue
				}
				meta := registry.Metas[prop.Type]
				if !meta.BlocksMove {
					continue
				}
				rasterizePropCircle(&grid, propPos.Local.X, propPos.Local.Z,
					meta.BBoxRadius*prop.Scale)
			}
		}

		// Phase 14.6 M14.6.1 - flag every cell whose centre falls inside any
		// Building.Footprint. NavService.cellAt then refuses surface expansion
		// into them; access remains only through TransitionEdges (Door/Stairs)
		// that route into Floor NavNodes.
		applyNavBuildings(&grid, rec.cc, footprints)

		if existing := sys.navGridMap.Get(rec.id); existing != nil {
			existing.Cells = grid.Cells
		} else {
			sys.navGridMap.Add(rec.id, &grid)
		}
		if !sys.navBakedMap.Has(rec.id) {
			sys.navBakedMap.Add(rec.id, &components.NavBaked{})
		}
	}
	_ = ctx
	return navTodo
}

// bakeNavSlope writes per-cell Cost from the heightmap. Each cell maps 1:1 to
// one heightmap quad (NavGridSide == ChunkResolution-1). The Heightmap is
// row-major along +Z (Heights[j*ChunkResolution + i] = column i, row j); same
// row-major layout is mirrored on NavGrid.Cells for cache-coherent A*.
//
// "Slope" here is max pairwise corner-height delta (1 m cell, raw deltas).
func bakeNavSlope(grid *components.NavGrid, hm *components.Heightmap) {
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			slope := maxPairwiseAbs4(h00, h10, h01, h11)

			var cost uint8
			switch {
			case slope < navSlopeOpen:
				cost = navCostOpen
			case slope < navSlopeRough:
				cost = navCostRough
			default:
				cost = 0
			}
			// Phase 13 M13.4: CoverDistance defaults to "no cover within scan
			// radius"; Pass 2 (CoverDistance bake) overwrites this for cells
			// near slots.
			grid.Cells[cj*components.NavGridSide+ci] = components.NavCell{
				Cost:          cost,
				CoverDistance: components.CoverDistanceFar,
			}
		}
	}
}

func maxPairwiseAbs4(a, b, c, d float32) float32 {
	best := absDelta(a, b)
	if v := absDelta(a, c); v > best {
		best = v
	}
	if v := absDelta(a, d); v > best {
		best = v
	}
	if v := absDelta(b, c); v > best {
		best = v
	}
	if v := absDelta(b, d); v > best {
		best = v
	}
	if v := absDelta(c, d); v > best {
		best = v
	}
	return best
}

func absDelta(a, b float32) float32 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}

// rasterizeWall marks every NavCell whose centre lies inside the wall's
// oriented rectangle (length x thickness, rotated by Yaw) as Cost=0. If the
// wall has a passable opening (only OpeningDoor + Door.State == DoorOpen), the
// opening segment along the wall axis is left untouched. Closed doors and
// windows leave the opening at Cost=0 (windows block movement; closed doors
// also block).
func rasterizeWall(grid *components.NavGrid, e wallEntry) {
	fromX, fromZ := e.local.X, e.local.Z
	yaw := e.w.Yaw
	length := e.w.Length
	halfT := e.w.Thickness * 0.5
	if length <= 0 || halfT <= 0 {
		return
	}

	sa := float32(math.Sin(float64(yaw)))
	ca := float32(math.Cos(float64(yaw)))

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
	iMin, iMax := clampCellRange(minX, maxX)
	jMin, jMax := clampCellRange(minZ, maxZ)
	if iMin >= iMax || jMin >= jMax {
		return
	}

	openCenter := e.w.OpeningCenterT * length
	openStart := openCenter - e.w.OpeningWidth*0.5
	openEnd := openCenter + e.w.OpeningWidth*0.5

	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			cx := float32(ci) + 0.5
			cz := float32(cj) + 0.5
			dx := cx - fromX
			dz := cz - fromZ
			t := dx*sa + dz*ca
			n := dx*ca - dz*sa
			if t < 0 || t > length || n < -halfT || n > halfT {
				continue
			}
			if e.openingPassable && e.w.OpeningWidth > 0 {
				if t >= openStart && t <= openEnd {
					continue
				}
			}
			grid.Cells[cj*components.NavGridSide+ci].Cost = 0
		}
	}
}

// rasterizePropCircle marks every NavCell within radius of (cx, cz) as Cost=0.
// Used for tree-stems / rocks / any prop with PropMeta.BlocksMove. BBoxRadius
// is a flat XZ approximation - fine for placeholder primitives, will be
// replaced by per-prop swept bounds when real meshes land in Phase 15.
func rasterizePropCircle(grid *components.NavGrid, cx, cz, r float32) {
	if r <= 0 {
		return
	}
	iMin, iMax := clampCellRange(cx-r, cx+r)
	jMin, jMax := clampCellRange(cz-r, cz+r)
	r2 := r * r
	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			ccx := float32(ci) + 0.5
			ccz := float32(cj) + 0.5
			dx := ccx - cx
			dz := ccz - cz
			if dx*dx+dz*dz > r2 {
				continue
			}
			grid.Cells[cj*components.NavGridSide+ci].Cost = 0
		}
	}
}

// applyNavRoads marks every cell whose centre lies within edge.Width/2 of any
// edge centre line as OnRoad with Cost=navCostRoad. Bridge-edges are treated
// the same as other roads - that's how the river-block pass later knows to
// leave them passable.
func applyNavRoads(grid *components.NavGrid, cc components.ChunkCoord, g *components.RoadGraph) {
	if g == nil || len(g.Edges) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for ei := range g.Edges {
		e := &g.Edges[ei]
		ax, az := worldXZ(g.Nodes[e.From].Pos)
		bx, bz := worldXZ(g.Nodes[e.To].Pos)
		halfW := e.Width * 0.5
		minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
		minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
		if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
			continue
		}
		stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
			func(idx int) {
				grid.Cells[idx].Flags |= components.NavOnRoad
				grid.Cells[idx].Cost = navCostRoad
			})
	}
}

// applyNavTrenches marks every cell within trench.Width/2 of any trench
// segment as InTrench with Cost=navCostTrench. Trenches are not impassable -
// just expensive - so AI prefers to skirt them on the open ground.
func applyNavTrenches(grid *components.NavGrid, cc components.ChunkCoord, tn *components.TrenchNetwork) {
	if tn == nil || len(tn.Lines) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for ti := range tn.Lines {
		t := &tn.Lines[ti]
		halfW := t.Width * 0.5
		for si := 0; si+1 < len(t.Points); si++ {
			ax, az := worldXZ(t.Points[si])
			bx, bz := worldXZ(t.Points[si+1])
			minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
			minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
			if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
				continue
			}
			stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
				func(idx int) {
					grid.Cells[idx].Flags |= components.NavInTrench
					grid.Cells[idx].Cost = navCostTrench
				})
		}
	}
}

// applyNavRiverBlock marks river cells as NearWater. Cost is forced to 0 only
// when the cell is *not* already OnRoad - i.e. only river cells with no bridge
// above become impassable. Bridge cells keep Cost=2 from the road pass and
// gain the NearWater flag for downstream consumers ("we are over water").
func applyNavRiverBlock(grid *components.NavGrid, cc components.ChunkCoord, rivers *components.Rivers) {
	if rivers == nil || len(rivers.Polylines) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for pi := range rivers.Polylines {
		pl := &rivers.Polylines[pi]
		halfW := pl.Width * 0.5
		for si := 0; si+1 < len(pl.Points); si++ {
			ax, az := worldXZ(pl.Points[si])
			bx, bz := worldXZ(pl.Points[si+1])
			minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
			minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
			if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
				continue
			}
			stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
				func(idx int) {
					grid.Cells[idx].Flags |= components.NavNearWater
					if grid.Cells[idx].Flags&components.NavOnRoad == 0 {
						grid.Cells[idx].Cost = 0
					}
				})
		}
	}
}

// applyNavBuildings stamps NavInBuilding onto every surface cell whose centre
// (XZ world coord) lies inside any building Footprint that intersects the
// chunk. Cost is left alone so wall-rasterised Cost=0 cells stay impassable
// and open cells keep their slope-derived Cost - the flag is the bit that
// NavService.cellAt reads to refuse surface expansion through the interior.
// Access to the inside is reserved for TransitionEdges (Door / Stairs) that
// route into Floor NavNodes; pure surface paths must skirt the footprint.
func applyNavBuildings(grid *components.NavGrid, cc components.ChunkCoord, footprints []components.AABB2D) {
	if len(footprints) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for fi := range footprints {
		fp := footprints[fi]
		if fp.MaxX <= chunkMinX || fp.MinX >= chunkMaxX ||
			fp.MaxZ <= chunkMinZ || fp.MinZ >= chunkMaxZ {
			continue
		}
		cellMinX, cellMaxX := clampCellRange(fp.MinX-chunkMinX, fp.MaxX-chunkMinX)
		cellMinZ, cellMaxZ := clampCellRange(fp.MinZ-chunkMinZ, fp.MaxZ-chunkMinZ)
		if cellMinX >= cellMaxX || cellMinZ >= cellMaxZ {
			continue
		}
		for cj := cellMinZ; cj < cellMaxZ; cj++ {
			cz := chunkMinZ + float32(cj) + 0.5
			if cz < fp.MinZ || cz > fp.MaxZ {
				continue
			}
			for ci := cellMinX; ci < cellMaxX; ci++ {
				cx := chunkMinX + float32(ci) + 0.5
				if cx < fp.MinX || cx > fp.MaxX {
					continue
				}
				grid.Cells[cj*components.NavGridSide+ci].Flags |= components.NavInBuilding
			}
		}
	}
}

// stripCellPass - common driver for the road / trench / river polyline-strip
// passes. Iterates exactly the cells whose AABB intersects the inflated edge
// bbox, runs a point-to-segment distance test, and invokes mark(idx) for any
// cell whose centre is within halfW of the segment.
func stripCellPass(grid *components.NavGrid, cc components.ChunkCoord,
	worldMinX, worldMaxX, worldMinZ, worldMaxZ float32,
	ax, az, bx, bz, halfW float32, mark func(idx int)) {
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	cellMinX, cellMaxX := clampCellRange(worldMinX-chunkMinX, worldMaxX-chunkMinX)
	cellMinZ, cellMaxZ := clampCellRange(worldMinZ-chunkMinZ, worldMaxZ-chunkMinZ)
	if cellMinX >= cellMaxX || cellMinZ >= cellMaxZ {
		return
	}
	for cj := cellMinZ; cj < cellMaxZ; cj++ {
		for ci := cellMinX; ci < cellMaxX; ci++ {
			wx := chunkMinX + float32(ci) + 0.5
			wz := chunkMinZ + float32(cj) + 0.5
			if pointToSegment2D(wx, wz, ax, az, bx, bz) > halfW {
				continue
			}
			mark(cj*components.NavGridSide + ci)
		}
	}
}

// clampCellRange - half-open [iMin, iMax) NavGrid cell indices that cover the
// world-XZ range [a, b]. Returns an empty range when the input is fully
// outside the chunk.
func clampCellRange(a, b float32) (int, int) {
	if a > b {
		a, b = b, a
	}
	iMin := int(math.Floor(float64(a)))
	iMax := int(math.Ceil(float64(b)))
	if iMin < 0 {
		iMin = 0
	}
	if iMax > components.NavGridSide {
		iMax = components.NavGridSide
	}
	if iMin > components.NavGridSide {
		iMin = components.NavGridSide
	}
	if iMax < 0 {
		iMax = 0
	}
	return iMin, iMax
}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

