package core

import "github.com/mlange-42/ark/ecs"

// SpatialHash is a 2D uniform-grid spatial index for radius/AABB queries
// on entities. Lives in `core/` so callers from any package can register
// one as a resource without importing `systems/`.
//
// Invariants:
//
//   - One instance per Locomotion class (infantry / vehicle).
//   - Full per-tick rebuild (no incremental update). Rebuild runs serially
//     before any reader system so this tick's queries see freshly-snapped
//     positions.
//   - Backing slices are reused between rebuilds (cells map values are
//     truncated, not replaced) so steady-state runs allocate zero per tick.
//   - Stale-entity guard is the reader's responsibility. Between rebuild
//     and query an entity may have been removed; callbacks MUST check
//     world.Alive(ent) before using the entity.
//   - The cells map is read concurrently by parallel-aware reader systems.
//     Reads are race-safe because no writes happen during the parallel
//     section. Reader callbacks must not mutate the hash.

// SpatialEntry inlines position so callers don't need a posMap.Get inside
// the radius callback.
type SpatialEntry struct {
	Ent  ecs.Entity
	X, Z float32
}

type SpatialHash struct {
	cellSize float32
	invCell  float32
	// cells: packed-key -> indices into entries. Slices truncated (not
	// replaced) across rebuilds so capacity persists; map keys live forever
	// once seen.
	cells   map[int64][]int32
	entries []SpatialEntry
}

// NewSpatialHash returns an empty hash with the given cellSize (metres).
// cellSize should roughly match the worst-case query radius - too small
// forces queries to scan many cells; too large blurs the index toward O(N).
func NewSpatialHash(cellSize float32) *SpatialHash {
	if cellSize <= 0 {
		cellSize = 32.0
	}
	return &SpatialHash{
		cellSize: cellSize,
		invCell:  1.0 / cellSize,
		cells:    make(map[int64][]int32, 64),
		entries:  make([]SpatialEntry, 0, 64),
	}
}

func (h *SpatialHash) CellSize() float32 { return h.cellSize }

func (h *SpatialHash) Len() int { return len(h.entries) }

func packKey(cx, cz int32) int64 {
	return (int64(cx) << 32) | int64(uint32(cz))
}

// cellOf computes floor(world / cellSize). Standard int32 cast truncates
// toward zero; we need floor for negative coords so cells stay continuous
// across the origin.
func (h *SpatialHash) cellOf(world float32) int32 {
	v := world * h.invCell
	if v >= 0 {
		return int32(v)
	}
	iv := int32(v)
	if float32(iv) != v {
		iv--
	}
	return iv
}

// Rebuild replaces the index contents with snapshot. Call once per tick
// from a thin "rebuild" system before any reader runs. Reused entry and
// cell slices keep capacity across ticks.
func (h *SpatialHash) Rebuild(snapshot []SpatialEntry) {
	h.entries = h.entries[:0]
	for k := range h.cells {
		h.cells[k] = h.cells[k][:0]
	}
	for i := range snapshot {
		e := snapshot[i]
		h.entries = append(h.entries, e)
		cx := h.cellOf(e.X)
		cz := h.cellOf(e.Z)
		key := packKey(cx, cz)
		h.cells[key] = append(h.cells[key], int32(len(h.entries)-1))
	}
}

// ForEachInRadius invokes fn(ent, distSq) for every indexed entity whose
// XZ position is within r metres of (x, z). distSq is the squared XZ
// distance.
//
// Callback responsibilities:
//   - skip self (ent == queryingEntity).
//   - verify world.Alive(ent) - between rebuild and this query the entity
//     may have been removed.
//   - avoid mutating the hash inside the callback.
func (h *SpatialHash) ForEachInRadius(x, z, r float32, fn func(ent ecs.Entity, distSq float32)) {
	if fn == nil || r <= 0 || len(h.entries) == 0 {
		return
	}
	rSq := r * r
	minCX := h.cellOf(x - r)
	maxCX := h.cellOf(x + r)
	minCZ := h.cellOf(z - r)
	maxCZ := h.cellOf(z + r)
	for cz := minCZ; cz <= maxCZ; cz++ {
		for cx := minCX; cx <= maxCX; cx++ {
			indices := h.cells[packKey(cx, cz)]
			for _, idx := range indices {
				e := h.entries[idx]
				dx := e.X - x
				dz := e.Z - z
				dSq := dx*dx + dz*dz
				if dSq > rSq {
					continue
				}
				fn(e.Ent, dSq)
			}
		}
	}
}

// QueryInto appends every entity within r metres of (x, z) onto buf and
// returns the resulting slice. Same alive-check rule as ForEachInRadius -
// the returned slice may contain stale entries if Rebuild ran before
// death/despawn.
func (h *SpatialHash) QueryInto(x, z, r float32, buf []ecs.Entity) []ecs.Entity {
	h.ForEachInRadius(x, z, r, func(ent ecs.Entity, _ float32) {
		buf = append(buf, ent)
	})
	return buf
}

// ApproximateMemory returns a rough byte-count for debug HUD use.
func (h *SpatialHash) ApproximateMemory() int {
	const (
		sizeofEntry = 4 + 4 + 8
		sizeofIdx   = 4
	)
	bytes := len(h.entries) * sizeofEntry
	for _, s := range h.cells {
		bytes += len(s) * sizeofIdx
	}
	return bytes
}
