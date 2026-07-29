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

	g.UI.TimelineHoverOK = false
	if g.Frame.Focused == ui.PanelTimeline && !g.chromeBusy() {
		panelTL := g.UI.PanelMgr.Get(ui.PanelTimeline)
		g.UI.TimelineHoverHit, g.UI.TimelineHoverOK = ui.TimelineHitTest(panelTL, g.UI.TimelineData, g.UI.TimelineView, g.Frame.Cursor)
		if g.UI.TimelineHoverHit.HitOrder {
			for _, r := range g.UI.TimelineData.Rows {
				if r.Squad != g.UI.TimelineHoverHit.Squad {
					continue
				}
				for _, ob := range r.Orders {
					if ob.Order == g.UI.TimelineHoverHit.Order {
						g.UI.TimelineHoverBlk = ob
						break
					}
				}
			}
		}
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			if g.Frame.Shift {
				factor := float32(math.Pow(1.15, float64(wheel)))
				next := g.UI.TimelineView.PixelsPerSec * factor
				if next < ui.TimelineMinPxPerSec {
					next = ui.TimelineMinPxPerSec
				}
				if next > ui.TimelineMaxPxPerSec {
					next = ui.TimelineMaxPxPerSec
				}
				g.UI.TimelineView.PixelsPerSec = next
				g.UI.TimelineView.Follow = false
			} else {
				g.UI.TimelineView.OffsetT -= wheel * 30 / g.UI.TimelineView.PixelsPerSec
				g.UI.TimelineView.Follow = false
			}
		}
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !g.scrollDragging() {
			if g.UI.TimelineHoverOK && g.UI.TimelineHoverHit.Squad != (ecs.Entity{}) &&
				g.App.World.Alive(g.UI.TimelineHoverHit.Squad) && g.isControllable(g.UI.TimelineHoverHit.Squad) {
				if r := g.Maps.Roster.Get(g.UI.TimelineHoverHit.Squad); r != nil {
					g.Sel.Units = g.Sel.Units[:0]
					for i := uint8(0); i < r.Count; i++ {
						if m := r.Members[i]; m != (ecs.Entity{}) && g.App.World.Alive(m) {
							g.Sel.Units = append(g.Sel.Units, m)
						}
					}
					g.Sel.NavPath = nil
				}
				if g.UI.TimelineHoverHit.HitOrder && g.App.World.Alive(g.UI.TimelineHoverHit.Order) {
					if t := g.Maps.OrderTarget.Get(g.UI.TimelineHoverHit.Order); t != nil {
						*g.Maps.Pos.Get(g.anchor) = t.Pos
					}
				}
			}
		}
		// RMB on background -> re-enable Follow.
		if rl.IsMouseButtonPressed(rl.MouseButtonRight) && g.UI.TimelineHoverOK && !g.UI.TimelineHoverHit.HitOrder {
			g.UI.TimelineView.Follow = true
		}
	}

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
			mapCtx := ui.MapRenderCtx{
				World: g.App.World, Cam: g.UI.MapCam, SquadFilter: g.Filt.Squad,
				SquadCenter:    g.squadCenter,
				MapMarkerCache: &g.Res.MapMarkerCache,
				ContactFilter:  g.Filt.Contact,
				ContactMap:     g.Maps.Contact,
			}
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
