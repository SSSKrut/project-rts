package systems

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func room(minX, minZ, maxX, maxZ float32) components.AABB2D {
	return components.AABB2D{MinX: minX, MinZ: minZ, MaxX: maxX, MaxZ: maxZ}
}

func TestClampAxis(t *testing.T) {
	cases := []struct {
		name        string
		v, lo, hi   float32
		inset, want float32
	}{
		{"inside stays", 5, 0, 10, 1, 5},
		{"below clamps up", -3, 0, 10, 1, 1},
		{"above clamps down", 99, 0, 10, 1, 9},
		{"on the inset boundary", 1, 0, 10, 1, 1},
		// Narrower than two insets: the middle beats a position in the wall.
		{"degenerate collapses to centre", 99, 0, 1, 1, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampAxis(tc.v, tc.lo, tc.hi, tc.inset); !nearlyEqual(got, tc.want) {
				t.Errorf("clampAxis(%v, %v, %v, %v) = %v, want %v",
					tc.v, tc.lo, tc.hi, tc.inset, got, tc.want)
			}
		})
	}
}

func TestClampSlotToRoomKeepsPositionsOffWalls(t *testing.T) {
	r := room(10, 10, 20, 20)
	// The raw Loose scatter reaches 2×spacing from the centre — outside any
	// room smaller than that.
	out := clampSlotToRoom(components.WorldPos{Local: rl.Vector3{X: 40, Y: 3, Z: 15}}, r, 0.6)
	x, z := worldXZ(out)
	if !nearlyEqual(x, 19.4) {
		t.Errorf("X = %f, want 19.4 (max minus inset)", x)
	}
	if !nearlyEqual(z, 15) {
		t.Errorf("Z = %f, want 15 unchanged", z)
	}
	if !nearlyEqual(out.Local.Y, 3) {
		t.Errorf("Y = %f, want the storey height preserved", out.Local.Y)
	}
}

func TestClampSlotToRoomLeavesInteriorPositions(t *testing.T) {
	r := room(10, 10, 20, 20)
	in := components.WorldPos{Local: rl.Vector3{X: 15, Y: 0, Z: 16}}
	out := clampSlotToRoom(in, r, 0.6)
	x, z := worldXZ(out)
	if !nearlyEqual(x, 15) || !nearlyEqual(z, 16) {
		t.Errorf("position moved to (%f, %f), want (15, 16)", x, z)
	}
}

// A clamped slot must stay a canonical WorldPos: nav resolves the target
// through Pos.Chunk, so an absolute coordinate parked in Local aims the
// walker at the wrong chunk entirely.
func TestClampSlotToRoomReturnsNormalisedPos(t *testing.T) {
	r := room(100, 100, 120, 120)
	out := clampSlotToRoom(components.WorldPos{Local: rl.Vector3{X: 110, Z: 110}}, r, 0.6)
	if out.Local.X < 0 || out.Local.X >= components.ChunkSize ||
		out.Local.Z < 0 || out.Local.Z >= components.ChunkSize {
		t.Errorf("Local = %+v, want it folded into [0, %v)", out.Local, components.ChunkSize)
	}
	x, z := worldXZ(out)
	if !nearlyEqual(x, 110) || !nearlyEqual(z, 110) {
		t.Errorf("world position = (%f, %f), want (110, 110) preserved", x, z)
	}
}

func TestPushOutOfKeepOutsClearsTransitCircles(t *testing.T) {
	r := room(0, 0, 20, 20)
	zones := []slotKeepOut{{x: 10, z: 10, r: 1}}
	// Standing right on a doorway.
	out := pushOutOfKeepOuts(components.WorldPos{Local: rl.Vector3{X: 10.2, Z: 10}},
		zones, r, 0.6, true)
	x, z := worldXZ(out)
	dx, dz := x-10, z-10
	if d := dx*dx + dz*dz; d < 1-1e-3 {
		t.Errorf("distance from the doorway = %f, want >= 1", d)
	}
}

func TestPushOutOfKeepOutsLeavesClearPositions(t *testing.T) {
	r := room(0, 0, 20, 20)
	zones := []slotKeepOut{{x: 10, z: 10, r: 1}}
	in := components.WorldPos{Local: rl.Vector3{X: 5, Z: 5}}
	out := pushOutOfKeepOuts(in, zones, r, 0.6, true)
	x, z := worldXZ(out)
	if !nearlyEqual(x, 5) || !nearlyEqual(z, 5) {
		t.Errorf("position moved to (%f, %f), want (5, 5)", x, z)
	}
	if got := pushOutOfKeepOuts(in, nil, r, 0.6, true); got != in {
		t.Errorf("no zones changed the position to %+v", got.Local)
	}
}

// Dead centre of a circle has no outward direction; the push must still
// produce a finite position rather than a NaN.
func TestPushOutOfKeepOutsHandlesDeadCentre(t *testing.T) {
	r := room(0, 0, 20, 20)
	zones := []slotKeepOut{{x: 10, z: 10, r: 1}}
	out := pushOutOfKeepOuts(components.WorldPos{Local: rl.Vector3{X: 10, Z: 10}},
		zones, r, 0.6, true)
	x, z := worldXZ(out)
	if x != x || z != z {
		t.Fatalf("produced NaN at (%f, %f)", x, z)
	}
	dx, dz := x-10, z-10
	if d := dx*dx + dz*dz; d < 1-1e-3 {
		t.Errorf("distance from centre squared = %f, want >= 1", d)
	}
}

// The escape has to respect the room too: pushing out of a doorway must not
// land the slot inside a wall.
func TestPushOutOfKeepOutsStaysInsideRoom(t *testing.T) {
	r := room(0, 0, 20, 20)
	// Doorway hard against the room's east wall.
	zones := []slotKeepOut{{x: 20, z: 10, r: 2}}
	out := pushOutOfKeepOuts(components.WorldPos{Local: rl.Vector3{X: 19.5, Z: 10}},
		zones, r, 0.6, true)
	x, z := worldXZ(out)
	if x > 20-0.6+1e-3 {
		t.Errorf("X = %f, want inside the room (max 19.4)", x)
	}
	if z < 0 || z > 20 {
		t.Errorf("Z = %f, want inside the room", z)
	}
}

func TestAddKeepOutRespectsCap(t *testing.T) {
	var f slotFloor
	for i := 0; i < maxKeepOutsPerFloor+5; i++ {
		f.addKeepOut(float32(i), 0, 1)
	}
	if int(f.keepOutN) != maxKeepOutsPerFloor {
		t.Errorf("keepOutN = %d, want the cap %d", f.keepOutN, maxKeepOutsPerFloor)
	}
	if !nearlyEqual(f.keepOut[0].x, 0) {
		t.Error("the cap dropped the earliest entries instead of the overflow")
	}
}

// Every planned position must land inside its own room: the Loose scatter it
// starts from reaches 2×spacing, which is outside a small room.
func TestFillRoomSlotsClampsIntoRooms(t *testing.T) {
	fl := slotFloor{
		pos:    components.WorldPos{Local: rl.Vector3{X: 15, Y: 0, Z: 15}},
		roomsN: 2,
	}
	fl.rooms[0] = room(10, 10, 14, 14)
	fl.rooms[1] = room(16, 16, 20, 20)
	out := make([]BuildingSlot, 6)
	fillRoomSlots(out, 0, []slotFloor{fl}, slotRoomSpacing)
	for i, s := range out {
		x, z := worldXZ(s.Pos)
		r := fl.rooms[i%2]
		if x < r.MinX+slotRoomInset-1e-3 || x > r.MaxX-slotRoomInset+1e-3 ||
			z < r.MinZ+slotRoomInset-1e-3 || z > r.MaxZ-slotRoomInset+1e-3 {
			t.Errorf("slot %d at (%.2f, %.2f) is outside room %v with inset %v",
				i, x, z, r, slotRoomInset)
		}
	}
}

func TestFillRoomSlotsAvoidsTransitCircles(t *testing.T) {
	fl := slotFloor{
		pos:    components.WorldPos{Local: rl.Vector3{X: 15, Y: 0, Z: 15}},
		roomsN: 2,
	}
	fl.rooms[0] = room(4, 4, 14, 14)
	fl.rooms[1] = room(16, 16, 26, 26)
	fl.addKeepOut(9, 9, 1.5) // a stair landing in the middle of room 0
	out := make([]BuildingSlot, 6)
	fillRoomSlots(out, 0, []slotFloor{fl}, slotRoomSpacing)
	for i, s := range out {
		x, z := worldXZ(s.Pos)
		dx, dz := x-9, z-9
		if dx*dx+dz*dz < 1.5*1.5-1e-3 {
			t.Errorf("slot %d at (%.2f, %.2f) sits on the stair landing", i, x, z)
		}
	}
}

func TestFillRoomSlotsSpreadsAcrossFloors(t *testing.T) {
	mk := func(y float32) slotFloor {
		f := slotFloor{pos: components.WorldPos{Local: rl.Vector3{X: 15, Y: y, Z: 15}}}
		return f
	}
	floors := []slotFloor{mk(0), mk(3)}
	out := make([]BuildingSlot, 4)
	fillRoomSlots(out, 0, floors, slotRoomSpacing)
	low, high := 0, 0
	for _, s := range out {
		if s.Pos.Local.Y < 1.5 {
			low++
		} else {
			high++
		}
	}
	if low != 2 || high != 2 {
		t.Errorf("storey split = %d/%d, want 2/2", low, high)
	}
}

func TestFillRoomSlotsRespectsWindowOffset(t *testing.T) {
	fl := slotFloor{pos: components.WorldPos{Local: rl.Vector3{X: 15, Z: 15}}}
	out := make([]BuildingSlot, 4)
	out[0] = BuildingSlot{Pos: components.WorldPos{Local: rl.Vector3{X: 1, Z: 1}}, Window: true}
	fillRoomSlots(out, 1, []slotFloor{fl}, slotRoomSpacing)
	if !out[0].Window {
		t.Error("fillRoomSlots overwrote a window slot it was told to skip")
	}
	x, z := worldXZ(out[0].Pos)
	if !nearlyEqual(x, 1) || !nearlyEqual(z, 1) {
		t.Errorf("window slot moved to (%f, %f)", x, z)
	}
}

func TestFillRoomSlotsHandlesEmptyInput(t *testing.T) {
	out := make([]BuildingSlot, 2)
	fillRoomSlots(out, 0, nil, slotRoomSpacing)
	for i, s := range out {
		if s.Pos != (components.WorldPos{}) {
			t.Errorf("slot %d written despite having no floors: %+v", i, s.Pos)
		}
	}
	fillRoomSlots(out, 5, []slotFloor{{}}, slotRoomSpacing) // from beyond len
}
