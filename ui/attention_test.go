package ui

import (
	"testing"

	"rts-go/components"
)

func TestAttentionMatrixCycleWraps(t *testing.T) {
	m := DefaultAttentionMatrix()
	start := m.For(components.EventEnemyContact)
	for i := 0; i < int(AutoReactionCount); i++ {
		m.Cycle(components.EventEnemyContact)
	}
	if got := m.For(components.EventEnemyContact); got != start {
		t.Fatalf("full cycle returned %v, want %v", got, start)
	}
	m.Cycle(components.EventEnemyContact)
	if got := m.For(components.EventEnemyContact); got >= AutoReactionCount {
		t.Fatalf("cycle produced out-of-range reaction %v", got)
	}
}

// Every configurable kind needs a label and a slot; a kind added to the enum
// without a row here would silently be unconfigurable.
func TestAttentionKindsAreInRange(t *testing.T) {
	for _, k := range AttentionKinds {
		if int(k) >= int(components.EventKindCount) {
			t.Fatalf("kind %v is out of matrix range", k)
		}
		if components.EventKindLabel(k) == "?" {
			t.Fatalf("kind %v has no label", k)
		}
	}
}

func TestAutoReactionLabelsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for r := AutoReaction(0); r < AutoReactionCount; r++ {
		l := AutoReactionLabel(r)
		if l == "?" || seen[l] {
			t.Fatalf("reaction %d has a missing or duplicate label %q", r, l)
		}
		seen[l] = true
	}
}
