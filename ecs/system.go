package ecs

import "time"

// UpdateContext provides mutable access to the world state and a deferred buffer for structural changes.
type UpdateContext struct {
	State    *WorldState
	Deferred *CommandBuffer
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
