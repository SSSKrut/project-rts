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
		_ = state.NewEntity()
	})
	return id
}

// RemoveEntity schedules an entity removal.
func (b *CommandBuffer) RemoveEntity(id EntityID) {
	b.enqueue(func(state *WorldState) {
		state.RemoveEntity(id)
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

// Enqueue exports so systems can queue custom logic.
func (b *CommandBuffer) Enqueue(command func(*WorldState)) {
	b.enqueue(command)
}

func (b *CommandBuffer) enqueue(command func(*WorldState)) {
	b.mu.Lock()
	b.commands = append(b.commands, command)
	b.mu.Unlock()
}
