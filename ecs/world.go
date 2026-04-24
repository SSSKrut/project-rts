package ecs

import "time"

type systemEntry struct {
	system  System
	lastRun map[LODLevel]time.Duration
}

// World owns the active state and runs systems in ordered phases.
type World struct {
	current  *WorldState
	systems  []systemEntry
	phases   []Phase
	elapsed  time.Duration
	schedule *schedule
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
	w.schedule = nil
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
	w.schedule = nil
}

func (w *World) ensureSchedule() *schedule {
	if w.schedule == nil {
		phases := w.phases
		if len(phases) == 0 {
			phases = DefaultPhases()
		}
		s := buildSchedule(w.systems, phases)
		w.schedule = &s
	}
	return w.schedule
}

// Tick advances the world by one step.
func (w *World) Tick(delta time.Duration) {
	if w.current == nil {
		w.current = NewWorldState()
	}
	w.elapsed += delta
	now := w.elapsed

	state := w.current
	buffer := NewCommandBuffer(state.NextEntityID())

	sched := w.ensureSchedule()

	phases := w.phases
	if len(phases) == 0 {
		phases = DefaultPhases()
	}

	for _, phase := range phases {
		for _, idx := range sched.order[phase] {
			entry := &w.systems[idx]
			system := entry.system

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
						State:    state,
						Deferred: buffer,
						Delta:    step,
						Now:      now,
						Phase:    phase,
						LOD:      level,
					})
					entry.lastRun[level] = now
				}
			}
		}

		// Sync point: apply structural changes between phases
		buffer.Apply(state)
		buffer = NewCommandBuffer(state.NextEntityID())
	}
}
