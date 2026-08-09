package main

import (
	"testing"

	"rts-go/components"
	"rts-go/ui"
)

func ev(kind components.EventKind, at float32) components.EventEntry {
	return components.EventEntry{Kind: kind, At: at, Text: "x"}
}

// Batches arrive newest-first, the way EventLog.Latest hands them over.
func TestScanAttentionLoudestWins(t *testing.T) {
	m := ui.DefaultAttentionMatrix()
	batch := []components.EventEntry{
		ev(components.EventOrderCompleted, 30), // ignored by default
		ev(components.EventKIA, 29),            // slow
		ev(components.EventSuppressionStart, 28),
	}
	got := scanAttention(batch, m, 20)
	if got.Level != ui.ReactSlow {
		t.Fatalf("level = %v, want ReactSlow", got.Level)
	}
	if got.Cause.Kind != components.EventKIA {
		t.Fatalf("cause = %v, want EventKIA", got.Cause.Kind)
	}
	if got.Newest != 30 {
		t.Fatalf("newest = %v, want 30", got.Newest)
	}
	// The ignored kind must not ask for a cue; the other two must.
	if got.Kinds&(1<<components.EventOrderCompleted) != 0 {
		t.Fatal("an ignored kind asked for a cue")
	}
	if got.Kinds&(1<<components.EventKIA) == 0 ||
		got.Kinds&(1<<components.EventSuppressionStart) == 0 {
		t.Fatalf("kinds mask %b is missing a reacting kind", got.Kinds)
	}
}

// Everything at or before the cursor was reacted to on an earlier frame.
func TestScanAttentionSkipsConsumed(t *testing.T) {
	m := ui.DefaultAttentionMatrix()
	batch := []components.EventEntry{
		ev(components.EventKIA, 12),
		ev(components.EventEnemyContact, 11),
	}
	got := scanAttention(batch, m, 12)
	if got.Level != ui.ReactIgnore {
		t.Fatalf("level = %v, want ReactIgnore", got.Level)
	}
	if got.Newest != 12 {
		t.Fatalf("newest = %v, want the cursor to stand still", got.Newest)
	}
	if got.Kinds != 0 {
		t.Fatalf("kinds mask %b, want nothing cued", got.Kinds)
	}
}

// An all-off matrix must never move the clock, however loud the events — but
// the cursor still advances, or the same backlog replays every frame.
func TestScanAttentionAllOff(t *testing.T) {
	var m ui.AttentionMatrix
	batch := []components.EventEntry{ev(components.EventKIA, 5)}
	got := scanAttention(batch, m, 0)
	if got.Level != ui.ReactIgnore {
		t.Fatalf("level = %v, want ReactIgnore", got.Level)
	}
	if got.Newest != 5 {
		t.Fatalf("newest = %v, want 5 — consumed but not acted on", got.Newest)
	}
}

// The cue buffer must be a well-formed RIFF/WAVE image or raylib rejects it
// silently and the player just gets no sound.
func TestSynthCueWAVHeader(t *testing.T) {
	pattern := []cueTone{{440, 50}, {0, 10}, {880, 50}}
	buf := synthCueWAV(pattern)
	if len(buf) <= 44 {
		t.Fatalf("buffer is %d bytes, want a header plus samples", len(buf))
	}
	if string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		t.Fatalf("bad magic: %q / %q", buf[0:4], buf[8:12])
	}
	// Segment lengths round down individually, so sum per segment.
	wantSamples := 0
	for _, t := range pattern {
		wantSamples += t.Ms * cueSampleRate / 1000
	}
	if got := (len(buf) - 44) / 2; got != wantSamples {
		t.Fatalf("%d samples, want %d", got, wantSamples)
	}
}

// Every configurable kind needs a cue, or turning its reaction on gives a
// silent notification.
func TestEveryAttentionKindHasCue(t *testing.T) {
	for _, k := range ui.AttentionKinds {
		if len(cuePatterns[k]) == 0 {
			t.Fatalf("kind %v has no cue pattern", k)
		}
	}
}
