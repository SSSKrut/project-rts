package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// handleInput covers sim controls, WASD, per-panel input (map / inspector /
// top bar / timeline) and LMB selection.
func (g *Game) handleInput() {
	if rl.IsKeyPressed(rl.KeySpace) {
		if g.App.TimeScale > 0 {
			g.App.LastNonZeroScale = g.App.TimeScale
			g.App.TimeScale = 0
		} else {
			if g.App.LastNonZeroScale <= 0 {
				g.App.LastNonZeroScale = 1
			}
			g.App.TimeScale = g.App.LastNonZeroScale
		}
	}
	if rl.IsKeyPressed(rl.KeyEqual) || rl.IsKeyPressed(rl.KeyKpAdd) {
		g.App.TimeScale = nextTimeScale(g.App.TimeScale, +1)
		g.App.LastNonZeroScale = g.App.TimeScale
	}
	if rl.IsKeyPressed(rl.KeyMinus) || rl.IsKeyPressed(rl.KeyKpSubtract) {
		g.App.TimeScale = nextTimeScale(g.App.TimeScale, -1)
		g.App.LastNonZeroScale = g.App.TimeScale
	}
	if rl.IsKeyPressed(rl.KeyR) {
		debugOverlay.Roofs = !debugOverlay.Roofs
	}
	if rl.IsKeyPressed(rl.KeyN) {
		debugOverlay.TerrainDetail = !debugOverlay.TerrainDetail
	}

	g.Frame.AnchorPos = g.Maps.Pos.Get(g.anchor)
	g.Frame.AnchorSpeed = float32(20.0)
	if g.Frame.Shift {
		g.Frame.AnchorSpeed *= 4.0
	}
	orbit := g.Maps.Orbit.Get(g.camEnt)
	sy := float32(math.Sin(float64(orbit.Yaw)))
	cy := float32(math.Cos(float64(orbit.Yaw)))
	var inFwd, inRight float32
	wasdAllowed := g.Frame.Focused == ui.Panel3D || g.Frame.Focused == ui.PanelNone
	if wasdAllowed {
		if rl.IsKeyDown(rl.KeyW) {
			inFwd += 1
		}
		if rl.IsKeyDown(rl.KeyS) {
			inFwd -= 1
		}
		if rl.IsKeyDown(rl.KeyD) {
			inRight += 1
		}
		if rl.IsKeyDown(rl.KeyA) {
			inRight -= 1
		}
	}
	g.Frame.WASDActive = inFwd != 0 || inRight != 0
	if g.Frame.WASDActive {
		if mag := float32(math.Sqrt(float64(inFwd*inFwd + inRight*inRight))); mag > 1 {
			inFwd /= mag
			inRight /= mag
		}
		step := g.Frame.AnchorSpeed * float32(g.Frame.DtReal.Seconds())
		move := rl.Vector3{
			X: step * (inFwd*(-sy) + inRight*cy),
			Z: step * (inFwd*(-cy) + inRight*(-sy)),
		}
		*g.Frame.AnchorPos = g.Frame.AnchorPos.Add(move)
		g.Sel.NavPath = nil
	}

	// Map's content rect (drawing surface minus chrome). Cursor conversions
	// go through this so clicks / zoom pivots align with what's drawn.
	g.Frame.PanelMapContent = ui.ContentRect(g.Frame.PanelMap)

	if g.Frame.Focused == ui.PanelMap && !g.chromeBusy() {
		if rl.IsMouseButtonPressed(rl.MouseButtonMiddle) {
			g.UI.MapPanning = true
			g.UI.MapPanCursor = g.Frame.Cursor
		}
		if g.UI.MapPanning && rl.IsMouseButtonDown(rl.MouseButtonMiddle) {
			dx := g.Frame.Cursor.X - g.UI.MapPanCursor.X
			dy := g.Frame.Cursor.Y - g.UI.MapPanCursor.Y
			g.UI.MapCam.Pan(dx, dy)
			g.UI.MapPanCursor = g.Frame.Cursor
		}
		if rl.IsMouseButtonReleased(rl.MouseButtonMiddle) {
			g.UI.MapPanning = false
		}
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			factor := float32(math.Pow(1.15, float64(wheel)))
			g.UI.MapCam.ZoomAt(g.Frame.Cursor, g.Frame.PanelMapContent, factor)
		}
	} else {
		g.UI.MapPanning = false
	}

	g.handlePanelScroll()

	if g.Frame.Focused == ui.PanelTopBar && !g.chromeBusy() && !g.scrollDragging() &&
		rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		switch ui.TopBarHitTest(g.Frame.Cursor, g.UI.TopBarHits) {
		case ui.TopBarHitPlayPause:
			if g.App.TimeScale > 0 {
				g.App.LastNonZeroScale = g.App.TimeScale
				g.App.TimeScale = 0
			} else {
				if g.App.LastNonZeroScale <= 0 {
					g.App.LastNonZeroScale = 1
				}
				g.App.TimeScale = g.App.LastNonZeroScale
			}
		case ui.TopBarHitSpeedDown:
			g.App.TimeScale = nextTimeScale(g.App.TimeScale, -1)
			g.App.LastNonZeroScale = g.App.TimeScale
		case ui.TopBarHitSpeedUp:
			g.App.TimeScale = nextTimeScale(g.App.TimeScale, +1)
			g.App.LastNonZeroScale = g.App.TimeScale
		case ui.TopBarHitToolSE:
			g.floatSpawn(ui.PanelSymbology, "Symbology",
				rl.Rectangle{X: 80, Y: 80, Width: 400, Height: 360})
		case ui.TopBarHitToolFE:
			_, homo := groupSelected(g.Sel.Units, g.Maps.SquadMember)
			if homo {
				g.floatSpawn(ui.PanelFormation, "Formation",
					rl.Rectangle{X: 80, Y: 80, Width: 380, Height: 360})
			}
		case ui.TopBarHitToolSettings:
			// Phase 18.5.E placeholder — Settings panel deferred.
		}
	}

	g.handleTimelineInput()

	// 3D panel cursor (content-rect-local). viewW/H match the content
	// rect (= RT size) so screen<->world projections match what's drawn.
	g.Frame.Panel3DContent = ui.ContentRect(g.Frame.Panel3D)
	g.Frame.Panel3DLocal = rl.Vector2{
		X: g.Frame.Cursor.X - g.Frame.Panel3DContent.X,
		Y: g.Frame.Cursor.Y - g.Frame.Panel3DContent.Y,
	}
	g.Frame.Panel3DW = int32(g.Frame.Panel3DContent.Width)
	g.Frame.Panel3DH = int32(g.Frame.Panel3DContent.Height)
	if g.Frame.Panel3DW < 1 {
		g.Frame.Panel3DW = 1
	}
	if g.Frame.Panel3DH < 1 {
		g.Frame.Panel3DH = 1
	}

	// LMB press. Splitter drag claims LMB exclusively. Building widget
	// chips also claim the press — skip marquee start when over a chip.
	widgetClickConsumed := false
	if !g.chromeBusy() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && g.UI.BuildingWidget != nil {
		if hit := ui.HitTestBuildingWidget(g.UI.BuildingWidget, g.Frame.Cursor); hit != nil {
			target := g.UI.BuildingWidget.Root
			if bvm := g.Maps.BuildingViewMode.Get(target); bvm != nil {
				levels := g.Res.BuildingPlanIx.Levels[target]
				switch hit.Kind {
				case ui.ChipKindLevel:
					if hit.Index >= 0 && hit.Index < len(levels) {
						bvm.CurrentLevel = levels[hit.Index]
					}
				case ui.ChipKindWallMode:
					bvm.WallMode = components.WallRenderMode(hit.Index)
				case ui.ChipKindInside:
					bvm.InteriorOpen = !bvm.InteriorOpen
				}
				// Pin the widget so it doesn't vanish on a follow-up click.
				g.Sel.Building = target
				widgetClickConsumed = true
			}
		}
	}
	if !g.chromeBusy() && !widgetClickConsumed && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		switch g.Frame.Focused {
		case ui.Panel3D:
			if g.Dev.SpawnKind != 0 {
				if target, ok := mouseTargetWorldPos(systems.CurrentCamera,
					g.Frame.AnchorPos.ToRenderSpace(systems.CurrentOriginChunk),
					g.Frame.Panel3DLocal, g.Frame.Panel3DW, g.Frame.Panel3DH); ok {
					g.devSpawnAt(target)
				}
				break // armed palette consumes the click — no marquee
			}
			g.UI.MarqueeStart = g.Frame.Cursor
			g.UI.MarqueeActive = true
			g.UI.MarqueeOrigin = ui.Panel3D
		case ui.PanelMap:
			mapCtx := g.mapPickCtx()
			now := g.Svc.Squad.Clock()
			const doubleClickWindow float32 = 0.35
			if cHit := ui.PickContactAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 14); cHit != (ecs.Entity{}) && g.App.World.Alive(cHit) {
				// Second consecutive click on the same contact within window → camera focus.
				if cHit == g.Sel.LastMapClickEnt && now-g.Sel.LastMapClickTime <= doubleClickWindow {
					if c := g.Maps.Contact.Get(cHit); c != nil {
						*g.Maps.Pos.Get(g.anchor) = c.EstimatedPos
					}
				} else {
					if g.Frame.Shift {
						if g.isSelected(cHit) < 0 {
							g.Sel.Units = append(g.Sel.Units, cHit)
						}
					} else {
						g.Sel.Units = append(g.Sel.Units[:0], cHit)
					}
				}
				g.Sel.LastMapClickEnt = cHit
				g.Sel.LastMapClickTime = now
			} else if own := ui.PickOwnEntityAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 12); own != (ecs.Entity{}) && g.App.World.Alive(own) && g.isControllable(own) {
				// Second click on the same marker centres the camera — the
				// gesture the contact layer already uses.
				if own == g.Sel.LastMapClickEnt && now-g.Sel.LastMapClickTime <= doubleClickWindow {
					if p := g.Maps.Pos.Get(own); p != nil {
						*g.Maps.Pos.Get(g.anchor) = *p
					}
				} else if g.Frame.Shift {
					g.toggleSelected(own)
				} else {
					g.Sel.Units = append(g.Sel.Units[:0], own)
				}
				g.Sel.LastMapClickEnt = own
				g.Sel.LastMapClickTime = now
			} else if hit := ui.PickSquadAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 12); hit != (ecs.Entity{}) && g.App.World.Alive(hit) && g.isControllable(hit) {
				if r := g.Maps.Roster.Get(hit); r != nil {
					if g.Frame.Shift {
						for i := uint8(0); i < r.Count; i++ {
							if g.isSelected(r.Members[i]) < 0 {
								g.Sel.Units = append(g.Sel.Units, r.Members[i])
							}
						}
					} else {
						g.Sel.Units = append(g.Sel.Units[:0], r.Members[:r.Count]...)
					}
				}
				g.Sel.LastMapClickEnt = ecs.Entity{}
			} else if !g.Frame.Shift {
				g.Sel.Units = nil
				g.Sel.LastMapClickEnt = ecs.Entity{}
			}
		}
	}

	if rl.IsMouseButtonReleased(rl.MouseButtonLeft) && g.UI.MarqueeActive {
		end := g.Frame.Cursor
		dx := end.X - g.UI.MarqueeStart.X
		dy := end.Y - g.UI.MarqueeStart.Y
		dragDist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if g.UI.MarqueeOrigin == ui.Panel3D {
			if dragDist < marqueeClickThreshold {
				localEnd := rl.Vector2{X: end.X - g.Frame.Panel3DContent.X, Y: end.Y - g.Frame.Panel3DContent.Y}
				if hit, ok := pickUnitFromMouse(g.Filt.UnitRender, g.Filt.VehicleRender, *g.Frame.AnchorPos, localEnd, g.Frame.Panel3DW, g.Frame.Panel3DH); ok && g.isControllable(hit) {
					if g.Frame.Shift {
						g.toggleSelected(hit)
					} else {
						g.Sel.Units = []ecs.Entity{hit}
					}
					g.Sel.Building = ecs.Entity{}
				} else if g.Sel.HoveredBuilding != (ecs.Entity{}) {
					// Empty 3D click on a footprint pins the widget.
					g.Sel.Building = g.Sel.HoveredBuilding
					if !g.Frame.Shift {
						g.Sel.Units = nil
					}
				} else if !g.Frame.Shift {
					g.Sel.Units = nil
					g.Sel.Building = ecs.Entity{}
				}
			} else {
				localStart := rl.Vector2{X: g.UI.MarqueeStart.X - g.Frame.Panel3DContent.X, Y: g.UI.MarqueeStart.Y - g.Frame.Panel3DContent.Y}
				localEnd := rl.Vector2{X: end.X - g.Frame.Panel3DContent.X, Y: end.Y - g.Frame.Panel3DContent.Y}
				minX, maxX := localStart.X, localEnd.X
				if maxX < minX {
					minX, maxX = maxX, minX
				}
				minY, maxY := localStart.Y, localEnd.Y
				if maxY < minY {
					minY, maxY = maxY, minY
				}
				hits := collectUnitsInRect(g.Filt.UnitRender, g.Filt.VehicleRender, minX, maxX, minY, maxY, g.Frame.Panel3DW, g.Frame.Panel3DH)
				own := hits[:0]
				for _, h := range hits {
					if g.isControllable(h) {
						own = append(own, h)
					}
				}
				hits = own
				if g.Frame.Shift {
					for _, h := range hits {
						if g.isSelected(h) < 0 {
							g.Sel.Units = append(g.Sel.Units, h)
						}
					}
				} else {
					g.Sel.Units = hits
				}
			}
		}
		g.UI.MarqueeActive = false
	}
}

// handleTimelineInput drives every timeline on screen — the workspace leaf and
// any floater. A live drag is serviced first and outside the focus gate, so a
// divider or slider keeps following the cursor once it leaves the panel.
func (g *Game) handleTimelineInput() {
	g.UI.TimelineHoverOK = false
	surfaces := g.timelineSurfaces()

	if g.UI.TimelineDragKind != timelineDragNone && !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
		g.UI.TimelineDragKind, g.UI.TimelineDragKey = timelineDragNone, ""
	}
	for _, sf := range surfaces {
		if g.UI.TimelineDragKey == sf.key {
			g.dragTimeline(sf)
		}
	}
	// chromeBusy folds in "cursor is over a floater", which would veto the very
	// floater we are driving; only live chrome drags block content input.
	if g.chromeDragging() || g.scrollDragging() {
		return
	}
	for _, sf := range surfaces {
		if sf.focused {
			g.handleTimelineSurface(sf)
		}
	}
}

// dragTimeline advances whichever drag this surface owns.
func (g *Game) dragTimeline(sf timelineSurface) {
	switch g.UI.TimelineDragKind {
	case timelineDragLabel:
		sf.view.LabelW = ui.ClampTimelineLabelW(sf.panel,
			g.Frame.Cursor.X-ui.ContentRect(sf.panel).X)
		rl.SetMouseCursor(rl.MouseCursorResizeEW)
	case timelineDragH:
		ui.SetTimelineOffsetFromThumb(sf.panel, sf.view, g.UI.TimelineData,
			g.Frame.Cursor.X-g.UI.TimelineDragOff)
	case timelineDragV:
		ui.SetTimelineScrollFromThumb(sf.panel, sf.view, g.UI.TimelineData,
			g.Frame.Cursor.Y-g.UI.TimelineDragOff)
	}
}

func (g *Game) handleTimelineSurface(sf timelineSurface) {
	press := rl.IsMouseButtonPressed(rl.MouseButtonLeft)
	dragging := g.UI.TimelineDragKind != timelineDragNone

	// Sliders and the divider claim the press before rows do, or grabbing one
	// would also select the squad underneath.
	if press && !dragging {
		hThumb := ui.TimelineHScrollThumb(sf.panel, *sf.view, g.UI.TimelineData)
		vThumb := ui.TimelineVScrollThumb(sf.panel, *sf.view, g.UI.TimelineData)
		switch {
		case hThumb.Width > 0 && rl.CheckCollisionPointRec(g.Frame.Cursor, hThumb):
			g.beginTimelineDrag(sf.key, timelineDragH, g.Frame.Cursor.X-hThumb.X)
		case vThumb.Height > 0 && rl.CheckCollisionPointRec(g.Frame.Cursor, vThumb):
			g.beginTimelineDrag(sf.key, timelineDragV, g.Frame.Cursor.Y-vThumb.Y)
		}
		dragging = g.UI.TimelineDragKind != timelineDragNone
	}
	if rl.CheckCollisionPointRec(g.Frame.Cursor, ui.TimelineDividerRect(sf.panel, *sf.view)) {
		rl.SetMouseCursor(rl.MouseCursorResizeEW)
		if press && !dragging {
			g.beginTimelineDrag(sf.key, timelineDragLabel, 0)
			dragging = true
		}
	}

	g.UI.TimelineHoverHit, g.UI.TimelineHoverOK =
		ui.TimelineHitTest(sf.panel, g.UI.TimelineData, *sf.view, g.Frame.Cursor)
	if blk, ok := g.UI.TimelineData.Block(g.UI.TimelineHoverHit); ok {
		g.UI.TimelineHoverBlk = blk
	} else {
		g.UI.TimelineHoverHit.HitOrder = false
	}

	if wheel := rl.GetMouseWheelMove(); wheel != 0 {
		g.wheelTimeline(sf, wheel)
	}
	if press && !dragging {
		g.clickTimeline()
	}
	// RMB on background -> re-enable Follow.
	if rl.IsMouseButtonPressed(rl.MouseButtonRight) && g.UI.TimelineHoverOK &&
		!g.UI.TimelineHoverHit.HitOrder {
		sf.view.Follow = true
	}
}

func (g *Game) beginTimelineDrag(key string, kind timelineDragKind, offset float32) {
	g.UI.TimelineDragKey = key
	g.UI.TimelineDragKind = kind
	g.UI.TimelineDragOff = offset
}

// wheelTimeline: over the squad list the wheel scrolls rows, over the tracks
// it pans time (Shift zooms).
func (g *Game) wheelTimeline(sf timelineSurface, wheel float32) {
	if ui.TimelineOverGutter(sf.panel, *sf.view, g.Frame.Cursor) {
		ui.ScrollTimelineRows(sf.panel, sf.view, g.UI.TimelineData, -wheel*timelineWheelRows)
		return
	}
	if g.Frame.Shift {
		next := sf.view.PixelsPerSec * float32(math.Pow(1.15, float64(wheel)))
		if next < ui.TimelineMinPxPerSec {
			next = ui.TimelineMinPxPerSec
		}
		if next > ui.TimelineMaxPxPerSec {
			next = ui.TimelineMaxPxPerSec
		}
		sf.view.PixelsPerSec = next
	} else {
		sf.view.OffsetT -= wheel * wheelScrollSpeed / sf.view.PixelsPerSec
	}
	sf.view.Follow = false
	// Without the clamp the wheel walks the view off the end of the mission
	// and only RMB-Follow brings it back.
	ui.ClampTimelineOffset(sf.panel, sf.view, g.UI.TimelineData)
}

// clickTimeline selects the squad under the cursor and, on a block, moves the
// anchor to that order's target. The block carries its own target, so a
// finished order — whose entity is long gone — is just as clickable.
func (g *Game) clickTimeline() {
	hit := g.UI.TimelineHoverHit
	if !g.UI.TimelineHoverOK || hit.Squad == (ecs.Entity{}) ||
		!g.App.World.Alive(hit.Squad) || !g.isControllable(hit.Squad) {
		return
	}
	g.selectSquad(hit.Squad)
	g.Sel.NavPath = nil
	if blk, ok := g.UI.TimelineData.Block(hit); ok {
		*g.Maps.Pos.Get(g.anchor) = blk.Target
	}
}

// nextTimeScale cycles 1 -> 2 -> 4 -> 8 -> 1 (step=+1) / reverse (step=-1).
// Paused → step=+1 jumps to 1x, step=-1 to 8x.
func nextTimeScale(cur float32, step int) float32 {
	stops := [...]float32{1, 2, 4, 8}
	if cur <= 0 {
		if step > 0 {
			return stops[0]
		}
		return stops[len(stops)-1]
	}
	idx := 0
	for i, v := range stops {
		if v == cur {
			idx = i
			break
		}
	}
	idx = (idx + step + len(stops)) % len(stops)
	return stops[idx]
}
