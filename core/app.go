package core

import (
	"time"

	"github.com/mlange-42/ark/ecs"
)

// LODTier defines update frequency buckets — for scheduling only, NOT stored in components.
type LODTier int

const (
	LODTierActive LODTier = iota
	LODTierRelevant
	LODTierDormant
)

const LODDisabled time.Duration = -1

// LODPolicy defines how often a system runs per LOD bucket.
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
	World   *ecs.World
	systems []systemEntry
	elapsed time.Duration
}

func NewApp() *App {
	return &App{
		World: ecs.NewWorld(1024),
	}
}

func (app *App) AddSystem(sys System) {
	app.systems = append(app.systems, systemEntry{
		sys:     sys,
		lastRun: make(map[LODTier]time.Duration),
	})
}

func (app *App) Tick(delta time.Duration) {
	app.elapsed += delta

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
				entry.sys.Update(UpdateContext{
					World: app.World,
					Delta: app.elapsed - last,
					Tier:  tier,
				})
				entry.lastRun[tier] = app.elapsed
			}
		}
	}
}
