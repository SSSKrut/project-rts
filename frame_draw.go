package main

import (
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
		World:              g.App.World,
		Cam:                g.UI.MapCam,
		Underlay:           &g.UI.Underlay,
		AnchorPos:          *g.Frame.AnchorPos,
		Selected:           g.Sel.Units,
		Hovered:            g.Sel.Hovered,
		PosMap:             g.Maps.Pos,
		RosterMap:          g.Maps.Roster,
		SquadMemberMap:     g.Maps.SquadMember,
		SquadFilter:        g.Filt.Squad,
		SquadCenter:        g.squadCenter,
		SquadColor:         g.squadColor,
		RoadGraph:          &g.Res.RoadGraph,
		Rivers:             &g.Res.Rivers,
		Buildings:          &g.Res.BuildingPlans,
		ShowDebugLayers:    g.UI.ShowMapDebugLy,
		OrderQueueMap:      g.Maps.OrderQueue,
		OrderKindMap:       g.Maps.OrderKind,
		OrderTargetMap:     g.Maps.OrderTarget,
		OrderChainMap:      g.Maps.OrderChain,
		SmoothedSquadPos:   g.UI.SmoothedSquadPos,
		MapMarkerCache:     &g.Res.MapMarkerCache,
		RoleMap:            g.Maps.Role,
		Font:               g.hudFont,
		MapPingFilter:      g.Filt.MapPing,
		Clock:              g.Svc.Squad.Clock(),
		ContactFilter:      g.Filt.Contact,
		ContactMap:         g.Maps.Contact,
		ContactOverrideMap: g.Maps.ContactOverride,
		LOSFanOrigin:       g.Ctx.LOS.origin,
		LOSFanRuns:         g.Ctx.LOS.fanRuns(),
		LOSFanRange:        g.Ctx.LOS.sensorR,
		LOSFanFalloff:      g.Ctx.LOS.falloff,
		LOSWeaponRs:        g.Ctx.LOS.weaponRs,
	}
	ui.DrawMap(g.Frame.PanelMap, mapCtx)

	inspectorFocused := g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelInspect
	inspectorPanel := g.UI.PanelMgr.Get(ui.PanelInspect)
	inspectorScroll := g.UI.PanelMgr.ScrollByID(ui.PanelInspect)
	ui.DrawInspector(inspectorPanel, ui.InspectorCtx{
		InspectorMaps: g.Ctx.Inspector,
		World:         g.App.World,
		Selected:      g.Sel.Units,
		Hovered:       g.Sel.Hovered,
		Font:          g.hudFont,
		EventLog:      g.Res.EventLog,
		Cursor:        g.Frame.Cursor,
		LMBPressed:    !g.chromeBusy() && !g.UI.ScrollDragging && rl.IsMouseButtonPressed(rl.MouseButtonLeft),
		PanelFocused:  inspectorFocused,
		Scroll:        inspectorScroll,
		SquadColor:    g.squadColor,
		RoadGraph:     &g.Res.RoadGraph,
	})
	// Scrollbar overlay drawn AFTER DrawInspector so EndScissorMode has released its clip.
	if inspectorScroll != nil {
		ui.ClampScrollOffset(inspectorPanel, inspectorScroll)
		ui.DrawScrollbar(inspectorPanel, inspectorScroll)
	}
	// Consume Inspector button requests (Phase 18.5).
	if ui.CameraFocusRequest.Active {
		*g.Maps.Pos.Get(g.anchor) = ui.CameraFocusRequest.Pos
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
	if ui.SymbolApplyRequest.Active {
		if len(g.Sel.Units) == 1 {
			tgt := g.Sel.Units[0]
			if g.App.World.Alive(tgt) {
				if g.Maps.Contact.Has(tgt) {
					if ov := g.Maps.ContactOverride.Get(tgt); ov != nil {
						ov.Spec = ui.SymbolApplyRequest.Spec
					} else {
						g.Maps.ContactOverride.Add(tgt, &components.ContactSymbolOverride{Spec: ui.SymbolApplyRequest.Spec})
					}
					// Player-set classification → freeze auto-promote.
					if !g.Maps.ContactPlayerSet.Has(tgt) {
						g.Maps.ContactPlayerSet.Add(tgt, &components.ContactPlayerSet{})
					}
					if c := g.Maps.Contact.Get(tgt); c != nil {
						c.PerceivedAffil = ui.SymbolApplyRequest.Spec.Affiliation
						c.PerceivedDim = ui.SymbolApplyRequest.Spec.Dimension
						c.Source = components.SourcePlayerClassified
					}
				} else if ov := g.Maps.UnitOverride.Get(tgt); ov != nil {
					ov.Spec = ui.SymbolApplyRequest.Spec
				} else {
					g.Maps.UnitOverride.Add(tgt, &components.UnitSymbolOverride{Spec: ui.SymbolApplyRequest.Spec})
				}
			}
		}
		ui.SymbolApplyRequest.Active = false
	}

	seEnabled := len(g.Sel.Units) == 1
	_, feHomo := groupSelected(g.Sel.Units, g.Maps.SquadMember)
	feEnabled := feHomo && len(g.Sel.Units) > 0
	seActive := g.UI.PanelMgr.LeafFor(ui.PanelSymbology) != nil ||
		g.UI.Floating.Get("float:"+string(ui.PanelSymbology)) != nil
	feActive := g.UI.PanelMgr.LeafFor(ui.PanelFormation) != nil ||
		g.UI.Floating.Get("float:"+string(ui.PanelFormation)) != nil
	g.UI.TopBarHits = ui.DrawTopBar(
		g.UI.PanelMgr.Get(ui.PanelTopBar), g.hudFont, ui.TimeDisplay{
			Scale:   g.App.TimeScale,
			Elapsed: float32(g.App.Elapsed().Seconds()),
		},
		ui.TopBarToolCtx{
			SEActive: seActive, SEEnabled: seEnabled,
			FEActive: feActive, FEEnabled: feEnabled,
			SettingsOn: false, SettingsCan: true,
		},
		g.Frame.Cursor)

	g.UI.TimelineData = buildTimelineData(g.App.World, g.Filt.Squad, g.Maps.Pos, g.Maps.Faction,
		g.Maps.OrderQueue, g.Maps.OrderChain, g.Maps.OrderKind, g.Maps.OrderTarget, g.Maps.OrderState,
		g.Maps.OrderProgress, g.Maps.OrderIssuedAt, g.squadColor,
		float32(g.App.Elapsed().Seconds()))
	ui.DrawTimelinePanel(g.UI.PanelMgr.Get(ui.PanelTimeline), g.hudFont, g.UI.TimelineData, &g.UI.TimelineView)
	if g.UI.TimelineHoverOK && g.UI.TimelineHoverHit.HitOrder {
		ui.DrawTimelineTooltip(g.hudFont, g.Frame.Cursor, g.UI.TimelineHoverBlk)
	}

	if leaf := g.UI.PanelMgr.LeafFor(ui.PanelFormation); leaf != nil {
		formationLMB := !g.chromeBusy() && !g.UI.ScrollDragging &&
			g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelFormation &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		g.UI.FormationEditor.DrawPanel(g.UI.PanelMgr.Get(ui.PanelFormation),
			g.hudFont, g.Frame.Cursor, formationLMB)
	}

	if g.UI.PanelMgr.LeafFor(ui.PanelSymbology) != nil {
		symFocused := g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelSymbology
		symLMB := symFocused && !g.chromeBusy() && !g.UI.ScrollDragging &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		g.UI.SymbolEditor.DrawPanel(g.UI.PanelMgr.Get(ui.PanelSymbology),
			g.hudFont, g.Frame.Cursor, symLMB, symFocused)
	}

	if leaf := g.UI.PanelMgr.LeafFor(ui.PanelDebug); leaf != nil {
		debugLMB := !g.chromeBusy() && !g.UI.ScrollDragging &&
			g.UI.PanelMgr.FocusedAt(g.Frame.Cursor) == ui.PanelDebug &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft)
		g.drawDebugWidget(g.UI.PanelMgr.Get(ui.PanelDebug), g.hudFont, g.Frame.Cursor, debugLMB)
	}

	g.UI.Scene3DRT.Composite(g.Frame.Panel3D)

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

	g.UI.ChevronMenu.Draw(g.hudFont, g.Frame.Cursor)

	g.UI.Floating.DrawAll(g.hudFont, g.Frame.Cursor, rl.IsMouseButtonPressed(rl.MouseButtonLeft))
	g.UI.Floating.DrawSwitchMenu(g.hudFont, g.Frame.Cursor)

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
			case ui.ContactMenuTagOpenBuilder:
				if c := g.Maps.Contact.Get(g.UI.ContactMenuTarget); c != nil {
					g.UI.SymbolEditor.InProgress = ui.DefaultSpecForDimension(c.PerceivedAffil, c.PerceivedDim)
				}
				g.Sel.Units = append(g.Sel.Units[:0], g.UI.ContactMenuTarget)
				g.floatSpawn(ui.PanelSymbology, "Symbology",
					rl.Rectangle{X: g.Frame.Cursor.X, Y: g.Frame.Cursor.Y, Width: 420, Height: 380})
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
		bridgesLive:  g.Frame.BridgesLive,
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

	rl.EndDrawing()

	recordTraceFrame(g.App, rl.GetFrameTime()*1000, rl.GetFPS(), cen)
	handleTraceHotkeys(g.App)
}

// buildTimelineData flattens all squads + their order queues for
// ui.DrawTimelinePanel. Queued blocks stack right after the head's estimated
// end so the timeline reads left-to-right.
func buildTimelineData(
	world *ecs.World,
	squadFilter *ecs.Filter2[components.Squad, components.CommandRoster],
	posMap *ecs.Map[components.WorldPos],
	factionMap *ecs.Map[components.Faction],
	orderQueueMap *ecs.Map[components.OrderQueueHead],
	orderChainMap *ecs.Map[components.OrderChain],
	orderKindMap *ecs.Map[components.OrderKind],
	orderTargetMap *ecs.Map[components.OrderTarget],
	orderStateMap *ecs.Map[components.OrderState],
	orderProgressMap *ecs.Map[components.OrderProgress],
	orderIssuedAtMap *ecs.Map[components.OrderIssuedAt],
	squadColor func(ent ecs.Entity) rl.Color,
	nowT float32,
) ui.TimelineData {
	data := ui.TimelineData{NowT: nowT}
	q := squadFilter.Query()
	for q.Next() {
		squad := q.Entity()
		_, roster := q.Get()
		center, _ := systems.SquadCenter(world, roster, posMap)

		head := orderQueueMap.Get(squad)
		row := ui.TimelineSquadRow{
			Squad: squad,
			Color: squadColor(squad),
		}
		if head != nil && head.First != (ecs.Entity{}) {
			lastEnd := float32(0)
			cur := head.First
			isHead := true
			for cur != (ecs.Entity{}) && world.Alive(cur) {
				kind := orderKindMap.Get(cur)
				state := orderStateMap.Get(cur)
				target := orderTargetMap.Get(cur)
				if kind == nil || state == nil || target == nil {
					break
				}
				startT := nowT
				if iss := orderIssuedAtMap.Get(cur); iss != nil {
					startT = iss.Time
				}
				if !isHead && startT < lastEnd {
					startT = lastEnd
				}
				est := estimateOrderDuration(kind.Code, center, target.Pos)
				endT := startT + est
				prog := float32(0)
				if pr := orderProgressMap.Get(cur); pr != nil {
					prog = pr.Value
				}
				row.Orders = append(row.Orders, ui.TimelineOrderBlock{
					Order:     cur,
					KindCode:  kind.Code,
					StateCode: state.Code,
					StartT:    startT,
					EndT:      endT,
					Progress:  prog,
					IsHead:    isHead,
				})
				lastEnd = endT
				ch := orderChainMap.Get(cur)
				if ch == nil {
					break
				}
				cur = ch.Next
				isHead = false
			}
		}
		data.Rows = append(data.Rows, row)
	}
	q.Close()
	_ = factionMap
	return data
}

// estimateOrderDuration is a heuristic display-only duration per order kind.
func estimateOrderDuration(kind components.OrderKindCode, from, to components.WorldPos) float32 {
	var moveTime float32
	switch kind {
	case components.OrderKindMoveTo, components.OrderKindGarrison,
		components.OrderKindOccupyBuilding, components.OrderKindClearBuilding,
		components.OrderKindOccupyTrench:
		moveTime = components.Distance(from, to) / ui.TimelineMoveSpeedMps
	}
	var est float32
	switch kind {
	case components.OrderKindMoveTo:
		est = moveTime
	case components.OrderKindGarrison, components.OrderKindOccupyBuilding, components.OrderKindClearBuilding:
		est = moveTime + ui.TimelineGarrisonDurationSec
	case components.OrderKindOccupyTrench:
		est = moveTime + ui.TimelineDefendDurationSec
	case components.OrderKindDefendPosition:
		est = ui.TimelineDefendDurationSec
	case components.OrderKindPatrol:
		est = ui.TimelinePatrolDurationSec
	case components.OrderKindAttackTarget:
		est = ui.TimelineAttackDurationSec
	case components.OrderKindSuppressFire:
		est = ui.TimelineSuppressDurationSec
	default:
		est = ui.TimelineUnknownDurationSec
	}
	if est < ui.TimelineMinBlockDurationSec {
		est = ui.TimelineMinBlockDurationSec
	}
	return est
}
