package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// A wall from (from) running `length` metres along `yaw`.
func makeLOSWall(fromX, fromZ, yaw, length float32) losWall {
	return losWall{
		fromX: fromX, fromZ: fromZ,
		sa:     float32(math.Sin(float64(yaw))),
		ca:     float32(math.Cos(float64(yaw))),
		length: length,
	}
}

func TestSegmentSegmentIntersect2DFindsTheCrossing(t *testing.T) {
	t1, t2, ok := segmentSegmentIntersect2D(0, 0, 10, 0, 5, -5, 5, 5)
	if !ok {
		t.Fatal("perpendicular segments reported no intersection")
	}
	if math.Abs(float64(t1-0.5)) > 1e-3 || math.Abs(float64(t2-0.5)) > 1e-3 {
		t.Fatalf("params (%.3f, %.3f), want (0.5, 0.5)", t1, t2)
	}
}

func TestSegmentSegmentIntersect2DRejectsParallel(t *testing.T) {
	if _, _, ok := segmentSegmentIntersect2D(0, 0, 10, 0, 0, 5, 10, 5); ok {
		t.Error("parallel segments reported an intersection")
	}
	if _, _, ok := segmentSegmentIntersect2D(0, 0, 10, 0, 0, 0, 10, 0); ok {
		t.Error("collinear segments reported an intersection")
	}
}

// Params outside [0,1] mean the infinite lines cross but the segments do not;
// callers must range-check, so the helper has to report the true params.
func TestSegmentSegmentIntersect2DReportsOutOfRangeParams(t *testing.T) {
	t1, _, ok := segmentSegmentIntersect2D(0, 0, 10, 0, 50, -5, 50, 5)
	if !ok {
		t.Fatal("no intersection reported")
	}
	if t1 <= 1 {
		t.Fatalf("t1 = %.3f, want > 1 (the crossing is past the segment end)", t1)
	}
}

func TestSegmentToWallsTStopsAtTheNearestWall(t *testing.T) {
	walls := []losWall{
		makeLOSWall(8, -5, 0, 10),  // crosses at x = 8
		makeLOSWall(3, -5, 0, 10),  // crosses at x = 3, nearer
		makeLOSWall(20, -5, 0, 10), // beyond the shot
	}
	tHit, blocked := segmentToWallsT(walls, 0, 0, 10, 0)
	if !blocked {
		t.Fatal("the shot passed through three walls")
	}
	if math.Abs(float64(tHit-0.3)) > 1e-3 {
		t.Fatalf("hit at t = %.3f, want 0.3 (the nearest wall)", tHit)
	}
}

func TestSegmentToWallsTIgnoresWallsOffTheLine(t *testing.T) {
	walls := []losWall{makeLOSWall(5, 20, 0, 10)}
	if _, blocked := segmentToWallsT(walls, 0, 0, 10, 0); blocked {
		t.Fatal("a wall 20 m to the side blocked the shot")
	}
	if _, blocked := segmentToWallsT(nil, 0, 0, 10, 0); blocked {
		t.Fatal("no walls blocked the shot")
	}
}

func TestSegmentToWallsTPassesThroughATransparentOpening(t *testing.T) {
	w := makeLOSWall(5, -5, 0, 10)
	w.openingPresent = true
	w.openStart, w.openEnd = 4, 6 // the shot crosses at 5 m along the wall
	w.openingTransparent = true
	if _, blocked := segmentToWallsT([]losWall{w}, 0, 0, 10, 0); blocked {
		t.Fatal("a window in line with the shot blocked it")
	}

	w.openingTransparent = false
	if _, blocked := segmentToWallsT([]losWall{w}, 0, 0, 10, 0); !blocked {
		t.Fatal("a closed door must block")
	}
}

func TestSegmentToWallsTBlocksBesideTheOpening(t *testing.T) {
	w := makeLOSWall(5, -5, 0, 10)
	w.openingPresent = true
	w.openStart, w.openEnd = 0, 2 // opening is at the far end of the wall
	w.openingTransparent = true
	if _, blocked := segmentToWallsT([]losWall{w}, 0, 0, 10, 0); !blocked {
		t.Fatal("solid wall beside the window let the shot through")
	}
}

func TestSegmentPointHitDetectsABodyOnTheLine(t *testing.T) {
	// Reported bool is "clear": false means the segment struck the circle.
	if _, clear := segmentPointHit(0, 0, 10, 0, 5, 0.2, 0.5); clear {
		t.Fatal("a body on the line was not hit")
	}
	if _, clear := segmentPointHit(0, 0, 10, 0, 5, 3, 0.5); !clear {
		t.Fatal("a body 3 m off the line was hit")
	}
}

func TestSegmentPointHitIgnoresBodiesOffTheSpan(t *testing.T) {
	if _, clear := segmentPointHit(0, 0, 10, 0, -5, 0, 0.5); !clear {
		t.Error("a body behind the muzzle was hit")
	}
	if _, clear := segmentPointHit(0, 0, 10, 0, 15, 0, 0.5); !clear {
		t.Error("a body past the impact point was hit")
	}
}

func TestSegmentPointHitReportsWhereAlongTheShot(t *testing.T) {
	tt, clear := segmentPointHit(0, 0, 20, 0, 5, 0, 0.5)
	if clear {
		t.Fatal("expected a hit")
	}
	if math.Abs(float64(tt-0.25)) > 1e-3 {
		t.Fatalf("hit at t = %.3f, want 0.25", tt)
	}
}

func TestSegmentPointHitDegenerateShot(t *testing.T) {
	if _, clear := segmentPointHit(1, 1, 1, 1, 5, 5, 0.5); !clear {
		t.Fatal("a zero-length shot must not hit anything")
	}
}

func vehicleTarget(yaw float32) targetSnap {
	return targetSnap{
		pos:     navPos(0, 0, 20, 20),
		hullYaw: yaw,
		armorF:  0.2,
		armorS:  0.6,
		armorR:  1.0,
	}
}

func TestArmorSectorMulPicksTheStruckFace(t *testing.T) {
	// Yaw 0 points the hull along +Z.
	tgt := vehicleTarget(0)
	// Muzzle further along +Z than the hull: the shot comes head-on.
	if got := armorSectorMul(&tgt, 20, 40); got != tgt.armorF {
		t.Errorf("head-on: %.2f, want front %.2f", got, tgt.armorF)
	}
	// Muzzle behind the hull.
	if got := armorSectorMul(&tgt, 20, 0); got != tgt.armorR {
		t.Errorf("from behind: %.2f, want rear %.2f", got, tgt.armorR)
	}
	// Muzzle abeam.
	if got := armorSectorMul(&tgt, 40, 20); got != tgt.armorS {
		t.Errorf("abeam: %.2f, want side %.2f", got, tgt.armorS)
	}
}

func TestArmorSectorMulRotatesWithTheHull(t *testing.T) {
	tgt := vehicleTarget(math.Pi / 2) // hull now points along +X
	if got := armorSectorMul(&tgt, 40, 20); got != tgt.armorF {
		t.Errorf("head-on after rotation: %.2f, want front %.2f", got, tgt.armorF)
	}
	if got := armorSectorMul(&tgt, 20, 40); got != tgt.armorS {
		t.Errorf("abeam after rotation: %.2f, want side %.2f", got, tgt.armorS)
	}
}

func TestArmorSectorMulDegenerateRangeIsSide(t *testing.T) {
	tgt := vehicleTarget(0)
	if got := armorSectorMul(&tgt, 20, 20); got != tgt.armorS {
		t.Fatalf("muzzle inside the hull: %.2f, want side %.2f", got, tgt.armorS)
	}
}

func TestLocalWallsGathersTheNineChunkWindow(t *testing.T) {
	byChunk := map[components.ChunkCoord][]losWall{
		{X: 0, Z: 0}:  {makeLOSWall(0, 0, 0, 1)},
		{X: 1, Z: 0}:  {makeLOSWall(1, 0, 0, 1)},
		{X: -1, Z: 1}: {makeLOSWall(2, 0, 0, 1), makeLOSWall(3, 0, 0, 1)},
		{X: 5, Z: 5}:  {makeLOSWall(4, 0, 0, 1)}, // outside the window
	}
	got := localWalls(byChunk, components.ChunkCoord{})
	if len(got) != 4 {
		t.Fatalf("gathered %d walls, want 4", len(got))
	}
	for _, w := range got {
		if w.fromX == 4 {
			t.Fatal("a wall from outside the 3x3 window leaked in")
		}
	}
}

func TestLocalWallsEmptyWindowIsNil(t *testing.T) {
	if got := localWalls(map[components.ChunkCoord][]losWall{}, components.ChunkCoord{}); got != nil {
		t.Fatalf("got %d walls, want nil", len(got))
	}
}

func TestWorldXYZUnfoldsChunkAndLifts(t *testing.T) {
	got := worldXYZ(components.WorldPos{
		Chunk: components.ChunkCoord{X: 2, Z: -1},
		Local: rl.Vector3{X: 10, Y: 5, Z: 20},
	}, 1.5)
	if got.X != 2*components.ChunkSize+10 || got.Z != -components.ChunkSize+20 {
		t.Errorf("XZ = (%.2f, %.2f)", got.X, got.Z)
	}
	if got.Y != 6.5 {
		t.Errorf("Y = %.2f, want 6.5", got.Y)
	}
}

func TestWorldDistSqIsHorizontal(t *testing.T) {
	a := navPos(0, 0, 0, 0)
	b := components.WorldPos{Local: rl.Vector3{X: 3, Y: 100, Z: 4}}
	if got := worldDistSq(a, b); math.Abs(float64(got-25)) > 1e-3 {
		t.Fatalf("dist^2 = %.3f, want 25 (height must not count)", got)
	}
}

func TestLerpVec3(t *testing.T) {
	a := rl.Vector3{X: 0, Y: 0, Z: 0}
	b := rl.Vector3{X: 10, Y: 20, Z: -40}
	mid := lerpVec3(a, b, 0.5)
	if mid.X != 5 || mid.Y != 10 || mid.Z != -20 {
		t.Fatalf("mid = %+v", mid)
	}
	if got := lerpVec3(a, b, 0); got != a {
		t.Errorf("t=0 gave %+v", got)
	}
	if got := lerpVec3(a, b, 1); got != b {
		t.Errorf("t=1 gave %+v", got)
	}
}

func TestClampWeaponRange(t *testing.T) {
	if got := clampWeaponRange(-5); got != 0 {
		t.Errorf("negative range = %.2f, want 0", got)
	}
	if got := clampWeaponRange(50); got != 50 {
		t.Errorf("in-band range = %.2f, want 50", got)
	}
	if got := clampWeaponRange(weaponMaxRange * 10); got != weaponMaxRange {
		t.Errorf("oversized range = %.2f, want %.2f", got, weaponMaxRange)
	}
}

func TestSplitmixIsDeterministicAndSpreads(t *testing.T) {
	a, b := uint64(1), uint64(1)
	for i := 0; i < 8; i++ {
		if splitmix(&a) != splitmix(&b) {
			t.Fatal("same seed produced different streams")
		}
	}
	// The stream must stay inside the documented [0, 2^31) range and not
	// collapse onto a handful of values.
	seed := uint64(12345)
	seen := map[uint32]bool{}
	for i := 0; i < 2000; i++ {
		v := splitmix(&seed)
		if v >= 1<<31 {
			t.Fatalf("value %d exceeds 2^31", v)
		}
		seen[v] = true
	}
	if len(seen) < 1990 {
		t.Fatalf("only %d distinct values in 2000 draws", len(seen))
	}
}

func TestSplitmixAdvancesTheSeed(t *testing.T) {
	seed := uint64(7)
	first := splitmix(&seed)
	if seed == 7 {
		t.Fatal("seed was not advanced")
	}
	if second := splitmix(&seed); second == first {
		t.Fatal("consecutive draws are identical")
	}
}
