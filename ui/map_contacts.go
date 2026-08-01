package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

const (
	clusterDotR    float32 = 2.5
	clusterSpokeW  float32 = 1.0
	clusterRingPad float32 = 5
)

// drawMapContacts renders one APP-6 symbol per cluster. Zoomed out that is
// all the player sees; once the members spread apart on screen each gets a
// dot wired back to the symbol, so a formation still reads as one unit
// instead of a scatter of tracks.
func drawMapContacts(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.Clusters == nil {
		return
	}
	for _, c := range ctx.Clusters.Clusters {
		centre, expand := ctx.Clusters.expanded(c, ctx.Cam, content)
		members := ctx.Clusters.members(c)
		pal := symbolPalette[c.Spec.Affiliation]
		if expand {
			spoke := withAlpha(pal.Outline, c.Alpha*0.6)
			for _, m := range members {
				p := MapWorldToPanel(m.Pos, ctx.Cam, content)
				rl.DrawLineEx(centre, p, clusterSpokeW, spoke)
				rl.DrawCircleV(p, clusterDotR, withAlpha(pal.Fill, m.Alpha))
				rl.DrawCircleLines(int32(p.X), int32(p.Y), clusterDotR,
					withAlpha(pal.Outline, m.Alpha))
				if isSelectedEntity(ctx.Selected, m.Entity) {
					rl.DrawCircleLines(int32(p.X), int32(p.Y), clusterDotR+3,
						withAlpha(mapSelectionRing, m.Alpha))
				}
				if ctx.Hovered == m.Entity {
					rl.DrawCircleLines(int32(p.X), int32(p.Y), clusterDotR+5,
						withAlpha(mapHoverRing, m.Alpha))
				}
			}
		}
		DrawSymbol(c.Spec, centre, contactSymbolHalf, c.Alpha)
		bounds := SymbolBounds(c.Spec.Affiliation, centre, contactSymbolHalf)
		if clusterHasAny(ctx.Selected, members) {
			rl.DrawRectangleLinesEx(InflateRect(bounds, 2), 1.5,
				withAlpha(mapSelectionRing, c.Alpha))
		}
		if clusterHas(ctx.Hovered, members) {
			rl.DrawRectangleLinesEx(InflateRect(bounds, clusterRingPad), 1.5,
				withAlpha(mapHoverRing, c.Alpha))
		}
	}
}

func withAlpha(c rl.Color, a float32) rl.Color {
	if a < 0 {
		a = 0
	}
	c.A = uint8(float32(c.A) * a)
	return c
}

func isSelectedEntity(selected []ecs.Entity, ent ecs.Entity) bool {
	for _, e := range selected {
		if e == ent {
			return true
		}
	}
	return false
}

func clusterHasAny(selected []ecs.Entity, members []ClusterMember) bool {
	for _, m := range members {
		if isSelectedEntity(selected, m.Entity) {
			return true
		}
	}
	return false
}

func clusterHas(ent ecs.Entity, members []ClusterMember) bool {
	if ent == (ecs.Entity{}) {
		return false
	}
	for _, m := range members {
		if m.Entity == ent {
			return true
		}
	}
	return false
}

// PickContactAt hit-tests exactly what drawMapContacts painted: member dots
// where the cluster is expanded, the aggregate symbol where it is not (which
// resolves to the cluster's freshest track). Zero entity when nothing matches.
func PickContactAt(screenPos rl.Vector2, ctx MapRenderCtx, panel Panel, pickRadiusPx float32) ecs.Entity {
	if ctx.Clusters == nil {
		return ecs.Entity{}
	}
	content := ContentRect(panel)
	if !pointInRect(screenPos, content) {
		return ecs.Entity{}
	}
	var best ecs.Entity
	bestD := pickRadiusPx * pickRadiusPx
	test := func(p rl.Vector2, ent ecs.Entity) {
		dx := p.X - screenPos.X
		dy := p.Y - screenPos.Y
		if d := dx*dx + dy*dy; d < bestD {
			bestD = d
			best = ent
		}
	}
	for _, c := range ctx.Clusters.Clusters {
		centre, expand := ctx.Clusters.expanded(c, ctx.Cam, content)
		if expand {
			for _, m := range ctx.Clusters.members(c) {
				test(MapWorldToPanel(m.Pos, ctx.Cam, content), m.Entity)
			}
			continue
		}
		test(centre, c.Rep)
	}
	return best
}
