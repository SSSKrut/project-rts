package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProfileWindow is the rolling sample count. 60 frames at 60 fps is short
// enough for the HUD to react to a code change and long enough to smooth out
// GC pauses and one-off mesh uploads.
const ProfileWindow = 60

// MaxSystems caps how many systems the Profiler can track. AddSystem panics
// if exceeded so the panic fires once at startup, not silently mid-session.
const MaxSystems = 64

type profileFrame struct {
	tickTotalNs int64
	perSystemNs [MaxSystems]int64
}

type Profiler struct {
	samples [ProfileWindow]profileFrame
	head    int
	full    bool

	systemNames [MaxSystems]string
	systemCount int

	// scratch is reused by Median* so HUD draws don't allocate.
	scratch [ProfileWindow]int64

	lastHeapBytes  uint64
	lastHeapUpdate time.Duration

	lastEntityUsed   int
	lastEntityUpdate time.Duration
}

// RegisterSystem assigns a stable index to a system name. Panics on overflow
// so the build fails loud rather than silently mid-session.
func (p *Profiler) RegisterSystem(name string) int {
	if p.systemCount >= MaxSystems {
		panic(fmt.Sprintf("profiler: more than %d systems registered (raise MaxSystems)", MaxSystems))
	}
	idx := p.systemCount
	p.systemNames[idx] = name
	p.systemCount++
	return idx
}

func (p *Profiler) SystemCount() int { return p.systemCount }

func (p *Profiler) SystemName(idx int) string {
	if idx < 0 || idx >= p.systemCount {
		return ""
	}
	return p.systemNames[idx]
}

func (p *Profiler) BeginTick() {
	f := &p.samples[p.head]
	f.tickTotalNs = 0
	for i := 0; i < p.systemCount; i++ {
		f.perSystemNs[i] = 0
	}
}

// RecordSystem accumulates into the current frame's slot. A system may be
// called up to three times per tick (one per LOD tier); they all sum into
// the same slot.
func (p *Profiler) RecordSystem(sysIdx int, d time.Duration) {
	if sysIdx < 0 || sysIdx >= MaxSystems {
		return
	}
	p.samples[p.head].perSystemNs[sysIdx] += int64(d)
}

func (p *Profiler) EndTick(tickTotal time.Duration) {
	p.samples[p.head].tickTotalNs = int64(tickTotal)
	p.head++
	if p.head >= ProfileWindow {
		p.head = 0
		p.full = true
	}
}

func (p *Profiler) sampleCount() int {
	if p.full {
		return ProfileWindow
	}
	return p.head
}

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

// medianInPlace sorts buf in place and returns the middle element.
func medianInPlace(buf []int64) int64 {
	sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
	return buf[len(buf)/2]
}

func (p *Profiler) SetHeap(heapBytes uint64, now time.Duration) {
	p.lastHeapBytes = heapBytes
	p.lastHeapUpdate = now
}

func (p *Profiler) HeapBytes() uint64 { return p.lastHeapBytes }

func (p *Profiler) HeapStale(now, interval time.Duration) bool {
	return now-p.lastHeapUpdate >= interval
}

func (p *Profiler) SetEntityCount(used int, now time.Duration) {
	p.lastEntityUsed = used
	p.lastEntityUpdate = now
}

func (p *Profiler) EntityCount() int { return p.lastEntityUsed }

func (p *Profiler) EntityCountStale(now, interval time.Duration) bool {
	return now-p.lastEntityUpdate >= interval
}

type SystemMedian struct {
	Name   string
	Median time.Duration
}

// LastFrameSystems returns the top-n systems of the most recent completed
// tick by raw duration (not median) — hitch forensics (ISSUES #15).
func (p *Profiler) LastFrameSystems(n int) []SystemMedian {
	idx := p.head - 1
	if idx < 0 {
		if !p.full {
			return nil
		}
		idx = ProfileWindow - 1
	}
	f := &p.samples[idx]
	out := make([]SystemMedian, p.systemCount)
	for i := 0; i < p.systemCount; i++ {
		out[i] = SystemMedian{Name: p.systemNames[i], Median: time.Duration(f.perSystemNs[i])}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Median > out[j].Median })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// LastFrameTick returns the most recent completed tick's total duration.
func (p *Profiler) LastFrameTick() time.Duration {
	idx := p.head - 1
	if idx < 0 {
		if !p.full {
			return 0
		}
		idx = ProfileWindow - 1
	}
	return time.Duration(p.samples[idx].tickTotalNs)
}

// SystemMediansSorted returns every registered system's median, sorted
// descending by Median.
func (p *Profiler) SystemMediansSorted() []SystemMedian {
	out := make([]SystemMedian, p.systemCount)
	for i := 0; i < p.systemCount; i++ {
		out[i] = SystemMedian{Name: p.systemNames[i], Median: p.MedianSystem(i)}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Median > out[j].Median })
	return out
}

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
