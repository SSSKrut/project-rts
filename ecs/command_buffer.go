package ecs

import (
	"sync"
	"sync/atomic"
)

// CommandBuffer collects changes produced by systems.
type CommandBuffer struct {
	mu           sync.Mutex
	commands     []func(*WorldState)
	nextEntityID atomic.Uint64
}

// NewCommandBuffer creates a new buffer starting from the provided entity id.
func NewCommandBuffer(start EntityID) *CommandBuffer {
	var buffer CommandBuffer
	buffer.nextEntityID.Store(uint64(start))
	return &buffer
}

// NextEntityID returns the next id after all queued creations.
func (b *CommandBuffer) NextEntityID() EntityID {
	return EntityID(b.nextEntityID.Load())
}

// CreateEntity schedules a new entity and returns its id.
func (b *CommandBuffer) CreateEntity() EntityID {
	id := EntityID(b.nextEntityID.Add(1) - 1)
	b.enqueue(func(state *WorldState) {
		state.ensureEntity(id)
	})
	return id
}

// RemoveEntity schedules an entity removal.
func (b *CommandBuffer) RemoveEntity(id EntityID) {
	b.enqueue(func(state *WorldState) {
		state.RemoveEntity(id)
	})
}

// SetComponent schedules a component write.
func (b *CommandBuffer) SetComponent(id EntityID, comp Component) {
	b.enqueue(func(state *WorldState) {
		state.SetComponent(id, comp)
	})
}

// RemoveComponent schedules a component removal.
func (b *CommandBuffer) RemoveComponent(id EntityID, ct ComponentType) {
	b.enqueue(func(state *WorldState) {
		state.RemoveComponent(id, ct)
	})
}

// Apply executes all queued commands on the provided state.
func (b *CommandBuffer) Apply(state *WorldState) {
	b.mu.Lock()
	commands := b.commands
	b.commands = nil
	b.mu.Unlock()

	for _, command := range commands {
		command(state)
	}
	state.nextEntity = b.NextEntityID()
}

// Ensure Enqueue is exported so systems can queue custom logic.
func (b *CommandBuffer) Enqueue(command func(*WorldState)) {
	b.enqueue(command)
}

func (b *CommandBuffer) enqueue(command func(*WorldState)) {
	b.mu.Lock()
	b.commands = append(b.commands, command)
	b.mu.Unlock()
}
