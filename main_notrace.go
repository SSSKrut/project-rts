//go:build !trace

package main

import "rts-go/core"

// initTrace is a no-op on builds without `-tags trace`. flag.Parse() is never
// called either, so an unknown -trace argument cannot crash the binary —
// users get the same UX as before, the flag simply doesn't exist.
func initTrace(_ *core.App) {}

// handleTraceHotkeys is a no-op on non-trace builds. The M key is free to
// be repurposed by future phases (currently unused).
func handleTraceHotkeys(_ *core.App) {}

// recordTraceFrame is a no-op on non-trace builds. Inlines to nothing, so
// the per-frame call site in main.go costs zero.
func recordTraceFrame(_ *core.App, _ float32, _ int32, _ census) {}
