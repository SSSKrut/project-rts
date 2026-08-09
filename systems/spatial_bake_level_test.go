package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// openLevel is a seeded storey grid: every cell walkable, as the bake leaves
// it before walls are rasterised.
func openLevel(sizeX, sizeZ uint8) *components.LevelNavGrid {
	g := &components.LevelNavGrid{SizeX: sizeX, SizeZ: sizeZ}
	for cj := uint8(0); cj < sizeZ; cj++ {
		for ci := uint8(0); ci < sizeX; ci++ {
			g.Cells[int(cj)*components.MaxLevelSide+int(ci)] = components.NavCell{
				Cost: navCostOpen, Flags: components.NavInBuilding,
			}
		}
	}
	return g
}

func levelCellAt(g *components.LevelNavGrid, ci, cj int) components.NavCell {
	return g.Cells[cj*components.MaxLevelSide+ci]
}

func levelBlocked(g *components.LevelNavGrid) int {
	n := 0
	for cj := 0; cj < int(g.SizeZ); cj++ {
		for ci := 0; ci < int(g.SizeX); ci++ {
			if levelCellAt(g, ci, cj).Cost == 0 {
				n++
			}
		}
	}
	return n
}

func partition(length, yaw float32) components.WallSegment {
	return components.WallSegment{Length: length, Yaw: yaw, Thickness: 0.3, Height: 3}
}

func TestRasterizeFloorWallBlocksItsLine(t *testing.T) {
	g := openLevel(20, 20)
	// A partition running along +X across the middle of the storey.
	rasterizeFloorWall(g, rl.Vector3{X: 0, Z: 10}, partition(20, math.Pi/2), false, 0, 0)

	if got := levelCellAt(g, 5, 10).Cost; got != 0 {
		t.Errorf("cell on the wall line cost %d, want 0", got)
	}
	if got := levelCellAt(g, 5, 5).Cost; got == 0 {
		t.Error("a cell 5 m off the wall was blocked")
	}
}

// Unlike the surface grid, the level grid inflates walls by half a cell: a
// room partition has to actually separate two rooms in a 1 m grid.
func TestRasterizeFloorWallInflatesUnlikeTheSurfacePass(t *testing.T) {
	level := openLevel(20, 20)
	rasterizeFloorWall(level, rl.Vector3{X: 0, Z: 10}, partition(20, math.Pi/2), false, 0, 0)

	surface := openGrid()
	rasterizeWall(surface, bakeWall(0, 10, math.Pi/2, 20, 0.3))

	if levelBlocked(level) == 0 {
		t.Fatal("a 0.3 m partition blocked nothing on the level grid")
	}
	if blockedCount(surface) != 0 {
		t.Fatal("the surface pass blocked cells for a 0.3 m wall — the inflate divergence is gone")
	}
}

func TestRasterizeFloorWallSeparatesTwoRooms(t *testing.T) {
	g := openLevel(20, 20)
	rasterizeFloorWall(g, rl.Vector3{X: 0, Z: 10}, partition(20, math.Pi/2), false, 0, 0)
	// Walking north to south must cross a blocked row somewhere.
	for ci := 0; ci < 20; ci++ {
		open := true
		for cj := 0; cj < 20; cj++ {
			if levelCellAt(g, ci, cj).Cost == 0 {
				open = false
				break
			}
		}
		if open {
			t.Fatalf("column %d has no blocked cell — the partition leaks", ci)
		}
	}
}

func TestRasterizeFloorWallCarvesAnOpenDoor(t *testing.T) {
	w := partition(20, math.Pi/2)
	w.OpeningKind = components.OpeningDoor
	w.OpeningCenterT = 0.5
	w.OpeningWidth = 1.2

	shut := openLevel(20, 20)
	rasterizeFloorWall(shut, rl.Vector3{X: 0, Z: 10}, w, false, 0, 0)
	open := openLevel(20, 20)
	rasterizeFloorWall(open, rl.Vector3{X: 0, Z: 10}, w, true, 0, 0)

	if levelBlocked(open) >= levelBlocked(shut) {
		t.Fatalf("open door blocked %d cells, closed %d", levelBlocked(open), levelBlocked(shut))
	}
}

// The property that matters for entering a room: an open door must leave a
// walkable cell, wherever along the wall it sits and whichever way the wall
// runs. A doorway that quantises away corks the storey.
func TestOpenDoorAlwaysLeavesAWalkableCell(t *testing.T) {
	for _, yawDeg := range []float32{0, 30, 45, 60, 90, 135, 180, 225, 270} {
		for _, centreT := range []float32{0.25, 0.4, 0.5, 0.6, 0.75} {
			w := partition(16, yawDeg*math.Pi/180)
			w.OpeningKind = components.OpeningDoor
			w.OpeningCenterT = centreT
			w.OpeningWidth = 1.2

			g := openLevel(32, 32)
			// Start mid-grid so a wall in any direction stays inside.
			from := rl.Vector3{X: 16, Z: 16}
			rasterizeFloorWall(g, from, w, true, 0, 0)

			sa := float32(math.Sin(float64(w.Yaw)))
			ca := float32(math.Cos(float64(w.Yaw)))
			doorX := from.X + sa*centreT*w.Length
			doorZ := from.Z + ca*centreT*w.Length

			found := false
			for cj := 0; cj < 32 && !found; cj++ {
				for ci := 0; ci < 32; ci++ {
					if levelCellAt(g, ci, cj).Cost == 0 {
						continue
					}
					dx := float32(ci) + 0.5 - doorX
					dz := float32(cj) + 0.5 - doorZ
					if dx*dx+dz*dz <= 1.5*1.5 {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("yaw %.0f centreT %.2f: no walkable cell within 1.5 m of the doorway",
					yawDeg, centreT)
			}
		}
	}
}

func TestRasterizeFloorWallHonoursTheOrigin(t *testing.T) {
	// Same wall, expressed once in level-local coords and once shifted with a
	// matching origin: the blocked set must be identical.
	plain := openLevel(20, 20)
	rasterizeFloorWall(plain, rl.Vector3{X: 2, Z: 10}, partition(12, math.Pi/2), false, 0, 0)

	shifted := openLevel(20, 20)
	rasterizeFloorWall(shifted, rl.Vector3{X: 102, Z: 110}, partition(12, math.Pi/2), false, 100, 100)

	for cj := 0; cj < 20; cj++ {
		for ci := 0; ci < 20; ci++ {
			if levelCellAt(plain, ci, cj).Cost != levelCellAt(shifted, ci, cj).Cost {
				t.Fatalf("cell (%d, %d) differs between origin 0 and origin 100", ci, cj)
			}
		}
	}
}

func TestRasterizeFloorWallClipsToTheStorey(t *testing.T) {
	g := openLevel(8, 8)
	rasterizeFloorWall(g, rl.Vector3{X: 0, Z: 4}, partition(200, math.Pi/2), false, 0, 0)
	// Nothing outside the declared 8x8 block may be touched.
	for cj := 0; cj < components.MaxLevelSide; cj++ {
		for ci := 0; ci < components.MaxLevelSide; ci++ {
			if ci < 8 && cj < 8 {
				continue
			}
			if levelCellAt(g, ci, cj).Cost != 0 || levelCellAt(g, ci, cj).Flags != 0 {
				t.Fatalf("cell (%d, %d) outside the storey was written", ci, cj)
			}
		}
	}
	if levelBlocked(g) == 0 {
		t.Fatal("the in-storey part of the wall blocked nothing")
	}
}

func TestRasterizeFloorWallIgnoresDegenerateWalls(t *testing.T) {
	g := openLevel(20, 20)
	rasterizeFloorWall(g, rl.Vector3{X: 5, Z: 5}, partition(0, 0), false, 0, 0)
	rasterizeFloorWall(g, rl.Vector3{X: 5, Z: 5}, partition(-4, 0), false, 0, 0)
	if n := levelBlocked(g); n != 0 {
		t.Fatalf("degenerate walls blocked %d cells", n)
	}
}

func TestRasterizeFloorWallOutsideTheStoreyIsANoop(t *testing.T) {
	g := openLevel(20, 20)
	rasterizeFloorWall(g, rl.Vector3{X: -100, Z: -100}, partition(10, math.Pi/2), false, 0, 0)
	rasterizeFloorWall(g, rl.Vector3{X: 500, Z: 500}, partition(10, math.Pi/2), false, 0, 0)
	if n := levelBlocked(g); n != 0 {
		t.Fatalf("walls outside the storey blocked %d cells", n)
	}
}

func TestRasterizeFloorWallLeavesFlagsAlone(t *testing.T) {
	g := openLevel(20, 20)
	rasterizeFloorWall(g, rl.Vector3{X: 0, Z: 10}, partition(20, math.Pi/2), false, 0, 0)
	if got := levelCellAt(g, 5, 10).Flags; got&components.NavInBuilding == 0 {
		t.Fatal("rasterising dropped the NavInBuilding flag")
	}
}

func TestRasterizeFloorWallBlocksJustPastTheEnds(t *testing.T) {
	g := openLevel(20, 20)
	// A 6 m wall from x=5: the inflate reaches half a cell past each end.
	rasterizeFloorWall(g, rl.Vector3{X: 5, Z: 10}, partition(6, math.Pi/2), false, 0, 0)
	if got := levelCellAt(g, 7, 10).Cost; got != 0 {
		t.Errorf("mid-wall cell cost %d, want 0", got)
	}
	if got := levelCellAt(g, 14, 10).Cost; got == 0 {
		t.Error("a cell 3 m past the wall end was blocked")
	}
}
