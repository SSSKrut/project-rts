package ui

import "rts-go/components"

// The attention layer: what the game does on its own when something happens
// while the player is running compressed time or looking at another corner of
// the map. Compression above a few times real speed is only safe if the game
// itself notices what the player would have missed.

// AutoReaction is a ladder, not a set: when several events land in the same
// frame the loudest one wins. It only ever slows the clock down — speeding
// back up stays a player action, because an automatic restore fights the
// player for the clock.
type AutoReaction uint8

const (
	ReactIgnore AutoReaction = iota
	// ReactNotify is banner-only; it deliberately spawns no MapPing, because
	// pings are ECS entities and the attention layer runs outside the tick.
	ReactNotify
	ReactSlow // drop to 1x
	ReactPause
	AutoReactionCount
)

func AutoReactionLabel(r AutoReaction) string {
	switch r {
	case ReactIgnore:
		return "off"
	case ReactNotify:
		return "notify"
	case ReactSlow:
		return "slow"
	case ReactPause:
		return "pause"
	}
	return "?"
}

// AttentionMatrix is the player's per-kind policy. Plain array so it copies
// by value and persists as a flat list of ints.
type AttentionMatrix [components.EventKindCount]AutoReaction

// AttentionKinds is what the config UI offers, in display order. EventNone is
// not a real event and never appears.
var AttentionKinds = [...]components.EventKind{
	components.EventEnemyContact,
	components.EventKIA,
	components.EventSuppressionStart,
	components.EventOrderFailed,
	components.EventOrderCompleted,
}

// DefaultAttentionMatrix: first contact and losses are worth interrupting a
// fast-forward for; a suppression or a refused order is worth a line, not the
// clock; a completed order is routine.
func DefaultAttentionMatrix() AttentionMatrix {
	var m AttentionMatrix
	m[components.EventEnemyContact] = ReactSlow
	m[components.EventKIA] = ReactSlow
	m[components.EventSuppressionStart] = ReactNotify
	m[components.EventOrderFailed] = ReactNotify
	m[components.EventOrderCompleted] = ReactIgnore
	return m
}

func (m AttentionMatrix) For(k components.EventKind) AutoReaction {
	if int(k) >= len(m) {
		return ReactIgnore
	}
	return m[k]
}

func (m *AttentionMatrix) Cycle(k components.EventKind) {
	if int(k) >= len(m) {
		return
	}
	m[k] = (m[k] + 1) % AutoReactionCount
}
