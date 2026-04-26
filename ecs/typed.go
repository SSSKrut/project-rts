package ecs

// Set attaches a typed component to a state.
func Set[T Component](state *WorldState, id EntityID, comp T) {
	state.SetComponent(id, comp)
}

// Get fetches a typed component from a state.
func Get[T Component](state *WorldState, id EntityID) (T, bool) {
	var zero T
	loc, ok := state.locations[id]
	if !ok {
		return zero, false
	}

	arch, ok := state.graph.GetArchetype(loc.archetype)
	if !ok {
		return zero, false
	}

	idx, ok := arch.typeIndex[TypeOf[T]()]
	if !ok {
		return zero, false
	}

	if loc.row >= len(arch.columns[idx]) {
		return zero, false
	}

	comp := arch.columns[idx][loc.row]
	typed, ok := comp.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// Remove removes a typed component from a state.
func Remove[T Component](state *WorldState, id EntityID) {
	state.RemoveComponent(id, TypeOf[T]())
}

// Has returns true if the entity has component T.
func Has[T Component](state *WorldState, id EntityID) bool {
	loc, ok := state.locations[id]
	if !ok {
		return false
	}

	arch, ok := state.graph.GetArchetype(loc.archetype)
	if !ok {
		return false
	}

	return arch.HasComponent(TypeOf[T]())
}
