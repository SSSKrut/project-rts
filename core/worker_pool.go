package core

import (
	"log"
	"sync"
)

// WorkerPool is a fixed-size goroutine pool used by simulation systems for
// data-parallel hot loops (Phase 11.5 M11.5.1). Lifecycle is owned by main:
// NewWorkerPool spins workers; Stop joins them on shutdown.
//
// ParallelFor is the primary entry point. It splits [0, total) into roughly
// even ranges (one per worker), dispatches each range as a job, and blocks
// until all jobs complete. The caller's fn must be race-safe over its own
// range — writing to shared maps / resources is forbidden. raylib calls are
// also forbidden inside fn (raylib-go isn't thread-safe; all draw / upload
// happens on the main goroutine).
//
// ParallelForIndexed is the variant that also passes a chunk index so
// callers can keep per-worker scratch buffers (used by FormationSystem in
// Phase 11.6 M11.6.4 for race-safe straggler collection).
type WorkerPool struct {
	workers int
	jobs    chan workerJob
	quit    chan struct{}
	wg      sync.WaitGroup
}

type workerJob struct {
	fn       func(start, end int)
	fnIndex  func(chunkIdx, start, end int)
	chunkIdx int
	start    int
	end      int
	done     *sync.WaitGroup
}

// SerialThresholdHint — when total <= this value, ParallelFor / ParallelForIndexed
// skip the worker dispatch and run the whole range in the caller goroutine.
// Below this, the ParallelFor synchronisation overhead (channel send + wait
// group + scheduler hops, ~5–10 μs on 8 workers) outweighs the parallel gain.
//
// Phase 11.6 M11.6.3: chosen empirically against the current 12-unit test
// scene where step cost is ~1 μs/unit. Phase 14 may revisit per-system if
// some systems grow heavier than others, but for now one global default
// keeps the API single-line simple.
const SerialThresholdHint = 64

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
			if j.fnIndex != nil {
				j.fnIndex(j.chunkIdx, j.start, j.end)
			} else if j.fn != nil {
				j.fn(j.start, j.end)
			}
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
// Falls back to a serial inline call when:
//   - p is nil or has at most 1 worker (test / stripped builds);
//   - total <= SerialThresholdHint (Phase 11.6 M11.6.3: small workloads where
//     the dispatch overhead would dominate).
func (p *WorkerPool) ParallelFor(total int, fn func(start, end int)) {
	if total <= 0 || fn == nil {
		return
	}
	if p == nil || p.workers <= 1 || total <= SerialThresholdHint {
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

// ParallelForIndexed is ParallelFor that also hands `fn` the chunk index
// (0..workers-1). Useful for per-worker scratch buffers — see
// FormationSystem.workerLeaveBufs.
//
// Same fallback rules as ParallelFor. In the serial-fallback case chunkIdx
// is always 0; callers should size their per-worker buffers to at least 1.
func (p *WorkerPool) ParallelForIndexed(total int, fn func(chunkIdx, start, end int)) {
	if total <= 0 || fn == nil {
		return
	}
	if p == nil || p.workers <= 1 || total <= SerialThresholdHint {
		fn(0, 0, total)
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
		p.jobs <- workerJob{fnIndex: fn, chunkIdx: i, start: start, end: end, done: &done}
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
