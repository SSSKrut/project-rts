package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/render"
	"rts-go/systems"
	"rts-go/ui"
)

// Roof fade band, in sine-of-pitch. Near the horizon a roof is the building's
// silhouette and should stay solid; from overhead it is a lid over the floor
// plan the player is trying to read, so it goes. ~20 deg -> solid, ~45 -> gone.
const (
	roofFadeStartSin = 0.34
	roofFadeEndSin   = 0.71
)

// roofFadeAlpha maps the camera's downward tilt to roof opacity.
func roofFadeAlpha(cam rl.Camera3D) float32 {
	dx := cam.Target.X - cam.Position.X
	dy := cam.Target.Y - cam.Position.Y
	dz := cam.Target.Z - cam.Position.Z
	l := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if l < 1e-4 {
		return 1
	}
	// Looking down => dy negative; sin of the depression angle.
	sinPitch := -dy / l
	if sinPitch <= roofFadeStartSin {
		return 1
	}
	if sinPitch >= roofFadeEndSin {
		return 0
	}
	return 1 - (sinPitch-roofFadeStartSin)/(roofFadeEndSin-roofFadeStartSin)
}

// drawScene3D renders the world into the scene render texture: terrain, units,
// vehicles, props, buildings, debug overlays, ghosts and particles.
func (g *Game) drawScene3D() {
	g.Frame.AnchorPos = g.Maps.Pos.Get(g.anchor)
	anchorRender := g.Frame.AnchorPos.ToRenderSpace(systems.CurrentOriginChunk)

	// Wind drift accumulates instead of multiplying time — a live weather
	// change must shift the field's future, not rewrite its history.
	atm := &g.Res.Atmosphere
	gust := systems.WindGust(atm, rl.GetTime())
	dtDrift := float32(g.Frame.DtReal.Seconds())
	g.Ctx.CloudDrift.X += atm.WindX * gust * dtDrift
	g.Ctx.CloudDrift.Y += atm.WindZ * gust * dtDrift

	// One daylight palette per frame drives terrain, clouds and particles.
	hours := g.Res.DayClock.HoursAt(g.App.Elapsed().Seconds())
	g.Ctx.Daylight = daylightPalette(hours)
	if g.Ctx.Particle.Puffs != nil {
		g.Ctx.Particle.Puffs.setLight(&g.Ctx.Daylight)
	}
	g.Ctx.Models.beginFrame(g.Ctx.Daylight, systems.CurrentCamera.Position)
	// Engine smoke is render-side now (see render_exhaust.go), so it ages on
	// wall time and drifts with the same wind the clouds use.
	g.Ctx.Exhaust.beginFrame(rl.GetFrameTime(), rl.Vector2{X: atm.WindX, Y: atm.WindZ})
	night := hours < 6.4 || hours > 19.6

	rl.BeginTextureMode(g.UI.Scene3DRT.RT)
	rl.ClearBackground(rl.RayWhite)
	rl.BeginMode3D(systems.CurrentCamera)

	g.Ctx.WorldShader.beginFrame(atm, g.Ctx.CloudDrift, &g.Ctx.Daylight)
	g.Ctx.WorldShader.setGround(true)

	g.Frame.ChunksActive = 0
	g.Frame.ChunksRel = 0
	qcA := g.Filt.ChunkActive.Query()
	for qcA.Next() {
		pos, mesh, _ := qcA.Get()
		g.Frame.ChunksActive++
		if !mesh.Uploaded {
			continue
		}
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		xform := rl.MatrixTranslate(renderPos.X, renderPos.Y, renderPos.Z)
		rl.DrawMesh(mesh.Mesh, g.terrainMaterial, xform)
	}
	qcR := g.Filt.ChunkRelevant.Query()
	for qcR.Next() {
		pos, mesh, _ := qcR.Get()
		g.Frame.ChunksRel++
		if !mesh.Uploaded {
			continue
		}
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		xform := rl.MatrixTranslate(renderPos.X, renderPos.Y, renderPos.Z)
		rl.DrawMesh(mesh.Mesh, g.terrainMaterial, xform)
	}

	g.Ctx.FarTerrain.ensure(g.Frame.AnchorPos.Chunk)
	g.Ctx.FarTerrain.draw(g.terrainMaterial)

	g.Ctx.WorldShader.setGround(false)
	g.Frame.RibbonsDrawn = g.Ctx.Ribbons.Roads.draw(g.terrainMaterial)

	rl.DrawCircle3D(anchorRender, 1, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Blue)
	if orb := g.Maps.Orbit.Get(g.camEnt); orb != nil && orb.ViewLift > 0.05 {
		top := anchorRender
		top.Y += orb.ViewLift
		rl.DrawLine3D(anchorRender, top, rl.Red)
		rl.DrawCircle3D(top, 0.6, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Red)
	}

	g.Frame.UnitsLive = 0
	fowNow := g.Svc.Squad.Clock()
	qu := g.Filt.UnitRender.Query()
	for qu.Next() {
		pos, _, st := qu.Get()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		ent := qu.Entity()
		// Default Rifleman keeps render resilient if a spawn path forgot AssignRole.
		role := components.RoleRifleman
		if r := g.Maps.Role.Get(ent); r != nil {
			role = r.Kind
		}
		// Phase 18.5.G FoW: non-PlayerFaction units render only when a
		// recent sensor refresh covers them. 2 sec tail with linear
		// alpha fade after LOS loss.
		alpha := float32(1.0)
		if f := g.Maps.Faction.Get(ent); f != nil && f.ID != components.FactionPlayer {
			contactEnt, ok := g.Res.ContactRegistry.Tracked[ent]
			if !ok || !g.App.World.Alive(contactEnt) {
				continue
			}
			c := g.Maps.Contact.Get(contactEnt)
			if c == nil {
				continue
			}
			age := fowNow - c.LastSeenTime
			const tailSec float32 = 2.0
			if age >= tailSec {
				continue
			}
			if age > 0 {
				alpha = 1.0 - age/tailSec
			}
		}
		if alpha >= 0.999 {
			drawUnitCube(renderPos, *st, role)
		} else {
			drawUnitCubeAlpha(renderPos, *st, role, alpha)
		}
		if g.isSelected(ent) >= 0 {
			height := unitStanceHeight(st.Code)
			rl.DrawCircle3D(renderPos, 1.0, rl.Vector3{X: 1, Y: 0, Z: 0}, 90,
				rl.Color{R: 0, G: 220, B: 220, A: 255})
			c := rl.Vector3{X: renderPos.X, Y: renderPos.Y + height*0.5, Z: renderPos.Z}
			rl.DrawCubeWiresV(c, rl.Vector3{X: 0.7, Y: height + 0.1, Z: 0.7},
				rl.Color{R: 0, G: 220, B: 220, A: 255})
		}
		if g.Sel.Hovered == ent {
			height := unitStanceHeight(st.Code)
			c := rl.Vector3{X: renderPos.X, Y: renderPos.Y + height*0.5, Z: renderPos.Z}
			rl.DrawCubeWiresV(c, rl.Vector3{X: 0.8, Y: height + 0.2, Z: 0.8},
				rl.Color{R: 240, G: 240, B: 120, A: 255})
		}
		g.Frame.UnitsLive++
	}

	qveh := g.Filt.VehicleRender.Query()
	for qveh.Next() {
		pos, veh := qveh.Get()
		ent := qveh.Entity()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		yaw := float32(0)
		if m := g.Svc.UnitFactory.MotionMap.Get(ent); m != nil {
			yaw = m.Yaw
		}
		turretYaw := float32(0)
		if t := g.Maps.Turret.Get(ent); t != nil {
			turretYaw = t.Yaw
		}
		// Which factory built the hull is an art question, so it is answered
		// from Faction here and never inside the tick.
		side := components.SideWest
		if f := g.Maps.Faction.Get(ent); f != nil {
			side = components.AssetSideForFaction(f.ID)
		}
		if g.Ctx.Models.has(veh.Kind, side) {
			lod := g.drawVehicleModel(ent, renderPos, veh.Kind, side, yaw, turretYaw, rl.White)
			g.drawVehicleNumber(ent, veh.Kind, side, renderPos, yaw, turretYaw, lod)
			g.drawVehicleLights(ent, renderPos, veh.Kind, side, yaw, turretYaw, night)
		} else {
			drawVehicleBox(renderPos, yaw, turretYaw, veh.Kind, g.squadColor(ent))
		}
		g.emitVehicleExhaust(ent, *pos, veh.Kind, side, yaw, turretYaw)
		if g.isSelected(ent) >= 0 {
			spec := components.SpecForVehicle(veh.Kind)
			rl.DrawCircle3D(renderPos, spec.ColliderR, rl.Vector3{X: 1, Y: 0, Z: 0}, 90,
				rl.Color{R: 0, G: 220, B: 220, A: 255})
		}
		if g.Sel.Hovered == ent {
			spec := components.SpecForVehicle(veh.Kind)
			rl.DrawCircle3D(renderPos, spec.ColliderR+0.3, rl.Vector3{X: 1, Y: 0, Z: 0}, 90,
				rl.Color{R: 240, G: 240, B: 120, A: 255})
		}
	}

	qair := g.Filt.AircraftRender.Query()
	for qair.Next() {
		pos, ac := qair.Get()
		ent := qair.Entity()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		yaw := float32(0)
		if m := g.Svc.UnitFactory.MotionMap.Get(ent); m != nil {
			yaw = m.Yaw
		}
		side := components.SideWest
		if f := g.Maps.Faction.Get(ent); f != nil {
			side = components.AssetSideForFaction(f.ID)
		}
		if g.Ctx.Models.hasAircraft(ac.Kind, side) {
			g.drawAircraftModel(ent, renderPos, ac.Kind, side, yaw, rl.White)
		} else {
			drawAircraftBox(renderPos, yaw, ac.Kind, g.squadColor(ent))
		}
		// A shadow disc is the only cue that says how high the airframe is:
		// on a 3D view from above, altitude reads as nothing at all.
		g.drawAircraftShadow(*pos, renderPos, ac.Kind)
		if g.isSelected(ent) >= 0 || g.Sel.Hovered == ent {
			spec := components.SpecForAircraft(ac.Kind)
			c := rl.Color{R: 0, G: 220, B: 220, A: 255}
			if g.Sel.Hovered == ent {
				c = rl.Color{R: 240, G: 240, B: 120, A: 255}
			}
			rl.DrawCircle3D(renderPos, spec.ColliderR, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, c)
		}
	}

	g.Ctx.WorldShader.beginObjects()
	g.Frame.PropsLive = 0
	camPos := systems.CurrentCamera.Position
	camFwdX := systems.CurrentCamera.Target.X - camPos.X
	camFwdZ := systems.CurrentCamera.Target.Z - camPos.Z
	qp := g.Filt.Prop.Query()
	for qp.Next() {
		pos, prop := qp.Get()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		pdx := renderPos.X - camPos.X
		pdz := renderPos.Z - camPos.Z
		pDistSq := pdx*pdx + pdz*pdz
		if pDistSq > propCullDistSq {
			continue
		}
		if pDistSq > propNearKeepDistSq && pdx*camFwdX+pdz*camFwdZ < 0 {
			continue
		}
		meta := g.Res.PropRegistry.Metas[prop.Type]
		if pDistSq > propFullDetailDistSq {
			switch meta.Primitive {
			case components.PrimitiveTree:
				drawPropFar(meta, renderPos, prop.Scale)
				g.Frame.PropsLive++
			case components.PrimitivePlane:
				drawProp(meta, renderPos, prop.Yaw, prop.Scale)
				g.Frame.PropsLive++
			}
			continue
		}
		drawProp(meta, renderPos, prop.Yaw, prop.Scale)
		g.Frame.PropsLive++
	}
	g.Ctx.WorldShader.endObjects()

	// A Level is hidden when its building has InteriorOpen AND its avgY
	// sits above CurrentLevel's avgY + epsilon.
	hiddenLevels := map[ecs.Entity]bool{}
	qLev := g.Filt.LevelCutaway.Query()
	for qLev.Next() {
		lvl, member := qLev.Get()
		bvm := g.Maps.BuildingViewMode.Get(member.Building)
		if bvm == nil || !bvm.InteriorOpen {
			continue
		}
		if bvm.CurrentLevel == (ecs.Entity{}) {
			continue
		}
		curLev := g.Maps.Level.Get(bvm.CurrentLevel)
		if curLev == nil {
			continue
		}
		if lvl.AABB.CenterY() > curLev.AABB.CenterY()+0.1 {
			hiddenLevels[qLev.Entity()] = true
		}
	}

	// A level is fogged when never discovered OR last seen >FogVisibleDuration ago.
	now := float32(g.App.Elapsed().Seconds())
	levelFogged := func(level ecs.Entity) bool {
		if level == (ecs.Entity{}) {
			return false
		}
		vis := g.Maps.LevelVisRead.Get(level)
		if vis == nil {
			return false
		}
		if !vis.Discovered {
			return true
		}
		return now-vis.LastSeenAt > components.FogVisibleDuration
	}

	g.Ctx.WorldShader.beginObjects()
	g.Frame.FloorsLive = 0
	qf := g.Filt.FloorRender.Query()
	for qf.Next() {
		pos, fl := qf.Get()
		fogged := false
		if lm := g.Maps.LevelMember.Get(qf.Entity()); lm != nil {
			if hiddenLevels[lm.Level] {
				continue
			}
			fogged = levelFogged(lm.Level)
		}
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		render.DrawFloor(renderPos, *fl, fogged)
		g.Frame.FloorsLive++
	}
	g.Frame.WallsLive = 0
	qw := g.Filt.WallRender.Query()
	for qw.Next() {
		pos, ws := qw.Get()
		e := qw.Entity()
		mode := components.WallRenderAll
		var outward rl.Vector3
		fogged := false
		if lm := g.Maps.LevelMember.Get(e); lm != nil {
			if hiddenLevels[lm.Level] {
				continue
			}
			fogged = levelFogged(lm.Level)
			// WallMode only applies to the currently-viewed level inside an open cutaway.
			if member := g.Maps.BuildingMember.Get(e); member != nil {
				if bvm := g.Maps.BuildingViewMode.Get(member.Building); bvm != nil &&
					bvm.InteriorOpen && lm.Level == bvm.CurrentLevel {
					mode = bvm.WallMode
				}
			}
			if mode == components.WallRenderCameraFacing {
				if cd := g.Maps.CoverDirRead.Get(e); cd != nil {
					outward = cd.Dir
				}
			}
		}
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		render.DrawWall(renderPos, *ws, mode, outward, systems.CurrentCamera.Position, fogged)
		g.Frame.WallsLive++
	}
	qst := g.Filt.StairsRender.Query()
	for qst.Next() {
		pos, st := qst.Get()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		render.DrawStairs(renderPos, *st)
	}

	roofAlpha := roofFadeAlpha(systems.CurrentCamera)
	if !debugOverlay.Roofs {
		roofAlpha = 0
	}
	// Decide BEFORE opening the query. Breaking out of a live Ark query without
	// Close() leaves the world locked, and the next tick's first NewEntity
	// panics in whatever system happens to run first — so gate the whole pass
	// instead of bailing inside the loop.
	if roofAlpha > 0.01 {
		qrf := g.Filt.RoofRender.Query()
		for qrf.Next() {
			pos, rf := qrf.Get()
			e := qrf.Entity()
			// The roof caps the TOP level, so the "hide levels above current"
			// rule only drops it while looking at a lower storey. Any open
			// cutaway must lose it — otherwise opening the top floor just
			// shows its lid.
			if member := g.Maps.BuildingMember.Get(e); member != nil {
				if bvm := g.Maps.BuildingViewMode.Get(member.Building); bvm != nil && bvm.InteriorOpen {
					continue
				}
			}
			fogged := false
			if lm := g.Maps.LevelMember.Get(e); lm != nil {
				if hiddenLevels[lm.Level] {
					continue
				}
				fogged = levelFogged(lm.Level)
			}
			renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
			render.DrawRoof(renderPos, *rf, roofAlpha, fogged)
		}
	}
	g.Ctx.WorldShader.endObjects()

	// Outline boxes around hovered + selected buildings. 0.15 m pad keeps
	// the wireframe legible against wall surfaces.
	drawBuildingOutline := func(root ecs.Entity, color rl.Color) {
		if root == (ecs.Entity{}) || !g.App.World.Alive(root) {
			return
		}
		bldg := g.Maps.Building.Get(root)
		rootPos := g.Maps.Pos.Get(root)
		if bldg == nil || rootPos == nil {
			return
		}
		const pad float32 = 0.15
		height := float32(bldg.Stories) * components.FloorHeight
		if height < 1 {
			height = components.FloorHeight
		}
		sizeX := bldg.Footprint.SizeX() + 2*pad
		sizeZ := bldg.Footprint.SizeZ() + 2*pad
		center := *rootPos
		center.Local.Y += height * 0.5
		rp := center.ToRenderSpace(systems.CurrentOriginChunk)
		rl.DrawCubeWires(rp, sizeX, height, sizeZ, color)
	}
	if g.Sel.HoveredBuilding != (ecs.Entity{}) && g.Sel.HoveredBuilding != g.Sel.Building {
		yellow := rl.Color{R: 255, G: 220, B: 60, A: 200}
		if g.Sel.HoveredLevel != (ecs.Entity{}) {
			if lvl := g.Maps.Level.Get(g.Sel.HoveredLevel); lvl != nil {
				drawLevelOutline(lvl, yellow)
			} else {
				drawBuildingOutline(g.Sel.HoveredBuilding, yellow)
			}
		} else {
			drawBuildingOutline(g.Sel.HoveredBuilding, yellow)
		}
	}
	if g.Sel.Building != (ecs.Entity{}) {
		drawBuildingOutline(g.Sel.Building, rl.Color{R: 90, G: 200, B: 240, A: 230})
	}

	// Hold-G (or the Debug-panel sticky toggle) also flips the map's
	// road / river / building debug layer.
	if rl.IsKeyDown(rl.KeyG) || debugOverlay.RoadGraph {
		drawRoadGraphDebug(&g.Res.RoadGraph)
		g.UI.ShowMapDebugLy = true
	} else {
		g.UI.ShowMapDebugLy = false
	}

	drawLOSPreview(g.Ctx.LOS)
	if debugOverlay.NavGrid {
		qNav := g.Filt.NavOverlay.Query()
		for qNav.Next() {
			pos, cc, grid, hm := qNav.Get()
			if !debugChunkInRadius(*cc, systems.CurrentOriginChunk) {
				continue
			}
			drawNavGridOverlay(*pos, *cc, grid, hm)
		}
	}
	if debugOverlay.CoverMap {
		qCov := g.Filt.CoverOverlay.Query()
		for qCov.Next() {
			pos, cc, cov, hm := qCov.Get()
			if !debugChunkInRadius(*cc, systems.CurrentOriginChunk) {
				continue
			}
			drawCoverMapOverlay(*pos, *cc, cov, hm)
		}
	}

	g.Frame.VisionPairs = 0
	if debugOverlay.Vision {
		qV := g.Filt.VisionAware.Query()
		for qV.Next() {
			pos, aware := qV.Get()
			if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
				continue
			}
			from := pos.ToRenderSpace(systems.CurrentOriginChunk)
			from.Y += 1.0
			for i := range aware.LastSeen {
				if aware.LastSeen[i].Time == 0 {
					continue
				}
				to := aware.LastSeen[i].Pos.ToRenderSpace(systems.CurrentOriginChunk)
				to.Y += 1.0
				rl.DrawLine3D(from, to, rl.Green)
				g.Frame.VisionPairs++
			}
		}
	} else {
		qV := g.Filt.VisionAware.Query()
		for qV.Next() {
			_, aware := qV.Get()
			for i := range aware.LastSeen {
				if aware.LastSeen[i].Time != 0 {
					g.Frame.VisionPairs++
				}
			}
		}
	}

	g.Frame.SquadsLive = 0
	g.Frame.SquadMembers = 0
	drawAllSquads := rl.IsKeyDown(rl.KeyK) || debugOverlay.SquadLines
	selectedSquad, selectedHomo := groupSelected(g.Sel.Units, g.Maps.SquadMember)
	qSq := g.Filt.Squad.Query()
	for qSq.Next() {
		_, roster := qSq.Get()
		g.Frame.SquadsLive++
		g.Frame.SquadMembers += int(roster.Count)
		squadEnt := qSq.Entity()
		isSelectedSquad := selectedHomo && selectedSquad != (ecs.Entity{}) && selectedSquad == squadEnt
		if !drawAllSquads && !isSelectedSquad {
			continue
		}
		centerWP, ok := systems.SquadCenter(g.App.World, roster, g.Maps.Pos)
		if !ok {
			continue
		}
		centerRender := centerWP.ToRenderSpace(systems.CurrentOriginChunk)
		centerRender.Y += 0.2
		memberPos := make([]rl.Vector3, 0, roster.Count)
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !g.App.World.Alive(mem) {
				continue
			}
			if p := g.Maps.Pos.Get(mem); p != nil {
				r := p.ToRenderSpace(systems.CurrentOriginChunk)
				r.Y += 0.2
				memberPos = append(memberPos, r)
			}
		}
		drawSquadConnections(centerRender, memberPos, g.squadColor(squadEnt))
	}

	if debugOverlay.LevelNavGrid {
		qFloor := g.Filt.FloorNav.Query()
		for qFloor.Next() {
			pos, _, grid := qFloor.Get()
			if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
				continue
			}
			drawFloorNavOverlay(*pos, grid)
		}
	}

	// Surface<->Level edges = green, Level<->Level = yellow. Missing
	// lines through a door/stair ⇒ bake failed to resolve LevelMember.
	if debugOverlay.Transitions {
		levelGridReadMap := ecs.NewMap[components.LevelNavGrid](g.App.World)
		nodeWorld := func(n components.NavNode) (rl.Vector3, bool) {
			switch n.Kind {
			case components.NodeSurface:
				wx := float32(n.Chunk.X)*components.ChunkSize + float32(n.I) + 0.5
				wz := float32(n.Chunk.Z)*components.ChunkSize + float32(n.J) + 0.5
				wp := components.WorldPos{}.Add(rl.Vector3{
					X: wx, Y: systems.GroundHeight(wx, wz) + 0.5, Z: wz,
				})
				return wp.ToRenderSpace(systems.CurrentOriginChunk), true
			case components.NodeLevel:
				rootPos := g.Maps.Pos.Get(n.Level)
				ng := levelGridReadMap.Get(n.Level)
				if rootPos == nil || ng == nil {
					return rl.Vector3{}, false
				}
				rChunkBaseX := float32(rootPos.Chunk.X) * components.ChunkSize
				rChunkBaseZ := float32(rootPos.Chunk.Z) * components.ChunkSize
				cx := rChunkBaseX + ng.Origin.X + float32(n.I) + 0.5
				cz := rChunkBaseZ + ng.Origin.Z + float32(n.J) + 0.5
				wp := components.WorldPos{}.Add(rl.Vector3{X: cx, Y: ng.Origin.Y + 0.5, Z: cz})
				return wp.ToRenderSpace(systems.CurrentOriginChunk), true
			}
			return rl.Vector3{}, false
		}
		for _, edges := range g.Res.Transitions.Out {
			for _, e := range edges {
				a, ok1 := nodeWorld(e.From)
				b, ok2 := nodeWorld(e.To)
				if !ok1 || !ok2 {
					continue
				}
				col := rl.Color{R: 50, G: 220, B: 80, A: 255}
				if e.From.Kind == components.NodeLevel && e.To.Kind == components.NodeLevel {
					col = rl.Color{R: 240, G: 220, B: 60, A: 255}
				}
				rl.DrawLine3D(a, b, col)
			}
		}
	}

	g.Frame.CoverSlots = 0
	if debugOverlay.CoverSlots {
		qSlot := g.Filt.CoverSlot.Query()
		for qSlot.Next() {
			pos, slot := qSlot.Get()
			if !debugChunkInRadius(pos.Chunk, systems.CurrentOriginChunk) {
				continue
			}
			render := pos.ToRenderSpace(systems.CurrentOriginChunk)
			rl.DrawCubeV(render, rl.Vector3{X: 0.25, Y: 0.25, Z: 0.25}, rl.Yellow)
			tip := rl.Vector3{
				X: render.X + slot.OriginDir.X*1.0,
				Y: render.Y,
				Z: render.Z + slot.OriginDir.Z*1.0,
			}
			rl.DrawLine3D(render, tip, rl.Magenta)
			g.Frame.CoverSlots++
		}
	} else {
		qSlot := g.Filt.CoverSlot.Query()
		for qSlot.Next() {
			qSlot.Get()
			g.Frame.CoverSlots++
		}
	}

	drawNavPath(g.Sel.NavPath, *g.Frame.AnchorPos)

	if debugOverlay.UnitPaths {
		drawUnitPaths(unitPathRenderCtx{
			filter:         g.Filt.UnitPathSquad,
			soloFilter:     g.Filt.UnitPathSolo,
			squadMemberMap: g.Maps.SquadMember,
			selectedSquad:  unitPathsSelectedSquad(g.Sel.Units, g.Maps.SquadMember),
		})
	}

	// Ghost preview rotates live during facing-drag so the orientation
	// matches what release will commit to.
	var ghostDragFacing *float32
	if g.UI.RMB.Active && g.UI.RMB.FacingActive {
		dx := g.Frame.Cursor.X - g.UI.RMB.PressOrigin.X
		dy := g.Frame.Cursor.Y - g.UI.RMB.PressOrigin.Y
		yaw := float32(math.Atan2(float64(dx), float64(-dy)))
		ghostDragFacing = &yaw
		g.Frame.GhostTarget = g.UI.RMB.PressTarget
		g.Frame.GhostTargetOK = true
	}
	// Popup-hover swaps ghost placement per item; anchored at press-time target.
	var ghostPopupItem *ui.ContextMenuItem
	if g.UI.CtxMenu.IsActive() {
		if item, ok := g.UI.CtxMenu.HoveredItemDetails(); ok {
			ghostPopupItem = &item
		}
		g.Frame.GhostTarget = g.UI.RMB.PressTarget
		g.Frame.GhostTargetOK = true
	}
	drawVehicleRoutes(g.Ctx.Route, g.Sel.Units,
		g.Frame.Focused == ui.Panel3D && g.Frame.GhostTargetOK, g.Frame.GhostTarget)
	drawSelectionGhost(g.Ctx.Ghost, g.Sel.Units, g.Frame.Focused == ui.Panel3D, g.Frame.GhostTarget, g.Frame.GhostTargetOK,
		ghostDragFacing, ghostPopupItem, g.UI.RMB.HoveredBldg, g.UI.RMB.PressRaw, g.Maps.Level)

	drawOrderMarkers3D(g.Ctx.OrderMarker, g.Sel.Units)
	drawAssignedBuildingSlots(g.Ctx.Ghost, g.Sel.Units)

	// Translucent: after every opaque draw, or the depth write would
	// reject whatever stands beyond the surface.
	g.Frame.RibbonsDrawn += g.Ctx.Ribbons.Water.draw(g.terrainMaterial)

	// Exhaust queues into the same billboard batch and must land before
	// drawParticles, which owns the flush.
	g.Ctx.Exhaust.draw(g.Ctx.Particle.Puffs,
		float32(systems.CurrentOriginChunk.X)*components.ChunkSize,
		float32(systems.CurrentOriginChunk.Z)*components.ChunkSize)
	drawParticles(g.Ctx.Particle, float32(g.App.Elapsed().Seconds()))

	rl.EndMode3D()
	rl.EndTextureMode()
}
