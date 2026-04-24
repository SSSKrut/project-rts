package ecs

import "reflect"

// WorldState is an immutable snapshot for systems and a mutable target for command application.
type WorldState struct {
	nextEntity EntityID
	entities   map[EntityID]struct{}
	components map[ComponentType]*SparseSet
	Grid       *SpatialGrid
}

const firstEntityID EntityID = 1

// NewWorldState creates a new world snapshot.
func NewWorldState() *WorldState {
	return &WorldState{
		nextEntity: firstEntityID,
		entities:   make(map[EntityID]struct{}),
		components: make(map[ComponentType]*SparseSet),
		Grid:       NewSpatialGrid(10.0), // chunk size of 10 units
	}
}

// Clone copies the world state. Components are shallow-copied.
func (s *WorldState) Clone() *WorldState {
	clone := NewWorldState()
	clone.nextEntity = s.nextEntity
	clone.Grid = s.Grid.Clone()
	for id := range s.entities {
		clone.entities[id] = struct{}{}
	}
	for ct, store := range s.components {
		clone.components[ct] = store.Clone()
	}
	return clone
}

// NextEntityID returns the next available entity id.
func (s *WorldState) NextEntityID() EntityID {
	return s.nextEntity
}

// NewEntity creates a new entity in this state.
func (s *WorldState) NewEntity() EntityID {
	id := s.nextEntity
	s.nextEntity++
	s.ensureEntity(id)
	return id
}

// RemoveEntity deletes an entity and all its components.
func (s *WorldState) RemoveEntity(id EntityID) {
	delete(s.entities, id)
	for _, store := range s.components {
		store.Remove(id)
	}
}

// SetComponent attaches a component to an entity.
func (s *WorldState) SetComponent(id EntityID, comp Component) {
	if comp == nil {
		return
	}
	ct := reflect.TypeOf(comp)
	store := s.ensureStore(ct)
	store.Set(id, comp)
	s.ensureEntity(id)
}

// RemoveComponent removes a component from an entity.
func (s *WorldState) RemoveComponent(id EntityID, ct ComponentType) {
	store, ok := s.components[ct]
	if !ok {
		return
	}
	store.Remove(id)
}

// GetComponent fetches a component by type.
func (s *WorldState) GetComponent(id EntityID, ct ComponentType) (Component, bool) {
	store, ok := s.components[ct]
	if !ok {
		return nil, false
	}
	return store.Get(id)
}

// HasComponents returns true if an entity has all of the provided component types.
func (s *WorldState) HasComponents(id EntityID, types ...ComponentType) bool {
	for _, ct := range types {
		store, ok := s.components[ct]
		if !ok {
			return false
		}
		if _, has := store.Get(id); !has {
			return false
		}
	}
	return true
}

// Entities returns all entity ids.
func (s *WorldState) Entities() []EntityID {
	ids := make([]EntityID, 0, len(s.entities))
	for id := range s.entities {
		ids = append(ids, id)
	}
	return ids
}

// Query returns entities that have all requested component types.
func (s *WorldState) Query(types ...ComponentType) []EntityID {
	if len(types) == 0 {
		return s.Entities()
	}
	base, ok := s.components[types[0]]
	if !ok {
		return nil
	}
	// For optimization, we could sort `types` by sparse set dense count.
	// We'll just iterate the first one:
	entities := base.Entities()
	ids := make([]EntityID, 0, len(entities))
	for _, id := range entities {
		if s.HasComponents(id, types[1:]...) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *WorldState) ensureStore(ct ComponentType) *SparseSet {
	store, ok := s.components[ct]
	if !ok {
		store = NewSparseSet()
		s.components[ct] = store
	}
	return store
}

func (s *WorldState) ensureEntity(id EntityID) {
	if s.entities == nil {
		s.entities = make(map[EntityID]struct{})
	}
	s.entities[id] = struct{}{}
}
