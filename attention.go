package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/ui"
)

// The attention layer (Phase 19.7 M1). Time compression is only safe if the
// game notices what the player would have missed, so the same EventLog that
// feeds the log panel also drives the clock. There is no second event bus.
//
// It lives in the render half of the frame: a headless gate run returns
// before this ever executes, and the only things it mutates are TimeScale and
// its own banner — no ECS state, no entities.

const (
	// One frame's worth of new events; only the loudest matters, so the cap
	// is about bounding work, not about correctness.
	attentionScanCap = 64
	// Sim seconds before an equally loud reaction may fire again — a
	// firefight would otherwise pause the game every other second.
	attentionCooldown float32 = 10
	// Real seconds. The banner is UI, so it ages on the wall clock and does
	// not freeze along with a paused sim.
	attentionBannerTTL float64 = 8
)

type attentionState struct {
	Matrix ui.AttentionMatrix
	Banner attentionBanner

	// Sim time of the newest entry already consumed.
	lastSeenAt float32
	primed     bool
	lastFireAt float32
	lastLevel  ui.AutoReaction
}

type attentionBanner struct {
	Text   string
	Pos    components.WorldPos
	HasPos bool
	Level  ui.AutoReaction
	Until  float64
	// Rect is what the last draw painted; input hit-tests it next frame,
	// the same handshake the top bar uses.
	Rect rl.Rectangle
}

// updateAttention consumes everything pushed since the last frame and applies
// the loudest reaction the matrix asks for.
func (g *Game) updateAttention() {
	st := &g.UI.Attention
	log := g.Res.EventLog
	if log == nil {
		return
	}
	now := g.simNow()
	// The first frame after boot or load swallows whatever is already in the
	// ring: reacting to events from before the player was watching is noise.
	// This has to happen even on an EMPTY log, or priming waits for the first
	// event and then eats it — and the first event of a session is exactly
	// the one worth reacting to.
	if !st.primed {
		st.primed = true
		st.lastSeenAt = newestEventAt(log)
		return
	}
	if log.Count == 0 {
		return
	}
	// A quickload rewinds the clock; entries stamped later than now belong to
	// the abandoned future and must not gag the restored one.
	if now < st.lastSeenAt {
		st.lastSeenAt = now
	}

	n := log.Count
	if n > attentionScanCap {
		n = attentionScanCap
	}
	scan := scanAttention(log.Latest(n), st.Matrix, st.lastSeenAt)
	st.lastSeenAt = scan.Newest

	if scan.Level == ui.ReactIgnore {
		return
	}
	// Every distinct kind gets its own cue: the clock reacts once to the
	// loudest thing, the ear wants to hear each kind that landed.
	for k := components.EventKind(0); k < components.EventKindCount; k++ {
		if scan.Kinds&(1<<k) != 0 {
			g.playEventCue(k)
		}
	}
	// A louder reaction always gets through; an equal or quieter one waits.
	if now-st.lastFireAt < attentionCooldown && scan.Level <= st.lastLevel {
		return
	}
	st.lastFireAt = now
	st.lastLevel = scan.Level
	g.applyAttentionReaction(scan.Level, scan.Cause)
}

func newestEventAt(log *components.EventLog) float32 {
	if latest := log.Latest(1); len(latest) == 1 {
		return latest[0].At
	}
	return 0
}

// attentionScan is one frame's worth of verdict: the loudest reaction, what
// earned it, where the cursor moves to, and which kinds want a cue.
type attentionScan struct {
	Level  ui.AutoReaction
	Cause  components.EventEntry
	Newest float32
	Kinds  uint32
}

// scanAttention walks a newest-first batch (the shape EventLog.Latest hands
// over) and reads everything stamped later than `since`. Entries carry sim
// time, so the timestamp IS the cursor — no index into the ring, which a
// save/load would invalidate anyway.
func scanAttention(batch []components.EventEntry, m ui.AttentionMatrix,
	since float32) attentionScan {
	out := attentionScan{Newest: since}
	for _, ev := range batch {
		if ev.At <= since {
			break
		}
		if ev.At > out.Newest {
			out.Newest = ev.At
		}
		r := m.For(ev.Kind)
		if r == ui.ReactIgnore {
			continue
		}
		out.Kinds |= 1 << ev.Kind
		if r > out.Level {
			out.Level, out.Cause = r, ev
		}
	}
	return out
}

// applyAttentionReaction only ever slows the clock. Restoring compression is
// the player's move — an automatic restore fights them for the clock, and
// after an interruption the speed to resume at is 1x, not whatever was
// running when the shooting started.
func (g *Game) applyAttentionReaction(level ui.AutoReaction, ev components.EventEntry) {
	action := ""
	switch level {
	case ui.ReactPause:
		// Only when this reaction is what stopped the clock: a game the
		// player paused by hand keeps the speed they meant to resume at.
		if g.App.TimeScale > 0 {
			g.App.TimeScale = 0
			g.App.LastNonZeroScale = 1
			action = "paused"
		}
	case ui.ReactSlow:
		if g.App.TimeScale > 1 {
			g.App.TimeScale = 1
			g.App.LastNonZeroScale = 1
			action = "slowed to 1x"
		}
	}
	text := ev.Text
	if action != "" {
		text += " - " + action
	}
	g.UI.Attention.Banner = attentionBanner{
		Text:   text,
		Pos:    ev.Pos,
		HasPos: true,
		Level:  level,
		Until:  rl.GetTime() + attentionBannerTTL,
	}
}

var (
	attentionBannerNotify = rl.Color{R: 40, G: 60, B: 84, A: 235}
	attentionBannerSlow   = rl.Color{R: 96, G: 70, B: 26, A: 235}
	attentionBannerPause  = rl.Color{R: 104, G: 40, B: 40, A: 235}
	attentionBannerEdge   = rl.Color{R: 230, G: 200, B: 120, A: 255}
	attentionBannerText   = rl.Color{R: 240, G: 240, B: 245, A: 255}
)

// drawAttentionBanner paints the live reaction just under the top bar and
// records its rect for next frame's hit test.
func (g *Game) drawAttentionBanner() {
	b := &g.UI.Attention.Banner
	if b.Text == "" {
		return
	}
	if rl.GetTime() > b.Until {
		*b = attentionBanner{}
		return
	}
	const fontSize float32 = 15
	label := "AUTO: " + b.Text
	if b.HasPos {
		label += "   [click to view]"
	}
	m := rl.MeasureTextEx(g.hudFont, label, fontSize, 1.0)
	w := m.X + 24
	if maxW := float32(g.UI.ScreenW) - 40; w > maxW {
		w = maxW
	}
	r := rl.Rectangle{
		X:      (float32(g.UI.ScreenW) - w) * 0.5,
		Y:      float32(ui.TopBarRect(g.UI.ScreenW).Height) + 6,
		Width:  w,
		Height: m.Y + 10,
	}
	bg := attentionBannerNotify
	switch b.Level {
	case ui.ReactSlow:
		bg = attentionBannerSlow
	case ui.ReactPause:
		bg = attentionBannerPause
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, attentionBannerEdge)
	rl.DrawTextEx(g.hudFont, label,
		rl.Vector2{X: r.X + 12, Y: r.Y + (r.Height-m.Y)*0.5}, fontSize, 1.0,
		attentionBannerText)
	b.Rect = r
}

// handleAttentionBannerClick returns true when it consumed the press, so the
// world below never sees it.
func (g *Game) handleAttentionBannerClick() bool {
	b := &g.UI.Attention.Banner
	if b.Text == "" || b.Rect.Width <= 0 || !rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		return false
	}
	if !rl.CheckCollisionPointRec(g.Frame.Cursor, b.Rect) {
		return false
	}
	if b.HasPos {
		g.flyTo(b.Pos)
	}
	*b = attentionBanner{}
	return true
}

// flyTo moves the anchor (the 3D camera orbits it) and re-centres the map, so
// both views land on the same place regardless of which one is in the big slot.
func (g *Game) flyTo(pos components.WorldPos) {
	if p := g.Maps.Pos.Get(g.anchor); p != nil {
		*p = pos
	}
	g.UI.MapCam.Center = pos
}
