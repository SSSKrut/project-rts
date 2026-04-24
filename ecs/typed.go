package ecs

// Set attaches a typed component to a state.
func Set[T Component](state *WorldState, id EntityID, comp T) {
	state.SetComponent(id, comp)
}

// Get fetches a typed component from a state.
func Get[T Component](state *WorldState, id EntityID) (T, bool) {
	var zero T
	comp, ok := state.GetComponent(id, TypeOf[T]())
	if !ok {
		return zero, false
	}
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

