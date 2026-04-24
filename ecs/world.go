package ecs

import "time"

type systemEntry struct {
	system  System
	lastRun map[LODLevel]time.Duration
}

// World owns the active state and runs systems in ordered phases.
type World struct {
	current *WorldState
	systems []systemEntry
	phases  []Phase
	elapsed time.Duration
}

// NewWorld creates a new ECS world.
func NewWorld() *World {
	return &World{
		current: NewWorldState(),
		phases:  DefaultPhases(),
	}
}

// SetPhases overrides the execution order of system phases.
func (w *World) SetPhases(phases []Phase) {
	w.phases = append([]Phase(nil), phases...)
}

// Current returns the active world state.
func (w *World) Current() *WorldState {
	return w.current
}

// AddSystem registers a system for execution.
func (w *World) AddSystem(system System) {
	w.systems = append(w.systems, systemEntry{
		system:  system,
		lastRun: make(map[LODLevel]time.Duration),
	})
}

// Tick advances the world by one step.
func (w *World) Tick(delta time.Duration) {
	if w.current == nil {
		w.current = NewWorldState()
	}
	w.elapsed += delta
	now := w.elapsed

	current := w.current
	next := current.Clone()
	buffer := NewCommandBuffer(current.NextEntityID())

	phases := w.phases
	if len(phases) == 0 {
		phases = DefaultPhases()
	}

	for _, phase := range phases {
		for i := range w.systems {
			entry := &w.systems[i]
			system := entry.system
			if system.Phase() != phase {
				continue
			}

			policy := system.LODPolicy()
			for _, level := range lodLevels {
				interval := policy.Interval(level)
				if interval == LODDisabled {
					continue
				}

				last, ok := entry.lastRun[level]
				if !ok {
					last = now - interval
				}

				if interval == 0 || now-last >= interval {
					step := delta
					if interval > 0 {
						step = now - last
					}
					system.Update(UpdateContext{
						Current:  current,
						Commands: buffer,
						Delta:    step,
						Now:      now,
						Phase:    phase,
						LOD:      level,
					})
					entry.lastRun[level] = now
				}
			}
		}
	}

	buffer.Apply(next)
	w.current = next
}
