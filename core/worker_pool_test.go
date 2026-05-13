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
