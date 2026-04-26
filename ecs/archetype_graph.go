package ecs

import (
	"reflect"
	"sort"
)

// ArchetypeGraph manages all archetypes and transitions between them.
type ArchetypeGraph struct {
	archetypes   map[ArchetypeID]*Archetype
	nextID       ArchetypeID
	root         *Archetype // empty archetype for new entities
	typeSetCache map[string]ArchetypeID
}

// NewArchetypeGraph creates a new archetype graph.
func NewArchetypeGraph() *ArchetypeGraph {
	g := &ArchetypeGraph{
		archetypes:   make(map[ArchetypeID]*Archetype),
		nextID:       1,
		typeSetCache: make(map[string]ArchetypeID),
	}
	// Create root archetype (empty component set)
	g.root = g.createArchetype(nil)
	return g
}

// Root returns the empty archetype for new entities.
func (g *ArchetypeGraph) Root() *Archetype {
	return g.root
}

// GetArchetype returns an archetype by ID.
func (g *ArchetypeGraph) GetArchetype(id ArchetypeID) (*Archetype, bool) {
	arch, ok := g.archetypes[id]
	return arch, ok
}

// typeSetKey creates a cache key from component types.
func typeSetKey(types []ComponentType) string {
	sorted := make([]ComponentType, len(types))
	copy(sorted, types)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].String() < sorted[j].String()
	})

	var key string
	for _, t := range sorted {
		key += t.String() + ";"
	}
	return key
}

// createArchetype creates a new archetype with the given types.
func (g *ArchetypeGraph) createArchetype(types []ComponentType) *Archetype {
	id := g.nextID
	g.nextID++

	arch := NewArchetype(id, types)
	g.archetypes[id] = g.archetypes[id]
	g.archetypes[id] = arch

	key := typeSetKey(types)
	g.typeSetCache[key] = id

	return arch
}

// GetOrCreateArchetype finds or creates an archetype with the given types.
func (g *ArchetypeGraph) GetOrCreateArchetype(types []ComponentType) *Archetype {
	key := typeSetKey(types)
	if id, ok := g.typeSetCache[key]; ok {
		return g.archetypes[id]
	}
	return g.createArchetype(types)
}

// AddArchetype creates a new archetype by adding a component type to an existing one.
func (g *ArchetypeGraph) AddArchetype(from *Archetype, addType ComponentType) *Archetype {
	// Check if edge already exists
	if arch, ok := from.addEdges[addType]; ok {
		return arch
	}

	// Check if archetype already exists
	newTypes := make([]ComponentType, 0, len(from.types)+1)
	newTypes = append(newTypes, from.types...)
	newTypes = append(newTypes, addType)

	key := typeSetKey(newTypes)
	var newArch *Archetype
	if id, ok := g.typeSetCache[key]; ok {
		newArch = g.archetypes[id]
	} else {
		newArch = g.createArchetype(newTypes)
	}

	// Link edges
	from.addEdges[addType] = newArch
	newArch.removeEdges[addType] = from

	return newArch
}

// RemoveArchetype creates a new archetype by removing a component type from an existing one.
func (g *ArchetypeGraph) RemoveArchetype(from *Archetype, removeType ComponentType) *Archetype {
	// Check if edge already exists
	if arch, ok := from.removeEdges[removeType]; ok {
		return arch
	}

	// Build new type list
	newTypes := make([]ComponentType, 0, len(from.types)-1)
	for _, t := range from.types {
		if t != removeType {
			newTypes = append(newTypes, t)
		}
	}

	key := typeSetKey(newTypes)
	var newArch *Archetype
	if id, ok := g.typeSetCache[key]; ok {
		newArch = g.archetypes[id]
	} else {
		newArch = g.createArchetype(newTypes)
	}

	// Link edges
	from.removeEdges[removeType] = newArch
	newArch.addEdges[removeType] = from

	return newArch
}

// AddComponent moves an entity to a new archetype by adding a component.
func (g *ArchetypeGraph) AddComponent(from *Archetype, row int, comp Component) (*Archetype, int) {
	ct := reflect.TypeOf(comp)
	newArch := g.AddArchetype(from, ct)

	// Copy all existing components
	components := make([]Component, len(newArch.types))
	for i, t := range from.types {
		c, _ := from.GetComponent(row, t)
		components[i] = c
	}
	// Add new component at the end
	components[len(from.types)] = comp

	// Remove from old archetype
	from.RemoveEntity(row)

	// Add to new archetype
	entityID := from.entities[len(from.entities)-1]
	newRow := newArch.AddEntity(entityID, components)

	return newArch, newRow
}

// RemoveComponent moves an entity to a new archetype by removing a component.
func (g *ArchetypeGraph) RemoveComponent(from *Archetype, row int, removeType ComponentType) (*Archetype, int) {
	newArch := g.RemoveArchetype(from, removeType)

	// Copy all components except the removed one
	components := make([]Component, len(newArch.types))
	newIdx := 0
	for _, t := range from.types {
		if t == removeType {
			continue
		}
		c, _ := from.GetComponent(row, t)
		components[newIdx] = c
		newIdx++
	}

	// Remove from old archetype
	from.RemoveEntity(row)

	// Add to new archetype
	entityID := from.entities[len(from.entities)-1]
	newRow := newArch.AddEntity(entityID, components)

	return newArch, newRow
}

// MoveEntity moves an entity from one archetype to another, copying all matching components.
func (g *ArchetypeGraph) MoveEntity(from *Archetype, row int, to *Archetype) int {
	// Copy all matching components
	components := make([]Component, len(to.types))
	for i, t := range to.types {
		c, ok := from.GetComponent(row, t)
		if ok {
			components[i] = c
		}
	}

	// Remove from old archetype
	from.RemoveEntity(row)

	// Add to new archetype
	entityID := from.entities[len(from.entities)-1]
	newRow := to.AddEntity(entityID, components)

	return newRow
}

// AllArchetypes returns all archetypes in the graph.
func (g *ArchetypeGraph) AllArchetypes() []*Archetype {
	result := make([]*Archetype, 0, len(g.archetypes))
	for _, arch := range g.archetypes {
		result = append(result, arch)
	}
	return result
}

// ArchetypesWith returns all archetypes that contain all given component types.
func (g *ArchetypeGraph) ArchetypesWith(types ...ComponentType) []*Archetype {
	result := make([]*Archetype, 0)
	for _, arch := range g.archetypes {
		hasAll := true
		for _, t := range types {
			if !arch.HasComponent(t) {
				hasAll = false
				break
			}
		}
		if hasAll {
			result = append(result, arch)
		}
	}
	return result
}

// Clone creates a deep copy of the archetype graph.
func (g *ArchetypeGraph) Clone() *ArchetypeGraph {
	clone := &ArchetypeGraph{
		archetypes:   make(map[ArchetypeID]*Archetype, len(g.archetypes)),
		nextID:       g.nextID,
		typeSetCache: make(map[string]ArchetypeID, len(g.typeSetCache)),
	}

	// Clone all archetypes first
	for id, arch := range g.archetypes {
		clone.archetypes[id] = arch.Clone()
	}

	// Set root
	clone.root = clone.archetypes[g.root.id]

	// Copy cache
	for k, v := range g.typeSetCache {
		clone.typeSetCache[k] = v
	}

	// Rebuild edges (pointers to cloned archetypes)
	for id, arch := range g.archetypes {
		clonedArch := clone.archetypes[id]
		for ct, edge := range arch.addEdges {
			clonedArch.addEdges[ct] = clone.archetypes[edge.id]
		}
		for ct, edge := range arch.removeEdges {
			clonedArch.removeEdges[ct] = clone.archetypes[edge.id]
		}
	}

	return clone
}
