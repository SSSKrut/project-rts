package components

import "testing"

func TestOrderHistoryRingWraps(t *testing.T) {
	h := NewOrderHistory()
	for i := 0; i < OrderHistoryCapacity+10; i++ {
		h.Push(OrderRecord{EndedT: float32(i)})
	}
	if h.Count != OrderHistoryCapacity {
		t.Errorf("Count = %d, want %d", h.Count, OrderHistoryCapacity)
	}
	var got []float32
	h.Each(func(r OrderRecord) { got = append(got, r.EndedT) })
	if len(got) != OrderHistoryCapacity {
		t.Fatalf("Each yielded %d records, want %d", len(got), OrderHistoryCapacity)
	}
	// Oldest first, and the ten overwritten records are gone.
	if got[0] != 10 {
		t.Errorf("oldest survivor = %v, want 10", got[0])
	}
	if last := got[len(got)-1]; last != OrderHistoryCapacity+9 {
		t.Errorf("newest = %v, want %d", last, OrderHistoryCapacity+9)
	}
}

func TestOrderHistoryEachIsChronological(t *testing.T) {
	h := NewOrderHistory()
	for i := 0; i < 5; i++ {
		h.Push(OrderRecord{EndedT: float32(i)})
	}
	prev := float32(-1)
	h.Each(func(r OrderRecord) {
		if r.EndedT <= prev {
			t.Errorf("out of order: %v after %v", r.EndedT, prev)
		}
		prev = r.EndedT
	})
	if prev != 4 {
		t.Errorf("walk ended at %v, want 4", prev)
	}
}

// An empty log must not yield the zero-filled backing array.
func TestOrderHistoryEmpty(t *testing.T) {
	n := 0
	NewOrderHistory().Each(func(OrderRecord) { n++ })
	if n != 0 {
		t.Errorf("empty history yielded %d records", n)
	}
}

func TestOrderRecordStarted(t *testing.T) {
	if (OrderRecord{StartedT: OrderNeverStarted}).Started() {
		t.Error("sentinel should read as never started")
	}
	if !(OrderRecord{StartedT: 0}).Started() {
		t.Error("mission tick 0 is a legitimate start time")
	}
}
