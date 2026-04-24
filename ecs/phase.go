package ecs

// Phase defines ordered execution stages for systems.
type Phase int

const (
	PhaseLogic Phase = iota
	PhasePhysics
	PhasePostPhysics
)

// DefaultPhases returns the default phase order.
func DefaultPhases() []Phase {
	return []Phase{PhaseLogic, PhasePhysics, PhasePostPhysics}
}
