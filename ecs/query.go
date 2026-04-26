package ecs

// ForEach iterates over all entities with component A.
func ForEach[A Component](state *WorldState, fn func(id EntityID, a *A)) {
	ct := TypeOf[A]()
	for _, arch := range state.graph.ArchetypesWith(ct) {
		idx, ok := arch.typeIndex[ct]
		if !ok {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			comp := arch.columns[idx][row]
			if typed, ok := comp.(A); ok {
				fn(entityID, &typed)
				arch.columns[idx][row] = typed
			}
		}
	}
}

// ForEach2 iterates over all entities with components A and B.
func ForEach2[A, B Component](state *WorldState, fn func(id EntityID, a *A, b *B)) {
	ctA := TypeOf[A]()
	ctB := TypeOf[B]()
	for _, arch := range state.graph.ArchetypesWith(ctA, ctB) {
		idxA, okA := arch.typeIndex[ctA]
		idxB, okB := arch.typeIndex[ctB]
		if !okA || !okB {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			compA := arch.columns[idxA][row]
			compB := arch.columns[idxB][row]
			typedA, okA := compA.(A)
			typedB, okB := compB.(B)
			if okA && okB {
				fn(entityID, &typedA, &typedB)
				arch.columns[idxA][row] = typedA
				arch.columns[idxB][row] = typedB
			}
		}
	}
}

// ForEach3 iterates over all entities with components A, B and C.
func ForEach3[A, B, C Component](state *WorldState, fn func(id EntityID, a *A, b *B, c *C)) {
	ctA := TypeOf[A]()
	ctB := TypeOf[B]()
	ctC := TypeOf[C]()
	for _, arch := range state.graph.ArchetypesWith(ctA, ctB, ctC) {
		idxA, okA := arch.typeIndex[ctA]
		idxB, okB := arch.typeIndex[ctB]
		idxC, okC := arch.typeIndex[ctC]
		if !okA || !okB || !okC {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			compA := arch.columns[idxA][row]
			compB := arch.columns[idxB][row]
			compC := arch.columns[idxC][row]
			typedA, okA := compA.(A)
			typedB, okB := compB.(B)
			typedC, okC := compC.(C)
			if okA && okB && okC {
				fn(entityID, &typedA, &typedB, &typedC)
				arch.columns[idxA][row] = typedA
				arch.columns[idxB][row] = typedB
				arch.columns[idxC][row] = typedC
			}
		}
	}
}

// ForEachLOD iterates over all entities with component A at the given LOD level.
func ForEachLOD[A Component](state *WorldState, level LODLevel, fn func(id EntityID, a *A)) {
	ct := TypeOf[A]()
	lodCT := TypeOf[LOD]()
	for _, arch := range state.graph.ArchetypesWith(ct, lodCT) {
		idx, ok := arch.typeIndex[ct]
		lodIdx, lodOk := arch.typeIndex[lodCT]
		if !ok || !lodOk {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			lodComp := arch.columns[lodIdx][row]
			if lod, ok := lodComp.(LOD); ok && lod.Level == level {
				comp := arch.columns[idx][row]
				if typed, ok := comp.(A); ok {
					fn(entityID, &typed)
					arch.columns[idx][row] = typed
				}
			}
		}
	}
}

// ForEach2LOD iterates over all entities with components A and B at the given LOD level.
func ForEach2LOD[A, B Component](state *WorldState, level LODLevel, fn func(id EntityID, a *A, b *B)) {
	ctA := TypeOf[A]()
	ctB := TypeOf[B]()
	lodCT := TypeOf[LOD]()
	for _, arch := range state.graph.ArchetypesWith(ctA, ctB, lodCT) {
		idxA, okA := arch.typeIndex[ctA]
		idxB, okB := arch.typeIndex[ctB]
		lodIdx, lodOk := arch.typeIndex[lodCT]
		if !okA || !okB || !lodOk {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			lodComp := arch.columns[lodIdx][row]
			if lod, ok := lodComp.(LOD); ok && lod.Level == level {
				compA := arch.columns[idxA][row]
				compB := arch.columns[idxB][row]
				typedA, okA := compA.(A)
				typedB, okB := compB.(B)
				if okA && okB {
					fn(entityID, &typedA, &typedB)
					arch.columns[idxA][row] = typedA
					arch.columns[idxB][row] = typedB
				}
			}
		}
	}
}

// ForEach3LOD iterates over all entities with components A, B and C at the given LOD level.
func ForEach3LOD[A, B, C Component](state *WorldState, level LODLevel, fn func(id EntityID, a *A, b *B, c *C)) {
	ctA := TypeOf[A]()
	ctB := TypeOf[B]()
	ctC := TypeOf[C]()
	lodCT := TypeOf[LOD]()
	for _, arch := range state.graph.ArchetypesWith(ctA, ctB, ctC, lodCT) {
		idxA, okA := arch.typeIndex[ctA]
		idxB, okB := arch.typeIndex[ctB]
		idxC, okC := arch.typeIndex[ctC]
		lodIdx, lodOk := arch.typeIndex[lodCT]
		if !okA || !okB || !okC || !lodOk {
			continue
		}
		for row := 0; row < arch.Len(); row++ {
			entityID := arch.EntityAt(row)
			lodComp := arch.columns[lodIdx][row]
			if lod, ok := lodComp.(LOD); ok && lod.Level == level {
				compA := arch.columns[idxA][row]
				compB := arch.columns[idxB][row]
				compC := arch.columns[idxC][row]
				typedA, okA := compA.(A)
				typedB, okB := compB.(B)
				typedC, okC := compC.(C)
				if okA && okB && okC {
					fn(entityID, &typedA, &typedB, &typedC)
					arch.columns[idxA][row] = typedA
					arch.columns[idxB][row] = typedB
					arch.columns[idxC][row] = typedC
				}
			}
		}
	}
}

// Query returns entities that have all requested component types.
// This is the legacy API, prefer ForEach variants for better performance.
func (s *WorldState) Query(types ...ComponentType) []EntityID {
	if len(types) == 0 {
		return s.Entities()
	}

	var ids []EntityID
	for _, arch := range s.graph.ArchetypesWith(types...) {
		for row := 0; row < arch.Len(); row++ {
			ids = append(ids, arch.EntityAt(row))
		}
	}
	return ids
}
