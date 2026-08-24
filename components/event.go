package components

import "github.com/mlange-42/ark/ecs"

type EventKind uint8

const (
	EventNone EventKind = iota
	EventEnemyContact
	EventKIA
	EventSuppressionStart
	EventOrderCompleted
	EventOrderFailed
	// EventAirDown — an airframe destroyed (Phase 20 M2). Appended so saved
	// AttentionMatrix indices stay stable.
	EventAirDown
	// EventKindCount bounds any per-kind table, e.g. the attention matrix.
	EventKindCount
)

const EventLogCapacity = 200

// EventEntry is one event record. Pos lets a future click-to-fly handler
// move the map camera. Text is a cached display string so the renderer
// doesn't fmt every frame.
type EventEntry struct {
	Kind  EventKind
	At    float32
	Pos   WorldPos
	Squad ecs.Entity
	Text  string
}

// EventLog is the world-wide ring buffer resource. Head is the next write
// slot; Count grows to EventLogCapacity then stays clamped.
type EventLog struct {
	Entries [EventLogCapacity]EventEntry
	Head    int
	Count   int
}

func NewEventLog() *EventLog { return &EventLog{} }

// Push appends an entry to the ring. Oldest entries are overwritten in
// place; callers do not need to drain the log.
func (l *EventLog) Push(e EventEntry) {
	l.Entries[l.Head] = e
	l.Head = (l.Head + 1) % EventLogCapacity
	if l.Count < EventLogCapacity {
		l.Count++
	}
}

// Latest returns the n most recent entries, newest first. n is clamped to
// the live Count; returns a fresh slice each call.
func (l *EventLog) Latest(n int) []EventEntry {
	if n > l.Count {
		n = l.Count
	}
	if n <= 0 {
		return nil
	}
	out := make([]EventEntry, n)
	for i := 0; i < n; i++ {
		idx := (l.Head - 1 - i + EventLogCapacity) % EventLogCapacity
		out[i] = l.Entries[idx]
	}
	return out
}

func EventKindLabel(k EventKind) string {
	switch k {
	case EventEnemyContact:
		return "Contact"
	case EventKIA:
		return "KIA"
	case EventSuppressionStart:
		return "Suppressed"
	case EventOrderCompleted:
		return "Done"
	case EventOrderFailed:
		return "Failed"
	case EventAirDown:
		return "Air down"
	}
	return "?"
}
