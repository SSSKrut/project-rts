package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

var forwardZ = rl.Vector3{X: 0, Y: 0, Z: 1}

func TestFormationOffsetSlotZeroIsCentre(t *testing.T) {
	for _, kind := range []components.FormationKind{
		components.FormationLine, components.FormationColumn,
		components.FormationWedge, components.FormationLoose,
	} {
		x, z := FormationOffset(kind, 0, 3, forwardZ)
		if x != 0 || z != 0 {
			t.Errorf("kind %d slot 0 = (%f, %f), want the commander on centre", kind, x, z)
		}
	}
}

func TestFormationOffsetLineFansOutward(t *testing.T) {
	const spacing float32 = 2
	// Slots 1..4 alternate sides at growing distance: +1, -1, +2, -2.
	want := []float32{spacing, -spacing, 2 * spacing, -2 * spacing}
	for i, w := range want {
		slot := uint8(i + 1)
		x, z := FormationOffset(components.FormationLine, slot, spacing, forwardZ)
		// Forward +Z → right = (fz, -fx) = (1, 0), so the offset lands on X.
		if !nearlyEqual(x, w) {
			t.Errorf("slot %d lateral = %f, want %f", slot, x, w)
		}
		if !nearlyEqual(z, 0) {
			t.Errorf("slot %d has along-march component %f, want 0", slot, z)
		}
	}
}

func TestFormationOffsetColumnTrailsBehind(t *testing.T) {
	const spacing float32 = 2
	for slot := uint8(1); slot <= 3; slot++ {
		x, z := FormationOffset(components.FormationColumn, slot, spacing, forwardZ)
		if !nearlyEqual(x, 0) {
			t.Errorf("slot %d lateral = %f, want 0 in column", slot, x)
		}
		if want := -float32(slot) * spacing; !nearlyEqual(z, want) {
			t.Errorf("slot %d along-march = %f, want %f (behind the leader)", slot, z, want)
		}
	}
}

func TestFormationOffsetWedgeIsSymmetricAndBehind(t *testing.T) {
	const spacing float32 = 2
	x1, z1 := FormationOffset(components.FormationWedge, 1, spacing, forwardZ)
	x2, z2 := FormationOffset(components.FormationWedge, 2, spacing, forwardZ)
	if !nearlyEqual(x1, -x2) {
		t.Errorf("wedge pair not mirrored: %f vs %f", x1, x2)
	}
	if !nearlyEqual(z1, z2) {
		t.Errorf("wedge pair at different depths: %f vs %f", z1, z2)
	}
	if z1 >= 0 {
		t.Errorf("wedge row 1 depth = %f, want behind the leader", z1)
	}
}

// Rotating Forward must rotate the whole formation rigidly: distances from
// centre are invariant.
func TestFormationOffsetRotatesWithForward(t *testing.T) {
	const spacing float32 = 2
	forwardX := rl.Vector3{X: 1, Y: 0, Z: 0}
	for _, kind := range []components.FormationKind{
		components.FormationLine, components.FormationColumn, components.FormationWedge,
	} {
		for slot := uint8(1); slot <= 4; slot++ {
			ax, az := FormationOffset(kind, slot, spacing, forwardZ)
			bx, bz := FormationOffset(kind, slot, spacing, forwardX)
			ra := float32(math.Hypot(float64(ax), float64(az)))
			rb := float32(math.Hypot(float64(bx), float64(bz)))
			if !nearlyEqual(ra, rb) {
				t.Errorf("kind %d slot %d: radius %f vs %f after rotating Forward", kind, slot, ra, rb)
			}
		}
	}
}

func TestFormationOffsetDegenerateForwardFallsBack(t *testing.T) {
	// A zero Forward must not produce NaN; the code substitutes +Z.
	x, z := FormationOffset(components.FormationLine, 1, 2, rl.Vector3{})
	fx, fz := FormationOffset(components.FormationLine, 1, 2, forwardZ)
	if !nearlyEqual(x, fx) || !nearlyEqual(z, fz) {
		t.Errorf("degenerate forward gave (%f, %f), want the +Z fallback (%f, %f)", x, z, fx, fz)
	}
}

func TestFormationOffsetLooseIsDeterministicAndSpread(t *testing.T) {
	const spacing float32 = 2
	seen := map[[2]float32]bool{}
	for slot := uint8(1); slot <= 8; slot++ {
		x1, z1 := FormationOffset(components.FormationLoose, slot, spacing, forwardZ)
		x2, z2 := FormationOffset(components.FormationLoose, slot, spacing, forwardZ)
		if x1 != x2 || z1 != z2 {
			t.Errorf("slot %d not deterministic", slot)
		}
		r := float32(math.Hypot(float64(x1), float64(z1)))
		// Pushed out by at least 0.5×(2×spacing) so nobody piles on the leader.
		if r < spacing-1e-3 {
			t.Errorf("slot %d radius = %f, want >= %f", slot, r, spacing)
		}
		if r > 2*spacing+1e-3 {
			t.Errorf("slot %d radius = %f, want <= %f", slot, r, 2*spacing)
		}
		key := [2]float32{x1, z1}
		if seen[key] {
			t.Errorf("slot %d duplicates an earlier scatter position", slot)
		}
		seen[key] = true
	}
}

func TestSlotHash32IsDeterministicAndSpreads(t *testing.T) {
	if slotHash32(7) != slotHash32(7) {
		t.Error("hash is not deterministic")
	}
	seen := map[uint32]bool{}
	for i := uint32(0); i < 64; i++ {
		h := slotHash32(i)
		if seen[h] {
			t.Errorf("collision at input %d", i)
		}
		seen[h] = true
	}
}

func wpAt(x, z float32) components.WorldPos {
	return components.WorldPos{Local: rl.Vector3{X: x, Z: z}}
}

func TestWakeAnchorWalksAlongPath(t *testing.T) {
	mp := &components.MicroPath{Count: 3}
	mp.Waypoints[0] = wpAt(10, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	mp.Waypoints[2] = wpAt(20, 10)
	leader := wpAt(0, 0)

	// 5 m along the first segment.
	got, walked := wakeAnchor(leader, mp, 5)
	if !nearlyEqual(got.Local.X, 5) || !nearlyEqual(got.Local.Z, 0) {
		t.Errorf("anchor = (%f, %f), want (5, 0)", got.Local.X, got.Local.Z)
	}
	if !nearlyEqual(walked, 5) {
		t.Errorf("walked = %f, want 5", walked)
	}

	// 25 m: 20 m along X then 5 m up Z.
	got, walked = wakeAnchor(leader, mp, 25)
	if !nearlyEqual(got.Local.X, 20) || !nearlyEqual(got.Local.Z, 5) {
		t.Errorf("anchor = (%f, %f), want (20, 5)", got.Local.X, got.Local.Z)
	}
	if !nearlyEqual(walked, 25) {
		t.Errorf("walked = %f, want 25", walked)
	}
}

// Past the end the anchor clamps to the last waypoint, and `walked` must
// report the CLAMPED distance — the caller subtracts it back along forward,
// and reporting the requested distance stretches a parked file.
func TestWakeAnchorClampsAtPathEnd(t *testing.T) {
	mp := &components.MicroPath{Count: 2}
	mp.Waypoints[0] = wpAt(10, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	got, walked := wakeAnchor(wpAt(0, 0), mp, 100)
	if !nearlyEqual(got.Local.X, 20) {
		t.Errorf("anchor X = %f, want the last waypoint at 20", got.Local.X)
	}
	if !nearlyEqual(walked, 20) {
		t.Errorf("walked = %f, want the clamped 20, not the requested 100", walked)
	}
}

func TestWakeAnchorHonoursHead(t *testing.T) {
	// Head skips consumed waypoints: the walk starts at the leader and runs
	// along what is LEFT of the path.
	mp := &components.MicroPath{Count: 3, Head: 2}
	mp.Waypoints[0] = wpAt(10, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	mp.Waypoints[2] = wpAt(20, 10)
	got, _ := wakeAnchor(wpAt(20, 0), mp, 4)
	if !nearlyEqual(got.Local.X, 20) || !nearlyEqual(got.Local.Z, 4) {
		t.Errorf("anchor = (%f, %f), want (20, 4) — only the live tail counts",
			got.Local.X, got.Local.Z)
	}
}

func TestWakeAnchorExhaustedPathReturnsLeader(t *testing.T) {
	mp := &components.MicroPath{Count: 2, Head: 2}
	leader := wpAt(7, 3)
	got, walked := wakeAnchor(leader, mp, 5)
	if got != leader {
		t.Errorf("anchor = %+v, want the leader position %+v", got.Local, leader.Local)
	}
	if walked != 0 {
		t.Errorf("walked = %f, want 0", walked)
	}
}

func TestWakeAnchorZeroDistanceIsLeader(t *testing.T) {
	mp := &components.MicroPath{Count: 1}
	mp.Waypoints[0] = wpAt(10, 0)
	got, walked := wakeAnchor(wpAt(2, 0), mp, 0)
	if !nearlyEqual(got.Local.X, 2) {
		t.Errorf("anchor X = %f, want the leader at 2", got.Local.X)
	}
	if walked != 0 {
		t.Errorf("walked = %f, want 0", walked)
	}
}
