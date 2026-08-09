package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// wallAt builds a colWall running along +Z from (x, zFrom) of the given
// length, standing on storey [yBase, yBase+height]. Yaw 0 means the wall
// direction is (sin 0, cos 0) = +Z, so it blocks movement along X.
func wallAt(x, zFrom, length, yBase, height float32) colWall {
	return makeColWall(
		components.WorldPos{Local: rl.Vector3{X: x, Y: yBase, Z: zFrom}},
		components.WallSegment{Length: length, Yaw: 0, Height: height},
		components.DoorClosed,
	)
}

// wallWithOpening is wallAt plus a centred opening of the given kind/width.
func wallWithOpening(x, zFrom, length, yBase, height float32,
	kind components.OpeningKind, centerT, width float32,
	door components.DoorState) colWall {

	return makeColWall(
		components.WorldPos{Local: rl.Vector3{X: x, Y: yBase, Z: zFrom}},
		components.WallSegment{
			Length: length, Yaw: 0, Height: height,
			OpeningKind: kind, OpeningCenterT: centerT, OpeningWidth: width,
		},
		door,
	)
}

func bucket(ws ...colWall) map[components.ChunkCoord][]colWall {
	return map[components.ChunkCoord][]colWall{{X: 0, Z: 0}: ws}
}

func TestMakeColWallGeometry(t *testing.T) {
	w := makeColWall(
		components.WorldPos{Chunk: components.ChunkCoord{X: 1, Z: 2},
			Local: rl.Vector3{X: 3, Y: 4, Z: 5}},
		components.WallSegment{
			Length: 10, Yaw: math.Pi / 2, Height: 3,
			OpeningKind: components.OpeningDoor, OpeningCenterT: 0.5, OpeningWidth: 2,
		},
		components.DoorOpen,
	)
	if !nearlyEqual(w.fromX, 1*components.ChunkSize+3) {
		t.Errorf("fromX = %f, want chunk-absolute %f", w.fromX, 1*components.ChunkSize+3)
	}
	if !nearlyEqual(w.fromZ, 2*components.ChunkSize+5) {
		t.Errorf("fromZ = %f, want chunk-absolute %f", w.fromZ, 2*components.ChunkSize+5)
	}
	// Yaw pi/2 → direction (sin, cos) = (1, 0): the wall runs along +X.
	if !nearlyEqual(w.sa, 1) || math.Abs(float64(w.ca)) > 1e-6 {
		t.Errorf("(sa, ca) = (%f, %f), want (1, 0) for yaw pi/2", w.sa, w.ca)
	}
	if !nearlyEqual(w.yBase, 4) || !nearlyEqual(w.yTop, 7) {
		t.Errorf("storey range = [%f, %f], want [4, 7]", w.yBase, w.yTop)
	}
	if !w.hasOpening || !w.openPassable {
		t.Errorf("open door should be a passable opening, got hasOpening=%v passable=%v",
			w.hasOpening, w.openPassable)
	}
	// Opening spans centerT*length ± width/2 in metres along the wall.
	if !nearlyEqual(w.openStart, 4) || !nearlyEqual(w.openEnd, 6) {
		t.Errorf("opening span = [%f, %f], want [4, 6]", w.openStart, w.openEnd)
	}
}

func TestMakeColWallOpeningPassability(t *testing.T) {
	cases := []struct {
		name      string
		kind      components.OpeningKind
		door      components.DoorState
		passable  bool
		hasOpenin bool
	}{
		{"plain wall", components.OpeningNone, components.DoorClosed, false, false},
		{"open door", components.OpeningDoor, components.DoorOpen, true, true},
		{"closed door", components.OpeningDoor, components.DoorClosed, false, true},
		// A window is an opening for LOS but never for movement.
		{"window", components.OpeningWindow, components.DoorOpen, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := makeColWall(components.WorldPos{},
				components.WallSegment{Length: 4, Height: 3,
					OpeningKind: tc.kind, OpeningCenterT: 0.5, OpeningWidth: 1},
				tc.door)
			if w.hasOpening != tc.hasOpenin {
				t.Errorf("hasOpening = %v, want %v", w.hasOpening, tc.hasOpenin)
			}
			if w.openPassable != tc.passable {
				t.Errorf("openPassable = %v, want %v", w.openPassable, tc.passable)
			}
		})
	}
}

func TestUnitMatchesWallStorey(t *testing.T) {
	// Wall occupies [3, 6]; the 0.3 m margin absorbs ground-snap noise at the
	// storey boundary.
	cases := []struct {
		unitY float32
		want  bool
	}{
		{3.0, true},
		{4.5, true},
		{6.0, true},
		{2.75, true},  // within the lower margin
		{6.25, true},  // within the upper margin
		{2.5, false},  // a storey below
		{6.5, false},  // a storey above
		{-1.0, false}, // basement
	}
	for _, tc := range cases {
		if got := unitMatchesWallStorey(tc.unitY, 3, 6); got != tc.want {
			t.Errorf("unitMatchesWallStorey(%.2f, 3, 6) = %v, want %v", tc.unitY, got, tc.want)
		}
	}
}

func TestClipStepAgainstWallsBlocksCrossing(t *testing.T) {
	// Wall along +Z at x = 20, spanning z ∈ [10, 30], storey [0, 3].
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	// Step from x = 18 straight at the wall, 4 m long: must stop short of 20.
	sx, sz := clipStepAgainstWalls(18, 20, 1, 4, 0, walls, components.ChunkCoord{})
	if sz != 0 {
		t.Errorf("lateral component = %f, want 0", sz)
	}
	if sx <= 0 {
		t.Errorf("step = %f, want a positive truncated step", sx)
	}
	if 18+sx >= 20 {
		t.Errorf("clipped end = %f, want strictly short of the wall at x=20", 18+sx)
	}
	if want := 2 - wallClipBackoff; !nearlyEqual(sx, want) {
		t.Errorf("step = %f, want %f (gap minus backoff)", sx, want)
	}
}

func TestClipStepAgainstWallsPassesWhenClear(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	cases := []struct {
		name           string
		x, z, y        float32
		stepX, stepZ   float32
		wantX, wantZ   float32
		describeReason string
	}{
		{"parallel to the wall", 18, 20, 1, 0, 5, 0, 5, "never crosses the line"},
		{"stops short", 15, 20, 1, 2, 0, 2, 0, "step ends before the wall"},
		{"beyond the segment ends", 18, 40, 1, 4, 0, 4, 0, "misses the 10..30 span"},
		{"another storey", 18, 20, 5, 4, 0, 4, 0, "wall is on the storey below"},
		{"zero step", 18, 20, 1, 0, 0, 0, 0, "nothing to clip"},
		{"moving away", 22, 20, 1, 4, 0, 4, 0, "leaving the wall behind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sx, sz := clipStepAgainstWalls(tc.x, tc.z, tc.y, tc.stepX, tc.stepZ,
				walls, components.ChunkCoord{})
			if !nearlyEqual(sx, tc.wantX) || !nearlyEqual(sz, tc.wantZ) {
				t.Errorf("step = (%f, %f), want (%f, %f) — %s",
					sx, sz, tc.wantX, tc.wantZ, tc.describeReason)
			}
		})
	}
}

func TestClipStepAgainstWallsNilMapIsIdentity(t *testing.T) {
	sx, sz := clipStepAgainstWalls(0, 0, 0, 3, 4, nil, components.ChunkCoord{})
	if !nearlyEqual(sx, 3) || !nearlyEqual(sz, 4) {
		t.Errorf("step = (%f, %f), want the input unchanged", sx, sz)
	}
}

func TestClipStepAgainstWallsOpenings(t *testing.T) {
	const wallX, zFrom, length = 20, 10, 20
	// Opening centred at t=0.5 → 10 m along the wall → world z = 20, 2 m wide.
	openDoor := bucket(wallWithOpening(wallX, zFrom, length, 0, 3,
		components.OpeningDoor, 0.5, 2, components.DoorOpen))
	closedDoor := bucket(wallWithOpening(wallX, zFrom, length, 0, 3,
		components.OpeningDoor, 0.5, 2, components.DoorClosed))
	window := bucket(wallWithOpening(wallX, zFrom, length, 0, 3,
		components.OpeningWindow, 0.5, 2, components.DoorOpen))

	cases := []struct {
		name    string
		walls   map[components.ChunkCoord][]colWall
		z       float32
		blocked bool
	}{
		{"through an open door", openDoor, 20, false},
		{"beside an open door", openDoor, 15, true},
		{"through a closed door", closedDoor, 20, true},
		{"through a window", window, 20, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sx, _ := clipStepAgainstWalls(18, tc.z, 1, 4, 0, tc.walls, components.ChunkCoord{})
			crossed := 18+sx >= wallX
			if crossed == tc.blocked {
				t.Errorf("crossed = %v (step %f), want blocked = %v", crossed, sx, tc.blocked)
			}
		})
	}
}

// A step angled into a wall must keep its along-wall component: truncating
// the whole vector would stall a walker who is sliding past a facade while a
// crowd clamps him out of a body.
func TestClipStepAgainstWallsSlidesRemainder(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	sx, sz := clipStepAgainstWalls(18, 20, 1, 4, 4, walls, components.ChunkCoord{})
	if 18+sx >= 20 {
		t.Errorf("crossed the wall: end X = %f", 18+sx)
	}
	if sz < 3.5 {
		t.Errorf("along-wall component = %f, want most of the 4 m preserved by the slide", sz)
	}
}

// Head-on into a facade there is no tangent to slide along, so the body
// simply arrives at the wall and stops there.
func TestClipStepAgainstWallsHeadOnStopsAtWall(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	sx, sz := clipStepAgainstWalls(18, 20, 1, 4, 0, walls, components.ChunkCoord{})
	if want := 2 - wallClipBackoff; !nearlyEqual(sx, want) {
		t.Errorf("step X = %f, want %f (up to the wall, minus backoff)", sx, want)
	}
	if !nearlyEqual(sz, 0) {
		t.Errorf("step Z = %f, want 0 — nothing to slide along", sz)
	}
}

// A corner: the slide off the first wall must not push the body through the
// second one.
func TestClipStepAgainstWallsCornerHoldsBothFaces(t *testing.T) {
	// Wall A along +Z at x = 20; wall B along +X at z = 30 (yaw pi/2).
	wallB := makeColWall(
		components.WorldPos{Local: rl.Vector3{X: 10, Y: 0, Z: 30}},
		components.WallSegment{Length: 20, Yaw: math.Pi / 2, Height: 3},
		components.DoorClosed,
	)
	walls := bucket(wallAt(20, 10, 20, 0, 3), wallB)
	sx, sz := clipStepAgainstWalls(19, 29, 1, 4, 4, walls, components.ChunkCoord{})
	if 19+sx >= 20 {
		t.Errorf("crossed wall A: end X = %f", 19+sx)
	}
	if 29+sz >= 30 {
		t.Errorf("crossed wall B: end Z = %f", 29+sz)
	}
}

// A body pressed flush against a facade must still be able to step away —
// the eps guard exists so a t1 ≈ 0 touch is not read as a crossing.
func TestClipStepAgainstWallsTouchingWallCanLeave(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	sx, _ := clipStepAgainstWalls(20, 20, 1, -3, 0, walls, components.ChunkCoord{})
	if !nearlyEqual(sx, -3) {
		t.Errorf("step = %f, want -3: standing on the line and moving away is not a crossing", sx)
	}
}

func TestClipStepAgainstWallsSearchesNeighbourChunks(t *testing.T) {
	// Wall lives in chunk (1,0) while the walker's home chunk is (0,0): the
	// 3×3 window has to find it or a body walks through the next chunk's wall.
	w := makeColWall(
		components.WorldPos{Chunk: components.ChunkCoord{X: 1},
			Local: rl.Vector3{X: 2, Y: 0, Z: 10}},
		components.WallSegment{Length: 20, Yaw: 0, Height: 3},
		components.DoorClosed,
	)
	walls := map[components.ChunkCoord][]colWall{{X: 1, Z: 0}: {w}}
	wallX := components.ChunkSize + 2
	sx, _ := clipStepAgainstWalls(wallX-2, 20, 1, 4, 0, walls, components.ChunkCoord{})
	if wallX-2+sx >= wallX {
		t.Errorf("crossed a wall parked in the neighbouring chunk (step %f)", sx)
	}
}

func TestWallBlocksApproach(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	pos := &components.WorldPos{Local: rl.Vector3{X: 18, Y: 1, Z: 20}}
	cases := []struct {
		name   string
		target components.WorldPos
		want   bool
	}{
		{"straight through the wall", components.WorldPos{Local: rl.Vector3{X: 22, Y: 1, Z: 20}}, true},
		{"same side", components.WorldPos{Local: rl.Vector3{X: 19, Y: 1, Z: 22}}, false},
		// Crosses the wall's LINE at z = 32.5, i.e. past the segment's end at 30.
		{"past the wall's end", components.WorldPos{Local: rl.Vector3{X: 22, Y: 1, Z: 45}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wallBlocksApproach(pos, tc.target, walls); got != tc.want {
				t.Errorf("wallBlocksApproach = %v, want %v", got, tc.want)
			}
		})
	}
	if wallBlocksApproach(pos, components.WorldPos{}, nil) {
		t.Error("a nil wall map must never report a block")
	}
}

func TestWallBlocksApproachOpenDoorIsNotABlock(t *testing.T) {
	walls := bucket(wallWithOpening(20, 10, 20, 0, 3,
		components.OpeningDoor, 0.5, 2, components.DoorOpen))
	pos := &components.WorldPos{Local: rl.Vector3{X: 18, Y: 1, Z: 20}}
	through := components.WorldPos{Local: rl.Vector3{X: 22, Y: 1, Z: 20}}
	if wallBlocksApproach(pos, through, walls) {
		t.Error("a line through an open doorway must not count as blocked")
	}
	beside := components.WorldPos{Local: rl.Vector3{X: 22, Y: 1, Z: 15}}
	if !wallBlocksApproach(pos, beside, walls) {
		t.Error("a line through the solid part of a doored wall must count as blocked")
	}
}

func TestWallBlocksApproachIgnoresOtherStoreys(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 3, 3)) // storey [3, 6]
	pos := &components.WorldPos{Local: rl.Vector3{X: 18, Y: 0.5, Z: 20}}
	through := components.WorldPos{Local: rl.Vector3{X: 22, Y: 0.5, Z: 20}}
	if wallBlocksApproach(pos, through, walls) {
		t.Error("a wall one storey up must not block a ground-floor approach")
	}
}

func TestReflectAgainstWallsSlidesAlongTangent(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	// Heading diagonally into the wall: the into-wall X component dies, the
	// along-wall Z component survives.
	vx, vz, escape := reflectAgainstWalls(19.5, 20, 1, 5, 3, 0.5, walls, components.ChunkCoord{})
	if vx > 1e-3 {
		t.Errorf("into-wall component = %f, want it removed", vx)
	}
	if !nearlyEqual(vz, 3) {
		t.Errorf("tangential component = %f, want 3 (preserved)", vz)
	}
	_ = escape
}

func TestReflectAgainstWallsLeavesFreeVelocityAlone(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	// Far from the wall and heading away: nothing to do.
	vx, vz, escape := reflectAgainstWalls(5, 20, 1, -2, 1, 0.1, walls, components.ChunkCoord{})
	if !nearlyEqual(vx, -2) || !nearlyEqual(vz, 1) {
		t.Errorf("velocity = (%f, %f), want (-2, 1) unchanged", vx, vz)
	}
	if escape {
		t.Error("escape spring engaged far from any wall")
	}
}

func TestReflectAgainstWallsEscapeSpringPushesOut(t *testing.T) {
	walls := bucket(wallAt(20, 10, 20, 0, 3))
	// Inside the 0.3 m escape margin with zero intent: the spring alone must
	// produce outward (negative X) motion.
	vx, _, escape := reflectAgainstWalls(19.9, 20, 1, 0, 0, 0.1, walls, components.ChunkCoord{})
	if !escape {
		t.Fatal("escape spring did not engage inside the margin")
	}
	if vx >= 0 {
		t.Errorf("escape velocity X = %f, want negative (away from the wall at x=20)", vx)
	}
}
