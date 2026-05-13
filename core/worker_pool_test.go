package core

import (
	"sync/atomic"
	"testing"
)

func TestWorkerPoolParallelFor(t *testing.T) {
	pool := NewWorkerPool(4)
	defer pool.Stop()

	const total = 1000
	var counts [total]int32
	pool.ParallelFor(total, func(start, end int) {
		for i := start; i < end; i++ {
			atomic.AddInt32(&counts[i], 1)
		}
	})

	for i, c := range counts {
		if c != 1 {
			t.Fatalf("element %d processed %d times, want 1", i, c)
		}
	}
}

func TestWorkerPoolNilSafe(t *testing.T) {
	var pool *WorkerPool
	called := 0
	pool.ParallelFor(10, func(start, end int) {
		called += end - start
	})
	if called != 10 {
		t.Fatalf("nil pool fallback ran %d items, want 10", called)
	}
}

func TestWorkerPoolStopIdempotent(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Stop()
	pool.Stop() // second call must not panic.
}

func TestWorkerPoolZeroTotal(t *testing.T) {
	pool := NewWorkerPool(2)
	defer pool.Stop()
	hit := false
	pool.ParallelFor(0, func(start, end int) { hit = true })
	if hit {
		t.Fatalf("fn called with total=0")
	}
}

// TestParallelForRaceSafe mirrors the hot-path systems' usage: each worker
// writes to a disjoint range of a shared output slice. Combined with
// `go test -race ./core/` this is a smoke check that ParallelFor doesn't
// have hidden shared writes inside the dispatcher.
func TestParallelForRaceSafe(t *testing.T) {
	pool := NewWorkerPool(4)
	defer pool.Stop()

	// Total chosen above SerialThresholdHint so the workers actually run in
	// parallel — otherwise the test degenerates to serial inline.
	const total = SerialThresholdHint * 4
	out := make([]int32, total)
	pool.ParallelFor(total, func(start, end int) {
		for i := start; i < end; i++ {
			out[i] = int32(i * 2)
		}
	})

	for i, v := range out {
		if v != int32(i*2) {
			t.Fatalf("index %d: got %d, want %d", i, v, i*2)
		}
	}
}

// TestParallelForIndexedPerWorkerBuf exercises the per-worker scratch-buffer
// pattern from FormationSystem.workerLeaveBufs: each worker pushes to its
// own buffer, then a serial post-pass merges them. Race-safe because each
// worker owns its slice header.
func TestParallelForIndexedPerWorkerBuf(t *testing.T) {
	pool := NewWorkerPool(4)
	defer pool.Stop()

	const total = SerialThresholdHint * 4
	workers := pool.Workers()
	bufs := make([][]int, workers)
	pool.ParallelForIndexed(total, func(chunkIdx, start, end int) {
		if chunkIdx >= len(bufs) {
			t.Errorf("chunkIdx %d out of range (workers=%d)", chunkIdx, workers)
			return
		}
		for i := start; i < end; i++ {
			bufs[chunkIdx] = append(bufs[chunkIdx], i)
		}
	})

	count := 0
	for _, b := range bufs {
		count += len(b)
	}
	if count != total {
		t.Fatalf("total elements across worker buffers: got %d, want %d", count, total)
	}
}

// TestParallelForSerialFallback verifies that small workloads (≤
// SerialThresholdHint) skip the dispatcher and run inline. We detect this by
// checking that fn is invoked exactly once with the full range.
func TestParallelForSerialFallback(t *testing.T) {
	pool := NewWorkerPool(4)
	defer pool.Stop()

	calls := 0
	var sawStart, sawEnd int
	pool.ParallelFor(SerialThresholdHint, func(start, end int) {
		calls++
		sawStart, sawEnd = start, end
	})
	if calls != 1 {
		t.Fatalf("serial fallback should invoke fn once, got %d", calls)
	}
	if sawStart != 0 || sawEnd != SerialThresholdHint {
		t.Fatalf("expected [0,%d), got [%d,%d)", SerialThresholdHint, sawStart, sawEnd)
	}
}
