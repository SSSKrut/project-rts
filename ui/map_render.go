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
	// CommanderFilter is everything that holds an order of its own — squads
	// AND soloists. Order markers read it; squad markers keep to SquadFilter,
	// because a lone truck is not a squad and must not be drawn as one.
	CommanderFilter *ecs.Filter2[components.CommandRoster, components.OrderQueueHead]
	// SquadCenter is injected to keep ui free of a systems import. Used
	// as a fallback when MapMarkerCache has no entry (cold cache).
	SquadCenter    func(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool)
	MapMarkerCache *components.MapMarkerCache
	// Squad marker symbology: SquadOverrideMap holds the player's assignment,
	// the rest feed the composition fallback. All nil-tolerant — a missing
	// handle degrades to a generic infantry symbol.
	RoleMap    *ecs.Map[components.UnitRole]
	VehicleMap *ecs.Map[components.Vehicle]
	// UnitFilter / VehicleFilter drive the individual-unit layer.
	// Nil → squad symbols only.
	UnitFilter       *ecs.Filter3[components.WorldPos, components.Unit, components.Stance]
	VehicleFilter    *ecs.Filter2[components.WorldPos, components.Vehicle]
	AircraftFilter   *ecs.Filter2[components.WorldPos, components.Aircraft]
	FactionMap       *ecs.Map[components.Faction]
	SquadOverrideMap *ecs.Map[components.SquadSymbolOverride]
	Font             rl.Font
	// SquadColor is injected to avoid a UI -> render-package cycle.
	SquadColor      func(ent ecs.Entity) rl.Color
	RoadGraph       *components.RoadGraph
	Rivers          *components.Rivers
	Buildings       *components.BuildingPlanList
	OrderQueueMap   *ecs.Map[components.OrderQueueHead]
	CommsMap        *ecs.Map[components.CommsState]
	EquipmentMap    *ecs.Map[components.Equipment]
	WeaponMap       *ecs.Map[components.Weapon]
	PointFilter     *ecs.Filter2[components.ControlPoint, components.WorldPos]
	OrderKindMap    *ecs.Map[components.OrderKind]
	OrderTargetMap  *ecs.Map[components.OrderTarget]
	OrderChainMap   *ecs.Map[components.OrderChain]
	// SmoothedSquadPos inter-frame lerps marker positions. Nil disables
	// smoothing — markers snap to the raw squad center.
	SmoothedSquadPos map[ecs.Entity]components.WorldPos
	MapPingFilter    *ecs.Filter2[components.WorldPos, components.MapPing]
	Clock            float32
	// Contacts — Phase 18.5 FoW, grouped into formations once per frame by
	// ContactClusterSet.Rebuild. Nil → contact rendering and picking skipped.
	Clusters *ContactClusterSet
	// LOS preview (hold V) mirrored from the 3D overlay. Empty runs → skip.
	LOSFanOrigin  components.WorldPos
	LOSFanRuns    [][]components.VisRun
	LOSFanRange   float32
	LOSFanFalloff components.FalloffKind
	LOSWeaponRs   []float32
	// Coverage — persistent reach of the current selection (Phase 20.7 L1).
	// Nil or !Active → skipped.
	Coverage *CoverageView
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
	drawTerrainLayers(content, ctx)
	drawMapCoverage(content, ctx)
	drawLOSFan(content, ctx)
	drawOrderMarkers(content, ctx)
	drawMapPings(content, ctx)
	drawControlPoints(content, ctx)
	drawSquadMarkers(content, ctx)
	drawOwnVehicles(content, ctx)
	drawOwnAircraft(content, ctx)
	drawSoloistUnits(content, ctx)
	drawMapFire(content, ctx)
	drawMapContacts(content, ctx)
	drawBearingLines(content, ctx)
	drawAnchorMarker(content, ctx)
}

// drawLOSFan mirrors the hold-V visibility fan on the map: filled sector
// triangles per visible run + sensor / weapon range circles.
func drawLOSFan(content rl.Rectangle, ctx MapRenderCtx) {
	if len(ctx.LOSFanRuns) == 0 {
		return
	}
	drawMapFanRuns(content, ctx.Cam, ctx.LOSFanOrigin, ctx.LOSFanRuns,
		ctx.LOSFanRange, ctx.LOSFanFalloff, 40, 100)
	center := MapWorldToPanel(ctx.LOSFanOrigin, ctx.Cam, content)
	radPx := ctx.LOSFanRange * ctx.Cam.Zoom
	rl.DrawCircleLines(int32(center.X), int32(center.Y), radPx, rl.Color{R: 240, G: 220, B: 80, A: 200})
	for _, wr := range ctx.LOSWeaponRs {
		rl.DrawCircleLines(int32(center.X), int32(center.Y), wr*ctx.Cam.Zoom, coverageWeaponColor)
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
	if ctx.CommanderFilter == nil {
		return
	}
	q := ctx.CommanderFilter.Query()
	for q.Next() {
		roster, head := q.Get()
		squad := q.Entity()
		// An order is a plan, and an enemy's plan is not something sensors
		// deliver. Same gate as the markers, same call, so the two cannot
		// drift apart.
		if SquadSymbolSpec(ctx, squad, roster).Affiliation != components.AffilFriend {
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
		// The map is the command surface, so a plan the net has not carried is
		// drawn here too — washed out, and unmistakably not a running order.
		if ctx.CommsMap != nil {
			if cs := ctx.CommsMap.Get(squad); cs != nil && cs.Undelivered != (ecs.Entity{}) &&
				ctx.World.Alive(cs.Undelivered) {
				if k := ctx.OrderKindMap.Get(cs.Undelivered); k != nil {
					if t := ctx.OrderTargetMap.Get(cs.Undelivered); t != nil {
						pale := rl.Color{R: col.R, G: col.G, B: col.B, A: 70}
						pt := MapWorldToPanel(t.Pos, ctx.Cam, content)
						rl.DrawLineEx(MapWorldToPanel(center, ctx.Cam, content), pt, 1.0, pale)
						drawOrderIcon(pt, k.Code, pale)
					}
				}
			}
		}
		if head.First == (ecs.Entity{}) || !ctx.World.Alive(head.First) {
			continue
		}
		kind := ctx.OrderKindMap.Get(head.First)
		target := ctx.OrderTargetMap.Get(head.First)
		if kind == nil || target == nil {
			continue
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

// drawTerrainLayers draws rivers, roads and buildings. These are not debug —
// they are the map. Hiding them behind a held key made the command surface
// blank exactly when the player needed to read the ground.
func drawTerrainLayers(content rl.Rectangle, ctx MapRenderCtx) {
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

// mapFireWindow is how long a shot stays lit on the map. Long enough to see a
// firefight as a firefight, short enough that the flashes track it moving.
const mapFireWindow float32 = 0.8

// drawMapFire marks where OUR weapons are going off. Fire is the loudest thing
// happening on the field and the map used to show none of it; an enemy's fire
// arrives as a contact instead (contact_gunfire.go), which is the honest
// asymmetry — you know where your own men are shooting from.
func drawMapFire(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.EquipmentMap == nil || ctx.WeaponMap == nil || ctx.UnitFilter == nil {
		return
	}
	flash := func(ent ecs.Entity, pos components.WorldPos) {
		eq := ctx.EquipmentMap.Get(ent)
		if eq == nil || eq.Active == (ecs.Entity{}) || !ctx.World.Alive(eq.Active) {
			return
		}
		w := ctx.WeaponMap.Get(eq.Active)
		if w == nil || w.LastFiredAt <= 0 || ctx.Clock-w.LastFiredAt > mapFireWindow {
			return
		}
		age := (ctx.Clock - w.LastFiredAt) / mapFireWindow
		screen := MapWorldToPanel(pos, ctx.Cam, content)
		col := rl.Color{R: 255, G: 220, B: 120, A: uint8(230 * (1 - age))}
		rl.DrawCircleV(screen, 3.5-2*age, col)
	}
	qU := ctx.UnitFilter.Query()
	for qU.Next() {
		pos, _, _ := qU.Get()
		ent := qU.Entity()
		if f := ctx.FactionMap.Get(ent); f != nil && f.ID != components.FactionPlayer {
			continue
		}
		flash(ent, *pos)
	}
	if ctx.VehicleFilter != nil {
		qV := ctx.VehicleFilter.Query()
		for qV.Next() {
			pos, _ := qV.Get()
			ent := qV.Entity()
			if f := ctx.FactionMap.Get(ent); f != nil && f.ID != components.FactionPlayer {
				continue
			}
			flash(ent, *pos)
		}
	}
}

// ControlPointColor is the owner's colour for a point, on the map and in 3D.
// Grey is nobody's — deliberately not neutral's colour, because "unclaimed" is
// the absence of a side, not a third one.
func ControlPointColor(owner uint8) rl.Color {
	switch owner {
	case components.FactionPlayer:
		return rl.Color{R: 90, G: 160, B: 240, A: 255}
	case components.FactionEnemyRed:
		return rl.Color{R: 235, G: 90, B: 80, A: 255}
	case components.FactionNeutral:
		return rl.Color{R: 120, G: 210, B: 130, A: 255}
	}
	return rl.Color{R: 165, G: 170, B: 178, A: 255}
}

// drawControlPoints draws the ground the game is played over: the radius as it
// really is (so the player can see where to stand), the owner as colour, and
// capture as an arc filling toward whoever is pushing. Contested points pulse
// on their outline — the state where nothing is moving has to look different
// from the state where something is.
func drawControlPoints(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.PointFilter == nil {
		return
	}
	q := ctx.PointFilter.Query()
	for q.Next() {
		cp, pos := q.Get()
		screen := MapWorldToPanel(*pos, ctx.Cam, content)
		if !rl.CheckCollisionPointRec(screen, content) {
			continue
		}
		rPix := cp.Radius * ctx.Cam.Zoom
		if rPix < 3 {
			rPix = 3
		}
		col := ControlPointColor(cp.Owner)
		fill := col
		fill.A = 40
		rl.DrawCircleV(screen, rPix, fill)
		ring := col
		if cp.Contested {
			ring = ControlPointColor(cp.Challenger)
		}
		rl.DrawCircleLines(int32(screen.X), int32(screen.Y), rPix, ring)
		if cp.Progress > 0 {
			// One filled sector, from north, in the challenger's colour: the
			// question a player asks is "how much longer", and an arc answers
			// it without a legend.
			ch := ControlPointColor(cp.Challenger)
			ch.A = 170
			rl.DrawCircleSector(screen, rPix*0.6, 0, cp.Progress*360, 24, ch)
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
		spec := SquadSymbolSpec(ctx, ent, roster)
		// Own squads only. A hostile squad's real map presence is its
		// contacts; a marker built straight off its roster reported a
		// formation the sensors had never found. The member dots below were
		// already gated on exactly this — the frame around them was not.
		if spec.Affiliation != components.AffilFriend {
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
		// A marker nobody is reporting fades exactly like an enemy contact —
		// same curve, because it is the same kind of fact: a last known place.
		alpha := float32(1)
		if ctx.MapMarkerCache != nil {
			if since, stale := ctx.MapMarkerCache.StaleSince[ent]; stale {
				alpha = components.ContactAgeAlpha(since, ctx.Clock)
			}
		}
		if alpha < 1 {
			col.A = uint8(float32(col.A) * alpha)
		}
		drawSquadMemberDots(content, ctx, roster, screen, col)
		DrawSymbol(spec, screen, squadSymbolHalf, alpha)
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
		// Offer only what drawSquadMarkers painted.
		if SquadSymbolSpec(ctx, ent, roster).Affiliation != components.AffilFriend {
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

// bearingRayM is how far the drawn ray runs. It is not a range estimate and
// must not look like one: the line leaves the panel, which is the honest
// picture of a track that has a direction and nothing else.
const bearingRayM float32 = 4000

// drawBearingLines renders ESM intercepts. Dashes rather than a solid line,
// because on this surface a solid line is what an order or a route looks like,
// and this is neither — it is the one thing on the map with no destination.
func drawBearingLines(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.Clusters == nil {
		return
	}
	for i := range ctx.Clusters.Bearings {
		b := &ctx.Clusters.Bearings[i]
		far := b.From.Add(rl.Vector3{
			X: float32(math.Sin(float64(b.Bearing))) * bearingRayM,
			Z: float32(math.Cos(float64(b.Bearing))) * bearingRayM,
		})
		a := MapWorldToPanel(b.From, ctx.Cam, content)
		z := MapWorldToPanel(far, ctx.Cam, content)
		col := rl.Color{R: 240, G: 190, B: 90, A: uint8(60 + 140*b.Alpha)}
		const dashes = 48
		for d := 0; d < dashes; d += 2 {
			t0 := float32(d) / dashes
			t1 := float32(d+1) / dashes
			rl.DrawLineV(
				rl.Vector2{X: a.X + (z.X-a.X)*t0, Y: a.Y + (z.Y-a.Y)*t0},
				rl.Vector2{X: a.X + (z.X-a.X)*t1, Y: a.Y + (z.Y-a.Y)*t1}, col)
		}
		rl.DrawCircleLines(int32(a.X), int32(a.Y), 3, col)
	}
}
