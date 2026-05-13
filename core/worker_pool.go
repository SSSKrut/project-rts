package core

import (
	"log"
	"sync"
)

// WorkerPool is a fixed-size goroutine pool used by simulation systems for
// data-parallel hot loops (Phase 11.5 M11.5.1). Lifecycle is owned by main:
// NewWorkerPool spins workers; Stop joins them on shutdown.
//
// ParallelFor is the only entry point. It splits [0, total) into roughly even
// ranges (one per worker), dispatches each range as a job, and blocks until
// all jobs complete. The caller's fn must be race-safe over its own range —
// writing to shared maps / resources is forbidden. raylib calls are also
// forbidden inside fn (raylib-go isn't thread-safe; all draw / upload
// happens on the main goroutine).
type WorkerPool struct {
	workers int
	jobs    chan workerJob
	quit    chan struct{}
	wg      sync.WaitGroup
}

type workerJob struct {
	fn    func(start, end int)
	start int
	end   int
	done  *sync.WaitGroup
}

// NewWorkerPool spawns n workers. n must be >= 1; values <= 0 are clamped to
// 1 to keep ParallelFor functional (otherwise there'd be no goroutine to
// receive the job).
func NewWorkerPool(n int) *WorkerPool {
	if n < 1 {
		n = 1
	}
	p := &WorkerPool{
		workers: n,
		jobs:    make(chan workerJob, n*2),
		quit:    make(chan struct{}),
	}
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go p.workerLoop()
	}
	return p
}

func (p *WorkerPool) workerLoop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.quit:
			return
		case j, ok := <-p.jobs:
			if !ok {
				return
			}
			j.fn(j.start, j.end)
			j.done.Done()
		}
	}
}

// Workers returns the current pool size.
func (p *WorkerPool) Workers() int {
	if p == nil {
		return 0
	}
	return p.workers
}

// ParallelFor splits [0, total) into p.workers contiguous ranges and runs fn
// once per range, in parallel. Blocks until every range completes. fn must
// be race-safe over its [start, end) slice — no shared writes, no raylib.
//
// When p is nil OR total == 0 the call is a no-op, letting systems fall back
// to a simple serial loop for unit tests where the pool isn't wired.
func (p *WorkerPool) ParallelFor(total int, fn func(start, end int)) {
	if total <= 0 || fn == nil {
		return
	}
	if p == nil || p.workers <= 1 || total == 1 {
		fn(0, total)
		return
	}
	workers := p.workers
	if workers > total {
		workers = total
	}
	chunk := total / workers
	rem := total % workers
	var done sync.WaitGroup
	done.Add(workers)
	start := 0
	for i := 0; i < workers; i++ {
		size := chunk
		if i < rem {
			size++
		}
		end := start + size
		p.jobs <- workerJob{fn: fn, start: start, end: end, done: &done}
		start = end
	}
	done.Wait()
}

// Resize is a stub for Phase 11.5 — the actual implementation lands when the
// in-game settings panel (Phase 21+) needs hot-tuning. For now we just log so
// callers can see the request without crashing.
func (p *WorkerPool) Resize(n int) {
	log.Printf("worker pool: Resize(%d) requested but not implemented (Phase 11.5 stub)", n)
}

// Stop signals every worker to exit and waits for the join. Idempotent.
func (p *WorkerPool) Stop() {
	if p == nil {
		return
	}
	select {
	case <-p.quit:
		return
	default:
	}
	close(p.quit)
	p.wg.Wait()
}
