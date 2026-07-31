package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// MapRenderCtx is built once per frame and passed to DrawMap.
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
	// SquadCenter is injected to keep ui free of a systems import. Used
	// as a fallback when MapMarkerCache has no entry (cold cache).
	SquadCenter    func(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool)
	MapMarkerCache *components.MapMarkerCache
	// Squad marker symbology: SquadOverrideMap holds the player's assignment,
	// the rest feed the composition fallback. All nil-tolerant — a missing
	// handle degrades to a generic infantry symbol.
	RoleMap          *ecs.Map[components.UnitRole]
	VehicleMap       *ecs.Map[components.Vehicle]
	FactionMap       *ecs.Map[components.Faction]
	SquadOverrideMap *ecs.Map[components.SquadSymbolOverride]
	Font             rl.Font
	// SquadColor is injected to avoid a UI -> render-package cycle.
	SquadColor      func(ent ecs.Entity) rl.Color
	RoadGraph       *components.RoadGraph
	Rivers          *components.Rivers
	Buildings       *components.BuildingPlanList
	ShowDebugLayers bool
	OrderQueueMap   *ecs.Map[components.OrderQueueHead]
	OrderKindMap    *ecs.Map[components.OrderKind]
	OrderTargetMap  *ecs.Map[components.OrderTarget]
	OrderChainMap   *ecs.Map[components.OrderChain]
	// SmoothedSquadPos inter-frame lerps marker positions. Nil disables
	// smoothing — markers snap to the raw squad center.
	SmoothedSquadPos map[ecs.Entity]components.WorldPos
	MapPingFilter    *ecs.Filter2[components.WorldPos, components.MapPing]
	Clock            float32
	// Contacts — Phase 18.5 FoW. Nil filter → contact rendering skipped.
	ContactFilter      *ecs.Filter1[components.Contact]
	ContactMap         *ecs.Map[components.Contact]
	ContactOverrideMap *ecs.Map[components.ContactSymbolOverride]
	// LOS preview (hold V) mirrored from the 3D overlay. Empty runs → skip.
	LOSFanOrigin  components.WorldPos
	LOSFanRuns    [][]components.VisRun
	LOSFanRange   float32
	LOSFanFalloff components.FalloffKind
	LOSWeaponRs   []float32
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

func DrawMap(panel Panel, ctx MapRenderCtx) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, mapBeyondUnderlayBG)
	rl.BeginScissorMode(int32(content.X), int32(content.Y), int32(content.Width), int32(content.Height))
	defer rl.EndScissorMode()

	drawUnderlay(content, ctx)
	if ctx.ShowDebugLayers {
		drawDebugLayers(content, ctx)
	}
	drawLOSFan(content, ctx)
	drawOrderMarkers(content, ctx)
	drawMapPings(content, ctx)
	drawSquadMarkers(content, ctx)
	drawMapContacts(content, ctx)
	drawAnchorMarker(content, ctx)
}

// drawLOSFan mirrors the hold-V visibility fan on the map: filled sector
// triangles per visible run + sensor / weapon range circles.
func drawLOSFan(content rl.Rectangle, ctx MapRenderCtx) {
	if len(ctx.LOSFanRuns) == 0 {
		return
	}
	ox := float32(ctx.LOSFanOrigin.Chunk.X)*components.ChunkSize + ctx.LOSFanOrigin.Local.X
	oz := float32(ctx.LOSFanOrigin.Chunk.Z)*components.ChunkSize + ctx.LOSFanOrigin.Local.Z
	at := func(dx, dz float32) rl.Vector2 {
		wp := components.WorldPos{Local: rl.Vector3{X: ox + dx, Z: oz + dz}}
		return MapWorldToPanel(wp, ctx.Cam, content)
	}
	center := at(0, 0)
	rays := len(ctx.LOSFanRuns)
	halfStep := math.Pi / float64(rays)
	fill := rl.Color{R: 70, G: 210, B: 130}
	for r, runs := range ctx.LOSFanRuns {
		angC := float64(r) * (2 * math.Pi / float64(rays))
		s0 := float32(math.Sin(angC - halfStep))
		c0 := float32(math.Cos(angC - halfStep))
		s1 := float32(math.Sin(angC + halfStep))
		c1 := float32(math.Cos(angC + halfStep))
		for _, run := range runs {
			if run.T1 <= run.T0 {
				continue
			}
			a := components.Falloff(ctx.LOSFanFalloff, (run.T0+run.T1)*0.5, ctx.LOSFanRange)
			col := fill
			col.A = uint8(40 + 100*a)
			v00 := at(s0*run.T0, c0*run.T0)
			v01 := at(s1*run.T0, c1*run.T0)
			v10 := at(s0*run.T1, c0*run.T1)
			v11 := at(s1*run.T1, c1*run.T1)
			rl.DrawTriangle(v00, v11, v01, col)
			rl.DrawTriangle(v00, v10, v11, col)
		}
	}
	edge := at(ctx.LOSFanRange, 0)
	radPx := float32(math.Hypot(float64(edge.X-center.X), float64(edge.Y-center.Y)))
	rl.DrawCircleLines(int32(center.X), int32(center.Y), radPx, rl.Color{R: 240, G: 220, B: 80, A: 200})
	for _, wr := range ctx.LOSWeaponRs {
		e := at(wr, 0)
		rp := float32(math.Hypot(float64(e.X-center.X), float64(e.Y-center.Y)))
		rl.DrawCircleLines(int32(center.X), int32(center.Y), rp, rl.Color{R: 240, G: 120, B: 80, A: 170})
	}
}

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

// drawOrderMarkers draws a thin line + per-kind icon at the order target
// (MoveTo dot / Defend diamond / Garrison square / Trench down-triangle /
// Patrol circle). Colour is the squad's palette colour.
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

		// Walk chain — queued orders dimmed.
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
		// World-space lerp keeps markers stable across map pan / zoom.
		// UnitMovement at Relevant/Dormant writes pos only every
		// 250-500 ms; raw markers would jitter at the framerate gap.
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
		spec := SquadSymbolSpec(ctx, ent, roster)
		DrawSymbol(spec, screen, squadSymbolHalf, 1.0)
		// The APP-6 frame carries affiliation only, so the squad's palette
		// colour rides as a strip below it — as an outline it would vanish
		// against a friend frame of the same blue.
		bounds := SymbolBounds(spec.Affiliation, screen, squadSymbolHalf)
		rl.DrawRectangleRec(rl.Rectangle{
			X: bounds.X, Y: bounds.Y + bounds.Height + 1,
			Width: bounds.Width, Height: squadTintStripH,
		}, col)
		marker := rl.Rectangle{X: bounds.X, Y: bounds.Y,
			Width: bounds.Width, Height: bounds.Height + 1 + squadTintStripH}
		hoverHere := ctx.Hovered == ent
		selectedHere := mapAnyMemberSelected(ctx.Selected, roster)
		if selectedHere {
			rl.DrawRectangleLinesEx(InflateRect(marker, 2), 1.5, mapSelectionRing)
		}
		if hoverHere {
			rl.DrawRectangleLinesEx(InflateRect(marker, 4), 1.5, mapHoverRing)
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

// PickSquadAt returns zero entity when nothing matches.
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

// lookupSquadCenter falls back to live SquadCenter when MapMarkerCache
// is cold (e.g. first tick after spawn).
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

func worldPosCenterXZ(wp components.WorldPos) (float32, float32) {
	return float32(wp.Chunk.X)*components.ChunkSize + wp.Local.X,
		float32(wp.Chunk.Z)*components.ChunkSize + wp.Local.Z
}
