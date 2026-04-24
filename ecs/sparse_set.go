package ecs

// SparseSet implements a sparse set for fast ECS component storage and iteration.
type SparseSet struct {
	sparse []int
	dense  []EntityID
	data   []Component
}

// NewSparseSet creates a new initialized SparseSet.
func NewSparseSet() *SparseSet {
	return &SparseSet{
		sparse: make([]int, 0),
		dense:  make([]EntityID, 0),
		data:   make([]Component, 0),
	}
}

// Set adds or updates a component for the given entity.
func (s *SparseSet) Set(id EntityID, comp Component) {
	idx := int(id)
	if idx >= len(s.sparse) {
		newSparse := make([]int, idx+1+100) // grow with margin
		for i := range newSparse {
			if i < len(s.sparse) {
				newSparse[i] = s.sparse[i]
			} else {
				newSparse[i] = -1
			}
		}
		s.sparse = newSparse
	}

	denseIdx := s.sparse[idx]
	if denseIdx != -1 {
		s.data[denseIdx] = comp
		return
	}

	s.sparse[idx] = len(s.dense)
	s.dense = append(s.dense, id)
	s.data = append(s.data, comp)
}

// Get retrieves a component for the entity if it exists.
func (s *SparseSet) Get(id EntityID) (Component, bool) {
	idx := int(id)
	if idx >= len(s.sparse) {
		return nil, false
	}
	denseIdx := s.sparse[idx]
	if denseIdx == -1 {
		return nil, false
	}
	return s.data[denseIdx], true
}

// Remove deletes a component for the entity.
func (s *SparseSet) Remove(id EntityID) {
	idx := int(id)
	if idx >= len(s.sparse) || s.sparse[idx] == -1 {
		return
	}

	denseIdx := s.sparse[idx]
	lastIdx := len(s.dense) - 1

	if denseIdx != lastIdx {
		lastEntity := s.dense[lastIdx]
		s.dense[denseIdx] = lastEntity
		s.data[denseIdx] = s.data[lastIdx]
		s.sparse[int(lastEntity)] = denseIdx
	}

	s.dense = s.dense[:lastIdx]
	s.data = s.data[:lastIdx] // prevent memory leak if pointers
	s.sparse[idx] = -1
}

// Clone creates a shallow copy of the SparseSet.
func (s *SparseSet) Clone() *SparseSet {
	clone := &SparseSet{
		sparse: make([]int, len(s.sparse)),
		dense:  make([]EntityID, len(s.dense)),
		data:   make([]Component, len(s.data)),
	}
	copy(clone.sparse, s.sparse)
	copy(clone.dense, s.dense)
	copy(clone.data, s.data)
	return clone
}

// Entities returns the dense array of entity IDs that have this component.
func (s *SparseSet) Entities() []EntityID {
	return s.dense
}
