package main

import (
	"flag"
	"math"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Frame capture for eyeballing render changes without a live session. The
// PNG path is resolved by raylib relative to the working directory.
var (
	shotPathFlag = flag.String("shot", "", "dev: write a PNG of frame -shot-at, then exit")
	shotAtFlag   = flag.Int("shot-at", 120, "dev: frame index for -shot")
	shotCamFlag  = flag.String("shot-cam", "", "dev: starting orbit as radius,pitchDeg,yawDeg")
)

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
