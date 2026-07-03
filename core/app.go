package core

import (
	"time"

	"github.com/mlange-42/ark/ecs"
)

// LODTier is a scheduling bucket only, NOT stored in components.
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
	// FrameIndex always grows; paused ticks still increment so render-time
	// animations stay live. Used by ShouldProcessBucket for time-slicing.
	FrameIndex uint32
	// SimNow is the canonical game-time clock (seconds, TimeScale-scaled).
	SimNow    float64
	TickIndex uint64
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
	tickIndex  uint64

	Prof  Profiler
	Trace Tracer

	// TimeScale multiplies the real-time delta passed to Tick. 0 = paused
	// (render loop keeps going); 1 = real-time; 2/4/8 = compressed. app.elapsed
	// accumulates scaled delta so LOD intervals are in game-time. Profiler /
	// Trace use time.Now() so perf numbers stay in real-time even when paused.
	TimeScale float32

	// LastNonZeroScale remembers the pre-pause speed so unpause restores it
	// rather than snapping to 1x.
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

func (app *App) Elapsed() time.Duration { return app.elapsed }

func (app *App) FrameIndex() uint32 { return app.frameIndex }

// maxTickDelta: a GPU-stall frame (Tab swap, resize) must not lurch the sim.
const maxTickDelta = 100 * time.Millisecond

func (app *App) Tick(delta time.Duration) {
	if delta > maxTickDelta {
		delta = maxTickDelta
	}
	scaled := time.Duration(float64(delta) * float64(app.TimeScale))
	delta = scaled
	app.elapsed += delta
	app.frameIndex++
	app.tickIndex++
	simNow := app.elapsed.Seconds()

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
					SimNow:     simNow,
					TickIndex:  app.tickIndex,
				})
				app.Prof.RecordSystem(i, time.Since(sysStart))
				entry.lastRun[tier] = app.elapsed
			}
		}
	}

	app.Prof.EndTick(time.Since(tickStart))
}
