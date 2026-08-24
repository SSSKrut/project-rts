package systems

import (
	"testing"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// squadWithHistory wires the minimum world a SquadService needs to issue and
// cancel orders: the history resource plus one squad entity carrying a queue.
func squadWithHistory(t *testing.T) (*SquadService, *components.OrderHistory, ecs.Entity) {
	t.Helper()
	w := ecs.NewWorld()
	hist := components.NewOrderHistory()
	ecs.AddResource(w, hist)

	svc := NewSquadService(w)
	squad := w.NewEntity()
	svc.squadMap.Add(squad, &components.Squad{})
	svc.rosterMap.Add(squad, &components.CommandRoster{})
	svc.orderQueueMap.Add(squad, &components.OrderQueueHead{})
	return svc, hist, squad
}

func records(h *components.OrderHistory) []components.OrderRecord {
	var out []components.OrderRecord
	h.Each(func(r components.OrderRecord) { out = append(out, r) })
	return out
}

// The whole point of the log: cancelling a chain must leave a trace, including
// for legs that never got their turn.
func TestCancelChainRecordsEveryOrder(t *testing.T) {
	svc, hist, squad := squadWithHistory(t)
	svc.SetClock(10)
	first := svc.IssueOrder(squad, components.OrderKindMoveTo,
		components.WorldPos{}, ecs.Entity{}, false, OrderParams{})
	svc.IssueOrder(squad, components.OrderKindDefendPosition,
		components.WorldPos{}, ecs.Entity{}, true, OrderParams{})

	// The head ran and got halfway; the queued leg never started.
	svc.orderIssuedAtMap.Get(first).StartedTime = 12
	svc.orderProgressMap.Get(first).Value = 0.5

	svc.SetClock(40)
	svc.CancelAllOrders(squad)

	got := records(hist)
	if len(got) != 2 {
		t.Fatalf("recorded %d orders, want 2 (head + queued leg)", len(got))
	}
	head, queued := got[0], got[1]
	if head.Kind != components.OrderKindMoveTo || head.Outcome != components.OutcomeCancelled {
		t.Errorf("head record = %+v", head)
	}
	if !head.Started() || head.StartedT != 12 {
		t.Errorf("head StartedT = %v, want 12", head.StartedT)
	}
	if head.Progress != 0.5 {
		t.Errorf("head kept %v of its progress, want 0.5", head.Progress)
	}
	if head.EndedT != 40 || head.IssuedT != 10 {
		t.Errorf("head times = issued %v ended %v, want 10 / 40", head.IssuedT, head.EndedT)
	}
	if queued.Started() {
		t.Errorf("queued leg never ran, got StartedT = %v", queued.StartedT)
	}
	if queued.Outcome != components.OutcomeCancelled {
		t.Errorf("queued leg outcome = %v", queued.Outcome)
	}
	if queued.Commander != squad {
		t.Errorf("record lost its squad: %v", queued.Commander)
	}
}

// Issuing a non-append order cancels the current chain — that path runs through
// cancelChain too, so it must file the replaced order rather than drop it.
func TestReplacingAnOrderRecordsTheOldOne(t *testing.T) {
	svc, hist, squad := squadWithHistory(t)
	svc.SetClock(5)
	svc.IssueOrder(squad, components.OrderKindMoveTo,
		components.WorldPos{}, ecs.Entity{}, false, OrderParams{})
	svc.SetClock(9)
	svc.IssueOrder(squad, components.OrderKindPatrol,
		components.WorldPos{}, ecs.Entity{}, false, OrderParams{})

	got := records(hist)
	if len(got) != 1 {
		t.Fatalf("recorded %d orders, want 1", len(got))
	}
	if got[0].Kind != components.OrderKindMoveTo {
		t.Errorf("filed the wrong order: %+v", got[0])
	}
	if got[0].EndedT != 9 {
		t.Errorf("EndedT = %v, want the replacement's clock 9", got[0].EndedT)
	}
}

func TestNewOrderStartsUnstarted(t *testing.T) {
	svc, _, squad := squadWithHistory(t)
	svc.SetClock(3)
	ord := svc.IssueOrder(squad, components.OrderKindMoveTo,
		components.WorldPos{}, ecs.Entity{}, false, OrderParams{})
	iss := svc.orderIssuedAtMap.Get(ord)
	if iss == nil || iss.StartedTime != components.OrderNeverStarted {
		t.Errorf("fresh order should be unstarted, got %+v", iss)
	}
	if iss.Time != 3 {
		t.Errorf("IssuedAt.Time = %v, want 3", iss.Time)
	}
}

func TestOutcomeForState(t *testing.T) {
	cases := map[components.OrderStateCode]components.OrderOutcome{
		components.OrderStateCompleted: components.OutcomeCompleted,
		components.OrderStateFailed:    components.OutcomeFailed,
		components.OrderStateCancelled: components.OutcomeCancelled,
	}
	for state, want := range cases {
		if got := outcomeForState(state); got != want {
			t.Errorf("state %d -> %v, want %v", state, got, want)
		}
	}
}
