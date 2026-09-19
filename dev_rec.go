package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Clip capture: the -shot machinery stretched over a frame range. A path
// ending in "/" writes one PNG per recorded frame; anything else receives raw
// RGBA frames back to back, which scripts/record_clip.sh feeds to ffmpeg
// through a fifo. Everything is frame-indexed, never wall-clock: a slow
// capture slows the loop, not the clip, and two runs of a scene record the
// same frames.
var (
	recPathFlag   = flag.String("rec", "", "dev: record frames to this path (dir/ = PNGs, else raw RGBA stream)")
	recFromFlag   = flag.Int("rec-from", 0, "dev: first frame to record")
	recToFlag     = flag.Int("rec-to", 600, "dev: last frame to record, then exit")
	recEveryFlag  = flag.Int("rec-every", 2, "dev: record every Nth frame")
	recSpeedFlag  = flag.Float64("rec-speed", 1, "dev: sim TimeScale for a -rec run")
	recSizeFlag   = flag.String("rec-size", "", "dev: window size WxH")
	recCleanFlag  = flag.Bool("rec-clean", false, "dev: chromeless — the field fills the window, no panels")
	recBareFlag   = flag.Bool("rec-bare", false, "dev: with -rec-clean, drop the unit labels and bars too")
	recOrbitFlag  = flag.Float64("rec-orbit", 0, "dev: camera yaw drift, degrees per second of clip")
	recDollyFlag  = flag.String("rec-dolly", "", "dev: orbit radius r0,r1 eased across the recorded range")
	recTrackFlag  = flag.String("rec-track", "", "dev: anchor follows the centroid of: sel | player | enemy | all")
	recSnapFlag   = flag.Float64("rec-track-snap", 0, "dev: with -rec-track, cut to the centroid only once it drifts this many metres (0 = glide)")
	recAnchorFlag = flag.String("rec-anchor", "", "dev: park the anchor at x,z (ignored with -rec-track)")
	recLayoutFlag = flag.String("rec-layout", "", "dev: workspace preset for the run: field | command (instead of the saved layout)")
)

var rec struct {
	out     *os.File
	dirMade bool
	snapped bool
}

func captureRun() bool { return *shotPathFlag != "" || *recPathFlag != "" }

func startScreenSize() (int32, int32) {
	parts := strings.Split(*recSizeFlag, "x")
	if len(parts) != 2 {
		return initialScreenWidth, initialScreenHeight
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w < 64 || h < 64 {
		return initialScreenWidth, initialScreenHeight
	}
	return int32(w), int32(h)
}

// maybeRecord runs inside BeginDrawing after everything else has drawn.
func (g *Game) maybeRecord() {
	if *recPathFlag == "" {
		return
	}
	f := int(g.App.FrameIndex())
	if f > *recToFlag {
		g.shotDone = true
		return
	}
	every := max(*recEveryFlag, 1)
	if f < *recFromFlag || (f-*recFromFlag)%every != 0 {
		return
	}
	// Same batch flush as maybeScreenshot: the last widget drawn is otherwise
	// missing from the readback.
	rl.BeginScissorMode(0, 0, int32(rl.GetScreenWidth()), int32(rl.GetScreenHeight()))
	rl.EndScissorMode()
	img := rl.LoadImageFromScreen()
	defer rl.UnloadImage(img)

	if strings.HasSuffix(*recPathFlag, "/") {
		if !rec.dirMade {
			os.MkdirAll(*recPathFlag, 0o755)
			rec.dirMade = true
		}
		rl.ExportImage(*img, filepath.Join(*recPathFlag, fmt.Sprintf("f%06d.png", f)))
		return
	}
	if rec.out == nil {
		out, err := os.OpenFile(*recPathFlag, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			fmt.Printf("rec: %v\n", err)
			*recPathFlag = ""
			return
		}
		rec.out = out
		fmt.Printf("[rec] %dx%d rgba, every %d frames, speed %gx\n",
			img.Width, img.Height, every, *recSpeedFlag)
	}
	n := int(img.Width) * int(img.Height) * 4
	if _, err := rec.out.Write(unsafe.Slice((*byte)(img.Data), n)); err != nil {
		fmt.Printf("rec: %v\n", err)
		g.shotDone = true
	}
}

func (g *Game) shutdownRecorder() {
	if rec.out != nil {
		rec.out.Close()
		rec.out = nil
	}
}

// applyRecCamera drives the cinematic moves before the tick, so the orbit
// system's pass in this frame already sees them. Per-frame increments, not
// per-second: one clip second is always 60 frames whatever -rec-every is.
func (g *Game) applyRecCamera() {
	if *recPathFlag == "" {
		return
	}
	orb := g.Maps.Orbit.Get(g.camEnt)
	if orb == nil {
		return
	}
	f := int(g.App.FrameIndex())
	if *recOrbitFlag != 0 {
		orb.Yaw += float32(*recOrbitFlag * math.Pi / 180 / 60)
	}
	if r0, r1, ok := parsePair(*recDollyFlag); ok {
		span := float64(max(*recToFlag-*recFromFlag, 1))
		t := math.Min(math.Max(float64(f-*recFromFlag)/span, 0), 1)
		t = t * t * (3 - 2*t)
		orb.Radius = float32(r0 + (r1-r0)*t)
	}
	if orb.Radius > orb.MaxRadius {
		orb.MaxRadius = orb.Radius
	}
	g.applyRecTrack()
}

// applyRecTrack eases the anchor toward the chosen group's centroid; ground
// stick keeps its height, the same way flyTo relies on it. The first frame
// snaps so a clip never opens on the camera sliding into place.
func (g *Game) applyRecTrack() {
	mode := *recTrackFlag
	anchor := g.Maps.Pos.Get(g.anchor)
	if anchor == nil {
		return
	}
	if mode == "" {
		if x, z, ok := parsePair(*recAnchorFlag); ok && !rec.snapped {
			rec.snapped = true
			*anchor = components.Normalize(components.WorldPos{}.Add(rl.Vector3{
				X: float32(x), Y: anchor.Local.Y, Z: float32(z),
			}))
		}
		return
	}
	var sum rl.Vector3
	n := 0
	add := func(p components.WorldPos) {
		d := p.Sub(*anchor)
		sum.X += d.X
		sum.Z += d.Z
		n++
	}
	if mode == "sel" {
		for _, e := range g.Sel.Units {
			if g.App.World.Alive(e) {
				if p := g.Maps.Pos.Get(e); p != nil {
					add(*p)
				}
			}
		}
	} else {
		want := func(e ecs.Entity) bool {
			id := uint8(components.FactionPlayer)
			if fac := g.Maps.Faction.Get(e); fac != nil {
				id = fac.ID
			}
			switch mode {
			case "player":
				return id == components.FactionPlayer
			case "enemy":
				return id == components.FactionEnemyRed
			}
			return true
		}
		q := g.Filt.UnitHit.Query()
		for q.Next() {
			if _, pos := q.Get(); want(q.Entity()) {
				add(*pos)
			}
		}
		qv := g.Filt.VehicleRender.Query()
		for qv.Next() {
			if pos, _ := qv.Get(); want(qv.Entity()) {
				add(*pos)
			}
		}
	}
	if n == 0 {
		return
	}
	// A gliding camera changes every pixel every frame, which a gif cannot
	// compress; cuts keep the background still between them.
	k := float32(0.04)
	if !rec.snapped {
		k = 1
		rec.snapped = true
	} else if snap := float32(*recSnapFlag); snap > 0 {
		dx, dz := sum.X/float32(n), sum.Z/float32(n)
		if dx*dx+dz*dz < snap*snap {
			return
		}
		k = 1
	}
	*anchor = components.Normalize(anchor.Add(rl.Vector3{
		X: sum.X / float32(n) * k, Z: sum.Z / float32(n) * k,
	}))
}

func parsePair(s string) (float64, float64, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, errA := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	b, errB := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return a, b, true
}
