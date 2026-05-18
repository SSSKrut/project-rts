package core

import (
	"time"

	"github.com/mlange-42/ark/ecs"
)

// LODTier - scheduling bucket only, NOT stored in components.
type LODTier int

const (
	LODTierActive LODTier = iota
	LODTierRelevant
	LODTierDormant
)

const LODDisabled time.Duration = -1

type LODPolicy struct {
	ActiveEvery   time.Duration
	RelevantEvery time.Duration
	DormantEvery  time.Duration
}

func (p LODPolicy) Interval(tier LODTier) time.Duration {
	switch tier {
	case LODTierActive:
		return p.ActiveEvery
	case LODTierRelevant:
		return p.RelevantEvery
	case LODTierDormant:
		return p.DormantEvery
	default:
		return LODDisabled
	}
}

type UpdateContext struct {
	World *ecs.World
	Delta time.Duration
	Tier  LODTier
	// FrameIndex is the App-level tick counter (Phase 11.5 P10). Used by
	// systems that hash entities into per-frame buckets via ShouldProcessBucket
	// for time-sliced workloads (vision/AI in later phases). Always grows;
	// paused ticks still increment so render-time animations stay live.
	FrameIndex uint32
}

type System interface {
	Name() string
	LODPolicy() LODPolicy
	Update(ctx UpdateContext)
}

type systemEntry struct {
	sys     System
	lastRun map[LODTier]time.Duration
}

type App struct {
	World      *ecs.World
	systems    []systemEntry
	elapsed    time.Duration
	frameIndex uint32

	// Prof collects per-tick timings (M7.5.1). Lives on App so tests / tools
	// can read it without globals; main.go owns the HUD presentation.
	Prof Profiler

	// Trace is a build-tag-gated per-frame JSONL writer. On a normal build
	// Tracer's methods compile to no-ops (see core/trace_off.go).
	Trace Tracer

	// TimeScale multiplies the real-time delta passed to Tick (Phase 10 P7).
	// 0 = paused (no system update, but the render loop keeps going);
	// 1 = real-time; 2/4/8 = compressed. `app.elapsed` accumulates the
	// scaled delta so per-system LOD intervals are in game-time. Profiler /
	// Trace use time.Now() / time.Since for their own bookkeeping, so perf
	// numbers stay in real-time even when the simulation is paused.
	TimeScale float32

	// LastNonZeroScale remembers the speed we were running at before Space
	// paused us, so an unpause restores it rather than snapping to 1×.
	LastNonZeroScale float32
}

func NewApp() *App {
	return &App{
		World:            ecs.NewWorld(1024),
		TimeScale:        1.0,
		LastNonZeroScale: 1.0,
	}
}

func (app *App) AddSystem(sys System) {
	app.Prof.RegisterSystem(sys.Name())
	app.systems = append(app.systems, systemEntry{
		sys:     sys,
		lastRun: make(map[LODTier]time.Duration),
	})
}

// Elapsed exposes the total simulation time since NewApp. Used by main.go to
// throttle expensive sampling (ReadMemStats, World.Stats).
func (app *App) Elapsed() time.Duration { return app.elapsed }

// FrameIndex exposes the rolling tick counter (Phase 11.5 P10). Time-sliced
// systems hash entity IDs against this to pick "their" frame.
func (app *App) FrameIndex() uint32 { return app.frameIndex }

func (app *App) Tick(delta time.Duration) {
	scaled := time.Duration(float64(delta) * float64(app.TimeScale))
	delta = scaled
	app.elapsed += delta
	app.frameIndex++

	tickStart := time.Now()
	app.Prof.BeginTick()

	for i := range app.systems {
		entry := &app.systems[i]
		policy := entry.sys.LODPolicy()

		for _, tier := range []LODTier{LODTierActive, LODTierRelevant, LODTierDormant} {
			interval := policy.Interval(tier)
			if interval == LODDisabled {
				continue
			}

			last := entry.lastRun[tier]
			if interval == 0 || app.elapsed-last >= interval {
				sysStart := time.Now()
				entry.sys.Update(UpdateContext{
					World:      app.World,
					Delta:      app.elapsed - last,
					Tier:       tier,
					FrameIndex: app.frameIndex,
				})
				app.Prof.RecordSystem(i, time.Since(sysStart))
				entry.lastRun[tier] = app.elapsed
			}
		}
	}

	app.Prof.EndTick(time.Since(tickStart))
}
