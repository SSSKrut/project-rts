package ecs

import (
	"testing"
)

// Position for benchmarking
type Position struct {
	X, Y, Z float32
}

// Velocity for benchmarking
type Velocity struct {
	X, Y, Z float32
}

// Health for benchmarking
type Health struct {
	Current, Max int
}

func BenchmarkArchetypeAddEntity(b *testing.B) {
	graph := NewArchetypeGraph()
	types := []ComponentType{TypeOf[Position](), TypeOf[Velocity](), TypeOf[Health]()}
	arch := graph.GetOrCreateArchetype(types)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		arch.AddEntity(EntityID(i+1), []Component{
			Position{float32(i), 0, 0},
			Velocity{1, 0, 0},
			Health{100, 100},
		})
	}
}

func BenchmarkArchetypeGetComponent(b *testing.B) {
	state := NewWorldState()

	// Create 10000 entities
	for i := 0; i < 10000; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
		Set(state, id, Health{100, 100})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := EntityID((i % 10000) + 1)
		_, _ = Get[Position](state, id)
	}
}

func BenchmarkForEachSingleComponent(b *testing.B) {
	state := NewWorldState()

	// Create 10000 entities
	for i := 0; i < 10000; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
		Set(state, id, Health{100, 100})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForEach[Position](state, func(id EntityID, pos *Position) {
			pos.X += 1
		})
	}
}

func BenchmarkForEachTwoComponents(b *testing.B) {
	state := NewWorldState()

	// Create 10000 entities
	for i := 0; i < 10000; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
		Set(state, id, Health{100, 100})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForEach2[Position, Velocity](state, func(id EntityID, pos *Position, vel *Velocity) {
			pos.X += vel.X
			pos.Y += vel.Y
			pos.Z += vel.Z
		})
	}
}

func BenchmarkForEachThreeComponents(b *testing.B) {
	state := NewWorldState()

	// Create 10000 entities
	for i := 0; i < 10000; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
		Set(state, id, Health{100, 100})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForEach3[Position, Velocity, Health](state, func(id EntityID, pos *Position, vel *Velocity, hp *Health) {
			pos.X += vel.X
			hp.Current -= 1
		})
	}
}

func BenchmarkSetComponent(b *testing.B) {
	state := NewWorldState()
	id := state.NewEntity()
	Set(state, id, Position{0, 0, 0})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Set(state, id, Position{float32(i), 0, 0})
	}
}

func BenchmarkNewEntity(b *testing.B) {
	state := NewWorldState()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
	}
}

func BenchmarkClone(b *testing.B) {
	state := NewWorldState()

	// Create 1000 entities with 3 components each
	for i := 0; i < 1000; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		Set(state, id, Velocity{1, 0, 0})
		Set(state, id, Health{100, 100})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.Clone()
	}
}

func BenchmarkArchetypesWith(b *testing.B) {
	state := NewWorldState()

	// Create 100 archetypes with different component combinations
	for i := 0; i < 100; i++ {
		id := state.NewEntity()
		Set(state, id, Position{float32(i), 0, 0})
		if i%2 == 0 {
			Set(state, id, Velocity{1, 0, 0})
		}
		if i%3 == 0 {
			Set(state, id, Health{100, 100})
		}
	}

	types := []ComponentType{TypeOf[Position](), TypeOf[Velocity]()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.graph.ArchetypesWith(types...)
	}
}
