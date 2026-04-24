package ecs

import (
	"fmt"
	"strings"
)

// schedule holds the topologically sorted system indices per phase.
type schedule struct {
	order map[Phase][]int
}

// buildSchedule constructs a dependency graph from Reads/Writes declarations
// and returns a topologically sorted execution order per phase.
func buildSchedule(systems []systemEntry, phases []Phase) schedule {
	s := schedule{order: make(map[Phase][]int, len(phases))}

	for _, phase := range phases {
		// Collect indices of systems in this phase.
		var indices []int
		for i, entry := range systems {
			if entry.system.Phase() == phase {
				indices = append(indices, i)
			}
		}

		if len(indices) <= 1 {
			s.order[phase] = indices
			continue
		}

		n := len(indices)
		// inDegree[pos] = number of predecessors for indices[pos]
		inDegree := make([]int, n)
		// adj[pos] = list of positions that depend on indices[pos]
		adj := make([][]int, n)

		for a := 0; a < n; a++ {
			for b := 0; b < n; b++ {
				if a == b {
					continue
				}
				// If system A writes something that system B reads → edge A→B
				if overlaps(systems[indices[a]].system.Writes(), systems[indices[b]].system.Reads()) {
					adj[a] = append(adj[a], b)
					inDegree[b]++
				}
			}
		}

		// Kahn's algorithm with stable tie-breaking (lower original index first).
		sorted := make([]int, 0, n)
		for {
			// Find the first (by original index) node with in-degree 0.
			pick := -1
			for i := 0; i < n; i++ {
				if inDegree[i] == 0 {
					if pick == -1 || indices[i] < indices[pick] {
						pick = i
					}
				}
			}
			if pick == -1 {
				break
			}

			sorted = append(sorted, indices[pick])
			inDegree[pick] = -1 // mark as processed

			for _, dep := range adj[pick] {
				inDegree[dep]--
			}
		}

		if len(sorted) != n {
			// Cycle detected — collect participating systems for the panic message.
			var cycle []string
			for i := 0; i < n; i++ {
				if inDegree[i] >= 0 {
					cycle = append(cycle, systems[indices[i]].system.Name())
				}
			}
			panic(fmt.Sprintf("ecs: circular dependency in phase %d among systems: %s",
				phase, strings.Join(cycle, ", ")))
		}

		s.order[phase] = sorted
	}

	return s
}

// overlaps returns true if sets a and b share at least one ComponentType.
func overlaps(a, b []ComponentType) bool {
	for _, at := range a {
		for _, bt := range b {
			if at == bt {
				return true
			}
		}
	}
	return false
}
