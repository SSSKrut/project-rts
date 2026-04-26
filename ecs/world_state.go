package ecs

import (
	"reflect"
)

// WorldState is the mutable game state that systems read from and write to directly.
type WorldState struct {
	nextEntity EntityID
	entities   map[EntityID]struct{}
	graph      *ArchetypeGraph
	locations  map[EntityID]EntityLocation
	Grid       *SpatialGrid
}

const firstEntityID EntityID = 1

// NewWorldState creates a new world snapshot.
func NewWorldState() *WorldState {
	return &WorldState{
		nextEntity: firstEntityID,
		entities:   make(map[EntityID]struct{}),
		graph:      NewArchetypeGraph(),
		locations:  make(map[EntityID]EntityLocation),
		Grid:       NewSpatialGrid(10.0), // chunk size of 10 units
	}
}

// Clone copies the world state. Components are shallow-copied.
func (s *WorldState) Clone() *WorldState {
	clone := NewWorldState()
	clone.nextEntity = s.nextEntity
	clone.Grid = s.Grid.Clone()
	clone.graph = s.graph.Clone()

	for id := range s.entities {
		clone.entities[id] = struct{}{}
	}

	// Copy entity locations to new graph
	clone.locations = make(map[EntityID]EntityLocation, len(s.locations))
	for id, loc := range s.locations {
		clone.locations[id] = loc
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
	s.entities[id] = struct{}{}

	// Add to root archetype
	row := s.graph.root.AddEntity(id, nil)
	s.locations[id] = EntityLocation{
		archetype: s.graph.root.ID(),
		row:       row,
	}

	return id
}

// RemoveEntity deletes an entity and all its components.
func (s *WorldState) RemoveEntity(id EntityID) {
	loc, ok := s.locations[id]
	if !ok {
		return
	}

	delete(s.entities, id)
	delete(s.locations, id)

	arch, ok := s.graph.GetArchetype(loc.archetype)
	if !ok {
		return
	}

	swappedID, newRow := arch.RemoveEntity(loc.row)
	if newRow >= 0 && swappedID != 0 {
		// Update location of swapped entity
		if swappedLoc, ok := s.locations[swappedID]; ok {
			swappedLoc.row = newRow
			s.locations[swappedID] = swappedLoc
		}
	}
}

// SetComponent attaches a component to an entity.
func (s *WorldState) SetComponent(id EntityID, comp Component) {
	if comp == nil {
		return
	}

	loc, ok := s.locations[id]
	if !ok {
		return
	}

	ct := reflect.TypeOf(comp)
	arch, ok := s.graph.GetArchetype(loc.archetype)
	if !ok {
		return
	}

	// If entity already has this component type, update in place
	if arch.HasComponent(ct) {
		arch.SetComponent(loc.row, comp)
		return
	}

	// Need to move to a new archetype
	newArch, newRow := s.graph.AddArchetype(arch, ct), 0

	// Copy all existing components
	components := make([]Component, len(newArch.types))
	for i, t := range arch.types {
		c, _ := arch.GetComponent(loc.row, t)
		components[i] = c
	}
	// Find index for new component
	for i, t := range newArch.types {
		if t == ct {
			components[i] = comp
			break
		}
	}

	// Remove from old archetype
	swappedID, oldNewRow := arch.RemoveEntity(loc.row)
	if oldNewRow >= 0 && swappedID != 0 {
		// Update location of swapped entity
		if swappedLoc, ok := s.locations[swappedID]; ok {
			swappedLoc.row = oldNewRow
			s.locations[swappedID] = swappedLoc
		}
	}

	// Add to new archetype
	entityID := id
	if len(arch.entities) > 0 && loc.row < len(arch.entities) {
		entityID = arch.entities[len(arch.entities)-1]
		if oldNewRow < 0 {
			entityID = id
		}
	}
	newRow = newArch.AddEntity(entityID, components)

	// Update location
	s.locations[id] = EntityLocation{
		archetype: newArch.ID(),
		row:       newRow,
	}
}

// RemoveComponent removes a component from an entity.
func (s *WorldState) RemoveComponent(id EntityID, ct ComponentType) {
	loc, ok := s.locations[id]
	if !ok {
		return
	}

	arch, ok := s.graph.GetArchetype(loc.archetype)
	if !ok {
		return
	}

	if !arch.HasComponent(ct) {
		return
	}

	// Move to archetype without this component
	newArch := s.graph.RemoveArchetype(arch, ct)

	// Copy all components except the removed one
	components := make([]Component, len(newArch.types))
	newIdx := 0
	for _, t := range arch.types {
		if t == ct {
			continue
		}
		c, _ := arch.GetComponent(loc.row, t)
		components[newIdx] = c
		newIdx++
	}

	// Remove from old archetype
	swappedID, oldNewRow := arch.RemoveEntity(loc.row)
	if oldNewRow >= 0 && swappedID != 0 {
		// Update location of swapped entity
		if swappedLoc, ok := s.locations[swappedID]; ok {
			swappedLoc.row = oldNewRow
			s.locations[swappedID] = swappedLoc
		}
	}

	// Add to new archetype
	entityID := id
	if len(arch.entities) > 0 && loc.row < len(arch.entities) {
		entityID = arch.entities[len(arch.entities)-1]
		if oldNewRow < 0 {
			entityID = id
		}
	}
	newRow := newArch.AddEntity(entityID, components)

	// Update location
	s.locations[id] = EntityLocation{
		archetype: newArch.ID(),
		row:       newRow,
	}
}

// GetComponent fetches a component by type.
func (s *WorldState) GetComponent(id EntityID, ct ComponentType) (Component, bool) {
	loc, ok := s.locations[id]
	if !ok {
		return nil, false
	}

	arch, ok := s.graph.GetArchetype(loc.archetype)
	if !ok {
		return nil, false
	}

	return arch.GetComponent(loc.row, ct)
}

// HasComponents returns true if an entity has all of the provided component types.
func (s *WorldState) HasComponents(id EntityID, types ...ComponentType) bool {
	loc, ok := s.locations[id]
	if !ok {
		return false
	}

	arch, ok := s.graph.GetArchetype(loc.archetype)
	if !ok {
		return false
	}

	for _, t := range types {
		if !arch.HasComponent(t) {
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

// GetLocation returns the entity location for direct archetype access.
func (s *WorldState) GetLocation(id EntityID) (EntityLocation, bool) {
	loc, ok := s.locations[id]
	return loc, ok
}

// GetArchetype returns the archetype for an entity.
func (s *WorldState) GetArchetype(id EntityID) (*Archetype, bool) {
	loc, ok := s.locations[id]
	if !ok {
		return nil, false
	}
	return s.graph.GetArchetype(loc.archetype)
}

// Graph returns the archetype graph for advanced queries.
func (s *WorldState) Graph() *ArchetypeGraph {
	return s.graph
}
