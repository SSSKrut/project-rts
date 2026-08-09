package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// openGrid is a fresh chunk bake: every cell walkable at navCostOpen.
func openGrid() *components.NavGrid {
	g := &components.NavGrid{}
	for i := range g.Cells {
		g.Cells[i] = components.NavCell{Cost: navCostOpen, CoverDistance: components.CoverDistanceFar}
	}
	return g
}

func cellAtXZ(g *components.NavGrid, ci, cj int) components.NavCell {
	return g.Cells[cj*components.NavGridSide+ci]
}

func blockedCount(g *components.NavGrid) int {
	n := 0
	for i := range g.Cells {
		if g.Cells[i].Cost == 0 {
			n++
		}
	}
	return n
}

func bakeWall(x, z, yaw, length, thickness float32) wallEntry {
	return wallEntry{
		local: rl.Vector3{X: x, Z: z},
		w:     components.WallSegment{Length: length, Yaw: yaw, Thickness: thickness, Height: 3},
	}
}

func TestBakeNavSlopeCostsByGradient(t *testing.T) {
	flat := &components.Heightmap{}
	grid := &components.NavGrid{}
	bakeNavSlope(grid, flat)
	if got := cellAtXZ(grid, 5, 5).Cost; got != navCostOpen {
		t.Errorf("flat ground cost %d, want %d", got, navCostOpen)
	}
	if got := cellAtXZ(grid, 5, 5).CoverDistance; got != components.CoverDistanceFar {
		t.Errorf("CoverDistance %d, want the far sentinel", got)
	}

	// A cliff: one vertex lifted well past the impassable threshold.
	steep := &components.Heightmap{}
	steep.Heights[10*components.ChunkResolution+10] = 50
	bakeNavSlope(grid, steep)
	if got := cellAtXZ(grid, 10, 10).Cost; got != 0 {
		t.Errorf("cliff cell cost %d, want 0 (impassable)", got)
	}
	// The lifted vertex is a corner of four cells; the rest stay open.
	if got := cellAtXZ(grid, 12, 12).Cost; got != navCostOpen {
		t.Errorf("cell away from the cliff cost %d, want %d", got, navCostOpen)
	}
}

func TestBakeNavSlopeMarksRoughGround(t *testing.T) {
	hm := &components.Heightmap{}
	// A gradient that lands between the open and impassable thresholds.
	mid := (navSlopeOpen + navSlopeRough) * 0.5
	hm.Heights[20*components.ChunkResolution+20] = mid
	grid := &components.NavGrid{}
	bakeNavSlope(grid, hm)
	if got := cellAtXZ(grid, 20, 20).Cost; got != navCostRough {
		t.Fatalf("mid-slope cell cost %d, want %d", got, navCostRough)
	}
}

func TestBakeNavSlopeCoversEveryCell(t *testing.T) {
	grid := &components.NavGrid{}
	bakeNavSlope(grid, &components.Heightmap{})
	for i := range grid.Cells {
		if grid.Cells[i].Cost == 0 {
			t.Fatalf("cell %d left at cost 0 over flat ground", i)
		}
	}
}

func TestRasterizeWallBlocksItsFootprintOnly(t *testing.T) {
	grid := openGrid()
	// Thick enough to actually reach cell centres (0.5 offsets).
	rasterizeWall(grid, bakeWall(10.5, 5.5, math.Pi/2, 8, 1.0))
	if blockedCount(grid) == 0 {
		t.Fatal("a 1 m thick wall blocked nothing")
	}
	// Along +X from x=10.5: cells 11..18 at row 5.
	if got := cellAtXZ(grid, 12, 5).Cost; got != 0 {
		t.Errorf("cell under the wall cost %d, want 0", got)
	}
	if got := cellAtXZ(grid, 12, 8).Cost; got == 0 {
		t.Error("a cell 3 m off the wall was blocked")
	}
	if got := cellAtXZ(grid, 25, 5).Cost; got == 0 {
		t.Error("a cell past the wall end was blocked")
	}
}

func TestRasterizeWallRespectsLength(t *testing.T) {
	grid := openGrid()
	rasterizeWall(grid, bakeWall(10.5, 5.5, math.Pi/2, 4, 1.0))
	if got := cellAtXZ(grid, 12, 5).Cost; got != 0 {
		t.Errorf("inside the span: cost %d, want 0", got)
	}
	if got := cellAtXZ(grid, 17, 5).Cost; got == 0 {
		t.Error("beyond the 4 m span the wall still blocked")
	}
}

func TestRasterizeWallCarvesAnOpenDoor(t *testing.T) {
	e := bakeWall(10.5, 5.5, math.Pi/2, 10, 1.0)
	e.w.OpeningKind = components.OpeningDoor
	e.w.OpeningCenterT = 0.5
	e.w.OpeningWidth = 3
	e.openingPassable = true

	open := openGrid()
	rasterizeWall(open, e)

	e.openingPassable = false
	shut := openGrid()
	rasterizeWall(shut, e)

	if blockedCount(open) >= blockedCount(shut) {
		t.Fatalf("an open door blocked %d cells, a closed one %d",
			blockedCount(open), blockedCount(shut))
	}
	// The opening straddles t = 5, i.e. x = 15.5.
	if got := cellAtXZ(open, 15, 5).Cost; got == 0 {
		t.Error("the doorway cell is blocked with the door open")
	}
	if got := cellAtXZ(shut, 15, 5).Cost; got != 0 {
		t.Error("the doorway cell is walkable with the door shut")
	}
}

func TestRasterizeWallIgnoresDegenerateWalls(t *testing.T) {
	grid := openGrid()
	rasterizeWall(grid, bakeWall(10, 10, 0, 0, 1))
	rasterizeWall(grid, bakeWall(10, 10, 0, 10, 0))
	rasterizeWall(grid, bakeWall(10, 10, 0, -5, 1))
	if n := blockedCount(grid); n != 0 {
		t.Fatalf("degenerate walls blocked %d cells", n)
	}
}

func TestRasterizeWallClipsToTheChunk(t *testing.T) {
	grid := openGrid()
	// Starts inside, runs far past the far edge.
	rasterizeWall(grid, bakeWall(60.5, 30.5, math.Pi/2, 200, 1.0))
	if got := cellAtXZ(grid, 62, 30).Cost; got != 0 {
		t.Error("the in-chunk part of the wall did not block")
	}
	// No panic and no wrap onto row 31.
	if got := cellAtXZ(grid, 2, 31).Cost; got == 0 {
		t.Error("the wall wrapped onto the next row")
	}
}

func TestRasterizeWallOutsideTheChunkIsANoop(t *testing.T) {
	grid := openGrid()
	rasterizeWall(grid, bakeWall(-100, -100, 0, 10, 1))
	rasterizeWall(grid, bakeWall(500, 500, 0, 10, 1))
	if n := blockedCount(grid); n != 0 {
		t.Fatalf("walls outside the chunk blocked %d cells", n)
	}
}

func TestRasterizePropCircleBlocksADisc(t *testing.T) {
	grid := openGrid()
	rasterizePropCircle(grid, 20.5, 20.5, 2.5)
	if got := cellAtXZ(grid, 20, 20).Cost; got != 0 {
		t.Error("the centre cell survived")
	}
	if got := cellAtXZ(grid, 22, 20).Cost; got != 0 {
		t.Error("a cell 2 m out survived")
	}
	if got := cellAtXZ(grid, 24, 20).Cost; got == 0 {
		t.Error("a cell 4 m out was blocked")
	}
	// Round, not square: the diagonal corner of the bounding box is clear.
	if got := cellAtXZ(grid, 22, 22).Cost; got == 0 {
		t.Error("the disc blocked its bounding-box corner")
	}
}

func TestRasterizePropCircleIgnoresNonPositiveRadius(t *testing.T) {
	grid := openGrid()
	rasterizePropCircle(grid, 20.5, 20.5, 0)
	rasterizePropCircle(grid, 20.5, 20.5, -3)
	if n := blockedCount(grid); n != 0 {
		t.Fatalf("blocked %d cells with no radius", n)
	}
}

func TestRasterizePropCircleClipsAtTheChunkEdge(t *testing.T) {
	grid := openGrid()
	rasterizePropCircle(grid, 0.5, 0.5, 3)
	if got := cellAtXZ(grid, 0, 0).Cost; got != 0 {
		t.Error("the centre cell survived")
	}
	if got := cellAtXZ(grid, components.NavGridSide-1, components.NavGridSide-1).Cost; got == 0 {
		t.Error("the disc wrapped to the opposite corner")
	}
}

func TestApplyNavBuildingsFlagsTheFootprint(t *testing.T) {
	grid := openGrid()
	foot := []components.AABB2D{{MinX: 10, MinZ: 10, MaxX: 20, MaxZ: 20}}
	applyNavBuildings(grid, components.ChunkCoord{}, foot)

	if got := cellAtXZ(grid, 15, 15).Flags; got&components.NavInBuilding == 0 {
		t.Error("a cell inside the footprint is not flagged")
	}
	if got := cellAtXZ(grid, 5, 5).Flags; got&components.NavInBuilding != 0 {
		t.Error("a cell outside the footprint is flagged")
	}
	// The flag is not a cost: entry through a door depends on the cell
	// staying nominally walkable.
	if got := cellAtXZ(grid, 15, 15).Cost; got == 0 {
		t.Error("applyNavBuildings zeroed the cost instead of flagging")
	}
}

func TestApplyNavBuildingsSkipsOtherChunks(t *testing.T) {
	grid := openGrid()
	foot := []components.AABB2D{{MinX: 200, MinZ: 200, MaxX: 220, MaxZ: 220}}
	applyNavBuildings(grid, components.ChunkCoord{}, foot)
	for i := range grid.Cells {
		if grid.Cells[i].Flags&components.NavInBuilding != 0 {
			t.Fatalf("cell %d flagged by a footprint two chunks away", i)
		}
	}
}

func TestApplyNavBuildingsWorksInANegativeChunk(t *testing.T) {
	grid := openGrid()
	cc := components.ChunkCoord{X: -1, Z: -1}
	// Chunk (-1,-1) spans world [-64, 0).
	foot := []components.AABB2D{{MinX: -20, MinZ: -20, MaxX: -10, MaxZ: -10}}
	applyNavBuildings(grid, cc, foot)
	// World -15 is local 49 in that chunk.
	if got := cellAtXZ(grid, 49, 49).Flags; got&components.NavInBuilding == 0 {
		t.Fatal("the footprint missed its cells in a negative chunk")
	}
}

func TestApplyNavRoadsMarksTheStrip(t *testing.T) {
	grid := openGrid()
	g := roadPair(0, 30, 64, 30, 8)
	applyNavRoads(grid, components.ChunkCoord{}, g)

	c := cellAtXZ(grid, 32, 30)
	if c.Flags&components.NavOnRoad == 0 {
		t.Error("the centre-line cell is not flagged OnRoad")
	}
	if c.Cost != navCostRoad {
		t.Errorf("road cell cost %d, want %d", c.Cost, navCostRoad)
	}
	if got := cellAtXZ(grid, 32, 40); got.Flags&components.NavOnRoad != 0 {
		t.Error("a cell 10 m off the road is flagged")
	}
}

func TestApplyNavRoadsRespectsWidth(t *testing.T) {
	narrow := openGrid()
	applyNavRoads(narrow, components.ChunkCoord{}, roadPair(0, 30, 64, 30, 2))
	wide := openGrid()
	applyNavRoads(wide, components.ChunkCoord{}, roadPair(0, 30, 64, 30, 16))

	count := func(g *components.NavGrid) int {
		n := 0
		for i := range g.Cells {
			if g.Cells[i].Flags&components.NavOnRoad != 0 {
				n++
			}
		}
		return n
	}
	if count(wide) <= count(narrow) {
		t.Fatalf("16 m road covers %d cells, 2 m road %d", count(wide), count(narrow))
	}
}

func TestApplyNavRoadsHandlesAnEmptyGraph(t *testing.T) {
	grid := openGrid()
	applyNavRoads(grid, components.ChunkCoord{}, nil)
	applyNavRoads(grid, components.ChunkCoord{}, &components.RoadGraph{})
	for i := range grid.Cells {
		if grid.Cells[i].Flags != 0 {
			t.Fatal("an empty graph flagged cells")
		}
	}
}

func TestApplyNavObstacleProximityDoublesTheRing(t *testing.T) {
	grid := openGrid()
	grid.Cells[30*components.NavGridSide+30].Cost = 0 // one blocked cell
	applyNavObstacleProximity(grid)

	if got := cellAtXZ(grid, 31, 30).Cost; got != navCostOpen*2 {
		t.Errorf("cardinal neighbour cost %d, want %d", got, navCostOpen*2)
	}
	if got := cellAtXZ(grid, 31, 31).Cost; got != navCostOpen*2 {
		t.Errorf("diagonal neighbour cost %d, want %d", got, navCostOpen*2)
	}
	if got := cellAtXZ(grid, 32, 30).Cost; got != navCostOpen {
		t.Errorf("second ring cost %d, want %d untouched", got, navCostOpen)
	}
	if got := cellAtXZ(grid, 30, 30).Cost; got != 0 {
		t.Error("the blocked cell itself was given a cost")
	}
}

// The penalty must not spread: it reads a snapshot, so a doubled cell is not
// itself an obstacle for its own neighbours.
func TestApplyNavObstacleProximityDoesNotCascade(t *testing.T) {
	grid := openGrid()
	grid.Cells[30*components.NavGridSide+30].Cost = 0
	applyNavObstacleProximity(grid)
	if got := cellAtXZ(grid, 33, 30).Cost; got != navCostOpen {
		t.Fatalf("third ring cost %d — the penalty cascaded", got)
	}
}

func TestApplyNavObstacleProximityTreatsFootprintsAsObstacles(t *testing.T) {
	grid := openGrid()
	grid.Cells[30*components.NavGridSide+30].Flags |= components.NavInBuilding
	applyNavObstacleProximity(grid)
	if got := cellAtXZ(grid, 31, 30).Cost; got != navCostOpen*2 {
		t.Fatalf("cell beside a footprint cost %d, want %d", got, navCostOpen*2)
	}
}

func TestApplyNavObstacleProximityNeverOverflows(t *testing.T) {
	grid := openGrid()
	for i := range grid.Cells {
		grid.Cells[i].Cost = 200
	}
	grid.Cells[30*components.NavGridSide+30].Cost = 0
	applyNavObstacleProximity(grid)
	if got := cellAtXZ(grid, 31, 30).Cost; got != 200 {
		t.Fatalf("a 200-cost cell became %d — doubling wrapped uint8", got)
	}
}

func TestApplyNavObstacleProximityLeavesAClearGridAlone(t *testing.T) {
	grid := openGrid()
	applyNavObstacleProximity(grid)
	for i := range grid.Cells {
		if grid.Cells[i].Cost != navCostOpen {
			t.Fatalf("cell %d cost %d on an obstacle-free grid", i, grid.Cells[i].Cost)
		}
	}
}

func TestApplyNavObstacleProximityHandlesGridEdges(t *testing.T) {
	grid := openGrid()
	grid.Cells[0].Cost = 0
	last := components.NavGridSide - 1
	grid.Cells[last*components.NavGridSide+last].Cost = 0
	applyNavObstacleProximity(grid) // must not index out of range
	if got := cellAtXZ(grid, 1, 0).Cost; got != navCostOpen*2 {
		t.Errorf("neighbour of the corner obstacle cost %d", got)
	}
	if got := cellAtXZ(grid, last-1, last).Cost; got != navCostOpen*2 {
		t.Errorf("neighbour of the far corner obstacle cost %d", got)
	}
}

func TestClearDoorOutsideNavInBuilding(t *testing.T) {
	grid := openGrid()
	for i := range grid.Cells {
		grid.Cells[i].Flags |= components.NavInBuilding
	}
	e := bakeWall(20.5, 30.5, math.Pi/2, 6, 0.3)
	e.w.OpeningKind = components.OpeningDoor
	e.w.OpeningCenterT = 0.5
	e.w.OpeningWidth = 1.2
	e.outward = rl.Vector3{Z: 1} // door faces +Z
	walls := map[components.ChunkCoord][]wallEntry{{}: {e}}

	clearDoorOutsideNavInBuilding(grid, components.ChunkCoord{}, walls)

	// Door centre is (23.5, 30.5); the outside sample sits 0.7 m along +Z.
	if got := cellAtXZ(grid, 23, 31).Flags; got&components.NavInBuilding != 0 {
		t.Error("the outside cell of the door is still flagged NavInBuilding")
	}
	if got := cellAtXZ(grid, 23, 29).Flags; got&components.NavInBuilding == 0 {
		t.Error("the inside cell was cleared too")
	}
}

func TestClearDoorOutsideIgnoresNonDoors(t *testing.T) {
	grid := openGrid()
	for i := range grid.Cells {
		grid.Cells[i].Flags |= components.NavInBuilding
	}
	e := bakeWall(20.5, 30.5, math.Pi/2, 6, 0.3)
	e.w.OpeningKind = components.OpeningWindow
	e.w.OpeningCenterT = 0.5
	e.w.OpeningWidth = 1.2
	e.outward = rl.Vector3{Z: 1}
	clearDoorOutsideNavInBuilding(grid, components.ChunkCoord{},
		map[components.ChunkCoord][]wallEntry{{}: {e}})

	for i := range grid.Cells {
		if grid.Cells[i].Flags&components.NavInBuilding == 0 {
			t.Fatalf("cell %d cleared by a window", i)
		}
	}
}

func TestClearDoorOutsideReachesAcrossChunks(t *testing.T) {
	grid := openGrid()
	for i := range grid.Cells {
		grid.Cells[i].Flags |= components.NavInBuilding
	}
	// A door owned by the chunk to the west, opening east into this one.
	e := bakeWall(63.5, 30.5, math.Pi/2, 2, 0.3)
	e.w.OpeningKind = components.OpeningDoor
	e.w.OpeningCenterT = 0
	e.w.OpeningWidth = 1.2
	e.outward = rl.Vector3{X: 1}
	walls := map[components.ChunkCoord][]wallEntry{{X: -1}: {e}}

	clearDoorOutsideNavInBuilding(grid, components.ChunkCoord{}, walls)

	// World x = -64 + 63.5 + 0.7 = 0.2 → cell 0 of this chunk.
	if got := cellAtXZ(grid, 0, 30).Flags; got&components.NavInBuilding != 0 {
		t.Fatal("a door in the neighbouring chunk did not clear its outside cell here")
	}
}
