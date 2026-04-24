package ecs

import "time"

// UpdateContext provides read-only access to the current world and a command buffer for writes.
type UpdateContext struct {
	Current  *WorldState
	Commands *CommandBuffer
	Delta    time.Duration
	Now      time.Duration
	Phase    Phase
	LOD      LODLevel
}

// System processes entities and emits changes through the command buffer.
type System interface {
	Name() string
	Phase() Phase
	LODPolicy() LODPolicy
	Reads() []ComponentType
	Writes() []ComponentType
	Update(ctx UpdateContext)
}
