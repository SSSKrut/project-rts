//go:build !trace

package main

import "rts-go/core"

func initTrace(_ *core.App) {}

func handleTraceHotkeys(_ *core.App) {}

func recordTraceFrame(_ *core.App, _ float32, _ int32, _ census) {}
