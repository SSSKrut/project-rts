package ui

import (
	"fmt"

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

func drawInspectorContact(ctx InspectorCtx, ent ecs.Entity, x, y int32) int32 {
	if !ctx.World.Alive(ent) {
		drawText(ctx.Font, "(contact no longer alive)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	c := ctx.ContactMap.Get(ent)
	if c == nil {
		drawText(ctx.Font, "(missing contact data)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}

	drawText(ctx.Font, fmt.Sprintf("Contact #%X", ent.ID()&0xFFFF),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH

	// APP-6 symbol chip (Phase 18.5.C). Player override wins.
	spec := DefaultSpecForDimension(c.PerceivedAffil, c.PerceivedDim)
	if ctx.ContactOverrideMap != nil {
		if ov := ctx.ContactOverrideMap.Get(ent); ov != nil {
			spec = ov.Spec
		}
	}
	center := rl.Vector2{X: float32(x) + 14, Y: float32(y+inspectorRowH/2)}
	DrawSymbol(spec, center, 12, 1.0)
	drawText(ctx.Font, affilLabel(c.PerceivedAffil)+" "+dimLabel(c.PerceivedDim),
		x+36, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2

	drawText(ctx.Font, "Source:    "+sourceLabel(c.Source),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH

	if c.LastSeenTime > 0 {
		ageS := timelineNow(ctx) - c.LastSeenTime
		drawText(ctx.Font, fmt.Sprintf("Last seen: %.1fs ago", ageS),
			x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
	}
	drawText(ctx.Font, fmt.Sprintf("Position:  chunk (%d, %d) local (%.1f, %.1f)",
		c.EstimatedPos.Chunk.X, c.EstimatedPos.Chunk.Z,
		c.EstimatedPos.Local.X, c.EstimatedPos.Local.Z),
		x, y, inspectorFontSize, inspectorTextDim)
	y += inspectorRowH * 2

	// [Focus camera] click region.
	btnW := int32(120)
	btnH := inspectorRowH
	focusRect := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(btnW), Height: float32(btnH)}
	focusHover := ctx.PanelFocused && rl.CheckCollisionPointRec(ctx.Cursor, focusRect)
	bg := rl.Color{R: 50, G: 70, B: 100, A: 220}
	if focusHover {
		bg = rl.Color{R: 80, G: 110, B: 150, A: 230}
	}
	rl.DrawRectangleRec(focusRect, bg)
	rl.DrawRectangleLinesEx(focusRect, 1, rl.Color{R: 20, G: 20, B: 20, A: 200})
	drawText(ctx.Font, "Focus camera", x+8, y+3, inspectorFontSize, inspectorText)
	if focusHover && ctx.LMBPressed {
		CameraFocusRequest.Active = true
		CameraFocusRequest.Pos = c.EstimatedPos
	}

	delRect := rl.Rectangle{X: float32(x + btnW + 8), Y: float32(y), Width: float32(btnW), Height: float32(btnH)}
	delHover := ctx.PanelFocused && rl.CheckCollisionPointRec(ctx.Cursor, delRect)
	delBg := rl.Color{R: 110, G: 60, B: 60, A: 220}
	if delHover {
		delBg = rl.Color{R: 150, G: 80, B: 80, A: 230}
	}
	rl.DrawRectangleRec(delRect, delBg)
	rl.DrawRectangleLinesEx(delRect, 1, rl.Color{R: 20, G: 20, B: 20, A: 200})
	drawText(ctx.Font, "Delete", x+btnW+8+8, y+3, inspectorFontSize, inspectorText)
	if delHover && ctx.LMBPressed {
		DeleteContactRequest.Active = true
		DeleteContactRequest.Entity = ent
	}

	y += inspectorRowH * 2
	return y
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

// timelineNow extracts a clock value the contact panel can use to compute
// "last seen Xs ago". Borrowed from SquadClock via the InspectorCtx by way
// of a hack — we approximate it via raylib's GetTime since neither InspectorCtx
// nor InspectorMaps expose Clock. Track 18.5.E will add a proper clock pipe.
func timelineNow(_ InspectorCtx) float32 {
	return float32(rl.GetTime())
}
