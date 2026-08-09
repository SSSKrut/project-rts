package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func moveTo(x, z float32) components.Action {
	return components.Action{
		Kind:   components.ActionMoveTo,
		Target: components.WorldPos{Local: rl.Vector3{X: x, Z: z}},
	}
}

// queueTargets lists the live actions in order, oldest first.
func queueTargets(q *components.ActionQueue) []float32 {
	out := make([]float32, 0, q.Count)
	for i := uint8(0); i < q.Count; i++ {
		idx := (q.Head + i) % components.ActionQueueSize
		out = append(out, q.Actions[idx].Target.Local.X)
	}
	return out
}

func equalF32(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPushActionAppendsInOrder(t *testing.T) {
	q := &components.ActionQueue{}
	for i := 1; i <= 3; i++ {
		PushAction(q, moveTo(float32(i), 0))
	}
	if q.Count != 3 {
		t.Fatalf("Count = %d, want 3", q.Count)
	}
	if got := queueTargets(q); !equalF32(got, []float32{1, 2, 3}) {
		t.Errorf("queue = %v, want [1 2 3]", got)
	}
}

// A full queue drops the OLDEST entry: the most recent intent is the one
// worth keeping.
func TestPushActionOverflowDropsOldest(t *testing.T) {
	q := &components.ActionQueue{}
	for i := 1; i <= components.ActionQueueSize+2; i++ {
		PushAction(q, moveTo(float32(i), 0))
	}
	if q.Count != components.ActionQueueSize {
		t.Fatalf("Count = %d, want the queue capped at %d", q.Count, components.ActionQueueSize)
	}
	want := []float32{3, 4, 5, 6}
	if got := queueTargets(q); !equalF32(got, want) {
		t.Errorf("queue = %v, want %v (oldest two dropped)", got, want)
	}
}

func TestPopActionAdvancesAndClearsSlot(t *testing.T) {
	q := &components.ActionQueue{}
	PushAction(q, moveTo(1, 0))
	PushAction(q, moveTo(2, 0))
	head := q.Head
	popAction(q)
	if q.Count != 1 {
		t.Errorf("Count = %d, want 1", q.Count)
	}
	if q.Actions[head].Kind != components.ActionNone {
		t.Error("popped slot was not cleared")
	}
	if got := queueTargets(q); !equalF32(got, []float32{2}) {
		t.Errorf("queue = %v, want [2]", got)
	}
}

func TestPopActionResetsStopTimer(t *testing.T) {
	q := &components.ActionQueue{StopUntil: 12.5}
	PushAction(q, components.Action{Kind: components.ActionStop})
	popAction(q)
	if q.StopUntil != 0 {
		t.Errorf("StopUntil = %f, want 0 — a stale timer would cut the next Stop short", q.StopUntil)
	}
}

func TestPopActionOnEmptyQueueIsSafe(t *testing.T) {
	q := &components.ActionQueue{}
	popAction(q)
	if q.Count != 0 || q.Head != 0 {
		t.Errorf("empty pop mutated the queue: Count=%d Head=%d", q.Count, q.Head)
	}
}

func TestPopActionWrapsAroundRing(t *testing.T) {
	q := &components.ActionQueue{}
	// Fill, drain, refill so Head/Tail sit past the wrap point.
	for i := 0; i < components.ActionQueueSize; i++ {
		PushAction(q, moveTo(float32(i), 0))
	}
	for i := 0; i < components.ActionQueueSize; i++ {
		popAction(q)
	}
	PushAction(q, moveTo(42, 0))
	if q.Count != 1 {
		t.Fatalf("Count = %d, want 1", q.Count)
	}
	if got := queueTargets(q); !equalF32(got, []float32{42}) {
		t.Errorf("queue = %v, want [42] after wrapping", got)
	}
}

func TestClearActionsEmptiesEverything(t *testing.T) {
	q := &components.ActionQueue{StopUntil: 3}
	for i := 0; i < components.ActionQueueSize; i++ {
		PushAction(q, moveTo(float32(i), 0))
	}
	ClearActions(q)
	if q.Count != 0 || q.Head != 0 || q.Tail != 0 || q.StopUntil != 0 {
		t.Errorf("cleared queue = %+v, want the zero state", *q)
	}
	for i := range q.Actions {
		if q.Actions[i].Kind != components.ActionNone {
			t.Errorf("slot %d survived ClearActions", i)
		}
	}
}

func TestWrapAngle(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{0, 0},
		{math.Pi / 2, math.Pi / 2},
		{-math.Pi / 2, -math.Pi / 2},
		// 350° must fold to -10°, so the yaw cap turns the short way.
		{2*math.Pi - 0.17453, -0.17453},
		{3 * math.Pi, math.Pi},
		// -pi and +pi are the same bearing; the loop lands on -pi.
		{-3 * math.Pi, -math.Pi},
		{4 * math.Pi, 0},
	}
	for _, tc := range cases {
		got := wrapAngle(tc.in)
		if math.Abs(float64(got-tc.want)) > 1e-4 {
			t.Errorf("wrapAngle(%f) = %f, want %f", tc.in, got, tc.want)
		}
	}
}

func TestWrapAngleAlwaysInRange(t *testing.T) {
	for i := -20; i <= 20; i++ {
		a := float32(i) * 0.7
		got := wrapAngle(a)
		if got > math.Pi+1e-5 || got < -math.Pi-1e-5 {
			t.Errorf("wrapAngle(%f) = %f, outside [-pi, pi]", a, got)
		}
	}
}
