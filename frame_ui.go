package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

var devSpawnLabels = []string{"Rifleman", "Enemy", "Truck", "BTR", "BMP", "Tank", "ATC"}

// initUI builds the panel layout, the 2D map underlay and the widget
// singletons. Teardown is shutdownUI, deferred by main right after this runs.
func (g *Game) initUI() {
	g.terrainMaterial = rl.LoadMaterialDefault()
	g.Ctx.WorldShader = newWorldShader()
	g.Ctx.WorldShader.apply(&g.terrainMaterial)
	g.Ctx.Ribbons.upload()

	g.UI.ScreenW, g.UI.ScreenH = initialScreenWidth, initialScreenHeight
	g.UI.PanelMgr = ui.NewPanelManager()
	// Restore split ratios from disk before the first Recompute.
	loadLayout(g.UI.PanelMgr, g.timelineLeafView())
	g.UI.PanelMgr.Recompute(g.UI.ScreenW, g.UI.ScreenH)
	g.UI.Scene3DRT = ui.NewScene3DRT(g.UI.PanelMgr.Get(ui.Panel3D))

	// Pre-bake the map underlay (2 km x 2 km, 4 m/pixel = 500x500 = 250 KB).
	g.UI.Underlay = ui.BakeUnderlay(0, 0, 2000, 4, func(wx, wz float32) float32 {
		return systems.GroundHeight(wx, wz)
	})
	g.UI.MapCam = ui.NewMapCamera()
	g.UI.Floating = ui.NewFloatingState()
	g.UI.SquadBar = ui.NewSquadBarState()
	g.UI.SmoothedSquadPos = make(map[ecs.Entity]components.WorldPos, 8)

	// Floating and workspace forms share this pointer so zoom / kind / custom
	// slots survive a re-dock; SelectionFn keeps it bound to the selected squad.
	formationEditorCtx := ui.FormationEditorCtx{
		World:          g.App.World,
		RosterMap:      g.Maps.Roster,
		FormationMap:   g.Maps.FormationData,
		OrientMap:      g.Maps.FormationOrient,
		CustomSlotsMap: g.Maps.FormationCustomSlots,
		RoleMap:        g.Maps.Role,
		PosMap:         g.Maps.Pos,
		SquadColor:     g.squadColor,
		Presets:        &g.Res.FormationPresets,
		SelectionFn: func() ecs.Entity {
			if cs, homo := groupSelected(g.Sel.Units, g.Maps.SquadMember); homo {
				return cs
			}
			return ecs.Entity{}
		},
	}
	g.UI.FormationEditor = ui.NewFormationEditor(ecs.Entity{}, formationEditorCtx)

	// Symbol Editor (Phase 18.5.D). Singleton shared between workspace leaf
	// and any future floating instance. Apply targets whatever
	// symbolApplyTarget resolves to.
	g.UI.SymbolEditor = ui.NewSymbolEditor(g.symbolApplyTarget)

	g.applyShotSelection()
}

// shutdownUI reverses initUI. Deferred after g.Shutdown so it fires first,
// matching the LIFO order the old main() produced.
func (g *Game) shutdownUI() {
	g.UI.Underlay.Unload()
	g.UI.Scene3DRT.Unload()
	// A capture run may have swapped a leaf for -shot-panel; persisting that
	// would leak a throwaway layout into the player's saved one.
	if *shotPathFlag == "" {
		saveLayout(g.UI.PanelMgr, g.timelineLeafView())
	}
	g.Ctx.Ribbons.unload()
	// Order is load-bearing: hand the borrowed shader / surface textures back
	// first, then free the material (only its map array is raylib's), then let
	// their real owner free them exactly once.
	g.Ctx.WorldShader.release(&g.terrainMaterial)
	rl.UnloadMaterial(g.terrainMaterial)
	g.Ctx.WorldShader.unload()
}

// chromeBusy = "UI chrome currently owns mouse/keyboard"; content layers
// skip LMB handlers when true so chrome doesn't double-fire into the world.
func (g *Game) chromeBusy() bool {
	return g.UI.PanelMgr.IsDragging() || g.UI.PanelMgr.IsCornerDragging() ||
		g.UI.PanelMgr.IsTitleDragging() ||
		g.UI.ChevronMenu.Open || g.UI.Floating.IsBusy(rl.GetMousePosition()) ||
		g.UI.Floating.SwitchMenuOpen()
}

// chromeDragging is chromeBusy without the "cursor is over a floater" clause:
// for input that belongs to a floater's own content, hovering it is the
// precondition, not a veto.
func (g *Game) chromeDragging() bool {
	return g.UI.PanelMgr.IsDragging() || g.UI.PanelMgr.IsCornerDragging() ||
		g.UI.PanelMgr.IsTitleDragging() ||
		g.UI.ChevronMenu.Open || g.UI.Floating.IsDragging() ||
		g.UI.Floating.IsResizing() || g.UI.Floating.SwitchMenuOpen()
}

// symbolApplyTarget is what the Symbol Editor's [Apply to selection] writes
// onto: a lone unit / contact, or the squad behind a whole-squad selection.
// Zero entity = nothing applicable, which disables the button.
func (g *Game) symbolApplyTarget() ecs.Entity {
	if len(g.Sel.Units) == 1 {
		return g.Sel.Units[0]
	}
	if squad, homo := groupSelected(g.Sel.Units, g.Maps.SquadMember); homo &&
		squad != (ecs.Entity{}) && g.App.World.Alive(squad) {
		return squad
	}
	return ecs.Entity{}
}

// mapPickCtx is the slice of MapRenderCtx the map's hit-tests need. One
// builder so hover / click / RMB can never disagree about what is drawn.
func (g *Game) mapPickCtx() ui.MapRenderCtx {
	return ui.MapRenderCtx{
		World:          g.App.World,
		Cam:            g.UI.MapCam,
		PosMap:         g.Maps.Pos,
		SquadFilter:    g.Filt.Squad,
		SquadMemberMap: g.Maps.SquadMember,
		SquadCenter:    g.squadCenter,
		MapMarkerCache: &g.Res.MapMarkerCache,
		FactionMap:     g.Maps.Faction,
		UnitFilter:     g.Filt.UnitRender,
		VehicleFilter:  g.Filt.VehicleRender,
		Clusters:       &g.Frame.MapClusters,
	}
}

func (g *Game) isSelected(e ecs.Entity) int {
	for i := range g.Sel.Units {
		if g.Sel.Units[i] == e {
			return i
		}
	}
	return -1
}

// scrollablePanels get wheel + thumb-drag handling. A widget only reports how
// tall its content came out; everything else is here.
var scrollablePanels = [...]ui.PanelID{ui.PanelInspect, ui.PanelSymbology, ui.PanelBehavior}

func (g *Game) scrollDragging() bool { return g.UI.ScrollDragKey != "" }

// scrollSurface is one scrollable instance of a widget. The same kind can be
// on screen twice (workspace leaf + floater) with different sizes, so state
// is per-surface and keyed by a stable string rather than by PanelID.
type scrollSurface struct {
	key     string
	panel   ui.Panel
	state   *ui.ScrollState
	focused bool
}

// scrollSurfaces enumerates what the wheel and the thumb can act on this
// frame: the workspace leaf of every scrollable kind, plus any floater
// hosting one. Floater focus is "topmost under the cursor" — its own chrome
// already shields whatever is beneath it.
func (g *Game) scrollSurfaces() []scrollSurface {
	out := g.UI.ScrollSurf[:0]
	// Frame.Focused only knows the workspace tree, so a leaf under a floater
	// still reads as focused — the floater has to veto it explicitly.
	top := g.UI.Floating.HitTest(g.Frame.Cursor)
	for _, id := range scrollablePanels {
		if g.UI.PanelMgr.LeafFor(id) == nil {
			continue
		}
		if s := g.UI.PanelMgr.ScrollByID(id); s != nil {
			out = append(out, scrollSurface{
				key:     string(id),
				panel:   g.UI.PanelMgr.Get(id),
				state:   s,
				focused: g.Frame.Focused == id && top == nil,
			})
		}
	}
	for _, p := range g.UI.Floating.Panels {
		if !isScrollableKind(p.PanelID) {
			continue
		}
		out = append(out, scrollSurface{
			key:     p.ID,
			panel:   p.AsPanel(),
			state:   &p.Scroll,
			focused: p == top,
		})
	}
	g.UI.ScrollSurf = out
	return out
}

func isScrollableKind(id ui.PanelID) bool {
	for _, s := range scrollablePanels {
		if s == id {
			return true
		}
	}
	return false
}

func (g *Game) handlePanelScroll() {
	// Release unconditionally: the surface being dragged can vanish mid-drag
	// (floater closed), and a stale key would wedge every LMB behind it.
	if g.UI.ScrollDragKey != "" && !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
		g.UI.ScrollDragKey = ""
	}
	// chromeBusy folds in "cursor is over a floater", which would veto the
	// very surface we are trying to scroll — only live drags block here.
	busy := g.chromeDragging()
	for _, sf := range g.scrollSurfaces() {
		if sf.focused && !busy {
			if wheel := rl.GetMouseWheelMove(); wheel != 0 {
				sf.state.OffsetY -= wheel * wheelScrollSpeed
				ui.ClampScrollOffset(sf.panel, sf.state)
			}
		}

		thumb := ui.ScrollbarThumbRect(sf.panel, sf.state)
		if !busy && !g.scrollDragging() && sf.focused &&
			thumb.Width > 0 && thumb.Height > 0 &&
			rl.IsMouseButtonPressed(rl.MouseButtonLeft) &&
			rl.CheckCollisionPointRec(g.Frame.Cursor, thumb) {
			g.UI.ScrollDragKey = sf.key
			g.UI.ScrollDragStartY = g.Frame.Cursor.Y
			g.UI.ScrollDragStartO = sf.state.OffsetY
		}
		if g.UI.ScrollDragKey != sf.key {
			continue
		}
		track := ui.ScrollbarRect(sf.panel)
		maxOffset := sf.state.ContentHeight - track.Height
		scrollableTrack := track.Height - thumb.Height
		if scrollableTrack > 0 && maxOffset > 0 {
			dy := g.Frame.Cursor.Y - g.UI.ScrollDragStartY
			sf.state.OffsetY = g.UI.ScrollDragStartO + dy*(maxOffset/scrollableTrack)
			ui.ClampScrollOffset(sf.panel, sf.state)
		}
	}
}

// timelineDragKind names what a timeline drag is moving; the surface it acts
// on is TimelineDragKey.
type timelineDragKind uint8

const (
	timelineDragNone timelineDragKind = iota
	timelineDragLabel
	timelineDragH
	timelineDragV
)

// timelineSurface is one instance of the timeline widget on screen. Like
// scrollSurface, state is per-surface: a leaf and a floater showing the same
// panel have different widths, so they cannot share a time offset.
type timelineSurface struct {
	key     string
	panel   ui.Panel
	view    *ui.TimelineViewState
	focused bool
}

// timelineView lazily creates a surface's view. A zero TimelineViewState has
// PixelsPerSec 0, which is not a usable scale, so every surface starts from
// NewTimelineView.
func (g *Game) timelineView(key string) *ui.TimelineViewState {
	if g.UI.TimelineViews == nil {
		g.UI.TimelineViews = map[string]*ui.TimelineViewState{}
	}
	if v, ok := g.UI.TimelineViews[key]; ok {
		return v
	}
	v := ui.NewTimelineView()
	g.UI.TimelineViews[key] = &v
	return &v
}

// timelineLeafView is the workspace leaf's view — the one that persists.
func (g *Game) timelineLeafView() *ui.TimelineViewState {
	return g.timelineView(string(ui.PanelTimeline))
}

// timelineSurfaces enumerates every timeline on screen this frame. Focus
// mirrors scrollSurfaces: a leaf under a floater is vetoed explicitly, since
// Frame.Focused only knows the workspace tree.
func (g *Game) timelineSurfaces() []timelineSurface {
	out := g.UI.TimelineSurf[:0]
	top := g.UI.Floating.HitTest(g.Frame.Cursor)
	if g.UI.PanelMgr.LeafFor(ui.PanelTimeline) != nil {
		out = append(out, timelineSurface{
			key:     string(ui.PanelTimeline),
			panel:   g.UI.PanelMgr.Get(ui.PanelTimeline),
			view:    g.timelineLeafView(),
			focused: g.Frame.Focused == ui.PanelTimeline && top == nil,
		})
	}
	for _, p := range g.UI.Floating.Panels {
		if p.PanelID != ui.PanelTimeline {
			continue
		}
		out = append(out, timelineSurface{
			key:     p.ID,
			panel:   p.AsPanel(),
			view:    g.timelineView(p.ID),
			focused: p == top,
		})
	}
	g.UI.TimelineSurf = out
	return out
}

// squadBarCtx bundles what the bar reads; the Inspector's handles cover it.
func (g *Game) squadBarCtx() ui.SquadBarCtx {
	return ui.SquadBarCtx{
		InspectorMaps: g.Ctx.Inspector,
		World:         g.App.World,
		Selected:      g.Sel.Units,
		Hovered:       g.Sel.Hovered,
		Font:          g.hudFont,
		Cursor:        g.Frame.Cursor,
		Shift:         g.Frame.Shift,
		Now:           g.simNow(),
		SquadColor:    g.squadColor,
	}
}

// layoutSquadBar runs before input: the strip has to exist as a rect while
// handleInput decides whether a click belongs to the world or to the bar.
// Hidden entirely when nothing is selected — an empty strip would just eat
// terrain clicks along the bottom edge of the view.
func (g *Game) layoutSquadBar() {
	g.Frame.SquadBar = ui.BarLayout{}
	if g.headless || len(g.Sel.Units) == 0 {
		return
	}
	content := ui.ContentRect(g.UI.PanelMgr.Get(ui.Panel3D))
	if content.Width <= 0 || content.Height <= ui.BarHeight {
		return
	}
	area := rl.Rectangle{
		X: content.X, Y: content.Y + content.Height - ui.BarHeight,
		Width: content.Width, Height: ui.BarHeight,
	}
	g.Frame.SquadBar = ui.ComputeSquadBarLayout(g.UI.SquadBar.Sync(g.squadBarCtx()), area)
}

// squadBarOwnsCursor vetoes world input over the placed cards only — the
// empty tail of the strip still belongs to the terrain.
func (g *Game) squadBarOwnsCursor() bool {
	used := g.Frame.SquadBar.Used
	return len(g.Frame.SquadBar.Cards) > 0 &&
		rl.CheckCollisionPointRec(g.Frame.Cursor, used)
}

// behaviorCtx builds the per-frame context both flavours of the Behavior
// panel share (workspace leaf and floater differ only in focus and scroll).
func (g *Game) behaviorCtx(font rl.Font, cursor rl.Vector2, lmbPress, focused bool,
	scroll *ui.ScrollState) ui.BehaviorCtx {
	return ui.BehaviorCtx{
		BehaviorMaps: g.Ctx.Behavior,
		World:        g.App.World,
		Selected:     g.Sel.Units,
		Font:         font,
		Cursor:       cursor,
		LMBPressed:   lmbPress,
		PanelFocused: focused,
		Scroll:       scroll,
	}
}

// simNow is the canonical UI clock: sim seconds, the same one systems stamp
// into components (Contact.LastSeenTime, OrderIssuedAt).
func (g *Game) simNow() float32 { return float32(g.App.Elapsed().Seconds()) }

// floatScroll is the scroll state of the floater being rendered. Valid only
// inside renderFloatingWidget — and keyed off the live floater rather than the
// "float:"+kind convention, which a chevron switch invalidates (the switch
// changes PanelID and keeps ID).
func (g *Game) floatScroll() *ui.ScrollState {
	if p := g.UI.Floating.Drawing(); p != nil {
		return &p.Scroll
	}
	return nil
}

// floatSurfaceKey identifies the floater being rendered for per-surface state.
func (g *Game) floatSurfaceKey(id ui.PanelID) string {
	if p := g.UI.Floating.Drawing(); p != nil {
		return p.ID
	}
	return "float:" + string(id)
}

// selectSquad replaces the selection with a squad's living members — what
// clicking a row in the inspector's squad list means.
func (g *Game) selectSquad(squad ecs.Entity) {
	if squad == (ecs.Entity{}) || !g.App.World.Alive(squad) {
		return
	}
	roster := g.Maps.Roster.Get(squad)
	if roster == nil {
		return
	}
	g.Sel.Units = g.Sel.Units[:0]
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !g.App.World.Alive(mem) {
			continue
		}
		if g.isControllable(mem) {
			g.Sel.Units = append(g.Sel.Units, mem)
		}
	}
}

func (g *Game) toggleSelected(e ecs.Entity) {
	if i := g.isSelected(e); i >= 0 {
		g.Sel.Units = append(g.Sel.Units[:i], g.Sel.Units[i+1:]...)
	} else {
		g.Sel.Units = append(g.Sel.Units, e)
	}
}

func (g *Game) squadCenter(world *ecs.World, roster *components.CommandRoster) (components.WorldPos, bool) {
	return systems.SquadCenter(world, roster, g.Maps.Pos)
}

func (g *Game) devSpawnAt(target components.WorldPos) {
	switch g.Dev.SpawnKind {
	case 1, 2:
		ent := g.unitFactory(target)
		if ent == (ecs.Entity{}) {
			return
		}
		g.Svc.Role.AssignRole(ent, components.RoleRifleman)
		if g.Dev.SpawnKind == 2 {
			if f := g.Maps.Faction.Get(ent); f != nil {
				f.ID = components.FactionEnemyRed
			}
			if c := g.Maps.Controller.Get(ent); c != nil {
				c.Owner = components.ControllerAI
			}
		}
	case 3, 4, 5, 6, 7:
		g.Svc.VehicleFactory.Spawn(target, components.VehicleKind(g.Dev.SpawnKind-3),
			components.FactionPlayer, components.ControllerLocal)
	}
}

func (g *Game) drawDebugWidget(panel ui.Panel, font rl.Font, cursorV rl.Vector2, lmb bool) {
	simLabel := fmt.Sprintf("tick %d  speed x%d", g.App.TickIndex(), int(g.App.TimeScale))
	if g.App.TimeScale == 0 {
		simLabel = fmt.Sprintf("tick %d  PAUSED", g.App.TickIndex())
	}
	spawnButtons := make([]ui.DebugButton, len(devSpawnLabels))
	for i, l := range devSpawnLabels {
		spawnButtons[i] = ui.DebugButton{Label: l, Armed: g.Dev.SpawnKind == i+1}
	}
	var dump []string
	if len(g.Sel.Units) > 0 {
		dump = devComponentDump(g.App.World, g.Sel.Units[0])
	}
	simIdx, spawnIdx := ui.DrawDebugPanel(panel, font, ui.DebugPanelCtx{
		Cursor:   cursorV,
		LMBPress: lmb,
		Toggles:  debugOverlayToggles(&debugOverlay),
		SimLabel: simLabel,
		SimButtons: []ui.DebugButton{
			{Label: "Step 1"}, {Label: "Step 10"}, {Label: "Step 60"},
		},
		SpawnLabel:   "Arm + LMB in 3D places the entity",
		SpawnButtons: spawnButtons,
		DumpLines:    dump,
		Footer:       "Overlay radius: 2 chunks around camera",
	})
	if simIdx >= 0 {
		g.App.TimeScale = 0 // stepping implies pause
		g.Dev.PendingSteps += []int{1, 10, 60}[simIdx]
	}
	if spawnIdx >= 0 {
		if k := spawnIdx + 1; g.Dev.SpawnKind == k {
			g.Dev.SpawnKind = 0
		} else {
			g.Dev.SpawnKind = k
		}
	}
}

// ContentToPanel synthesises a Panel whose ContentRect recovers `content`
// so each widget's chrome-aware draw code lands in the right place.
// Panel3D is intentionally a no-op: the scene RT is sized to the
// workspace leaf; detaching would need a second render texture.
func (g *Game) renderFloatingWidget(id ui.PanelID, content rl.Rectangle,
	cursor rl.Vector2, font rl.Font, lmbPress bool) bool {
	syn := func(title string) ui.Panel { return ui.ContentToPanel(content, title, id) }
	switch id {
	case ui.PanelFormation:
		g.UI.FormationEditor.DrawPanel(syn("Formation"), font, cursor, lmbPress)
	case ui.PanelSymbology:
		g.UI.SymbolEditor.DrawPanel(syn("Symbology"), font, cursor, lmbPress, true,
			g.floatScroll())
	case ui.PanelMap:
		ui.DrawMap(syn("Map"), ui.MapRenderCtx{
			World:            g.App.World,
			Cam:              g.UI.MapCam,
			Underlay:         &g.UI.Underlay,
			AnchorPos:        *g.Maps.Pos.Get(g.anchor),
			Selected:         g.Sel.Units,
			Hovered:          g.Sel.Hovered,
			PosMap:           g.Maps.Pos,
			RosterMap:        g.Maps.Roster,
			SquadMemberMap:   g.Maps.SquadMember,
			SquadFilter:      g.Filt.Squad,
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
			FactionMap:       g.Maps.Faction,
			SquadOverrideMap: g.Maps.SquadOverride,
			Font:             font,
			MapPingFilter:    g.Filt.MapPing,
			Clock:            g.Svc.Squad.Clock(),
			Clusters:         &g.Frame.MapClusters,
			LOSFanOrigin:     g.Ctx.LOS.origin,
			LOSFanRuns:       g.Ctx.LOS.fanRuns(),
			LOSFanRange:      g.Ctx.LOS.sensorR,
			LOSFanFalloff:    g.Ctx.LOS.falloff,
			LOSWeaponRs:      g.Ctx.LOS.weaponRs,
		})
	case ui.PanelInspect:
		ui.DrawInspector(syn("Inspector"), ui.InspectorCtx{
			InspectorMaps: g.Ctx.Inspector,
			Behavior:      g.Ctx.Behavior,
			World:         g.App.World,
			Selected:      g.Sel.Units,
			Hovered:       g.Sel.Hovered,
			Font:          font,
			EventLog:      g.Res.EventLog,
			Now:           g.simNow(),
			Cursor:        cursor,
			LMBPressed:    lmbPress,
			PanelFocused:  true,
			Scroll:        g.floatScroll(),
			SquadColor:    g.squadColor,
			RoadGraph:     &g.Res.RoadGraph,
		})
	case ui.PanelBehavior:
		ui.DrawBehaviorPanel(syn("Behavior"), g.behaviorCtx(font, cursor, lmbPress, true,
			g.floatScroll()))
	case ui.PanelTimeline:
		key := g.floatSurfaceKey(ui.PanelTimeline)
		ui.DrawTimelinePanel(syn("Timeline"), font, g.UI.TimelineData, g.timelineView(key),
			cursor, g.UI.TimelineDragKey == key && g.UI.TimelineDragKind == timelineDragLabel)
	case ui.PanelDebug:
		g.drawDebugWidget(syn("Debug"), font, cursor, lmbPress)
	case ui.Panel3D:
		// Not floatable; chevron menu disables Float pane for the 3D leaf.
	}
	return false
}

func (g *Game) makeFloatingRender(id ui.PanelID) ui.FloatingRenderFn {
	return func(c rl.Rectangle, cu rl.Vector2, f rl.Font, l bool) bool {
		return g.renderFloatingWidget(id, c, cu, f, l)
	}
}

func (g *Game) floatSpawn(id ui.PanelID, title string, bounds rl.Rectangle) {
	// Use the leaf's previous bounds so the floater materialises in place.
	if bounds.Width < 240 {
		bounds.Width = 360
	}
	if bounds.Height < 160 {
		bounds.Height = 280
	}
	g.UI.Floating.Open(&ui.FloatingPanel{
		ID:        "float:" + string(id),
		Title:     title,
		Bounds:    bounds,
		PanelID:   id,
		RenderFor: g.makeFloatingRender,
	})
}
