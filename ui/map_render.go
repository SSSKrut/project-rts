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
	// to keep the ui package free of a systems import. Used as the fallback
	// when MapMarkerCache has no entry yet (e.g. fresh squad before the first
	// MapMarkerCacheSystem tick).
	SquadCenter func(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool)
	// MapMarkerCache is the per-squad position cache populated by
	// MapMarkerCacheSystem @ 250 ms (Phase 11.5 P7). When non-nil drawSquadMarkers
	// / drawOrderMarkers / PickSquadAt read from it; nil falls back to per-call
	// SquadCenter.
	MapMarkerCache *components.MapMarkerCache
	// Phase 12: commander role drives the ShortLabel rendered inside the
	// squad marker. Nil -> marker stays a plain coloured dot.
	RoleMap *ecs.Map[components.UnitRole]
	Font    rl.Font
	// SquadColor mirrors InspectorCtx.SquadColor.
	SquadColor func(ent ecs.Entity) rl.Color
	// Optional debug layers - checked by drawDebugLayers.
	RoadGraph *components.RoadGraph
	Rivers    *components.Rivers
	Buildings *components.BuildingPlanList
	// ShowDebugLayers toggles roads/rivers/buildings overlay.
	ShowDebugLayers bool
	// Phase 11 order overlay maps. When non-nil DrawMap renders the head
	// order's destination icon + line for each squad.
	OrderQueueMap  *ecs.Map[components.OrderQueueHead]
	OrderKindMap   *ecs.Map[components.OrderKind]
	OrderTargetMap *ecs.Map[components.OrderTarget]
	OrderChainMap  *ecs.Map[components.OrderChain]
	// SmoothedSquadPos is the inter-frame lerp store for squad-marker
	// positions on the map (ISSUES #1 fix). Owned by main.go so its lifetime
	// matches the camera; DrawMap reads + writes per frame. Nil disables
	// smoothing - markers snap to the raw squad center.
	SmoothedSquadPos map[ecs.Entity]components.WorldPos
	// Phase 15 M15.C.3 - MapPing rendering. Filter + clock are nil-safe;
	// nil filter just skips the pulsing-ring pass.
	MapPingFilter *ecs.Filter2[components.WorldPos, components.MapPing]
	Clock         float32
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

// DrawMap paints the map panel: underlay -> debug layers -> squad markers ->
// anchor -> selection / hover overlays. Everything inside scissor so out-of-
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
	drawOrderMarkers(content, ctx)
	drawMapPings(content, ctx)
	drawSquadMarkers(content, ctx)
	drawAnchorMarker(content, ctx)
}

// drawMapPings paints every live MapPing as a pulsing ring + small dot at the
// ping's world position. Radius = BaseRadM * (1 + 0.5*sin(t * 4)); alpha
// fades linearly to zero across TTL so the ring softly dies.
func drawMapPings(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.MapPingFilter == nil {
		return
	}
	q := ctx.MapPingFilter.Query()
	for q.Next() {
		pos, ping := q.Get()
		age := ctx.Clock - ping.SpawnAt
		if age < 0 || age > ping.TTL {
			continue
		}
		life := 1 - age/ping.TTL
		if life < 0 {
			life = 0
		}
		pulse := 1 + 0.5*float32(math.Sin(float64(age*4)))
		radM := ping.BaseRadM * pulse
		screen := MapWorldToPanel(*pos, ctx.Cam, content)
		// Convert metres to pixels via current camera zoom (pixels-per-metre).
		radPx := radM * ctx.Cam.Zoom
		alpha := uint8(life * 220)
		col := ping.Color
		col.A = alpha
		rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radPx, col)
		dot := ping.Color
		dot.A = uint8(life * 255)
		rl.DrawCircle(int32(screen.X), int32(screen.Y), 2, dot)
	}
}

// drawOrderMarkers draws, for every squad with an active head order, a thin
// line from the squad center to the order's target Pos and a small icon at
// the target. Icon shape encodes order kind (MoveTo dot, Defend diamond,
// Garrison square, Trench down-triangle, Patrol circle). Colour is the
// squad's palette colour.
func drawOrderMarkers(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.OrderQueueMap == nil || ctx.OrderKindMap == nil || ctx.OrderTargetMap == nil {
		return
	}
	q := ctx.SquadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		squad := q.Entity()
		head := ctx.OrderQueueMap.Get(squad)
		if head == nil || head.First == (ecs.Entity{}) || !ctx.World.Alive(head.First) {
			continue
		}
		kind := ctx.OrderKindMap.Get(head.First)
		target := ctx.OrderTargetMap.Get(head.First)
		if kind == nil || target == nil {
			continue
		}
		center, ok := lookupSquadCenter(ctx, squad, roster)
		if !ok {
			continue
		}
		col := rl.Color{R: 230, G: 230, B: 230, A: 220}
		if ctx.SquadColor != nil {
			col = ctx.SquadColor(squad)
		}
		from := MapWorldToPanel(center, ctx.Cam, content)
		to := MapWorldToPanel(target.Pos, ctx.Cam, content)
		rl.DrawLineEx(from, to, 1.5, col)
		drawOrderIcon(to, kind.Code, col)

		// Walk chain - paint queued orders' targets dimmed.
		cur := head.First
		dim := rl.Color{R: col.R, G: col.G, B: col.B, A: 120}
		prev := to
		for ctx.OrderChainMap != nil {
			ch := ctx.OrderChainMap.Get(cur)
			if ch == nil || ch.Next == (ecs.Entity{}) || !ctx.World.Alive(ch.Next) {
				break
			}
			cur = ch.Next
			tt := ctx.OrderTargetMap.Get(cur)
			kk := ctx.OrderKindMap.Get(cur)
			if tt == nil || kk == nil {
				break
			}
			next := MapWorldToPanel(tt.Pos, ctx.Cam, content)
			rl.DrawLineEx(prev, next, 1.0, dim)
			drawOrderIcon(next, kk.Code, dim)
			prev = next
		}
	}
}

// drawOrderIcon paints one per-kind glyph at `at`. Geometry kept tiny (~6 px)
// so multiple orders don't crowd the map at default zoom.
func drawOrderIcon(at rl.Vector2, k components.OrderKindCode, col rl.Color) {
	const r float32 = 6
	switch k {
	case components.OrderKindMoveTo:
		rl.DrawCircleV(at, 4, col)
		rl.DrawCircleLines(int32(at.X), int32(at.Y), 4, rl.Black)
	case components.OrderKindGarrison:
		rl.DrawRectangle(int32(at.X-r*0.5), int32(at.Y-r*0.5), int32(r), int32(r), col)
		rl.DrawRectangleLines(int32(at.X-r*0.5), int32(at.Y-r*0.5), int32(r), int32(r), rl.Black)
	case components.OrderKindOccupyTrench:
		v1 := rl.Vector2{X: at.X, Y: at.Y + r*0.7}
		v2 := rl.Vector2{X: at.X - r*0.7, Y: at.Y - r*0.5}
		v3 := rl.Vector2{X: at.X + r*0.7, Y: at.Y - r*0.5}
		rl.DrawTriangle(v2, v1, v3, col)
		rl.DrawTriangleLines(v2, v1, v3, rl.Black)
	case components.OrderKindDefendPosition:
		// Diamond - top / right / bottom / left.
		v1 := rl.Vector2{X: at.X, Y: at.Y - r*0.7}
		v2 := rl.Vector2{X: at.X + r*0.7, Y: at.Y}
		v3 := rl.Vector2{X: at.X, Y: at.Y + r*0.7}
		v4 := rl.Vector2{X: at.X - r*0.7, Y: at.Y}
		rl.DrawTriangle(v1, v4, v2, col)
		rl.DrawTriangle(v2, v4, v3, col)
		rl.DrawLineEx(v1, v2, 1.0, rl.Black)
		rl.DrawLineEx(v2, v3, 1.0, rl.Black)
		rl.DrawLineEx(v3, v4, 1.0, rl.Black)
		rl.DrawLineEx(v4, v1, 1.0, rl.Black)
	case components.OrderKindPatrol:
		rl.DrawCircleLines(int32(at.X), int32(at.Y), r*0.7, col)
		rl.DrawCircleLines(int32(at.X), int32(at.Y), r*0.7+1, rl.Black)
		// Arrow head - small triangle to the right.
		v1 := rl.Vector2{X: at.X + r, Y: at.Y - r*0.4}
		v2 := rl.Vector2{X: at.X + r, Y: at.Y + r*0.4}
		v3 := rl.Vector2{X: at.X + r*1.6, Y: at.Y}
		rl.DrawTriangle(v1, v3, v2, col)
	}
}

func drawUnderlay(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.Underlay == nil {
		return
	}
	u := ctx.Underlay
	cam := ctx.Cam
	// World-space corners of the underlay -> screen-space rectangle.
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
	// Rivers - polylines in blue.
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
	// Roads - polylines colour-coded by kind.
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
	// Buildings - small filled rectangles at footprint extent.
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
		if roster.Count == 0 {
			continue
		}
		center, ok := lookupSquadCenter(ctx, ent, roster)
		if !ok {
			continue
		}
		// ISSUES #1: lerp toward the latest center in world space. UnitMovement
		// at Relevant/Dormant tier writes pos only every 250-500 ms; raw
		// markers jitter at the framerate gap. World-space lerp keeps the
		// marker stable under map pan / zoom.
		if ctx.SmoothedSquadPos != nil {
			if prev, hadPrev := ctx.SmoothedSquadPos[ent]; hadPrev {
				const factor float32 = 0.18 // ~6-frame settle at 60 FPS
				diff := center.Sub(prev)
				lerped := prev.Add(rl.Vector3{
					X: diff.X * factor, Y: diff.Y * factor, Z: diff.Z * factor,
				})
				center = lerped
			}
			ctx.SmoothedSquadPos[ent] = center
		}
		screen := MapWorldToPanel(center, ctx.Cam, content)
		col := rl.Color{R: 200, G: 200, B: 200, A: 240}
		if ctx.SquadColor != nil {
			col = ctx.SquadColor(ent)
		}
		// Phase 12 P6 / Note: marker radius bumped 7 -> 9 so 1-2 char commander
		// ShortLabel ("L", "MG", "AT") fits inside the disc legibly.
		const radius float32 = 9
		rl.DrawCircleV(screen, radius, col)
		rl.DrawCircleLines(int32(screen.X), int32(screen.Y), radius,
			rl.Color{R: 20, G: 20, B: 20, A: 200})
		// Commander ShortLabel inside the disc. Contrast colour picked off
		// the squad's palette colour so 8 different squad tints all stay
		// readable.
		if ctx.RoleMap != nil && roster.Count > 0 {
			commander := roster.Members[0]
			if commander != (ecs.Entity{}) && ctx.World.Alive(commander) {
				if r := ctx.RoleMap.Get(commander); r != nil {
					label := r.Kind.ShortLabel()
					const fontSize float32 = 13
					sz := rl.MeasureTextEx(ctx.Font, label, fontSize, 1)
					txt := rl.Color{R: 0, G: 0, B: 0, A: 230}
					if (0.299*float32(col.R) + 0.587*float32(col.G) + 0.114*float32(col.B)) < 140 {
						txt = rl.Color{R: 255, G: 255, B: 255, A: 240}
					}
					rl.DrawTextEx(ctx.Font, label, rl.Vector2{
						X: screen.X - sz.X*0.5,
						Y: screen.Y - sz.Y*0.5,
					}, fontSize, 1, txt)
				}
			}
		}
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
		if roster.Count == 0 {
			continue
		}
		center, ok := lookupSquadCenter(ctx, ent, roster)
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

// lookupSquadCenter returns the cached position when MapMarkerCache has an
// entry, otherwise falls back to the live SquadCenter call (covers
// first-tick / cache-cold cases). Keeps every map draw path consistent.
func lookupSquadCenter(ctx MapRenderCtx, squad ecs.Entity,
	roster *components.CommandRoster) (components.WorldPos, bool) {
	if ctx.MapMarkerCache != nil {
		if wp, ok := ctx.MapMarkerCache.Position[squad]; ok {
			return wp, true
		}
	}
	if ctx.SquadCenter == nil {
		return components.WorldPos{}, false
	}
	return ctx.SquadCenter(ctx.World, roster)
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
