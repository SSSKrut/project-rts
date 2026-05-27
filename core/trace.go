package core

import "time"

// FrameMetrics is the per-frame trace payload built in main.go's
// end-of-frame path. EntityCounts is reused across frames by the caller
// (same map pointer passed every call); WriteFrame must not retain it past
// the call.
type FrameMetrics struct {
	Frame     int64
	Elapsed   time.Duration
	FrameMs   float64
	FPS       int32
	HeapBytes uint64
	// EntityCounts keys are stable across a run so JSONL diffs stay aligned.
	EntityCounts map[string]int
}
