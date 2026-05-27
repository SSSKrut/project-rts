package core

import (
	"log"
	"sync"
)

// WorkerPool is a fixed-size goroutine pool for data-parallel hot loops.
// fn must be race-safe over its own range - no shared writes, no raylib
// calls (raylib-go isn't thread-safe; all draw / upload happens on the
// main goroutine).
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

// SerialThresholdHint: below this total, ParallelFor* skip the worker
// dispatch and run inline. Channel send + wait group + scheduler hops
// cost ~5-10 us; below this threshold the parallel gain doesn't cover it.
const SerialThresholdHint = 64

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

func (p *WorkerPool) Workers() int {
	if p == nil {
		return 0
	}
	return p.workers
}

// ParallelFor splits [0, total) into p.workers contiguous ranges and runs
// fn in parallel. Falls back to inline when p is nil / has one worker /
// total <= SerialThresholdHint.
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

// ParallelForIndexed is ParallelFor that also hands fn the chunk index
// (0..workers-1) for per-worker scratch buffers. In the serial-fallback
// case chunkIdx is always 0, so per-worker buffers should be sized to at
// least 1.
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

// Resize is a stub. TODO: implement when the in-game settings panel needs
// hot-tuning.
func (p *WorkerPool) Resize(n int) {
	log.Printf("worker pool: Resize(%d) requested but not implemented", n)
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
