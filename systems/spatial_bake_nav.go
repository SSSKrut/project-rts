package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
)

// Slope thresholds for NavCell.Cost (foot locomotion):
//
//	slope < 0.30 (~17°)      →  navCostOpen
//	0.30 ≤ slope < 0.60     →  navCostRough
//	slope ≥ 0.60 (~31°)      →  Cost = 0 (impassable)
//
// Slope = max pairwise corner-height delta per 1 m cell. Diagonals are NOT
// divided by sqrt2 — bake takes the raw max so a ridge-on-diagonal still
// flags as steep.
const (
	navSlopeOpen  float32 = 0.30
	navSlopeRough float32 = 0.60

	navCostOpen   uint8 = 4
	navCostRough  uint8 = 8
	navCostRoad   uint8 = 2
	navCostTrench uint8 = 16
)

// bakeNavPass is Pass 1: walks navFilter (chunks without NavBaked) and emits
// one NavGrid per chunk. Bake order is calibrated for the priority hierarchy:
//
//	slope < OnRoad < InTrench < NearWater < Walls/Props
//
// Apply lower priority first; later passes overwrite. Two twists:
//   - River-block leaves Cost untouched on already-OnRoad cells (bridge keeps
//     Cost=2 over the bed instead of "water = impassable").
//   - Walls/props always win with hard Cost=0; flags from earlier passes are
//     kept so AI can see "this blocked cell is also on a road".
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

	// Bucket walls once — avoids quadratic re-scanning when multiple chunks
	// bake in the same tick.
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
		// Capture CoverDirection.Dir so the NavInBuilding-clear sweep can
		// compute the door's outside cell without re-querying.
		var outward rl.Vector3
		if cd := sys.coverDirMap.Get(qW.Entity()); cd != nil {
			outward = cd.Dir
		}
		wallsByChunk[pos.Chunk] = append(wallsByChunk[pos.Chunk], wallEntry{
			local:           pos.Local,
			w:               *w,
			openingPassable: passable,
			outward:         outward,
		})
	}

	propIdx := sys.propIndexRes.Get()
	registry := sys.registryRes.Get()
	graph := sys.roadGraphRes.Get()
	trenches := sys.trenchRes.Get()
	rivers := sys.riversRes.Get()

	// Snapshot every Building's Footprint once for the NavInBuilding stamp.
	// Building roots are AlwaysActive — one filter sweep covers the world.
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

		// Flag cells whose centre falls inside any Building.Footprint so
		// NavService.cellAt refuses surface expansion through them; access
		// remains only via TransitionEdges into Floor NavNodes.
		applyNavBuildings(&grid, rec.cc, footprints)

		// Door outside cells are intentional transition points, not
		// building interior. In a compound case where flush-touching wings
		// overlap, a door's outside cell can fall inside the neighbour's
		// footprint — the NavInBuilding bit would make it unreachable from
		// open terrain. Clear the bit on door outside cells. Sweep 9-chunk
		// window — a door in a neighbour chunk can project into rec.cc.
		clearDoorOutsideNavInBuilding(&grid, rec.cc, wallsByChunk)

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

// bakeNavSlope writes per-cell Cost from the heightmap. Cell ↔ heightmap
// quad 1:1 (NavGridSide == ChunkResolution-1). Row-major along +Z, mirrored
// on NavGrid.Cells for cache-coherent A*.
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
			// CoverDistance defaults to "no cover within scan radius"; Pass 2
			// overwrites for cells near slots.
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

// rasterizeWall marks every NavCell inside the wall's oriented rectangle
// as Cost=0. Open doors carve a passable opening along the wall axis;
// closed doors / windows leave Cost=0.
//
// On the surface grid this is effectively a no-op for current 0.3 m wall
// thickness (cell centres are 0.5 m from wall lines, halfT = 0.15). Don't
// inflate halfT — it over-blocked cells immediately outside footprints
// (broken tangential approach in compound_west / compound_north /
// office_front). Surface blocking relies on the NavInBuilding footprint
// flag set by applyNavBuildings; LevelNavGrid still inflates (see
// rasterizeFloorWall) because interior walls partition rooms and the level
// grid has no footprint flag.
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

// rasterizePropCircle marks every NavCell within radius of (cx, cz) as
// Cost=0 — used for any prop with PropMeta.BlocksMove.
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

// applyNavRoads marks cells within edge.Width/2 of any edge centre line as
// OnRoad with Cost=navCostRoad. Bridges share this — that's how the
// river-block pass knows to leave them passable.
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

// applyNavTrenches marks cells within trench.Width/2 as InTrench with
// Cost=navCostTrench. Not impassable, just expensive — AI prefers to skirt.
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

// applyNavRiverBlock marks river cells as NearWater. Cost is forced to 0
// only when the cell is NOT already OnRoad — bridge cells keep Cost=2 and
// gain the flag for downstream consumers.
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

// applyNavBuildings stamps NavInBuilding onto cells whose centre lies inside
// any building Footprint. Cost is left alone — the flag is what
// NavService.cellAt reads to refuse surface expansion through interior;
// access is reserved for TransitionEdges into Floor NavNodes.
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

// stripCellPass — common driver for road / trench / river polyline-strip
// passes. Iterates cells whose AABB intersects the inflated edge bbox,
// runs a point-to-segment distance test, invokes mark(idx) on hits.
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

// clampCellRange returns half-open [iMin, iMax) NavGrid cell indices that
// cover the world-XZ range [a, b]. Empty range when fully outside.
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

// clearDoorOutsideNavInBuilding clears NavInBuilding on the outside surface
// cell of every Door in the 9-chunk window around `cc`. The outside cell is
// wallCentre + outward*0.7 (same offset Pass 4 transitions use).
//
// Without this, a compound's inner doors (whose outside cells lie inside the
// neighbouring wing's footprint) become walkable only via registry-override
// islands — circular-reachable from each other but not from outside. We
// touch only the flag; Cost was already stamped by rasterizeWall.
func clearDoorOutsideNavInBuilding(
	grid *components.NavGrid,
	cc components.ChunkCoord,
	wallsByChunk map[components.ChunkCoord][]wallEntry,
) {
	for dcZ := int32(-1); dcZ <= 1; dcZ++ {
		for dcX := int32(-1); dcX <= 1; dcX++ {
			ncc := components.ChunkCoord{X: cc.X + dcX, Z: cc.Z + dcZ}
			bucket := wallsByChunk[ncc]
			wallChunkBaseX := float32(ncc.X) * components.ChunkSize
			wallChunkBaseZ := float32(ncc.Z) * components.ChunkSize
			gridChunkBaseX := float32(cc.X) * components.ChunkSize
			gridChunkBaseZ := float32(cc.Z) * components.ChunkSize
			for i := range bucket {
				we := &bucket[i]
				if we.w.OpeningKind != components.OpeningDoor {
					continue
				}
				sa := float32(math.Sin(float64(we.w.Yaw)))
				ca := float32(math.Cos(float64(we.w.Yaw)))
				centreT := we.w.OpeningCenterT * we.w.Length
				cxWorld := wallChunkBaseX + we.local.X + sa*centreT
				czWorld := wallChunkBaseZ + we.local.Z + ca*centreT
				outsideX := cxWorld + we.outward.X*0.7
				outsideZ := czWorld + we.outward.Z*0.7
				lx := outsideX - gridChunkBaseX
				lz := outsideZ - gridChunkBaseZ
				ci := int(math.Floor(float64(lx)))
				cj := int(math.Floor(float64(lz)))
				if ci < 0 || ci >= components.NavGridSide ||
					cj < 0 || cj >= components.NavGridSide {
					continue
				}
				grid.Cells[cj*components.NavGridSide+ci].Flags &^= components.NavInBuilding
			}
		}
	}
}

