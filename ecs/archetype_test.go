package ecs

import (
	"testing"
)

func TestArchetypeBasic(t *testing.T) {
	arch := NewArchetype(1, []ComponentType{TypeOf[int](), TypeOf[string]()})

	if arch.ID() != 1 {
		t.Errorf("expected ID 1, got %d", arch.ID())
	}

	if arch.Len() != 0 {
		t.Errorf("expected empty archetype, got %d entities", arch.Len())
	}

	if !arch.HasComponent(TypeOf[int]()) {
		t.Error("expected archetype to have int component")
	}

	if !arch.HasComponent(TypeOf[string]()) {
		t.Error("expected archetype to have string component")
	}

	if arch.HasComponent(TypeOf[float64]()) {
		t.Error("expected archetype to not have float64 component")
	}
}

func TestArchetypeAddRemove(t *testing.T) {
	arch := NewArchetype(1, []ComponentType{TypeOf[int](), TypeOf[string]()})

	// Add entities
	row1 := arch.AddEntity(1, []Component{42, "hello"})
	row2 := arch.AddEntity(2, []Component{100, "world"})

	if arch.Len() != 2 {
		t.Errorf("expected 2 entities, got %d", arch.Len())
	}

	if row1 != 0 || row2 != 1 {
		t.Errorf("expected rows 0 and 1, got %d and %d", row1, row2)
	}

	// Get components
	comp, ok := arch.GetComponent(0, TypeOf[int]())
	if !ok || comp.(int) != 42 {
		t.Errorf("expected int 42, got %v", comp)
	}

	comp, ok = arch.GetComponent(1, TypeOf[string]())
	if !ok || comp.(string) != "world" {
		t.Errorf("expected string 'world', got %v", comp)
	}

	// Remove entity (swap-remove)
	swappedID, newRow := arch.RemoveEntity(0)
	if arch.Len() != 1 {
		t.Errorf("expected 1 entity after removal, got %d", arch.Len())
	}

	if swappedID != 2 || newRow != 0 {
		t.Errorf("expected swappedID=2, newRow=0, got %d, %d", swappedID, newRow)
	}

	// Check that the swapped entity is now at row 0
	comp, ok = arch.GetComponent(0, TypeOf[string]())
	if !ok || comp.(string) != "world" {
		t.Errorf("expected string 'world' at row 0 after swap, got %v", comp)
	}
}

func TestArchetypeGraphBasic(t *testing.T) {
	graph := NewArchetypeGraph()

	if graph.Root() == nil {
		t.Error("expected root archetype")
	}

	if graph.Root().Len() != 0 {
		t.Error("expected empty root archetype")
	}

	// Create archetype with specific types
	arch := graph.GetOrCreateArchetype([]ComponentType{TypeOf[int](), TypeOf[string]()})

	if arch == nil {
		t.Error("expected archetype to be created")
	}

	// Should return same archetype for same types
	arch2 := graph.GetOrCreateArchetype([]ComponentType{TypeOf[string](), TypeOf[int]()})
	if arch.ID() != arch2.ID() {
		t.Error("expected same archetype for same types in different order")
	}
}

func TestArchetypeGraphEdges(t *testing.T) {
	graph := NewArchetypeGraph()

	// Add component to root
	arch1 := graph.AddArchetype(graph.Root(), TypeOf[int]())
	if arch1 == nil {
		t.Error("expected archetype to be created")
	}

	// Add another component
	arch2 := graph.AddArchetype(arch1, TypeOf[string]())
	if arch2 == nil {
		t.Error("expected archetype to be created")
	}

	// Check that edges are cached
	arch1Again := graph.AddArchetype(graph.Root(), TypeOf[int]())
	if arch1.ID() != arch1Again.ID() {
		t.Error("expected same archetype for cached edge")
	}

	// Remove component
	archBack := graph.RemoveArchetype(arch2, TypeOf[string]())
	if archBack.ID() != arch1.ID() {
		t.Errorf("expected to get back archetype %d, got %d", arch1.ID(), archBack.ID())
	}
}

func TestWorldStateBasic(t *testing.T) {
	state := NewWorldState()

	// Create entity
	id := state.NewEntity()
	if id != 1 {
		t.Errorf("expected first entity ID to be 1, got %d", id)
	}

	// Set component
	state.SetComponent(id, 42)

	// Get component
	comp, ok := state.GetComponent(id, TypeOf[int]())
	if !ok {
		t.Error("expected to find component")
	}
	if comp.(int) != 42 {
		t.Errorf("expected int 42, got %v", comp)
	}

	// Update component
	state.SetComponent(id, 100)
	comp, ok = state.GetComponent(id, TypeOf[int]())
	if !ok || comp.(int) != 100 {
		t.Errorf("expected int 100, got %v", comp)
	}

	// Add another component
	state.SetComponent(id, "hello")
	comp, ok = state.GetComponent(id, TypeOf[string]())
	if !ok || comp.(string) != "hello" {
		t.Errorf("expected string 'hello', got %v", comp)
	}

	// Remove component
	state.RemoveComponent(id, TypeOf[int]())
	_, ok = state.GetComponent(id, TypeOf[int]())
	if ok {
		t.Error("expected component to be removed")
	}

	// String component should still exist
	comp, ok = state.GetComponent(id, TypeOf[string]())
	if !ok || comp.(string) != "hello" {
		t.Errorf("expected string 'hello' to remain, got %v", comp)
	}

	// Remove entity
	state.RemoveEntity(id)
	_, ok = state.GetComponent(id, TypeOf[string]())
	if ok {
		t.Error("expected entity to be removed")
	}
}

func TestWorldStateMultipleEntities(t *testing.T) {
	state := NewWorldState()

	// Create multiple entities
	id1 := state.NewEntity()
	id2 := state.NewEntity()
	id3 := state.NewEntity()

	state.SetComponent(id1, 1)
	state.SetComponent(id2, 2)
	state.SetComponent(id3, 3)

	state.SetComponent(id1, "one")
	state.SetComponent(id2, "two")
	state.SetComponent(id3, "three")

	// Verify all components
	comp1, _ := state.GetComponent(id1, TypeOf[int]())
	comp2, _ := state.GetComponent(id2, TypeOf[int]())
	comp3, _ := state.GetComponent(id3, TypeOf[int]())

	if comp1.(int) != 1 || comp2.(int) != 2 || comp3.(int) != 3 {
		t.Errorf("expected ints 1, 2, 3, got %v, %v, %v", comp1, comp2, comp3)
	}

	str1, _ := state.GetComponent(id1, TypeOf[string]())
	str2, _ := state.GetComponent(id2, TypeOf[string]())
	str3, _ := state.GetComponent(id3, TypeOf[string]())

	if str1.(string) != "one" || str2.(string) != "two" || str3.(string) != "three" {
		t.Errorf("expected strings one, two, three, got %v, %v, %v", str1, str2, str3)
	}
}

func TestTypedGetSet(t *testing.T) {
	state := NewWorldState()
	id := state.NewEntity()

	Set(state, id, 42)
	Set(state, id, "hello")

	val, ok := Get[int](state, id)
	if !ok || val != 42 {
		t.Errorf("expected int 42, got %v", val)
	}

	str, ok := Get[string](state, id)
	if !ok || str != "hello" {
		t.Errorf("expected string 'hello', got %v", str)
	}

	// Test non-existent
	_, ok = Get[float64](state, id)
	if ok {
		t.Error("expected false for non-existent component")
	}

	// Test Has
	if !Has[int](state, id) {
		t.Error("expected Has[int] to return true")
	}
	if Has[float64](state, id) {
		t.Error("expected Has[float64] to return false")
	}

	// Test Remove
	Remove[int](state, id)
	if Has[int](state, id) {
		t.Error("expected Has[int] to return false after Remove")
	}
}

func TestForEach(t *testing.T) {
	state := NewWorldState()

	id1 := state.NewEntity()
	id2 := state.NewEntity()
	id3 := state.NewEntity()

	Set(state, id1, 1)
	Set(state, id2, 2)
	Set(state, id3, 3)

	Set(state, id1, "one")
	Set(state, id2, "two")
	// id3 has no string component

	count := 0
	sum := 0
	ForEach[int](state, func(id EntityID, val *int) {
		count++
		sum += *val
	})

	if count != 3 {
		t.Errorf("expected 3 iterations, got %d", count)
	}
	if sum != 6 {
		t.Errorf("expected sum 6, got %d", sum)
	}

	// Test ForEach2
	count = 0
	ForEach2[int, string](state, func(id EntityID, val *int, str *string) {
		count++
	})

	if count != 2 {
		t.Errorf("expected 2 iterations for ForEach2, got %d", count)
	}
}

func TestClone(t *testing.T) {
	state := NewWorldState()

	id := state.NewEntity()
	Set(state, id, 42)
	Set(state, id, "hello")

	clone := state.Clone()

	val, ok := Get[int](clone, id)
	if !ok || val != 42 {
		t.Errorf("expected cloned int 42, got %v", val)
	}

	// Modify original
	Set(state, id, 100)

	// Clone should have original value
	val, ok = Get[int](clone, id)
	if !ok || val != 42 {
		t.Errorf("expected cloned int to remain 42, got %v", val)
	}

	// Original should have new value
	val, ok = Get[int](state, id)
	if !ok || val != 100 {
		t.Errorf("expected original int 100, got %v", val)
	}
}
