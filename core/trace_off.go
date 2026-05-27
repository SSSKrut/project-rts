//go:build !trace

package core

// Tracer is a no-op on builds without the `trace` build tag. Every method
// inlines away so `app.Trace.WriteFrame(...)` has zero runtime cost. The
// real implementation lives in trace_on.go with identical signatures.
type Tracer struct{}

func (Tracer) Open(_ string) error                    { return nil }
func (Tracer) WriteFrame(_ FrameMetrics, _ *Profiler) {}
func (Tracer) Mark(_ string)                          {}
func (Tracer) Close() error                           { return nil }
func (Tracer) IsOpen() bool                           { return false }

func TraceEnabled() bool { return false }
