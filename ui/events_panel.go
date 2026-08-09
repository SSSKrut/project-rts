package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// The Events panel is the log's own surface. The log used to be a tail
// section of the empty Inspector, so it vanished the moment anything was
// selected — which is exactly when events matter.

// EventFocusRequest: a clicked event row asks the game to move the camera to
// where it happened. Resolved in drawUI like every other UI request.
var EventFocusRequest struct {
	Active bool
	Pos    components.WorldPos
}

// AttentionCycleRequest: a clicked matrix chip asks for the next reaction on
// that kind. The ui package neither owns the matrix nor persists it.
var AttentionCycleRequest struct {
	Active bool
	Kind   components.EventKind
}

// AttentionMuteRequest toggles the event cues.
var AttentionMuteRequest struct{ Active bool }

type EventsCtx struct {
	Log          *components.EventLog
	Matrix       AttentionMatrix
	Muted        bool
	Font         rl.Font
	Cursor       rl.Vector2
	LMBPressed   bool
	PanelFocused bool
	Scroll       *ScrollState
}

const (
	eventsChipW     float32 = 64
	eventsChipGap   float32 = 6
	eventsPanelRows         = 64
)

func DrawEventsPanel(panel Panel, ctx EventsCtx) {
	st := InspectorStyle(ctx.Font)
	in := WidgetInput{Cursor: ctx.Cursor, Press: ctx.LMBPressed, Enabled: ctx.PanelFocused}
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, inspectorBG)
	sc := BeginScroll(content, ctx.Scroll, float32(inspectorPadX), float32(inspectorPadY))
	defer sc.End()
	col := &sc.Col

	Header(col, &st, "Auto-reaction")
	for _, k := range AttentionKinds {
		row := col.Band(st.RowH)
		chip := rl.Rectangle{
			X: row.X + row.Width - eventsChipW, Y: row.Y + 1,
			Width: eventsChipW, Height: row.Height - 3,
		}
		label := rl.Rectangle{
			X: row.X, Y: row.Y,
			Width: row.Width - eventsChipW - eventsChipGap, Height: row.Height,
		}
		TextClipped(&st, label, components.EventKindLabel(k), st.Text)
		r := ctx.Matrix.For(k)
		if Chip(in, &st, chip, AutoReactionLabel(r), r != ReactIgnore) {
			AttentionCycleRequest.Active = true
			AttentionCycleRequest.Kind = k
		}
	}

	if Toggle(in, &st, col.Band(st.RowH), "Sound cues", !ctx.Muted) {
		AttentionMuteRequest.Active = true
	}

	col.Skip(st.RowH * 0.5)
	Header(col, &st, "Events")
	if ctx.Log == nil || ctx.Log.Count == 0 {
		TextRow(col, &st, "(none yet)", st.TextDim)
		return
	}
	for _, ev := range ctx.Log.Latest(eventsPanelRows) {
		DrawEventRow(col, &st, in, ev)
	}
}

// DrawEventRow is shared with the Inspector's tail list so both surfaces
// clip and fly-to identically.
func DrawEventRow(col *Column, st *Style, in WidgetInput, ev components.EventEntry) {
	row := col.Band(st.RowH)
	band := rl.Rectangle{X: row.X - 2, Y: row.Y - 2, Width: row.Width, Height: row.Height}
	if in.Hover(band) {
		rl.DrawRectangleRec(band, inspectorRowHoverBG)
	}
	if in.Clicked(band) {
		EventFocusRequest.Active = true
		EventFocusRequest.Pos = ev.Pos
	}
	TextClipped(st, row, fmt.Sprintf("[%5.1fs] %-10s %s", ev.At,
		components.EventKindLabel(ev.Kind), ev.Text), EventKindColor(ev.Kind, st))
}

func EventKindColor(k components.EventKind, st *Style) rl.Color {
	switch k {
	case components.EventKIA, components.EventOrderFailed:
		return rl.Color{R: 230, G: 110, B: 80, A: 255}
	case components.EventSuppressionStart:
		return rl.Color{R: 230, G: 170, B: 90, A: 255}
	case components.EventEnemyContact:
		return rl.Color{R: 240, G: 200, B: 110, A: 255}
	}
	return st.Text
}
