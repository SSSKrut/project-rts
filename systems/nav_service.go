package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// NavService is the multi-chunk A* pathfinder. Service object (not a System);
// pre-built handles via NewNavService and reused across calls.
//
// Phase 6 ships one locomotion class (foot); per-class cost variants land in
// Phase 8. The API is forward-compatible: callers pass NavOpts.Locomotion and
// get the foot path until the planner learns about wheels and tracks.
type NavService struct {
	indexRes   ecs.Resource[TerrainChunkIndex]
	navGridMap *ecs.Map[components.NavGrid]
}

func NewNavService(w *ecs.World) *NavService {
	return &NavService{
		indexRes:   ecs.NewResource[TerrainChunkIndex](w),
		navGridMap: ecs.NewMap[components.NavGrid](w),
	}
}

// NavOpts is the per-call planner config. Keep small; pre-Phase-6 state
// is intentional — Phase 8 will widen it without breaking the call site.
type NavOpts struct {
	Locomotion components.Locomotion
}

// navMaxIter caps the number of cells A* will expand per call. A worst-case
// search across a fully loaded ring (11×11 chunks × 4096 = ~500K cells) is way
// over this; the cap is a safety net against pathological loops, not a tuning
// knob.
const navMaxIter = 50000

// navArrivalRadius — when walking the path returned by FindPath, the agent
// can pop a waypoint whenever it gets within this many metres of it. Used by
// the main loop's anchor walker; not consulted by the planner itself.
const navArrivalRadius float32 = 0.5

// FindPath returns waypoints (cell centres) along a least-cost path from
// `from` to `to`. Empty result = no path or target outside the loaded ring;
// nil = both endpoints fall in the same cell. No smoothing — that's the
// steering layer's job (Phase 7).
func (s *NavService) FindPath(from, to components.WorldPos, opts NavOpts) []components.WorldPos {
	_ = opts

	idx := s.indexRes.Get()
	if idx == nil {
		return []components.WorldPos{}
	}

	fromGI, fromGJ := worldPosToCell(from)
	toGI, toGJ := worldPosToCell(to)
	if fromGI == toGI && fromGJ == toGJ {
		return nil
	}

	// Reject impossible queries up front: target chunk must be loaded *and*
	// the target cell itself must be passable, otherwise A* would burn its
	// entire iteration budget exploring fluently around a sealed goal.
	cache := navGridCache{}
	if c, ok := cache.cellAt(idx, s.navGridMap, toGI, toGJ); !ok || c.Cost == 0 {
		return []components.WorldPos{}
	}
	if c, ok := cache.cellAt(idx, s.navGridMap, fromGI, fromGJ); !ok || c.Cost == 0 {
		return []components.WorldPos{}
	}

	type state struct {
		g                float32
		parentI, parentJ int32
		hasParent        bool
	}
	states := map[navGlobalCell]state{}
	closed := map[navGlobalCell]bool{}

	startCell := navGlobalCell{fromGI, fromGJ}
	states[startCell] = state{g: 0}

	open := navHeap{}
	open.push(navHeapEntry{f: navHeuristic(fromGI, fromGJ, toGI, toGJ), gi: fromGI, gj: fromGJ})

	const sqrt2 float32 = 1.41421356

	type neighbourOffset struct {
		di, dj int32
		step   float32
	}
	neighbours := [8]neighbourOffset{
		{1, 0, 1}, {-1, 0, 1}, {0, 1, 1}, {0, -1, 1},
		{1, 1, sqrt2}, {1, -1, sqrt2}, {-1, 1, sqrt2}, {-1, -1, sqrt2},
	}

	found := false
	for iter := 0; iter < navMaxIter && open.len() > 0; iter++ {
		cur := open.pop()
		gi, gj := cur.gi, cur.gj
		if gi == toGI && gj == toGJ {
			found = true
			break
		}
		gc := navGlobalCell{gi, gj}
		if closed[gc] {
			continue
		}
		closed[gc] = true
		curG := states[gc].g

		for _, off := range neighbours {
			ni, nj := gi+off.di, gj+off.dj
			ngc := navGlobalCell{ni, nj}
			if closed[ngc] {
				continue
			}
			cell, ok := cache.cellAt(idx, s.navGridMap, ni, nj)
			if !ok || cell.Cost == 0 {
				continue
			}
			tentativeG := curG + float32(cell.Cost)*off.step
			if existing, has := states[ngc]; has && tentativeG >= existing.g {
				continue
			}
			states[ngc] = state{g: tentativeG, parentI: gi, parentJ: gj, hasParent: true}
			open.push(navHeapEntry{
				f:  tentativeG + navHeuristic(ni, nj, toGI, toGJ),
				gi: ni, gj: nj,
			})
		}
	}

	if !found {
		return []components.WorldPos{}
	}

	// Reconstruct from goal to start, then reverse.
	var cells []navGlobalCell
	ci, cj := toGI, toGJ
	for {
		cells = append(cells, navGlobalCell{ci, cj})
		st := states[navGlobalCell{ci, cj}]
		if !st.hasParent {
			break
		}
		ci, cj = st.parentI, st.parentJ
	}
	waypoints := make([]components.WorldPos, len(cells))
	for i, gc := range cells {
		waypoints[len(cells)-1-i] = cellToWorldPos(gc.gi, gc.gj)
	}

	// Post-A* string-pulling: drop every waypoint that the agent can reach
	// directly from its predecessor with a clear, equally-cheap line. Restores
	// natural diagonal motion through open ground while still respecting the
	// cost contour A* picked (a trench-avoiding detour stays detoured).
	return s.smoothPath(waypoints)
}

// smoothPath collapses a cell-by-cell A* result into corner-only waypoints by
// LOS-rasterising the line between candidates and dropping intermediates whose
// removal doesn't cut through impassable terrain or *more expensive* cells
// than the original sub-path crossed.
//
// The cost-aware rule is what distinguishes this from a generic string-pulling
// pass: a path that A* routed around a trench (Cost=16, navCostTrench) won't
// be re-routed through the trench just because LOS happens to be clear — we
// require lineMax ≤ runMax over the bypassed sub-path. Without that, the
// smoother would undo A*'s cost-preference work in M6.4.
//
// The leading waypoint (start cell centre) is intentionally dropped: the
// caller's anchor is already approximately there, and the path-walker would
// otherwise spend its first metres lateral-correcting onto the cell centre.
func (s *NavService) smoothPath(path []components.WorldPos) []components.WorldPos {
	if len(path) <= 1 {
		return path
	}
	idx := s.indexRes.Get()
	if idx == nil {
		return path
	}

	cache := navGridCache{}
	costs := make([]uint8, len(path))
	for i, wp := range path {
		gi, gj := worldPosToCell(wp)
		if c, ok := cache.cellAt(idx, s.navGridMap, gi, gj); ok {
			costs[i] = c.Cost
		}
	}

	out := make([]components.WorldPos, 0, len(path))
	anchor := 0
	for anchor < len(path)-1 {
		next := anchor + 1
		runMax := costs[next]
		for j := anchor + 2; j < len(path); j++ {
			if costs[j] > runMax {
				runMax = costs[j]
			}
			lineMax, ok := s.lineMaxCost(path[anchor], path[j], idx, &cache)
			if !ok || lineMax > runMax {
				break
			}
			next = j
		}
		out = append(out, path[next])
		anchor = next
	}
	return out
}

// lineMaxCost rasterises the world-XZ line a→b at fixed step and returns the
// maximum NavCell.Cost touched along the way, with `ok=false` on any
// impassable (Cost=0) or unloaded-chunk sample. Step is sub-cell (0.25 m =
// 4× per cell) so a line tangent to a wall corner still catches the wall.
func (s *NavService) lineMaxCost(a, b components.WorldPos, idx *TerrainChunkIndex, cache *navGridCache) (uint8, bool) {
	aWX, aWZ := worldXZ(a)
	bWX, bWZ := worldXZ(b)
	dx := bWX - aWX
	dz := bWZ - aWZ
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if dist <= 0 {
		return 0, true
	}
	const sampleStep float32 = 0.25
	samples := int(math.Ceil(float64(dist / sampleStep)))
	if samples < 1 {
		samples = 1
	}
	var maxCost uint8 = 0
	for k := 0; k <= samples; k++ {
		t := float32(k) / float32(samples)
		sx := aWX + t*dx
		sz := aWZ + t*dz
		gi := int32(math.Floor(float64(sx)))
		gj := int32(math.Floor(float64(sz)))
		cell, ok := cache.cellAt(idx, s.navGridMap, gi, gj)
		if !ok || cell.Cost == 0 {
			return 0, false
		}
		if cell.Cost > maxCost {
			maxCost = cell.Cost
		}
	}
	return maxCost, true
}

// navHeuristic — Chebyshev distance × min cell cost, in the same units the
// gScore uses. Admissible: actual cost = Σ cellCost × step ≥ minCost × cells.
// minCost in Phase 6 is navCostRoad (2).
func navHeuristic(fromI, fromJ, toI, toJ int32) float32 {
	di := fromI - toI
	if di < 0 {
		di = -di
	}
	dj := fromJ - toJ
	if dj < 0 {
		dj = -dj
	}
	cheb := di
	if dj > cheb {
		cheb = dj
	}
	return float32(cheb) * float32(navCostRoad)
}

// navGlobalCell is a (chunk-aware) cell coordinate covering the entire world.
// gi = chunk.X*NavGridSide + i; gj = chunk.Z*NavGridSide + j.
type navGlobalCell struct {
	gi, gj int32
}

// navHeapEntry is one slot in the open set, keyed by f-score.
type navHeapEntry struct {
	f      float32
	gi, gj int32
}

// navHeap is a min-heap on f. Stay-local rather than container/heap to avoid
// the interface boxing — A* hits this in the inner loop.
type navHeap struct {
	entries []navHeapEntry
}

func (h *navHeap) len() int { return len(h.entries) }

func (h *navHeap) push(e navHeapEntry) {
	h.entries = append(h.entries, e)
	h.siftUp(len(h.entries) - 1)
}

func (h *navHeap) pop() navHeapEntry {
	top := h.entries[0]
	last := len(h.entries) - 1
	h.entries[0] = h.entries[last]
	h.entries = h.entries[:last]
	if len(h.entries) > 0 {
		h.siftDown(0)
	}
	return top
}

func (h *navHeap) siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if h.entries[p].f <= h.entries[i].f {
			return
		}
		h.entries[p], h.entries[i] = h.entries[i], h.entries[p]
		i = p
	}
}

func (h *navHeap) siftDown(i int) {
	n := len(h.entries)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		r := l + 1
		s := l
		if r < n && h.entries[r].f < h.entries[l].f {
			s = r
		}
		if h.entries[s].f >= h.entries[i].f {
			return
		}
		h.entries[i], h.entries[s] = h.entries[s], h.entries[i]
		i = s
	}
}

// navGridCache is a tiny per-call lookup cache: A* reads the same chunk's grid
// for many consecutive cells, so caching the most recent few avoids a hashmap
// hit every step. Capacity 4 covers all four chunks meeting at a corner.
type navGridCache struct {
	entries [4]struct {
		cc    components.ChunkCoord
		grid  *components.NavGrid
		valid bool
	}
}

func (c *navGridCache) cellAt(idx *TerrainChunkIndex, gridMap *ecs.Map[components.NavGrid],
	gi, gj int32) (components.NavCell, bool) {
	// NavGridSide == 64 == 2^navGridShift; arithmetic right shift gives the
	// correct floor-division for negative gi/gj as well (Go's >> on signed
	// ints sign-extends).
	cc := components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift}
	var grid *components.NavGrid
	for k := range c.entries {
		if c.entries[k].valid && c.entries[k].cc == cc {
			grid = c.entries[k].grid
			break
		}
	}
	if grid == nil {
		ent, ok := idx.Loaded[cc]
		if !ok {
			return components.NavCell{}, false
		}
		grid = gridMap.Get(ent)
		if grid == nil {
			return components.NavCell{}, false
		}
		// Evict oldest slot (round-robin via the first invalid; if all valid,
		// reuse slot 0 — cache locality matters far more than perfect LRU at
		// this size).
		slot := -1
		for k := range c.entries {
			if !c.entries[k].valid {
				slot = k
				break
			}
		}
		if slot < 0 {
			slot = 0
		}
		c.entries[slot].cc = cc
		c.entries[slot].grid = grid
		c.entries[slot].valid = true
	}
	li := int(gi & navGridMask)
	lj := int(gj & navGridMask)
	return grid.Cells[lj*components.NavGridSide+li], true
}

// navGridShift / navGridMask — NavGridSide is fixed at 64 (1 m cells, 64 m
// chunk). Compile-time bit ops decode (gi, gj) into (chunk, local) without
// signed-mod surprises near negative coordinates.
const (
	navGridShift = 6
	navGridMask  = components.NavGridSide - 1
)

// worldPosToCell — global (gi, gj) cell index of a WorldPos. NavGrid step is
// 1 m and ChunkSize is 64 m, so the cell index of the chunk's (Local.X = 0)
// edge is exactly chunk.X << navGridShift.
func worldPosToCell(p components.WorldPos) (int32, int32) {
	gi := p.Chunk.X<<navGridShift + int32(math.Floor(float64(p.Local.X)))
	gj := p.Chunk.Z<<navGridShift + int32(math.Floor(float64(p.Local.Z)))
	return gi, gj
}

// cellToWorldPos — centre-of-cell WorldPos. Y is the procgen surface so the
// waypoint marker hovers visibly above the ground; the anchor walker overwrites
// Y via GroundStickSystem each tick anyway.
func cellToWorldPos(gi, gj int32) components.WorldPos {
	cc := components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift}
	li := float32(gi&navGridMask) + 0.5
	lj := float32(gj&navGridMask) + 0.5
	wx := float32(cc.X)*components.ChunkSize + li
	wz := float32(cc.Z)*components.ChunkSize + lj
	return components.WorldPos{
		Chunk: cc,
		Local: rl.Vector3{X: li, Y: GroundHeight(wx, wz), Z: lj},
	}
}
