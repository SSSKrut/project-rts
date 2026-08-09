package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func slotAt(x, y, z float32) BuildingSlot {
	return BuildingSlot{Pos: components.WorldPos{Local: rl.Vector3{X: x, Y: y, Z: z}}}
}

func manAt(x, y, z float32) SlotCandidate {
	return SlotCandidate{
		Pos:  components.WorldPos{Local: rl.Vector3{X: x, Y: y, Z: z}},
		Live: true,
	}
}

func TestSlotDeliveryCostIsPlanarDistance(t *testing.T) {
	got := slotDeliveryCost(manAt(0, 0, 0), slotAt(3, 0, 4))
	if !nearlyEqual(got, 5) {
		t.Errorf("cost = %f, want 5", got)
	}
}

// A slot one storey up must cost noticeably more than the same walk on the
// flat: stairs are a detour and a queue.
func TestSlotDeliveryCostChargesForStoreys(t *testing.T) {
	flat := slotDeliveryCost(manAt(0, 0, 0), slotAt(3, 0, 4))
	oneUp := slotDeliveryCost(manAt(0, 0, 0), slotAt(3, 3, 4))
	twoUp := slotDeliveryCost(manAt(0, 0, 0), slotAt(3, 6, 4))
	if !nearlyEqual(oneUp-flat, slotStoreyPenalty) {
		t.Errorf("one storey costs %f extra, want %f", oneUp-flat, slotStoreyPenalty)
	}
	if !nearlyEqual(twoUp-flat, 2*slotStoreyPenalty) {
		t.Errorf("two storeys cost %f extra, want %f", twoUp-flat, 2*slotStoreyPenalty)
	}
	// Ground-snap noise is not a storey.
	same := slotDeliveryCost(manAt(0, 0, 0), slotAt(3, 0.9, 4))
	if !nearlyEqual(same, flat) {
		t.Errorf("a 0.9 m Y difference charged %f extra, want none", same-flat)
	}
}

func TestAssignSlotsMatchesNearest(t *testing.T) {
	cands := []SlotCandidate{manAt(0, 0, 0), manAt(20, 0, 0)}
	slots := []BuildingSlot{slotAt(21, 0, 0), slotAt(1, 0, 0)}
	got := assignSlots(cands, slots, nil)
	if got[0] != 1 || got[1] != 0 {
		t.Errorf("assignment = %v, want [1 0] — each man takes the slot beside him", got)
	}
}

// Ownership is what keeps a squad from re-shuffling: a man holding a slot
// keeps it even when a nearer rival appears.
func TestAssignSlotsHoldIsSticky(t *testing.T) {
	holder := manAt(20, 0, 0)
	holder.Held = 1 + 0 // owns slot 0
	rival := manAt(0.5, 0, 0)
	slots := []BuildingSlot{slotAt(0, 0, 0), slotAt(30, 0, 0)}
	got := assignSlots([]SlotCandidate{holder, rival}, slots, nil)
	if got[0] != 0 {
		t.Errorf("holder got slot %d, want to keep slot 0", got[0])
	}
	if got[1] != 1 {
		t.Errorf("rival got slot %d, want the free slot 1", got[1])
	}
}

func TestAssignSlotsStaleHoldOutOfRangeIsIgnored(t *testing.T) {
	c := manAt(0, 0, 0)
	c.Held = 1 + 9 // a slot index that no longer exists
	got := assignSlots([]SlotCandidate{c}, []BuildingSlot{slotAt(1, 0, 0)}, nil)
	if got[0] != 0 {
		t.Errorf("assignment = %v, want the live slot 0 despite the stale hold", got)
	}
}

// Two men claiming the same stored slot: one keeps it, the other is re-matched
// rather than both pointing at one position.
func TestAssignSlotsDuplicateHoldResolves(t *testing.T) {
	a := manAt(0, 0, 0)
	a.Held = 1
	b := manAt(10, 0, 0)
	b.Held = 1
	slots := []BuildingSlot{slotAt(0, 0, 0), slotAt(10, 0, 0)}
	got := assignSlots([]SlotCandidate{a, b}, slots, nil)
	if got[0] == got[1] {
		t.Fatalf("both men assigned slot %d", got[0])
	}
	if got[0] != 0 || got[1] != 1 {
		t.Errorf("assignment = %v, want [0 1]", got)
	}
}

// A man under an override is not a candidate: his position must go to
// somebody who can actually walk to it.
func TestAssignSlotsSkipsNonLiveAndFreesTheSlot(t *testing.T) {
	dead := manAt(0, 0, 0)
	dead.Live = false
	dead.Held = 1 // used to own slot 0
	live := manAt(5, 0, 0)
	slots := []BuildingSlot{slotAt(0, 0, 0), slotAt(50, 0, 0)}
	got := assignSlots([]SlotCandidate{dead, live}, slots, nil)
	if got[0] != noSlot {
		t.Errorf("non-live candidate got slot %d, want none", got[0])
	}
	if got[1] != 0 {
		t.Errorf("live man got slot %d, want the freed slot 0", got[1])
	}
}

// Fewer men than slots: the planner's PRIORITY PREFIX must be filled, not the
// cheapest pairs. Dropping the far room is what leaves a storey unoccupied.
func TestAssignSlotsFillsPriorityPrefixWhenShortHanded(t *testing.T) {
	cands := []SlotCandidate{manAt(0, 0, 0), manAt(1, 0, 0)}
	slots := []BuildingSlot{
		slotAt(100, 0, 0), // far, but first in priority
		slotAt(90, 0, 0),
		slotAt(2, 0, 0), // near, but a low-priority extra
	}
	got := assignSlots(cands, slots, nil)
	used := map[uint8]bool{got[0]: true, got[1]: true}
	if !used[0] || !used[1] {
		t.Errorf("assignment = %v, want the two priority slots 0 and 1 filled", got)
	}
	if used[2] {
		t.Errorf("assignment = %v, want the low-priority extra left empty", got)
	}
}

// Slot-major order: the FIRST slot picks its best man before later slots take
// anybody. Cheapest-pair-first hands out the near positions and leaves an
// important far one to whoever is left, who then arrives late or not at all.
func TestAssignSlotsPrioritySlotPicksItsBestMan(t *testing.T) {
	// Man 0 is nearest both slots; slot 0 is the priority position.
	cands := []SlotCandidate{manAt(0, 0, 0), manAt(50, 0, 0)}
	slots := []BuildingSlot{slotAt(10, 0, 0), slotAt(1, 0, 0)}
	got := assignSlots(cands, slots, nil)
	if got[0] != 0 {
		t.Errorf("assignment = %v, want man 0 on the priority slot 0", got)
	}
	if got[1] != 1 {
		t.Errorf("assignment = %v, want man 1 on slot 1", got)
	}
}

func TestAssignSlotsMoreMenThanSlots(t *testing.T) {
	cands := []SlotCandidate{manAt(0, 0, 0), manAt(1, 0, 0), manAt(2, 0, 0)}
	got := assignSlots(cands, []BuildingSlot{slotAt(0, 0, 0)}, nil)
	assigned := 0
	for _, v := range got {
		if v != noSlot {
			assigned++
		}
	}
	if assigned != 1 {
		t.Errorf("assignment = %v, want exactly one man placed", got)
	}
}

func TestAssignSlotsEmptyInputs(t *testing.T) {
	if got := assignSlots(nil, []BuildingSlot{slotAt(0, 0, 0)}, nil); len(got) != 0 {
		t.Errorf("got %v for no candidates, want empty", got)
	}
	got := assignSlots([]SlotCandidate{manAt(0, 0, 0)}, nil, nil)
	if len(got) != 1 || got[0] != noSlot {
		t.Errorf("got %v with no slots, want [noSlot]", got)
	}
	dead := manAt(0, 0, 0)
	dead.Live = false
	got = assignSlots([]SlotCandidate{dead}, []BuildingSlot{slotAt(0, 0, 0)}, nil)
	if got[0] != noSlot {
		t.Errorf("got %v with no live candidates, want [noSlot]", got)
	}
}

// Equal costs must resolve the same way every run — the replay hash notices
// otherwise.
func TestAssignSlotsIsDeterministicOnTies(t *testing.T) {
	cands := []SlotCandidate{manAt(0, 0, 0), manAt(0, 0, 0), manAt(0, 0, 0)}
	slots := []BuildingSlot{slotAt(5, 0, 0), slotAt(-5, 0, 0), slotAt(0, 0, 5)}
	first := assignSlots(cands, slots, nil)
	for i := 0; i < 8; i++ {
		again := assignSlots(cands, slots, nil)
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("run %d gave %v, first run gave %v", i, again, first)
			}
		}
	}
	// Lowest candidate index wins the tie.
	if first[0] != 0 {
		t.Errorf("tie went to candidate order %v, want candidate 0 on slot 0", first)
	}
}

func TestAssignSlotsReusesBuffer(t *testing.T) {
	buf := make([]uint8, 0, 4)
	got := assignSlots([]SlotCandidate{manAt(0, 0, 0)}, []BuildingSlot{slotAt(0, 0, 0)}, buf)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if &got[0] != &buf[:1][0] {
		t.Error("assignSlots allocated instead of reusing the caller's buffer")
	}
}

func TestSlotIndexAtGuardsShortTables(t *testing.T) {
	if got := slotIndexAt(nil, 0); got != noSlot {
		t.Errorf("nil table gave %d, want noSlot", got)
	}
	if got := slotIndexAt([]uint8{3}, 5); got != noSlot {
		t.Errorf("out-of-range index gave %d, want noSlot", got)
	}
	if got := slotIndexAt([]uint8{3}, 0); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestFacadeSectorQuantisesToFourWalls(t *testing.T) {
	// Yaw is atan2(outward.X, outward.Z): 0 = +Z, pi/2 = +X.
	cases := []struct {
		yaw  float32
		want int
	}{
		{0, 0}, {0.4, 0}, {-0.4, 0},
		{math.Pi / 2, 1},
		{math.Pi, 2}, {-math.Pi, 2},
		{-math.Pi / 2, 3},
	}
	for _, tc := range cases {
		if got := facadeSector(tc.yaw); got != tc.want {
			t.Errorf("facadeSector(%.2f) = %d, want %d", tc.yaw, got, tc.want)
		}
	}
}

// Without a facing order the squad defends all round: consecutive slots must
// come off different walls instead of filling one facade first.
func TestInterleaveByFacadeSpreadsWalls(t *testing.T) {
	north := BuildingSlot{Yaw: 0, Window: true}
	east := BuildingSlot{Yaw: math.Pi / 2, Window: true}
	slots := []BuildingSlot{north, north, north, east, east}
	got := interleaveByFacade(slots)
	if len(got) != len(slots) {
		t.Fatalf("len = %d, want %d — interleaving must not drop slots", len(got), len(slots))
	}
	if facadeSector(got[0].Yaw) == facadeSector(got[1].Yaw) {
		t.Errorf("first two slots share a facade: %v", []float32{got[0].Yaw, got[1].Yaw})
	}
	counts := map[int]int{}
	for _, s := range got {
		counts[facadeSector(s.Yaw)]++
	}
	if counts[0] != 3 || counts[1] != 2 {
		t.Errorf("facade counts = %v, want the input multiset preserved", counts)
	}
}

func TestInterleaveByFacadeShortInputUnchanged(t *testing.T) {
	one := []BuildingSlot{{Yaw: 1}}
	if got := interleaveByFacade(one); len(got) != 1 || got[0].Yaw != 1 {
		t.Errorf("got %v, want the single slot untouched", got)
	}
	if got := interleaveByFacade(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestHeldSlotEncodingRoundTrips(t *testing.T) {
	var bb components.LocalBlackboard
	if _, ok := bb.HeldSlotIndex(); ok {
		t.Error("the zero value must read as 'no slot held'")
	}
	bb.SetHeldSlot(0)
	if idx, ok := bb.HeldSlotIndex(); !ok || idx != 0 {
		t.Errorf("slot 0 round-tripped to (%d, %v)", idx, ok)
	}
	bb.SetHeldSlot(7)
	if idx, ok := bb.HeldSlotIndex(); !ok || idx != 7 {
		t.Errorf("slot 7 round-tripped to (%d, %v)", idx, ok)
	}
	bb.ClearHeldSlot()
	if _, ok := bb.HeldSlotIndex(); ok {
		t.Error("ClearHeldSlot left a slot held")
	}
}
