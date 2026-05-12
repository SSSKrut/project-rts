package core

import "time"

// FrameMetrics is the per-frame trace payload built in main.go's end-of-frame
// path and forwarded to Tracer.WriteFrame. Fields are deliberately concrete
// (not pointers) so the value is cheap to pass and easy to JSON-encode.
//
// EntityCounts is reused across frames by Tracer (the same map pointer is
// passed every call); WriteFrame should not retain it past the call. main.go
// repopulates it each frame.
type FrameMetrics struct {
	Frame     int64
	Elapsed   time.Duration
	FrameMs   float64
	FPS       int32
	HeapBytes uint64
	// EntityCounts maps a display name (e.g. "chunks", "walls", "units") to
	// its current count. The set of keys is stable across a run so JSONL diffs
	// stay aligned.
	EntityCounts map[string]int
}
