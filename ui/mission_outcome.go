package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// MissionOutcomeCtx is everything the end-of-mission card needs. It is all
// numbers on purpose (block D P4): "why did I lose" has to be answerable from
// the same figures the player was watching, and one of them is how long the
// radio net actually held — the mission scores that, which is what makes comms
// a resource rather than a nuisance.
type MissionOutcomeCtx struct {
	Mission *components.Mission
	State   *components.MissionState
	Font    rl.Font
}

var (
	moBackdrop = rl.Color{R: 8, G: 10, B: 14, A: 200}
	moPanel    = rl.Color{R: 22, G: 26, B: 34, A: 245}
	moWin      = rl.Color{R: 120, G: 220, B: 140, A: 255}
	moLoss     = rl.Color{R: 235, G: 100, B: 90, A: 255}
	moDraw     = rl.Color{R: 210, G: 200, B: 130, A: 255}
)

// DrawMissionOutcome paints the result over the frozen field.
func DrawMissionOutcome(screen rl.Rectangle, ctx MissionOutcomeCtx) {
	if ctx.Mission == nil || ctx.State == nil || !ctx.State.Over() {
		return
	}
	st := ctx.State
	rl.DrawRectangleRec(screen, moBackdrop)

	const w, h float32 = 420, 210
	card := rl.Rectangle{
		X: screen.X + (screen.Width-w)*0.5, Y: screen.Y + (screen.Height-h)*0.5,
		Width: w, Height: h,
	}
	rl.DrawRectangleRec(card, moPanel)
	rl.DrawRectangleLinesEx(card, 1, rl.Color{R: 70, G: 80, B: 95, A: 255})

	head := moDraw
	switch st.Outcome {
	case components.OutcomePlayerWin:
		head = moWin
	case components.OutcomePlayerLoss:
		head = moLoss
	}
	st2 := InspectorStyle(ctx.Font)
	col := Column{X: card.X + 18, Y: card.Y + 16, W: card.Width - 36}
	TextCentered(&st2, rl.Rectangle{X: card.X, Y: col.Y, Width: card.Width, Height: 26},
		st.Outcome.String(), head)
	col.Y += 34
	TextRowClipped(&col, &st2, ctx.Mission.Name, st2.TextDim)
	col.Skip(6)

	row := func(label, value string) {
		TextRowClipped(&col, &st2, fmt.Sprintf("%-18s %s", label, value), st2.Text)
	}
	row("Points held", fmt.Sprintf("%d  (enemy %d)",
		st.Held[components.FactionPlayer], st.Held[components.FactionEnemyRed]))
	row("Losses", fmt.Sprintf("%d  (enemy %d)",
		st.Losses[components.FactionPlayer], st.Losses[components.FactionEnemyRed]))
	row("Time on the net", fmt.Sprintf("%.0f s of %.0f", st.LinkedSec, st.EndedAt))
	row("Elapsed", fmt.Sprintf("%.0f s of %.0f", st.EndedAt, ctx.Mission.TimeSec))
	col.Skip(6)
	TextRowClipped(&col, &st2, "ESC to quit", st2.TextDim)
}
