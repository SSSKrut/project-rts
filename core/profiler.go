package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProfileWindow is the rolling sample count. 60 frames = ~1 s at 60 fps —
// short enough for the HUD to react to "I just changed code, is it faster?",
// long enough to smooth out GC pauses and one-off mesh uploads.
const ProfileWindow = 60

// MaxSystems caps how many systems the Profiler can track. ~17 systems are
// registered today; ×2 headroom. AddSystem panics if exceeded so the panic
// fires once at startup, not silently in the middle of a session.
const MaxSystems = 32

// profileFrame is one tick's worth of per-system timings plus the tick total.
// Fixed-size array — no per-frame allocations.
type profileFrame struct {
	tickTotalNs int64
	perSystemNs [MaxSystems]int64
}

// Profiler is a fixed-window rolling collector of per-tick timings. Value
// stored by pointer on App. Reads (medians) are O(ProfileWindow × log) which
// is ~µs per HUD draw — negligible.
type Profiler struct {
	samples [ProfileWindow]profileFrame
	head    int  // index to write the NEXT frame
	full    bool // true once head wrapped at least once

	systemNames [MaxSystems]string
	systemCount int

	// Scratch buffer reused by Median* so HUD draws don't allocate.
	scratch [ProfileWindow]int64

	// Last heap value, refreshed by ReadHeapEvery. Bytes.
	lastHeapBytes  uint64
	lastHeapUpdate time.Duration

	// Last entity count, refreshed by ReadEntityCountEvery. Used by HUD/trace.
	lastEntityUsed   int
	lastEntityUpdate time.Duration
}

// RegisterSystem assigns a stable index to a system name. Called once per
// system by App.AddSystem. Panics on overflow so the build fails loud, not
// silently truncates timings.
func (p *Profiler) RegisterSystem(name string) int {
	if p.systemCount >= MaxSystems {
		panic(fmt.Sprintf("profiler: more than %d systems registered (raise MaxSystems)", MaxSystems))
	}
	idx := p.systemCount
	p.systemNames[idx] = name
	p.systemCount++
	return idx
}

// SystemCount returns how many slots are in use.
func (p *Profiler) SystemCount() int { return p.systemCount }

// SystemName returns the registered name for a system index.
func (p *Profiler) SystemName(idx int) string {
	if idx < 0 || idx >= p.systemCount {
		return ""
	}
	return p.systemNames[idx]
}

// BeginTick zeroes the slot the next RecordSystem calls will accumulate into.
// Called by App.Tick at the top of the tick.
func (p *Profiler) BeginTick() {
	f := &p.samples[p.head]
	f.tickTotalNs = 0
	for i := 0; i < p.systemCount; i++ {
		f.perSystemNs[i] = 0
	}
}

// RecordSystem accumulates a duration into the current frame's slot for sysIdx.
// A system may be called up to three times per tick (one per LOD tier) — they
// all sum into the same slot, matching P3 (per-system aggregated across tiers).
func (p *Profiler) RecordSystem(sysIdx int, d time.Duration) {
	if sysIdx < 0 || sysIdx >= MaxSystems {
		return
	}
	p.samples[p.head].perSystemNs[sysIdx] += int64(d)
}

// EndTick stamps the total tick duration and advances the ring head.
func (p *Profiler) EndTick(tickTotal time.Duration) {
	p.samples[p.head].tickTotalNs = int64(tickTotal)
	p.head++
	if p.head >= ProfileWindow {
		p.head = 0
		p.full = true
	}
}

// sampleCount returns how many filled slots are in the ring.
func (p *Profiler) sampleCount() int {
	if p.full {
		return ProfileWindow
	}
	return p.head
}

// MedianTick returns the median total-tick duration over the current window.
// Returns 0 before the first tick.
func (p *Profiler) MedianTick() time.Duration {
	n := p.sampleCount()
	if n == 0 {
		return 0
	}
	for i := 0; i < n; i++ {
		p.scratch[i] = p.samples[i].tickTotalNs
	}
	return time.Duration(medianInPlace(p.scratch[:n]))
}

// MedianSystem returns the median tick duration for sysIdx over the window.
func (p *Profiler) MedianSystem(sysIdx int) time.Duration {
	if sysIdx < 0 || sysIdx >= p.systemCount {
		return 0
	}
	n := p.sampleCount()
	if n == 0 {
		return 0
	}
	for i := 0; i < n; i++ {
		p.scratch[i] = p.samples[i].perSystemNs[sysIdx]
	}
	return time.Duration(medianInPlace(p.scratch[:n]))
}

// medianInPlace sorts buf and returns the middle element. buf is clobbered.
func medianInPlace(buf []int64) int64 {
	sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
	return buf[len(buf)/2]
}

// SetHeap records a heap-size sample. Caller decides cadence (1 Hz typically).
func (p *Profiler) SetHeap(heapBytes uint64, now time.Duration) {
	p.lastHeapBytes = heapBytes
	p.lastHeapUpdate = now
}

// HeapBytes returns the last sampled heap usage. Zero if no SetHeap yet.
func (p *Profiler) HeapBytes() uint64 { return p.lastHeapBytes }

// HeapStale reports whether enough time has passed since the last SetHeap that
// the caller should refresh. Used by main.go to throttle ReadMemStats.
func (p *Profiler) HeapStale(now, interval time.Duration) bool {
	return now-p.lastHeapUpdate >= interval
}

// SetEntityCount records a world-entity count sample.
func (p *Profiler) SetEntityCount(used int, now time.Duration) {
	p.lastEntityUsed = used
	p.lastEntityUpdate = now
}

// EntityCount returns the last sampled total entity count.
func (p *Profiler) EntityCount() int { return p.lastEntityUsed }

// EntityCountStale reports whether the cached count should be refreshed.
func (p *Profiler) EntityCountStale(now, interval time.Duration) bool {
	return now-p.lastEntityUpdate >= interval
}

// SystemMedian is a sortable (name, median) pair for HUD presentation.
type SystemMedian struct {
	Name   string
	Median time.Duration
}

// SystemMediansSorted returns every registered system's median, sorted
// descending by Median. The returned slice is freshly allocated — HUD calls
// it once per frame and the slice is small (~17 entries), so the cost is in
// the noise.
func (p *Profiler) SystemMediansSorted() []SystemMedian {
	out := make([]SystemMedian, p.systemCount)
	for i := 0; i < p.systemCount; i++ {
		out[i] = SystemMedian{Name: p.systemNames[i], Median: p.MedianSystem(i)}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Median > out[j].Median })
	return out
}

// PrintSnapshot writes a copy-pasteable two-column report to stdout. Tick total
// first, then every system sorted desc by median. Fixed width so the output
// stays diffable between snapshots.
func (p *Profiler) PrintSnapshot() {
	var sb strings.Builder
	sb.WriteString("\n─── Profiler snapshot ───\n")
	fmt.Fprintf(&sb, "  %-24s %8.3f ms\n", "tick total", float64(p.MedianTick().Nanoseconds())/1e6)
	sb.WriteString("\n  systems (median ms over window)\n")
	for _, sm := range p.SystemMediansSorted() {
		fmt.Fprintf(&sb, "  %-24s %8.3f ms\n", sm.Name, float64(sm.Median.Nanoseconds())/1e6)
	}
	fmt.Fprintf(&sb, "\n  heap            %8.2f MB\n", float64(p.HeapBytes())/(1024*1024))
	fmt.Fprintf(&sb, "  entities        %8d\n", p.EntityCount())
	fmt.Fprintf(&sb, "  window samples  %8d / %d\n", p.sampleCount(), ProfileWindow)
	sb.WriteString("─────────────────────────\n")
	fmt.Print(sb.String())
}
