//go:build trace

package main

import (
	"flag"
	"log"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/core"
)

// traceFlag is registered only on `-tags trace` builds. Plain `go build`
// never sees this flag, so the binary's CLI surface stays clean.
var traceFlag = flag.String("trace", "", "write per-frame JSONL trace to path")

// traceEntityMap is reused across frames. WriteFrame iterates it inside the
// json.Encoder, then we clear and refill - no per-frame allocations.
var traceEntityMap = make(map[string]int, 16)

// initTrace parses CLI flags and opens the JSONL writer if -trace was given.
// Called once from main.go right after core.NewApp().
func initTrace(app *core.App) {
	flag.Parse()
	if *traceFlag == "" {
		return
	}
	if err := app.Trace.Open(*traceFlag); err != nil {
		log.Fatalf("trace: %v", err)
	}
}

// handleTraceHotkeys checks every frame for the M key - a one-shot marker
// written into the JSONL stream. Useful for `jq 'select(.event=="mark")'`
// when diffing optimization runs.
func handleTraceHotkeys(app *core.App) {
	if rl.IsKeyPressed(rl.KeyM) {
		app.Trace.Mark("manual")
	}
}

// recordTraceFrame builds FrameMetrics from the just-completed frame and
// emits one JSONL line. Skipped early if no -trace flag was given.
func recordTraceFrame(app *core.App, frameMs float32, fps int32, cen census) {
	if !app.Trace.IsOpen() {
		return
	}
	for k := range traceEntityMap {
		delete(traceEntityMap, k)
	}
	traceEntityMap["chunks_active"] = cen.chunksActive
	traceEntityMap["chunks_rel"] = cen.chunksRel
	traceEntityMap["chunks_total"] = cen.chunksTotal
	traceEntityMap["nav_chunks"] = cen.navChunks
	traceEntityMap["props"] = cen.props
	traceEntityMap["walls"] = cen.walls
	traceEntityMap["floors"] = cen.floors
	traceEntityMap["stairs"] = cen.stairs
	traceEntityMap["cover_slots"] = cen.coverSlots
	traceEntityMap["units"] = cen.units
	traceEntityMap["weapons"] = cen.weapons
	traceEntityMap["transitions"] = cen.transitions
	traceEntityMap["bridges_live"] = cen.bridgesLive
	traceEntityMap["vision_pairs"] = cen.visionPairs

	app.Trace.WriteFrame(core.FrameMetrics{
		Elapsed:      app.Elapsed(),
		FrameMs:      float64(frameMs),
		FPS:          fps,
		HeapBytes:    app.Prof.HeapBytes(),
		EntityCounts: traceEntityMap,
	}, &app.Prof)
}
