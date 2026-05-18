//go:build !trace

package core

// Tracer is a no-op on builds without the `trace` build tag. Every method is
// empty and inlines away - `app.Trace.WriteFrame(...)` in main.go has zero
// runtime cost. The real implementation lives in trace_on.go with identical
// signatures so the rest of the codebase doesn't care which build it's in.
type Tracer struct{}

// Open is a no-op on non-trace builds.
func (Tracer) Open(_ string) error { return nil }

// WriteFrame is a no-op on non-trace builds.
func (Tracer) WriteFrame(_ FrameMetrics, _ *Profiler) {}

// Mark is a no-op on non-trace builds.
func (Tracer) Mark(_ string) {}

// Close is a no-op on non-trace builds.
func (Tracer) Close() error { return nil }

// IsOpen reports whether a trace sink is currently writing. Always false
// without the `trace` build tag.
func (Tracer) IsOpen() bool { return false }

// TraceEnabled reports whether this build includes file-trace support.
func TraceEnabled() bool { return false }
