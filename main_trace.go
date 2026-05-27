//go:build trace

package main

import (
	"flag"
	"log"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/core"
)

var traceFlag = flag.String("trace", "", "write per-frame JSONL trace to path")

var traceEntityMap = make(map[string]int, 16)

func initTrace(app *core.App) {
	flag.Parse()
	if *traceFlag == "" {
		return
	}
	if err := app.Trace.Open(*traceFlag); err != nil {
		log.Fatalf("trace: %v", err)
	}
}

func handleTraceHotkeys(app *core.App) {
	if rl.IsKeyPressed(rl.KeyM) {
		app.Trace.Mark("manual")
	}
}

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
