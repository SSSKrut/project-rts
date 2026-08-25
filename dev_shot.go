package main

import (
	"flag"
	"math"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// Frame capture for eyeballing render changes without a live session. The
// PNG path is resolved by raylib relative to the working directory.
var (
	shotPathFlag   = flag.String("shot", "", "dev: write a PNG of frame -shot-at, then exit")
	shotAtFlag     = flag.Int("shot-at", 120, "dev: frame index for -shot")
	shotCamFlag    = flag.String("shot-cam", "", "dev: starting orbit as radius,pitchDeg,yawDeg")
	shotSelectFlag = flag.Int("shot-select", 0, "dev: preselect N units so panels render populated")
	shotScrollFlag = flag.Float64("shot-scroll", 0, "dev: scroll offset applied to every scrollable panel for -shot")
	shotPanelFlag  = flag.String("shot-panel", "", "dev: show this widget kind in the Inspector leaf for -shot (e.g. behavior)")
	shotVehFlag    = flag.Int("shot-select-veh", 0, "dev: also preselect N vehicles (squad bar / vehicle panels)")
	shotAirFlag    = flag.Int("shot-select-air", 0, "dev: also preselect N aircraft (airframe dial panel)")
	shotOrderFlag  = flag.String("shot-order", "", "dev: issue a MoveTo chain to the preselected soloists as x,z[;x,z...] — the order surfaces are empty without one")
	shotLiftFlag   = flag.Float64("shot-lift", 0, "dev: starting camera ViewLift in metres for -shot")
)

// applyShotSelection preselects units for a capture — inspector panels are
// mostly empty without a selection, which is the half worth looking at.
//
// Called every frame of a shot run, not once at boot: an entity that arrives
// on a schedule does not exist yet when initUI runs, and an airframe released
// at t=2 was silently unselectable and therefore uncapturable. Idempotent —
// the selection is rebuilt from scratch, since nothing else owns it here.
func (g *Game) applyShotSelection() {
	g.Sel.Units = g.Sel.Units[:0]
	// A widget that isn't in the default layout can't be captured otherwise.
	// The swap is never persisted: shutdownUI skips saveLayout during a shot.
	if *shotPanelFlag != "" {
		if leaf := g.UI.PanelMgr.LeafFor(ui.PanelInspect); leaf != nil {
			id := ui.PanelID(*shotPanelFlag)
			leaf.Panel = id
			leaf.Title = ui.WidgetTitle(id)
		}
	}
	if *shotScrollFlag != 0 {
		for _, id := range scrollablePanels {
			g.UI.PanelMgr.ScrollByID(id).OffsetY = float32(*shotScrollFlag)
		}
	}
	if *shotSelectFlag > 0 {
		q := g.Filt.UnitHit.Query()
		for q.Next() {
			if len(g.Sel.Units) >= *shotSelectFlag {
				q.Close()
				break
			}
			ent := q.Entity()
			if g.isControllable(ent) {
				g.Sel.Units = append(g.Sel.Units, ent)
			}
		}
	}
	if *shotVehFlag > 0 {
		picked := 0
		qv := g.Filt.VehicleRender.Query()
		for qv.Next() {
			if picked >= *shotVehFlag {
				qv.Close()
				break
			}
			ent := qv.Entity()
			if g.isControllable(ent) {
				g.Sel.Units = append(g.Sel.Units, ent)
				picked++
			}
		}
	}
	if *shotAirFlag > 0 {
		picked := 0
		qa := g.Filt.AircraftRender.Query()
		for qa.Next() {
			if picked >= *shotAirFlag {
				qa.Close()
				break
			}
			ent := qa.Entity()
			if g.isControllable(ent) {
				g.Sel.Units = append(g.Sel.Units, ent)
				picked++
			}
		}
	}
}

// maybeScreenshot runs inside BeginDrawing so it reads the composed frame,
// and ends the run once the capture is on disk.
func (g *Game) maybeScreenshot() {
	if *shotPathFlag == "" || int(g.App.FrameIndex()) < *shotAtFlag {
		return
	}
	// TakeScreenshot reads the framebuffer, but raylib batches draw calls:
	// anything issued since the last flush is still in the batch and would be
	// missing from the capture, while the stale pixels underneath (identical
	// for static chrome) make the shot look complete. A scissor pair is the
	// cheapest flush raylib-go exposes — without it, whatever the last widget
	// drew is invisible to every capture. Cost a squad-bar debugging session.
	rl.BeginScissorMode(0, 0, int32(rl.GetScreenWidth()), int32(rl.GetScreenHeight()))
	rl.EndScissorMode()
	rl.TakeScreenshot(*shotPathFlag)
	g.shotDone = true
}

// shotCam* override the starting orbit so a capture can frame what it needs.
// An absent or malformed flag keeps the gameplay defaults.
func shotCamRadius(def float32) float32 { return shotCamField(0, def) }
func shotCamPitch(def float32) float32  { return shotCamField(1, def) }
func shotCamYaw(def float32) float32    { return shotCamField(2, def) }

func shotCamField(i int, def float32) float32 {
	parts := strings.Split(*shotCamFlag, ",")
	if len(parts) != 3 {
		return def
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 32)
	if err != nil {
		return def
	}
	if i == 0 {
		return float32(v)
	}
	return float32(v) * math.Pi / 180
}

// applyShotOrders gives the preselected soloists a route so the map markers and
// the timeline have something to draw. Capture-only: the interactive path is
// RMB, and a screenshot cannot click.
func (g *Game) applyShotOrders() {
	// Latch only once there is something to order. An airframe arrives on a
	// schedule and does not exist on frame 0, so a flag set before the
	// selection exists means the route is never issued at all.
	if *shotOrderFlag == "" || g.Sel.OrderShot || len(g.Sel.Units) == 0 {
		return
	}
	g.Sel.OrderShot = true
	for _, leg := range strings.Split(*shotOrderFlag, ";") {
		xz := strings.Split(leg, ",")
		if len(xz) != 2 {
			continue
		}
		x, _ := strconv.ParseFloat(strings.TrimSpace(xz[0]), 32)
		z, _ := strconv.ParseFloat(strings.TrimSpace(xz[1]), 32)
		target := components.WorldPos{}.Add(rl.Vector3{
			X: float32(x), Y: systems.GroundHeight(float32(x), float32(z)), Z: float32(z),
		})
		for _, e := range g.Sel.Units {
			g.Svc.Squad.IssueOrder(e, components.OrderKindMoveTo, target,
				ecs.Entity{}, g.Sel.OrderShotLegs > 0, systems.OrderParams{})
		}
		g.Sel.OrderShotLegs++
	}
}
