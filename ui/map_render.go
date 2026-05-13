package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// MapRenderCtx bundles per-frame inputs the map needs. Built once in main.go
// and passed to DrawMap. Same pattern as InspectorCtx.
type MapRenderCtx struct {
	World          *ecs.World
	Cam            MapCamera
	Underlay       *MapUnderlay
	AnchorPos      components.WorldPos
	Selected       []ecs.Entity
	Hovered        ecs.Entity
	PosMap         *ecs.Map[components.WorldPos]
	RosterMap      *ecs.Map[components.CommandRoster]
	SquadMemberMap *ecs.Map[components.SquadMember]
	SquadFilter    *ecs.Filter2[components.Squad, components.CommandRoster]
	// SquadCenter resolves a roster to its centre WorldPos. Injected as func
	// to keep the ui package free of a systems import.
	SquadCenter func(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool)
	// SquadColor mirrors InspectorCtx.SquadColor.
	SquadColor func(id uint32) rl.Color
	// Optional debug layers — checked by drawDebugLayers.
	RoadGraph *components.RoadGraph
	Rivers    *components.Rivers
	Buildings *components.BuildingPlanList
	// ShowDebugLayers toggles roads/rivers/buildings overlay.
	ShowDebugLayers bool
}

var (
	mapBeyondUnderlayBG = rl.Color{R: 28, G: 32, B: 40, A: 255}
	mapAnchorColor      = rl.Color{R: 60, G: 180, B: 240, A: 255}
	mapSelectionRing    = rl.Color{R: 0, G: 220, B: 220, A: 230}
	mapHoverRing        = rl.Color{R: 240, G: 240, B: 120, A: 200}
	mapRoadHighway      = rl.Color{R: 220, G: 220, B: 220, A: 220}
	mapRoadLocal        = rl.Color{R: 120, G: 160, B: 220, A: 220}
	mapRoadDirt         = rl.Color{R: 170, G: 130, B: 80, A: 220}
	mapRoadBridge       = rl.Color{R: 220, G: 200, B: 80, A: 220}
	mapRiverColor       = rl.Color{R: 60, G: 120, B: 220, A: 230}
	mapBuildingColor    = rl.Color{R: 160, G: 150, B: 140, A: 220}
)

// DrawMap paints the map panel: underlay → debug layers → squad markers →
// anchor → selection / hover overlays. Everything inside scissor so out-of-
// panel pixels stay clean.
func DrawMap(panel Panel, ctx MapRenderCtx) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, mapBeyondUnderlayBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	drawUnderlay(content, ctx)
	if ctx.ShowDebugLayers {
		drawDebugLayers(content, ctx)
	}
	drawSquadMarkers(content, ctx)
	drawAnchorMarker(content, ctx)
}

func drawUnderlay(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.Underlay == nil {
		return
	}
	u := ctx.Underlay
	cam := ctx.Cam
	// World-space corners of the underlay → screen-space rectangle.
	tl := components.WorldPos{}.Add(rl.Vector3{X: u.WorldOriginX, Z: u.WorldOriginZ})
	br := components.WorldPos{}.Add(rl.Vector3{X: u.WorldOriginX + u.SizeM, Z: u.WorldOriginZ + u.SizeM})
	tlS := MapWorldToPanel(tl, cam, content)
	brS := MapWorldToPanel(br, cam, content)
	dst := rl.Rectangle{X: tlS.X, Y: tlS.Y, Width: brS.X - tlS.X, Height: brS.Y - tlS.Y}
	src := rl.Rectangle{X: 0, Y: 0,
		Width: float32(u.Texture.Width), Height: float32(u.Texture.Height)}
	rl.DrawTexturePro(u.Texture, src, dst, rl.Vector2{}, 0, rl.White)
}

func drawDebugLayers(content rl.Rectangle, ctx MapRenderCtx) {
	cam := ctx.Cam
	// Rivers — polylines in blue.
	if ctx.Rivers != nil {
		for _, riv := range ctx.Rivers.Polylines {
			thick := float32(math.Max(1.5, float64(riv.Width)*float64(cam.Zoom)*0.5))
			for i := 1; i < len(riv.Points); i++ {
				a := MapWorldToPanel(riv.Points[i-1], cam, content)
				b := MapWorldToPanel(riv.Points[i], cam, content)
				rl.DrawLineEx(a, b, thick, mapRiverColor)
			}
		}
	}
	// Roads — polylines colour-coded by kind.
	if ctx.RoadGraph != nil {
		for i := range ctx.RoadGraph.Edges {
			e := &ctx.RoadGraph.Edges[i]
			a := MapWorldToPanel(ctx.RoadGraph.Nodes[e.From].Pos, cam, content)
			b := MapWorldToPanel(ctx.RoadGraph.Nodes[e.To].Pos, cam, content)
			col := roadColor(e.Kind)
			thick := float32(math.Max(1.0, float64(e.Width)*float64(cam.Zoom)*0.5))
			rl.DrawLineEx(a, b, thick, col)
		}
	}
	// Buildings — small filled rectangles at footprint extent.
	if ctx.Buildings != nil {
		for i := range ctx.Buildings.Plans {
			p := &ctx.Buildings.Plans[i]
			cx, cz := worldPosCenterXZ(p.Pos)
			halfW := p.Size.X * 0.5
			halfH := p.Size.Y * 0.5
			tl := components.WorldPos{}.Add(rl.Vector3{X: cx - halfW, Z: cz - halfH})
			br := components.WorldPos{}.Add(rl.Vector3{X: cx + halfW, Z: cz + halfH})
			a := MapWorldToPanel(tl, cam, content)
			b := MapWorldToPanel(br, cam, content)
			rect := rl.Rectangle{X: a.X, Y: a.Y, Width: b.X - a.X, Height: b.Y - a.Y}
			rl.DrawRectangleRec(rect, mapBuildingColor)
		}
	}
}

func drawSquadMarkers(content rl.Rectangle, ctx MapRenderCtx) {
	q := ctx.SquadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		ent := q.Entity()
		if roster.Count == 0 || ctx.SquadCenter == nil {
			continue
		}
		center, ok := ctx.SquadCenter(ctx.World, roster)
		if !ok {
			continue
		}
		screen := MapWorldToPanel(center, ctx.Cam, content)
		col := rl.Color{R: 200, G: 200, B: 200, A: 240}
		if ctx.SquadColor != nil {
			col = ctx.SquadColor(ent.ID())
		}
		const radius float32 = 7
		rl.DrawCircleV(screen, radius, col)
		rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radius,
			rl.Color{R: 20, G: 20, B: 20, A: 200})
		// Hover & selection rings.
		hoverHere := ctx.Hovered == ent
		selectedHere := mapAnyMemberSelected(ctx.Selected, roster)
		if selectedHere {
			rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radius+3, mapSelectionRing)
			rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radius+4, mapSelectionRing)
		}
		if hoverHere {
			rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radius+6, mapHoverRing)
		}
	}
}

func drawAnchorMarker(content rl.Rectangle, ctx MapRenderCtx) {
	s := MapWorldToPanel(ctx.AnchorPos, ctx.Cam, content)
	const arm float32 = 6
	rl.DrawLineEx(rl.Vector2{X: s.X - arm, Y: s.Y}, rl.Vector2{X: s.X + arm, Y: s.Y}, 2, mapAnchorColor)
	rl.DrawLineEx(rl.Vector2{X: s.X, Y: s.Y - arm}, rl.Vector2{X: s.X, Y: s.Y + arm}, 2, mapAnchorColor)
	rl.DrawCircleLines(int32(s.X), int32(s.Y), arm, mapAnchorColor)
}

// PickSquadAt returns the squad whose marker is nearest to `screenPos`
// within `pickRadiusPx`. Returns zero entity when nothing matches. Used by
// the map's LMB / hover.
func PickSquadAt(screenPos rl.Vector2, ctx MapRenderCtx, panel Panel,
	pickRadiusPx float32) ecs.Entity {
	content := ContentRect(panel)
	if !pointInRect(screenPos, content) {
		return ecs.Entity{}
	}
	var best ecs.Entity
	bestD := pickRadiusPx * pickRadiusPx
	q := ctx.SquadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		ent := q.Entity()
		if roster.Count == 0 || ctx.SquadCenter == nil {
			continue
		}
		center, ok := ctx.SquadCenter(ctx.World, roster)
		if !ok {
			continue
		}
		s := MapWorldToPanel(center, ctx.Cam, content)
		dx := s.X - screenPos.X
		dy := s.Y - screenPos.Y
		d := dx*dx + dy*dy
		if d < bestD {
			bestD = d
			best = ent
		}
	}
	return best
}

func mapAnyMemberSelected(selected []ecs.Entity, roster *components.CommandRoster) bool {
	for i := uint8(0); i < roster.Count; i++ {
		m := roster.Members[i]
		for _, e := range selected {
			if e == m {
				return true
			}
		}
	}
	return false
}

func roadColor(k components.RoadKind) rl.Color {
	switch k {
	case components.RoadHighway:
		return mapRoadHighway
	case components.RoadLocal:
		return mapRoadLocal
	case components.RoadDirtTrack:
		return mapRoadDirt
	case components.RoadBridge:
		return mapRoadBridge
	default:
		return rl.Magenta
	}
}

// worldPosCenterXZ unwraps a WorldPos into its absolute world XZ pair. Used
// when the map needs to lay out a building footprint that the plan stores as
// (Pos + Size).
func worldPosCenterXZ(wp components.WorldPos) (float32, float32) {
	return float32(wp.Chunk.X)*components.ChunkSize + wp.Local.X,
		float32(wp.Chunk.Z)*components.ChunkSize + wp.Local.Z
}
