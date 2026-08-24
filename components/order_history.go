package components

import "github.com/mlange-42/ark/ecs"

// An Order entity is the only record of itself, and it is removed the moment
// it reaches a terminal state. OrderHistory is the tombstone log that outlives
// it, so the plan timeline can still show what a squad did and how far it got
// before the order ended.

// OrderHistoryCapacity is the world-wide ring depth. Orders are issued by
// hand — a handful per squad per engagement — so this spans a long operation.
const OrderHistoryCapacity = 256

// OrderOutcome is how an order left the queue. Deliberately not OrderStateCode:
// only terminal states can be recorded, and Cancelled never reaches the
// resolver (cancelChain removes the entity outright).
type OrderOutcome uint8

const (
	OutcomeCompleted OrderOutcome = iota
	OutcomeFailed
	OutcomeCancelled
)

func OrderOutcomeLabel(o OrderOutcome) string {
	switch o {
	case OutcomeCompleted:
		return "done"
	case OutcomeFailed:
		return "failed"
	case OutcomeCancelled:
		return "cancelled"
	}
	return "?"
}

// OrderRecord is one finished order, carrying enough to redraw its block
// without the entity.
type OrderRecord struct {
	// Commander that owned the order — a squad, or a soloist. Not "Squad":
	// see OrderOwner.
	Commander ecs.Entity
	Target    WorldPos
	Kind      OrderKindCode
	Outcome   OrderOutcome
	IssuedT   float32
	// StartedT is OrderNeverStarted for a queued order cancelled before its
	// turn came — it has an issue time and an end, but never ran.
	StartedT float32
	EndedT   float32
	// Progress at the moment it ended. A MoveTo cancelled at 0.6 got 60 % of
	// the way there, which is the whole reason to keep the record.
	Progress float32
}

func (r OrderRecord) Started() bool { return r.StartedT > OrderNeverStarted }

// OrderHistory is the world-wide ring buffer resource. Head is the next write
// slot; Count grows to OrderHistoryCapacity then stays clamped.
type OrderHistory struct {
	Entries [OrderHistoryCapacity]OrderRecord
	Head    int
	Count   int
}

func NewOrderHistory() *OrderHistory { return &OrderHistory{} }

func (h *OrderHistory) Push(r OrderRecord) {
	h.Entries[h.Head] = r
	h.Head = (h.Head + 1) % OrderHistoryCapacity
	if h.Count < OrderHistoryCapacity {
		h.Count++
	}
}

// Each walks the live records oldest first, which is the order the timeline
// wants to lay blocks down in. Non-allocating.
func (h *OrderHistory) Each(fn func(OrderRecord)) {
	start := (h.Head - h.Count + OrderHistoryCapacity) % OrderHistoryCapacity
	for i := 0; i < h.Count; i++ {
		fn(h.Entries[(start+i)%OrderHistoryCapacity])
	}
}
