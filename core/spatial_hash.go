package core

import "github.com/mlange-42/ark/ecs"

// Phase 14.5 P4 - SpatialHash is a generic 2D uniform-grid spatial index for
// radius/AABB queries on entities. Lives in `core/` so callers from any
// package can register one as a resource without taking an import on
// `systems/`.
//
// Design choices (PHASE-14.5.md P4/P5/P6):
//
//   - One instance per Locomotion class. Phase 14.5 ships a single
//     `unitSpatialHash` for infantry. Phase 16 adds a second
//     `vehicleSpatialHash` with a coarser cellSize; both query through the
//     same API.
//   - Full per-tick rebuild (no incremental update). Rebuild runs serially
//     before UnitMovement so this tick's separation steering sees the
//     freshly-snapped positions.
//   - Backing slices are reused between rebuilds (cells map values are
//     truncated, not replaced) so steady-state runs allocate zero per tick.
//   - Stale-entity guard is the reader's responsibility. Between rebuild and
//     query, an entity may have been removed (DamageService.ApplyDeath).
//     Callbacks MUST check `world.Alive(ent)` before using the entity.
//   - The cells map is read concurrently by parallel-aware reader systems
//     (WeaponSystem). Reads are race-safe because no writes happen during
//     the parallel section. Reader callbacks must not mutate the hash.
//
// Adding a second SpatialHash for Phase 16 (vehicles):
//
//	vehicleHash := core.NewSpatialHash(48.0)
//	ecs.AddResource(world, vehicleHash)
//	// In the rebuild system: snapshot Filter2[Vehicle, WorldPos] into a
//	// separate buffer and call vehicleHash.Rebuild(buffer).
//	// Vehicle-readers query vehicleHash; infantry readers continue with
//	// the unit hash.
//
// Cells map key uses a packed int64 for zero per-query allocation; cell
// coordinates derive from worldX / cellSize, worldZ / cellSize using
// `floorDiv` to keep negative coordinates well-behaved.

// SpatialEntry - one entity in the hash. Position is inlined so callers
// don't need a posMap.Get inside the radius callback.
type SpatialEntry struct {
	Ent  ecs.Entity
	X, Z float32
}

// SpatialHash is a uniform-grid index built from a flat slice of entries.
// Cells are addressed by (cx, cz) integer pairs derived from world XZ.
type SpatialHash struct {
	cellSize float32
	invCell  float32
	// cells: packed-key -> indices into `entries`. Reuse cell slices across
	// rebuilds by truncating to len 0 (capacity preserved). Map keys live
	// forever once seen; map.Clear walks all keys but doesn't free buckets.
	cells map[int64][]int32
	// entries: flat snapshot. Rebuild copies the caller's slice in.
	entries []SpatialEntry
}

// NewSpatialHash returns an empty hash with the given cellSize (metres).
// cellSize should roughly match the worst-case query radius - too small
// forces queries to scan many cells; too large blurs the index toward O(N).
// PHASE-14.5.md P4 picks 32 m for Units (separation query radius ~1.5 m,
// vision range ~64 m -> 2x2 cells; weapon range ~300 m -> ~10x10 cells, still
// far below O(N) for 200 units).
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

// CellSize returns the configured cell size in metres.
func (h *SpatialHash) CellSize() float32 { return h.cellSize }

// Len returns the number of entries currently indexed.
func (h *SpatialHash) Len() int { return len(h.entries) }

// packKey turns a cell coordinate pair into a single int64 map key. XZ
// components are int32; high bits get X, low bits get Z. Negative coords
// survive thanks to int32 sign extension.
func packKey(cx, cz int32) int64 {
	return (int64(cx) << 32) | int64(uint32(cz))
}

// floorDiv computes floor(value / cellSize) as an int32. Standard cast
// truncates toward zero; we need floor for negative coords so cells stay
// continuous across the origin.
func (h *SpatialHash) cellOf(world float32) int32 {
	v := world * h.invCell
	if v >= 0 {
		return int32(v)
	}
	// floor for negatives.
	iv := int32(v)
	if float32(iv) != v {
		iv--
	}
	return iv
}

// Rebuild replaces the index contents with `snapshot`. The previous entry
// slice is reused (truncated to 0) when possible; per-cell index slices are
// truncated in place so the underlying capacity persists across ticks. Call
// this once per tick from a thin "rebuild" system before any reader runs.
func (h *SpatialHash) Rebuild(snapshot []SpatialEntry) {
	// Reset entries (capacity preserved).
	h.entries = h.entries[:0]
	// Truncate every cell slice; keep the underlying capacity for reuse.
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

// ForEachInRadius invokes `fn(ent, distSq)` for every indexed entity whose
// XZ position is within `r` metres of (x, z). Walks every cell whose AABB
// could contain a point in the radius (a conservative cell footprint -
// some cells may have zero hits).
//
// `distSq` is the squared XZ distance from the query point to the entity.
// Callback responsibilities:
//   - skip self (`ent == queryingEntity`).
//   - verify `world.Alive(ent)` - between rebuild and this query the entity
//     may have been removed.
//   - avoid mutating the hash inside the callback.
//
// Zero-alloc: the callback is invoked directly from the iteration loop, no
// intermediate slice is built.
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

// QueryInto appends every entity within `r` metres of (x, z) onto `buf` and
// returns the resulting slice. Convenience wrapper for callsites that want
// a slice rather than a callback. Caller passes a reusable scratch buffer
// (typically a struct field reset with `buf = buf[:0]`).
//
// Same alive-check rule applies - the returned slice may contain stale
// entries if Rebuild ran before death/despawn.
func (h *SpatialHash) QueryInto(x, z, r float32, buf []ecs.Entity) []ecs.Entity {
	h.ForEachInRadius(x, z, r, func(ent ecs.Entity, _ float32) {
		buf = append(buf, ent)
	})
	return buf
}

// ApproximateMemory returns a rough byte-count for debug HUD use. Counts
// entry slice + cell slice headers + cell map buckets. Approximate, not
// exact.
func (h *SpatialHash) ApproximateMemory() int {
	const (
		sizeofEntry = 4 + 4 + 8 // X, Z, ecs.Entity (assume 8B)
		sizeofIdx   = 4         // int32
	)
	bytes := len(h.entries) * sizeofEntry
	for _, s := range h.cells {
		bytes += len(s) * sizeofIdx
	}
	return bytes
}
