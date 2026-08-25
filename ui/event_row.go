package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// The event log lives in the Inspector's empty state. The lite cut removed its
// dedicated panel; the row renderer stays here because the log itself did not
// go anywhere, only the widget that duplicated it.

// EventFocusRequest: a clicked event row asks the game to move the camera to
// where it happened. Resolved in drawUI like every other UI request.
var EventFocusRequest struct {
	Active bool
	Pos    components.WorldPos
}

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

// dimColor scales a colour toward black; used by map and inspector rows to
// fade stale information.
func dimColor(c rl.Color, f float32) rl.Color {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return rl.Color{
		R: uint8(float32(c.R) * f),
		G: uint8(float32(c.G) * f),
		B: uint8(float32(c.B) * f),
		A: c.A,
	}
}
