package main

import (
	"fmt"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"

	"github.com/mlange-42/ark/ecs"
)

// census aggregates per-archetype entity counts for the expanded HUD.
type census struct {
	chunksActive int
	chunksRel    int
	chunksTotal  int
	navChunks    int
	props        int
	bridgesLive  int
	walls        int
	floors       int
	stairs       int
	coverSlots   int
	units        int
	weapons      int
	transitions  int
	visionPairs  int
	selection    int
	pathWaypts   int
	roadNodes    int
	roadEdges    int
	bridgeEdges  int
	rivers       int
	bldgPlans    int
	trenches     int
	squads       int
	squadMembers int
	soloists     int
}

func countFilter1[A any](f *ecs.Filter1[A]) int {
	n := 0
	q := f.Query()
	for q.Next() {
		n++
	}
	return n
}

func transitionEdgeCount(reg *components.TransitionRegistry) int {
	n := 0
	for _, edges := range reg.Out {
		n += len(edges)
	}
	return n
}

// hudFontAtlasSize is the largest draw size in the UI so glyphs are never
// upscaled. Smaller draw sizes (13..18 pt) minify under bilinear.
const hudFontAtlasSize int32 = 22

// hudFontPaths is the portable monospace font search list. First hit wins;
// on miss the HUD falls back to raylib's default bitmap.
var hudFontPaths = []string{
	"/usr/share/fonts/noto/NotoSansMono-Medium.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf",
	"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
	"/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono-Bold.ttf",
	"/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf",
	"/usr/share/fonts/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/TTF/DejaVuSansMono-Bold.ttf",
	"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
	"/System/Library/Fonts/Menlo.ttc",
	"/System/Library/Fonts/Monaco.ttf",
	"C:/Windows/Fonts/consolab.ttf",
	"C:/Windows/Fonts/consola.ttf",
}

// loadHUDFont returns the first TTF that loads cleanly. Second return is
// true when a real TTF was loaded — caller must defer UnloadFont only then
// (raylib's default font is engine-owned).
func loadHUDFont() (rl.Font, bool) {
	for _, p := range hudFontPaths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		f := rl.LoadFontEx(p, hudFontAtlasSize, nil)
		if f.BaseSize > 0 {
			// Bilinear is required because we draw at sizes smaller than the
			// atlas; point-filtering looks distorted.
			rl.SetTextureFilter(f.Texture, rl.FilterBilinear)
			return f, true
		}
	}
	return rl.GetFontDefault(), false
}

func drawHUDText(font rl.Font, text string, x, y int32, size int32, tint rl.Color) {
	rl.DrawTextEx(font, text,
		rl.Vector2{X: float32(x), Y: float32(y)},
		float32(size), 1.0, tint)
}

func measureHUDText(font rl.Font, text string, size int32) int32 {
	return int32(rl.MeasureTextEx(font, text, float32(size), 1.0).X)
}

func drawCollapsedProfHUD(p *core.Profiler, screenW int32, font rl.Font) {
	fps := rl.GetFPS()
	frameMs := rl.GetFrameTime() * 1000.0
	tickMs := float32(p.MedianTick().Nanoseconds()) / 1e6
	otherMs := frameMs - tickMs
	if otherMs < 0 {
		otherMs = 0
	}
	heapMB := float32(p.HeapBytes()) / (1024 * 1024)

	line1 := fmt.Sprintf("%d FPS  %.1f ms (tick %.1f / other %.1f)", fps, frameMs, tickMs, otherMs)
	line2 := fmt.Sprintf("heap %.1f MB  ents %d", heapMB, p.EntityCount())

	const fontSize int32 = 22
	const lineH int32 = 28
	const margin int32 = 10
	w1 := measureHUDText(font, line1, fontSize)
	w2 := measureHUDText(font, line2, fontSize)
	w := w1
	if w2 > w {
		w = w2
	}
	x := screenW - margin - w
	rl.DrawRectangle(x-6, margin-4, w+12, lineH*2+8, rl.Color{R: 0, G: 0, B: 0, A: 130})
	drawHUDText(font, line1, x, margin, fontSize, rl.RayWhite)
	drawHUDText(font, line2, x, margin+lineH, fontSize, rl.RayWhite)
}

// drawExpandedProfHUD draws the toggleable detail panel: per-system median
// ms, entity counts, world-data summary, hotkey hints.
func drawExpandedProfHUD(p *core.Profiler, screenW int32, cen census, font rl.Font) {
	const expandedHUDRows = 10
	const fontSize int32 = 18
	const margin int32 = 10
	const headerOffset int32 = 72
	const lineH int32 = 26

	rows := []string{"--- systems (ms median) ---"}
	medians := p.SystemMediansSorted()
	if len(medians) > expandedHUDRows {
		medians = medians[:expandedHUDRows]
	}
	for _, sm := range medians {
		rows = append(rows, fmt.Sprintf("  %-20s %6.2f",
			sm.Name, float64(sm.Median.Nanoseconds())/1e6))
	}
	rows = append(rows, "--- entity counts ---")
	rows = append(rows,
		fmt.Sprintf("  chunks: active=%d rel=%d total=%d", cen.chunksActive, cen.chunksRel, cen.chunksTotal),
		fmt.Sprintf("  nav chunks=%d transitions=%d", cen.navChunks, cen.transitions),
		fmt.Sprintf("  walls=%d floors=%d stairs=%d", cen.walls, cen.floors, cen.stairs),
		fmt.Sprintf("  cover-slots=%d props=%d bridges-live=%d", cen.coverSlots, cen.props, cen.bridgesLive),
		fmt.Sprintf("  units=%d weapons=%d selected=%d", cen.units, cen.weapons, cen.selection),
		fmt.Sprintf("  squads=%d squad-members=%d soloists=%d", cen.squads, cen.squadMembers, cen.soloists),
		fmt.Sprintf("  vision-pairs=%d path-wpts=%d", cen.visionPairs, cen.pathWaypts),
		"--- world data ---",
		fmt.Sprintf("  roads: nodes=%d edges=%d bridges=%d", cen.roadNodes, cen.roadEdges, cen.bridgeEdges),
		fmt.Sprintf("  rivers=%d buildings=%d trenches=%d", cen.rivers, cen.bldgPlans, cen.trenches),
		"--- hotkeys ---",
		"  Tab swap layout  WASD move  RMB-drag orbit  wheel zoom",
		"  LMB pick / drag-marquee  Shift+LMB toggle  RMB MoveTo",
		"  H halt  T squad  U ungroup  F1-F4 formation",
		"  Ctrl+1..5 bind / 1..5 recall  K (hold) all-squad overlay",
		"  Space pause  +/- speed (1/2/4/8)  X crater (3D-focus)",
		"  G/N/C/V/F/Y (hold) road / nav / cover / slot / floor / vision",
		"  P toggle HUD  Ctrl+P snapshot to stdout  MMB-drag pan map",
	)

	maxW := int32(0)
	for _, s := range rows {
		if w := measureHUDText(font, s, fontSize); w > maxW {
			maxW = w
		}
	}
	x := screenW - margin - maxW
	totalH := int32(len(rows))*lineH + 6
	rl.DrawRectangle(x-6, headerOffset-2, maxW+12, totalH, rl.Color{R: 0, G: 0, B: 0, A: 150})
	for i, s := range rows {
		drawHUDText(font, s, x, headerOffset+int32(i)*lineH, fontSize, rl.RayWhite)
	}
}
