package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// CameraFocusRequest is set by drawInspectorContact when the player clicks
// the [Focus camera] button; main.go reads/clears it once per frame and
// teleports the camera anchor. Avoids exposing posMap to the ui package.
var CameraFocusRequest struct {
	Active bool
	Pos    components.WorldPos
}

// DeleteContactRequest is set when [Delete] is clicked; main.go consumes it
// and calls World.RemoveEntity. Decoupled the same way as CameraFocusRequest.
var DeleteContactRequest struct {
	Active bool
	Entity ecs.Entity
}

func drawInspectorContact(ctx InspectorCtx, ent ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	if !ctx.World.Alive(ent) {
		TextRow(&col, &ctx.st, "(contact no longer alive)", ctx.st.TextDim)
		return int32(col.Y)
	}
	c := ctx.ContactMap.Get(ent)
	if c == nil {
		TextRow(&col, &ctx.st, "(missing contact data)", ctx.st.TextDim)
		return int32(col.Y)
	}

	TextRow(&col, &ctx.st, fmt.Sprintf("Contact #%X", ent.ID()&0xFFFF), ctx.st.Text)

	// APP-6 symbol chip (Phase 18.5.C). Player override wins.
	spec := DefaultSpecForDimension(c.PerceivedAffil, c.PerceivedDim)
	if ctx.ContactOverrideMap != nil {
		if ov := ctx.ContactOverrideMap.Get(ent); ov != nil {
			spec = ov.Spec
		}
	}
	symRow := col.Band(ctx.st.RowH)
	DrawSymbol(spec, rl.Vector2{X: symRow.X + 14, Y: symRow.Y + symRow.Height*0.5}, 12, 1.0)
	TextClipped(&ctx.st, rl.Rectangle{X: symRow.X + 36, Y: symRow.Y,
		Width: symRow.Width - 36, Height: symRow.Height},
		affilLabel(c.PerceivedAffil)+" "+dimLabel(c.PerceivedDim), ctx.st.Text)
	col.Skip(ctx.st.RowH)

	TextRowClipped(&col, &ctx.st, "Source:    "+sourceLabel(c.Source), ctx.st.Text)
	if c.LastSeenTime > 0 {
		TextRowClipped(&col, &ctx.st,
			fmt.Sprintf("Last seen: %.1fs ago", ctx.Now-c.LastSeenTime), ctx.st.TextDim)
	}
	if c.BearingOnly {
		// Say what is and is not known, in that order. A passive intercept
		// gives a line, and reporting the receiver's coordinates as "position"
		// would read as a fix that was never made.
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Bearing:   %.0f deg from the receiver",
			normalizeDeg(c.Bearing*180/math.Pi)), inspectorHighlight)
		TextRowClipped(&col, &ctx.st, "Range:     unknown (emitter, not seen)", ctx.st.TextDim)
	} else {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Position:  chunk (%d, %d) local (%.1f, %.1f)",
			c.EstimatedPos.Chunk.X, c.EstimatedPos.Chunk.Z,
			c.EstimatedPos.Local.X, c.EstimatedPos.Local.Z), ctx.st.TextDim)
	}
	col.Skip(ctx.st.RowH)

	// Delete is destructive, so it gets its own palette rather than the
	// surface default — same widget, different weight.
	danger := ctx.st
	danger.Fill = rl.Color{R: 110, G: 60, B: 60, A: 220}
	danger.FillHover = rl.Color{R: 150, G: 80, B: 80, A: 230}

	row := col.Band(ctx.st.RowH)
	if Chip(ctx.in, &ctx.st, SplitX(row, 0, 2, srChipGap), "Focus camera", false) {
		CameraFocusRequest.Active = true
		CameraFocusRequest.Pos = c.EstimatedPos
	}
	if Chip(ctx.in, &danger, SplitX(row, 1, 2, srChipGap), "Delete", false) {
		DeleteContactRequest.Active = true
		DeleteContactRequest.Entity = ent
	}
	col.Skip(ctx.st.RowH)
	return int32(col.Y)
}

func affilLabel(a components.Affiliation) string {
	switch a {
	case components.AffilFriend:
		return "Friend"
	case components.AffilHostile:
		return "Hostile"
	case components.AffilNeutral:
		return "Neutral"
	}
	return "Unknown"
}

func dimLabel(d components.Dimension) string {
	switch d {
	case components.DimInfantryClass:
		return "Infantry"
	case components.DimVehicleClass:
		return "Vehicle"
	case components.DimAirClass:
		return "Air"
	case components.DimNavalClass:
		return "Naval"
	}
	return "Unknown"
}

func sourceLabel(s components.ClassificationSource) string {
	switch s {
	case components.SourceCombatEvidence:
		return "Combat evidence"
	case components.SourceCloseRangeID:
		return "Close-range ID"
	case components.SourcePlayerClassified:
		return "Player classified"
	}
	return "Sensor"
}

// normalizeDeg folds a bearing into 0..360 for display. The map ruler already
// calls north -Z and counts clockwise; this matches it.
func normalizeDeg(d float32) float32 {
	for d < 0 {
		d += 360
	}
	for d >= 360 {
		d -= 360
	}
	return d
}
