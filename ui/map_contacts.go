package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// drawMapContacts iterates ContactFilter and renders an APP-6-style symbol at
// each EstimatedPos via DrawSymbol. Alpha decays with age via contactAlphaFor
// (mirror of systems.ContactAgeAlpha — ui must not import systems).
func drawMapContacts(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.ContactFilter == nil {
		return
	}
	q := ctx.ContactFilter.Query()
	for q.Next() {
		c := q.Get()
		ent := q.Entity()
		alpha := contactAlphaFor(c.LastSeenTime, ctx.Clock)
		screen := MapWorldToPanel(c.EstimatedPos, ctx.Cam, content)
		spec := DefaultSpecForDimension(c.PerceivedAffil, c.PerceivedDim)
		if ctx.ContactOverrideMap != nil {
			if ov := ctx.ContactOverrideMap.Get(ent); ov != nil {
				spec = ov.Spec
			}
		}
		DrawSymbol(spec, screen, 9, alpha)
		if isSelectedEntity(ctx.Selected, ent) {
			rl.DrawCircleLines(int32(screen.X), int32(screen.Y), 14,
				rl.Color{R: 0, G: 220, B: 220, A: uint8(230 * alpha)})
		}
	}
	q.Close()
}

// contactAlphaFor mirrors systems.ContactAgeAlpha; duplicated to keep ui
// package free of systems-import cycles.
func contactAlphaFor(lastSeen, now float32) float32 {
	const ghostAfter float32 = 5.0
	const fadeDur float32 = 25.0
	const floor float32 = 0.3
	age := now - lastSeen
	if age <= ghostAfter {
		return 1.0
	}
	if age >= ghostAfter+fadeDur {
		return floor
	}
	t := (age - ghostAfter) / fadeDur
	return 1.0 - (1.0-floor)*t
}

func isSelectedEntity(selected []ecs.Entity, ent ecs.Entity) bool {
	for _, e := range selected {
		if e == ent {
			return true
		}
	}
	return false
}

// PickContactAt returns the closest Contact entity within pickRadiusPx of
// the screen position, or zero Entity if none matches.
func PickContactAt(screenPos rl.Vector2, ctx MapRenderCtx, panel Panel, pickRadiusPx float32) ecs.Entity {
	if ctx.ContactFilter == nil {
		return ecs.Entity{}
	}
	content := ContentRect(panel)
	if !pointInRect(screenPos, content) {
		return ecs.Entity{}
	}
	var best ecs.Entity
	bestD := pickRadiusPx * pickRadiusPx
	q := ctx.ContactFilter.Query()
	for q.Next() {
		c := q.Get()
		s := MapWorldToPanel(c.EstimatedPos, ctx.Cam, content)
		dx := s.X - screenPos.X
		dy := s.Y - screenPos.Y
		d := dx*dx + dy*dy
		if d < bestD {
			bestD = d
			best = q.Entity()
		}
	}
	q.Close()
	return best
}
