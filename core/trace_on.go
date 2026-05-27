//go:build trace

package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Tracer streams one JSONL line per frame to a file. Active only on builds
// with `-tags trace`.
type Tracer struct {
	file   *os.File
	writer *bufio.Writer
	frame  int64
	open   bool
}

func (t *Tracer) Open(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	t.file = f
	t.writer = bufio.NewWriterSize(f, 4096)
	t.frame = 0
	t.open = true
	fmt.Printf("trace: writing to %s\n", path)
	return nil
}

func (t *Tracer) IsOpen() bool { return t != nil && t.open }

// frameRecord JSON field names are part of an external contract - jq
// filters from one run must keep working across runs.
type frameRecord struct {
	Frame    int64              `json:"frame"`
	ElapsMs  float64            `json:"elapsed_ms"`
	FPS      int32              `json:"fps"`
	FrameMs  float64            `json:"frame_ms"`
	TickMs   float64            `json:"tick_ms"`
	Systems  map[string]float64 `json:"systems"`
	Entities map[string]int     `json:"entities"`
	HeapMB   float64            `json:"heap_mb"`
}

type markRecord struct {
	Event   string  `json:"event"`
	Label   string  `json:"label,omitempty"`
	Frame   int64   `json:"frame"`
	ElapsMs float64 `json:"elapsed_ms"`
}

// WriteFrame re-allocates the Systems map every call (~17 keys) - cheap
// enough given the file-IO bound, and avoids stale entries if a system is
// removed mid-run. json.Encoder appends a newline automatically.
func (t *Tracer) WriteFrame(m FrameMetrics, p *Profiler) {
	if !t.open {
		return
	}
	systems := make(map[string]float64, p.SystemCount())
	for i := 0; i < p.SystemCount(); i++ {
		systems[p.SystemName(i)] = float64(p.MedianSystem(i).Nanoseconds()) / 1e6
	}
	rec := frameRecord{
		Frame:    t.frame,
		ElapsMs:  float64(m.Elapsed.Nanoseconds()) / 1e6,
		FPS:      m.FPS,
		FrameMs:  m.FrameMs,
		TickMs:   float64(p.MedianTick().Nanoseconds()) / 1e6,
		Systems:  systems,
		Entities: m.EntityCounts,
		HeapMB:   float64(m.HeapBytes) / (1024 * 1024),
	}
	if err := json.NewEncoder(t.writer).Encode(rec); err != nil {
		fmt.Fprintf(os.Stderr, "trace: write error: %v\n", err)
		return
	}
	t.frame++
}

// Mark writes a one-off marker line. Safe to call any time; ignored when
// the tracer is closed.
func (t *Tracer) Mark(label string) {
	if !t.open {
		return
	}
	rec := markRecord{
		Event:   "mark",
		Label:   label,
		Frame:   t.frame,
		ElapsMs: float64(time.Now().UnixNano()-startWallNs) / 1e6,
	}
	if err := json.NewEncoder(t.writer).Encode(rec); err != nil {
		fmt.Fprintf(os.Stderr, "trace: mark write error: %v\n", err)
	}
}

// Close flushes pending writes and closes the file. Idempotent.
func (t *Tracer) Close() error {
	if !t.open {
		return nil
	}
	t.open = false
	if err := t.writer.Flush(); err != nil {
		_ = t.file.Close()
		return err
	}
	return t.file.Close()
}

func TraceEnabled() bool { return true }

// startWallNs is captured at process start for Mark elapsed-ms timestamps.
// Frame records use FrameMetrics.Elapsed (simulation clock) so they stay
// aligned with the Profiler; Marks happen on rare hotkey input so
// wall-clock is fine.
var startWallNs = time.Now().UnixNano()
