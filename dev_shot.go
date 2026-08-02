package main

import (
	"flag"
	"math"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"

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
)

// applyShotSelection preselects units for a capture — inspector panels are
// mostly empty without a selection, which is the half worth looking at.
func (g *Game) applyShotSelection() {
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
	if *shotSelectFlag <= 0 {
		return
	}
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

// maybeScreenshot runs inside BeginDrawing so it reads the composed frame,
// and ends the run once the capture is on disk.
func (g *Game) maybeScreenshot() {
	if *shotPathFlag == "" || int(g.App.FrameIndex()) < *shotAtFlag {
		return
	}
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
