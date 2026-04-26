package ecs

import "reflect"

// ArchetypeID uniquely identifies an archetype.
type ArchetypeID uint64

// Archetype stores all entities with the same component types together.
type Archetype struct {
	id        ArchetypeID
	types     []ComponentType
	typeIndex map[ComponentType]int

	// SoA layout - cache-friendly storage
	entities []EntityID
	columns  [][]Component

	// Graph edges for add/remove transitions
	addEdges    map[ComponentType]*Archetype
	removeEdges map[ComponentType]*Archetype
}

// NewArchetype creates a new archetype with given component types.
func NewArchetype(id ArchetypeID, types []ComponentType) *Archetype {
	a := &Archetype{
		id:          id,
		types:       types,
		typeIndex:   make(map[ComponentType]int, len(types)),
		entities:    make([]EntityID, 0),
		columns:     make([][]Component, len(types)),
		addEdges:    make(map[ComponentType]*Archetype),
		removeEdges: make(map[ComponentType]*Archetype),
	}
	for i, ct := range types {
		a.typeIndex[ct] = i
		a.columns[i] = make([]Component, 0)
	}
	return a
}

// ID returns the archetype's unique identifier.
func (a *Archetype) ID() ArchetypeID {
	return a.id
}

// Types returns the component types this archetype holds.
func (a *Archetype) Types() []ComponentType {
	return a.types
}

// Len returns the number of entities in this archetype.
func (a *Archetype) Len() int {
	return len(a.entities)
}

// HasComponent checks if this archetype has a component type.
func (a *Archetype) HasComponent(ct ComponentType) bool {
	_, ok := a.typeIndex[ct]
	return ok
}

// AddEntity adds a new entity to this archetype.
func (a *Archetype) AddEntity(id EntityID, components []Component) int {
	row := len(a.entities)
	a.entities = append(a.entities, id)
	for i, comp := range components {
		a.columns[i] = append(a.columns[i], comp)
	}
	return row
}

// RemoveEntity removes an entity at the given row using swap-remove.
func (a *Archetype) RemoveEntity(row int) (EntityID, int) {
	last := len(a.entities) - 1
	if row == last {
		a.entities = a.entities[:last]
		for i := range a.columns {
			a.columns[i] = a.columns[i][:last]
		}
		return 0, -1
	}

	swappedID := a.entities[last]
	a.entities[row] = swappedID
	a.entities = a.entities[:last]

	for i := range a.columns {
		a.columns[i][row] = a.columns[i][last]
		a.columns[i] = a.columns[i][:last]
	}

	return swappedID, row
}

// GetComponent returns a pointer to the component at the given row.
func (a *Archetype) GetComponent(row int, ct ComponentType) (Component, bool) {
	idx, ok := a.typeIndex[ct]
	if !ok {
		return nil, false
	}
	if row >= len(a.columns[idx]) {
		return nil, false
	}
	return a.columns[idx][row], true
}

// SetComponent updates a component at the given row.
func (a *Archetype) SetComponent(row int, comp Component) {
	ct := reflect.TypeOf(comp)
	idx, ok := a.typeIndex[ct]
	if !ok {
		return
	}
	a.columns[idx][row] = comp
}

// EntityAt returns the entity ID at the given row.
func (a *Archetype) EntityAt(row int) EntityID {
	return a.entities[row]
}

// Entities returns all entity IDs in this archetype.
func (a *Archetype) Entities() []EntityID {
	return a.entities
}

// Clone creates a shallow copy of the archetype.
func (a *Archetype) Clone() *Archetype {
	clone := &Archetype{
		id:          a.id,
		types:       append([]ComponentType(nil), a.types...),
		typeIndex:   make(map[ComponentType]int, len(a.typeIndex)),
		entities:    make([]EntityID, len(a.entities)),
		columns:     make([][]Component, len(a.columns)),
		addEdges:    make(map[ComponentType]*Archetype),
		removeEdges: make(map[ComponentType]*Archetype),
	}
	for ct, idx := range a.typeIndex {
		clone.typeIndex[ct] = idx
	}
	copy(clone.entities, a.entities)
	for i := range a.columns {
		clone.columns[i] = make([]Component, len(a.columns[i]))
		copy(clone.columns[i], a.columns[i])
	}
	return clone
}

// EntityLocation stores where an entity is located in the archetype graph.
type EntityLocation struct {
	archetype ArchetypeID
	row       int
}

// Archetype returns the archetype ID.
func (l EntityLocation) Archetype() ArchetypeID {
	return l.archetype
}

// Row returns the row index within the archetype.
func (l EntityLocation) Row() int {
	return l.row
}
