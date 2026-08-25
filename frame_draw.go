package main

import (
	"fmt"
	"runtime"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// drawUI composites the scene texture and draws the 2D half: panels, widgets,
// chrome, menus and the profiler HUD.
func (g *Game) drawUI() {
	rl.BeginDrawing()
	rl.ClearBackground(rl.Color{R: 8, G: 10, B: 14, A: 255})

	// Panel content drawn before chrome so title bars overlay cleanly.
	mapCtx := ui.MapRenderCtx{
		World:            g.App.World,
		Cam:              g.UI.MapCam,
		Underlay:         &g.UI.Underlay,
		AnchorPos:        *g.Frame.AnchorPos,
		Selected:         g.Sel.Units,
		Hovered:          g.Sel.Hovered,
		PosMap:           g.Maps.Pos,
		RosterMap:        g.Maps.Roster,
		SquadMemberMap:   g.Maps.SquadMember,
		SquadFilter:      g.Filt.Squad,
		CommanderFilter:  g.Filt.Commander,
		SquadCenter:      g.squadCenter,
		SquadColor:       g.squadColor,
		RoadGraph:        &g.Res.RoadGraph,
		Rivers:           &g.Res.Rivers,
		Buildings:        &g.Res.BuildingPlans,
		ShowDebugLayers:  g.UI.ShowMapDebugLy,
		OrderQueueMap:    g.Maps.OrderQueue,
		OrderKindMap:     g.Maps.OrderKind,
		OrderTargetMap:   g.Maps.OrderTarget,
		OrderChainMap:    g.Maps.OrderChain,
		SmoothedSquadPos: g.UI.SmoothedSquadPos,
		MapMarkerCache:   &g.Res.MapMarkerCache,
		RoleMap:          g.Maps.Role,
		VehicleMap:       g.Maps.Vehicle,
		UnitFilter:       g.Filt.UnitRender,
		VehicleFilter:    g.Filt.VehicleRender,
		AircraftFilter:   g.Filt.AircraftRender,
		FactionMap:       g.Maps.Faction,
		SquadOverrideMap: g.Maps.SquadOverride,
		Font:             g.hudFont,
		MapPingFilter:    g.Filt.MapPing,
		Clock:            g.Svc.Squad.Clock(),
		Clusters:         &g.Frame.MapClusters,
		LOSFanOrigin:     g.Ctx.LOS.origin,
		LOSFanRuns:       g.Ctx.LOS.fanRuns(),
		LOSFanRange:      g.Ctx.LOS.sensorR,
		LOSFanFalloff:    g.Ctx.LOS.falloff,
		LOSWeaponRs:      g.Ctx.LOS.weaponRs,
		Coverage:         &g.Ctx.Coverage.View,
	}
	ui.DrawMap(g.Frame.PanelMap, mapCtx)
	g.drawMapRuler()

	inspectorFocused := g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelInspect
	inspectorPanel := g.UI.PanelMgr.Get(ui.PanelInspect)
	inspectorScroll := g.UI.PanelMgr.ScrollByID(ui.PanelInspect)
	ui.DrawInspector(inspectorPanel, ui.InspectorCtx{
		InspectorMaps: g.Ctx.Inspector,
		Behavior:      g.Ctx.Behavior,
		World:         g.App.World,
		Selected:      g.Sel.Units,
		Hovered:       g.Sel.Hovered,
		Font:          g.hudFont,
		EventLog:      g.Res.EventLog,
		Now:           g.simNow(),
		Cursor:        g.Frame.Cursor,
		LMBPressed:    !g.chromeBusy() && !g.scrollDragging() && rl.IsMouseButtonPressed(rl.MouseButtonLeft),
		PanelFocused:  inspectorFocused,
		Scroll:        inspectorScroll,
		SquadColor:    g.squadColor,
		RoadGraph:     &g.Res.RoadGraph,
		GroundAt:      g.groundAt,
		Shift:         g.Frame.Shift,
	})
	// Scrollbar overlay drawn AFTER DrawInspector so EndScissorMode has released its clip.
	if inspectorScroll != nil {
		ui.ClampScrollOffset(inspectorPanel, inspectorScroll)
		ui.DrawScrollbar(inspectorPanel, inspectorScroll)
	}
	// Consume Inspector button requests (Phase 18.5).
	if ui.CameraFocusRequest.Active {
		g.flyAnchorTo(ui.CameraFocusRequest.Pos)
		ui.CameraFocusRequest.Active = false
	}
	if ui.DeleteContactRequest.Active {
		tgt := ui.DeleteContactRequest.Entity
		if tgt != (ecs.Entity{}) && g.App.World.Alive(tgt) {
			if c := g.Maps.Contact.Get(tgt); c != nil {
				delete(g.Res.ContactRegistry.Tracked, c.Tracked)
			}
			g.App.World.RemoveEntity(tgt)
			for i, e := range g.Sel.Units {
				if e == tgt {
					g.Sel.Units = append(g.Sel.Units[:i], g.Sel.Units[i+1:]...)
					break
				}
			}
		}
		ui.DeleteContactRequest.Active = false
		ui.DeleteContactRequest.Entity = ecs.Entity{}
	}
	if ui.SelectSquadRequest.Active {
		g.selectSquad(ui.SelectSquadRequest.Squad)
		ui.SelectSquadRequest.Active = false
		ui.SelectSquadRequest.Squad = ecs.Entity{}
	}
	if ui.SelectUnitRequest.Active {
		unit := ui.SelectUnitRequest.Unit
		if g.App.World.Alive(unit) && g.isControllable(unit) {
			if ui.SelectUnitRequest.Additive {
				g.toggleSelected(unit)
			} else {
				g.Sel.Units = append(g.Sel.Units[:0], unit)
			}
		}
		ui.SelectUnitRequest.Active = false
		ui.SelectUnitRequest.Unit = ecs.Entity{}
		ui.SelectUnitRequest.Additive = false
	}
	if ui.EventFocusRequest.Active {
		g.flyTo(ui.EventFocusRequest.Pos)
		ui.EventFocusRequest.Active = false
	}
	if ui.OpenWidgetRequest.Active {
		id := ui.OpenWidgetRequest.Panel
		// Already on screen as a workspace leaf? Then the click has nothing
		// to open — floating a second copy would just duplicate it.
		if g.UI.PanelMgr.LeafFor(id) == nil && g.UI.Floating.Get("float:"+string(id)) == nil {
			g.floatSpawn(id, ui.WidgetTitle(id),
				rl.Rectangle{X: g.Frame.Cursor.X, Y: g.Frame.Cursor.Y, Width: 380, Height: 440})
		}
		ui.OpenWidgetRequest.Active = false
	}

	g.UI.TopBarHits = ui.DrawTopBar(
		g.UI.PanelMgr.Get(ui.PanelTopBar), g.hudFont, ui.TimeDisplay{
			Scale:     g.App.TimeScale,
			Elapsed:   float32(g.App.Elapsed().Seconds()),
			Throttled: g.App.Throttled(),
		},
		ui.TopBarToolCtx{SettingsOn: false, SettingsCan: true},
		g.Frame.Cursor)

	if g.UI.PanelMgr.LeafFor(ui.PanelBehavior) != nil {
		behPanel := g.UI.PanelMgr.Get(ui.PanelBehavior)
		behScroll := g.UI.PanelMgr.ScrollByID(ui.PanelBehavior)
		behFocused := g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelBehavior
		behLMB := behFocused && !g.chromeBusy() && !g.scrollDragging() &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		ui.DrawBehaviorPanel(behPanel,
			g.behaviorCtx(g.hudFont, g.Frame.Cursor, behLMB, behFocused, behScroll))
		// After the panel released its scissor.
		ui.ClampScrollOffset(behPanel, behScroll)
		ui.DrawScrollbar(behPanel, behScroll)
	}

	if leaf := g.UI.PanelMgr.LeafFor(ui.PanelDebug); leaf != nil {
		debugLMB := !g.chromeBusy() && !g.scrollDragging() &&
			g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelDebug &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		g.drawDebugWidget(g.UI.PanelMgr.Get(ui.PanelDebug), g.hudFont, g.Frame.Cursor, debugLMB)
	}

	if debugOverlay.Clouds && g.UI.Scene3DRT.DepthTex && g.Ctx.Clouds != nil && g.Ctx.Clouds.ok {
		g.Ctx.Clouds.composite(g.UI.Scene3DRT, g.Frame.Panel3D, &g.Res.Atmosphere,
			g.Ctx.CloudDrift, &g.Ctx.Daylight)
	} else {
		g.UI.Scene3DRT.Composite(g.Frame.Panel3D)
	}

	// Role labels are 2D screen-projected after RT composite; scissored
	// to Panel3D so they don't bleed onto neighbouring panels.
	rl.BeginScissorMode(int32(g.Frame.Panel3DContent.X), int32(g.Frame.Panel3DContent.Y),
		int32(g.Frame.Panel3DContent.Width), int32(g.Frame.Panel3DContent.Height))
	quLabels := g.Filt.UnitRender.Query()
	for quLabels.Next() {
		pos, _, st := quLabels.Get()
		ent := quLabels.Entity()
		renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
		role := components.RoleRifleman
		if r := g.Maps.Role.Get(ent); r != nil {
			role = r.Kind
		}
		drawUnitRoleLabel(renderPos, *st, role, g.hudFont, g.Frame.Panel3DContent)
		if stam := g.Maps.Stamina.Get(ent); stam != nil {
			drawUnitStaminaBar(renderPos, *st, role, stam.Current, stam.MaxLevel, g.Frame.Panel3DContent)
		}
		if hp := g.Maps.HP.Get(ent); hp != nil {
			drawUnitHPBar(renderPos, *st, role, hp.Current, hp.Max, g.Frame.Panel3DContent)
		}
		if fac := g.Maps.Faction.Get(ent); fac == nil || fac.ID == components.FactionPlayer {
			if det := g.Maps.Detectability.Get(ent); det != nil {
				drawUnitExposureBar(renderPos, *st, role, det.Meter[components.FactionEnemyRed], g.Frame.Panel3DContent)
			}
		}
	}
	rl.EndScissorMode()

	// Squad bar over the composited scene: the layout was frozen before the
	// tick, values are re-read now (a card's owner may have died meanwhile —
	// drawBarCard checks Alive).
	if len(g.Frame.SquadBar.Cards) > 0 {
		barCtx := g.squadBarCtx()
		barCtx.LMBPressed = !g.chromeBusy() && !g.scrollDragging() &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		ui.DrawSquadBar(g.Frame.SquadBar, barCtx)
	}

	if g.UI.BuildingWidget != nil {
		rl.BeginScissorMode(int32(g.Frame.Panel3DContent.X), int32(g.Frame.Panel3DContent.Y),
			int32(g.Frame.Panel3DContent.Width), int32(g.Frame.Panel3DContent.Height))
		ui.DrawBuildingWidget(g.UI.BuildingWidget, g.hudFont)
		rl.EndScissorMode()
	}

	// Marquee drawn after composite, scissored to Panel3D.
	if g.UI.MarqueeActive && g.UI.MarqueeOrigin == ui.Panel3D {
		end := g.Frame.Cursor
		minX, maxX := g.UI.MarqueeStart.X, end.X
		if maxX < minX {
			minX, maxX = maxX, minX
		}
		minY, maxY := g.UI.MarqueeStart.Y, end.Y
		if maxY < minY {
			minY, maxY = maxY, minY
		}
		rl.BeginScissorMode(int32(g.Frame.Panel3D.Bounds.X), int32(g.Frame.Panel3D.Bounds.Y),
			int32(g.Frame.Panel3D.Bounds.Width), int32(g.Frame.Panel3D.Bounds.Height))
		rl.DrawRectangleLines(int32(minX), int32(minY),
			int32(maxX-minX), int32(maxY-minY),
			rl.Color{R: 0, G: 220, B: 220, A: 255})
		rl.DrawRectangle(int32(minX), int32(minY),
			int32(maxX-minX), int32(maxY-minY),
			rl.Color{R: 0, G: 220, B: 220, A: 40})
		rl.EndScissorMode()
	}

	// Chrome drawn last so it overlays content (incl. marquee strokes
	// that bleed onto title bars). PanelTopBar draws its own chrome.
	g.UI.PanelMgr.Workspace.WalkLeaves(func(l *ui.LayoutNode) {
		ui.DrawChrome(ui.Panel{ID: l.Panel, Bounds: l.Bounds, Title: l.Title}, g.hudFont, 16)
	})

	ui.DrawCornerHandles(g.UI.PanelMgr, g.Frame.Cursor)
	if g.UI.PanelMgr.IsCornerDragging() {
		ui.DrawCornerDragPreview(g.UI.PanelMgr, g.Frame.Cursor)
	}
	ui.DrawTitleDragPreview(g.UI.PanelMgr, g.Frame.Cursor)

	// Over the chrome (it must not be hidden by a title bar), under the
	// floaters and menus (those own the cursor when they are open).
	g.drawAttentionBanner()

	g.UI.ChevronMenu.Draw(g.hudFont, g.Frame.Cursor)

	g.UI.Floating.DrawAll(g.hudFont, g.Frame.Cursor, rl.IsMouseButtonPressed(rl.MouseButtonLeft))
	g.UI.Floating.DrawSwitchMenu(g.hudFont, g.Frame.Cursor)

	// After the floaters: the hovered block may belong to one of them, and a
	// tooltip drawn earlier would be painted over by its own panel.

	g.UI.CtxMenu.Draw(g.hudFont, g.Frame.Cursor)

	// Phase 18.5.F: contact RMB menu — separate tick + draw lane so it
	// never clashes with the order popup.
	if g.UI.ContactCtxMenu.IsActive() {
		escPressed := rl.IsKeyPressed(rl.KeyEscape)
		lmbPressed := rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		res := g.UI.ContactCtxMenu.Tick(g.Frame.Cursor, lmbPressed, escPressed)
		if res.Committed && g.UI.ContactMenuTarget != (ecs.Entity{}) && g.App.World.Alive(g.UI.ContactMenuTarget) {
			switch res.Item.Tag {
			case ui.ContactMenuTagApplyPreset:
				if ov := g.Maps.ContactOverride.Get(g.UI.ContactMenuTarget); ov != nil {
					ov.Spec = res.Item.ContactSpec
				} else {
					g.Maps.ContactOverride.Add(g.UI.ContactMenuTarget, &components.ContactSymbolOverride{Spec: res.Item.ContactSpec})
				}
				if !g.Maps.ContactPlayerSet.Has(g.UI.ContactMenuTarget) {
					g.Maps.ContactPlayerSet.Add(g.UI.ContactMenuTarget, &components.ContactPlayerSet{})
				}
				if c := g.Maps.Contact.Get(g.UI.ContactMenuTarget); c != nil {
					c.PerceivedAffil = res.Item.ContactSpec.Affiliation
					c.PerceivedDim = res.Item.ContactSpec.Dimension
					c.Source = components.SourcePlayerClassified
				}
			case ui.ContactMenuTagResetToAuto:
				if g.Maps.ContactOverride.Has(g.UI.ContactMenuTarget) {
					g.Maps.ContactOverride.Remove(g.UI.ContactMenuTarget)
				}
				if g.Maps.ContactPlayerSet.Has(g.UI.ContactMenuTarget) {
					g.Maps.ContactPlayerSet.Remove(g.UI.ContactMenuTarget)
				}
				if c := g.Maps.Contact.Get(g.UI.ContactMenuTarget); c != nil {
					c.Source = components.SourceSensor
					c.PerceivedAffil = components.AffilUnknown
					c.PerceivedDim = components.DimUnknownClass
				}
			case ui.ContactMenuTagDelete:
				if c := g.Maps.Contact.Get(g.UI.ContactMenuTarget); c != nil {
					delete(g.Res.ContactRegistry.Tracked, c.Tracked)
				}
				g.App.World.RemoveEntity(g.UI.ContactMenuTarget)
				for i, e := range g.Sel.Units {
					if e == g.UI.ContactMenuTarget {
						g.Sel.Units = append(g.Sel.Units[:i], g.Sel.Units[i+1:]...)
						break
					}
				}
			}
			g.UI.ContactMenuTarget = ecs.Entity{}
		} else if res.Cancelled {
			g.UI.ContactMenuTarget = ecs.Entity{}
		}
		g.UI.ContactCtxMenu.Draw(g.hudFont, g.Frame.Cursor)
	}

	const heapInterval = time.Second
	if g.App.Prof.HeapStale(g.App.Elapsed(), heapInterval) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		g.App.Prof.SetHeap(ms.HeapAlloc, g.App.Elapsed())
	}
	if g.App.Prof.EntityCountStale(g.App.Elapsed(), heapInterval) {
		st := g.App.World.Stats()
		g.App.Prof.SetEntityCount(st.Entities.Used, g.App.Elapsed())
	}

	totalUnits := countFilter1(g.Filt.Unit)
	soloists := totalUnits - g.Frame.SquadMembers
	if soloists < 0 {
		soloists = 0
	}
	cen := census{
		chunksActive: g.Frame.ChunksActive,
		chunksRel:    g.Frame.ChunksRel,
		chunksTotal:  countFilter1(g.Filt.ChunkAll),
		navChunks:    countFilter1(g.Filt.NavGridChunk),
		props:        g.Frame.PropsLive,
		ribbonsDrawn: g.Frame.RibbonsDrawn,
		walls:        g.Frame.WallsLive,
		floors:       g.Frame.FloorsLive,
		stairs:       countFilter1(g.Filt.StairsCount),
		coverSlots:   g.Frame.CoverSlots,
		units:        totalUnits,
		weapons:      countFilter1(g.Filt.Weapon),
		squads:       g.Frame.SquadsLive,
		squadMembers: g.Frame.SquadMembers,
		soloists:     soloists,
		transitions:  transitionEdgeCount(&g.Res.Transitions),
		visionPairs:  g.Frame.VisionPairs,
		selection:    len(g.Sel.Units),
		pathWaypts:   len(g.Sel.NavPath),
		roadNodes:    len(g.Res.RoadGraph.Nodes),
		roadEdges:    len(g.Res.RoadGraph.Edges),
		bridgeEdges:  g.Res.BridgeEdges,
		rivers:       len(g.Res.Rivers.Polylines),
		bldgPlans:    len(g.Res.BuildingPlans.Plans),
		trenches:     len(g.Res.Trenches.Lines),
	}

	drawCollapsedProfHUD(&g.App.Prof, g.UI.ScreenW, g.hudFont)
	if g.UI.ExpandedHUD {
		drawExpandedProfHUD(&g.App.Prof, g.UI.ScreenW, cen, g.hudFont)
	}

	g.maybeScreenshot()
	rl.EndDrawing()

	recordTraceFrame(g.App, rl.GetFrameTime()*1000, rl.GetFPS(), cen)
	handleTraceHotkeys(g.App)
}

// soloCommanderName labels a one-body row. "Squad #A3" would be a lie and
// "#A3" tells the player nothing about which of their three trucks it is.
func (g *Game) soloCommanderName(ent ecs.Entity) string {
	id := ent.ID() & 0xFFF
	if ac := g.Maps.Aircraft.Get(ent); ac != nil {
		return fmt.Sprintf("%s #%X", components.SpecForAircraft(ac.Kind).Name, id)
	}
	if v := g.Maps.Vehicle.Get(ent); v != nil {
		return fmt.Sprintf("%s #%X", components.SpecForVehicle(v.Kind).Name, id)
	}
	return fmt.Sprintf("Unit #%X", id)
}
