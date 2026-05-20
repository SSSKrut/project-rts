package systems

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Phase 16.B.1.b debug: print first empty FindPath result and first
// successful FindPath so we can tell empty-path bug from wrong-path bug.
var navDebugReported bool
var navOKReported bool
var navSnapshotReported bool

// NavService is the multi-graph A* pathfinder. Service object (not a System);
// pre-built handles via NewNavService and reused across calls.
//
// Phase 7 widens the planner past per-chunk surface NavGrids: each Floor
// entity also carries a LevelNavGrid, and surface<->floor / floor<->floor
// transitions live in a TransitionRegistry resource. WorldPos->NavNode
// resolution checks floor footprint membership first; in-grid neighbours +
// registry edges are unified in one A* expansion.
type NavService struct {
	indexRes      ecs.Resource[TerrainChunkIndex]
	transitionRes ecs.Resource[components.TransitionRegistry]
	buildingIndex ecs.Resource[BuildingChildIndex]
	navGridMap    *ecs.Map[components.NavGrid]
	floorNavMap   *ecs.Map[components.LevelNavGrid]
	levelMap      *ecs.Map[components.Level]
	posMap        *ecs.Map[components.WorldPos]
	levelFilter   *ecs.Filter2[components.Level, components.WorldPos]
}

func NewNavService(w *ecs.World) *NavService {
	return &NavService{
		indexRes:      ecs.NewResource[TerrainChunkIndex](w),
		transitionRes: ecs.NewResource[components.TransitionRegistry](w),
		buildingIndex: ecs.NewResource[BuildingChildIndex](w),
		navGridMap:    ecs.NewMap[components.NavGrid](w),
		floorNavMap:   ecs.NewMap[components.LevelNavGrid](w),
		levelMap:      ecs.NewMap[components.Level](w),
		posMap:        ecs.NewMap[components.WorldPos](w),
		levelFilter:   ecs.NewFilter2[components.Level, components.WorldPos](w),
	}
}

// NavOpts is the per-call planner config.
type NavOpts struct {
	Locomotion components.Locomotion
	// AvoidOpenedDoors - Phase 12 stealth stub; ignored in Phase 7.
	AvoidOpenedDoors bool
	// PathStyle is the per-squad routing preference (PHASE-13.md P4). The
	// modifier table is applied as a float multiplier on NavCell.Cost
	// during A* expansion. Default Direct = x1.0 (no change).
	PathStyle components.PathStyle
}

// navMaxIter caps the number of cells A* will expand per call.
const navMaxIter = 50000

// navArrivalRadius - when walking the path returned by FindPath, the agent
// can pop a waypoint whenever it gets within this many metres of it.
const navArrivalRadius float32 = 0.5

// FindPath returns waypoints (cell centres) along a least-cost path from
// `from` to `to`. Empty result = no path; nil = both endpoints fall in the
// same cell. The multi-graph A* expansion picks up surface<->floor transitions
// from the TransitionRegistry resource so a path through a doorway or up a
// staircase just works.
//
// PHASE-13.md P4: opts.PathStyle scales NavCell.Cost via styleCellCost. Direct
// is a no-op; RoadPrefer / RoadAvoid bias by NavOnRoad flag; CoverSeek biases
// by NavCell.CoverDistance (baked in M13.4 SpatialBakeSystem Pass 2).
func (s *NavService) FindPath(from, to components.WorldPos, opts NavOpts) []components.WorldPos {
	idx := s.indexRes.Get()
	if idx == nil {
		return []components.WorldPos{}
	}
	registry := s.transitionRes.Get()

	floors := s.snapshotLevels()
	// Phase 16.B.1.b diagnostic: spot-check the door surface cell of House #0
	// (43, 24) in chunk(-1,-1). If it's cost=0 or NavInBuilding-flagged, the
	// transition edge is reachable in the registry but not via gridNeighbours.
	if !navSnapshotReported {
		doorSurf := components.NavNode{Kind: components.NodeSurface,
			Chunk: components.ChunkCoord{X: -1, Z: -1}, I: 43, J: 24}
		doorCell, doorOK := s.cellAt(doorSurf, idx, floors)
		fmt.Printf("[nav] doorSurfCell(43,24): ok=%v cost=%d flags=%d\n", doorOK, doorCell.Cost, doorCell.Flags)
		// Same for (43, 23) and (43, 25) - adjacent cells; if (43, 24) is OK
		// but unreachable, neighbour state matters.
		for j := int16(19); j <= 25; j++ {
			n := components.NavNode{Kind: components.NodeSurface,
				Chunk: components.ChunkCoord{X: -1, Z: -1}, I: 43, J: j}
			c, ok := s.cellAt(n, idx, floors)
			fmt.Printf("[nav] cell I=43 J=%d ok=%v cost=%d flags=%d\n", j, ok, c.Cost, c.Flags)
		}
	}
	if !navSnapshotReported {
		for i, lr := range floors {
			fmt.Printf("[nav] snapshot[%d] ent=%v chunk=%v origin=(%.1f,%.1f) size=%dx%d aabbY=[%.1f..%.1f]\n",
				i, lr.ent, lr.chunk, lr.originX, lr.originZ, lr.sizeX, lr.sizeZ, lr.aabb.MinY, lr.aabb.MaxY)
		}
		navSnapshotReported = true
	}

	fromNode, fromOK := s.resolveNode(from, idx, floors)
	toNode, toOK := s.resolveNode(to, idx, floors)
	if !fromOK || !toOK {
		fmt.Printf("[nav] EMPTY: resolve failed fromOK=%v toOK=%v to=(%.1f,%.1f)\n",
			fromOK, toOK,
			to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
			to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize)
		return []components.WorldPos{}
	}
	if fromNode == toNode {
		fmt.Printf("[nav] SAME node: kind=%d I=%d J=%d (from==to, no path needed)\n",
			fromNode.Kind, fromNode.I, fromNode.J)
		return nil
	}

	fromCell, fromCellOK := s.cellAt(fromNode, idx, floors)
	if !fromCellOK || fromCell.Cost == 0 {
		fmt.Printf("[nav] EMPTY: from blocked kind=%d cost=%d flags=%d\n",
			fromNode.Kind, fromCell.Cost, fromCell.Flags)
		return []components.WorldPos{}
	}
	toCell, toCellOK := s.cellAt(toNode, idx, floors)
	if !toCellOK || toCell.Cost == 0 {
		fmt.Printf("[nav] EMPTY: to blocked kind=%d cost=%d flags=%d to=(%.1f,%.1f)\n",
			toNode.Kind, toCell.Cost, toCell.Flags,
			to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
			to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize)
		return []components.WorldPos{}
	}

	type stateRec struct {
		g         float32
		parent    components.NavNode
		hasParent bool
	}
	states := map[components.NavNode]stateRec{}
	closed := map[components.NavNode]bool{}
	states[fromNode] = stateRec{g: 0}

	toWP := s.nodeWorldPos(toNode, floors)

	open := nodeHeap{}
	open.push(nodeHeapEntry{f: nodeHeuristic(s.nodeWorldPos(fromNode, floors), toWP), node: fromNode})

	const sqrt2 float32 = 1.41421356

	found := false
	for iter := 0; iter < navMaxIter && open.len() > 0; iter++ {
		cur := open.pop()
		if cur.node == toNode {
			found = true
			break
		}
		if closed[cur.node] {
			continue
		}
		closed[cur.node] = true
		curG := states[cur.node].g

		// In-grid 8 neighbours.
		neighbours := s.gridNeighbours(cur.node)
		for _, n := range neighbours {
			if closed[n.node] {
				continue
			}
			cell, ok := s.cellAt(n.node, idx, floors)
			if !ok || cell.Cost == 0 {
				continue
			}
			step := float32(1)
			if n.diag {
				step = sqrt2
			}
			tentativeG := curG + styleCellCost(cell, opts.PathStyle)*step
			if existing, has := states[n.node]; has && tentativeG >= existing.g {
				continue
			}
			states[n.node] = stateRec{g: tentativeG, parent: cur.node, hasParent: true}
			open.push(nodeHeapEntry{
				f:    tentativeG + nodeHeuristic(s.nodeWorldPos(n.node, floors), toWP),
				node: n.node,
			})
		}

		// Cross-graph transitions.
		if registry != nil {
			for _, edge := range registry.Out[cur.node] {
				if edge.Cost == 0 {
					continue
				}
				if closed[edge.To] {
					continue
				}
				tentativeG := curG + float32(edge.Cost)
				if existing, has := states[edge.To]; has && tentativeG >= existing.g {
					continue
				}
				states[edge.To] = stateRec{g: tentativeG, parent: cur.node, hasParent: true}
				open.push(nodeHeapEntry{
					f:    tentativeG + nodeHeuristic(s.nodeWorldPos(edge.To, floors), toWP),
					node: edge.To,
				})
			}
		}
	}

	if !found {
		closedCount := len(closed)
		statesCount := len(states)
		surfaceClosed := 0
		levelClosed := 0
		levelMatchClosed := 0
		doorSurfClosed := false
		goalCellClosed := false
		for n := range closed {
			switch n.Kind {
			case components.NodeSurface:
				surfaceClosed++
				if n.I == 43 && n.J == 24 && n.Chunk.X == -1 && n.Chunk.Z == -1 {
					doorSurfClosed = true
				}
			case components.NodeLevel:
				levelClosed++
				if n.Level == toNode.Level {
					levelMatchClosed++
					if n.I == toNode.I && n.J == toNode.J {
						goalCellClosed = true
					}
				}
			}
		}
		fmt.Printf("[nav] NOT FOUND closed=%d (surf=%d level=%d sameLevel=%d) openLen=%d doorSurfClosed=%v goalClosed=%v\n",
			closedCount, surfaceClosed, levelClosed, levelMatchClosed,
			open.len(), doorSurfClosed, goalCellClosed)
		fmt.Printf("[nav]     from=%+v to=%+v states=%d\n", fromNode, toNode, statesCount)
		return []components.WorldPos{}
	}

	// Reconstruct goal->start, then reverse into start->goal.
	var nodes []components.NavNode
	cur := toNode
	for {
		nodes = append(nodes, cur)
		st := states[cur]
		if !st.hasParent {
			break
		}
		cur = st.parent
	}
	waypoints := make([]components.WorldPos, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0; i-- {
		waypoints = append(waypoints, s.nodeWorldPos(nodes[i], floors))
	}
	// Drop the leading waypoint (start cell centre) - the caller's agent is
	// already there; the walker would otherwise spend its first metres
	// lateral-correcting onto the cell centre.
	if len(waypoints) > 1 {
		waypoints = waypoints[1:]
	}
	usesTransition := false
	for _, n := range nodes {
		if n.Kind == components.NodeLevel {
			usesTransition = true
			break
		}
	}
	fmt.Printf("[nav] OK len=%d via-level=%v to=(%.1f,%.1f) toKind=%d\n",
		len(waypoints), usesTransition,
		to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
		to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize,
		toNode.Kind)
	return waypoints
}

// levelRec - per-Level lookup record used during one FindPath call.
// Phase 16.B.1.b: the navigation grid is now anchored on a Level entity,
// one per interior volume; floors are render-only.
type levelRec struct {
	ent     ecs.Entity
	chunk   components.ChunkCoord
	aabb    components.AABB3D
	originX float32 // chunk-local origin of the grid (= AABB.MinX - chunkBaseX)
	originZ float32
	sizeX   uint8
	sizeZ   uint8
	grid    *components.LevelNavGrid
}

// snapshotLevels walks every Level entity that already has a baked
// LevelNavGrid and returns a per-call slice for resolve / neighbour
// expansion. Level entities are AlwaysActive so they aren't tied to a
// BuildingChildIndex - we filter directly.
func (s *NavService) snapshotLevels() []levelRec {
	var out []levelRec
	q := s.levelFilter.Query()
	for q.Next() {
		lvl, pos := q.Get()
		grid := s.floorNavMap.Get(q.Entity())
		if grid == nil {
			continue
		}
		chunkBaseX := float32(pos.Chunk.X) * components.ChunkSize
		chunkBaseZ := float32(pos.Chunk.Z) * components.ChunkSize
		out = append(out, levelRec{
			ent:     q.Entity(),
			chunk:   pos.Chunk,
			aabb:    lvl.AABB,
			originX: lvl.AABB.MinX - chunkBaseX,
			originZ: lvl.AABB.MinZ - chunkBaseZ,
			sizeX:   grid.SizeX,
			sizeZ:   grid.SizeZ,
			grid:    grid,
		})
	}
	return out
}

// resolveNode maps a WorldPos to a NavNode. If the position falls inside a
// Level's AABB (XZ + Y proximity), the NavNode is NodeLevel. Otherwise it's
// NodeSurface, derived from the global cell math.
func (s *NavService) resolveNode(wp components.WorldPos, idx *TerrainChunkIndex, levels []levelRec) (components.NavNode, bool) {
	worldX := wp.Local.X + float32(wp.Chunk.X)*components.ChunkSize
	worldZ := wp.Local.Z + float32(wp.Chunk.Z)*components.ChunkSize
	const yPad float32 = 0.6
	for i := range levels {
		lr := &levels[i]
		if !lr.aabb.ContainsXZ(worldX, worldZ) {
			continue
		}
		if wp.Local.Y < lr.aabb.MinY-yPad || wp.Local.Y > lr.aabb.MaxY+yPad {
			continue
		}
		gridChunkBaseX := float32(lr.chunk.X) * components.ChunkSize
		gridChunkBaseZ := float32(lr.chunk.Z) * components.ChunkSize
		lx := worldX - (gridChunkBaseX + lr.originX)
		lz := worldZ - (gridChunkBaseZ + lr.originZ)
		if lx < 0 || lz < 0 {
			continue
		}
		ci := int16(math.Floor(float64(lx)))
		cj := int16(math.Floor(float64(lz)))
		if ci < 0 || ci >= int16(lr.sizeX) || cj < 0 || cj >= int16(lr.sizeZ) {
			continue
		}
		return components.NavNode{Kind: components.NodeLevel, Level: lr.ent, I: ci, J: cj}, true
	}
	gi, gj := worldPosToCell(wp)
	cc := components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift}
	if _, ok := idx.Loaded[cc]; !ok {
		return components.NavNode{}, false
	}
	return components.NavNode{
		Kind:  components.NodeSurface,
		Chunk: cc,
		I:     int16(gi & navGridMask),
		J:     int16(gj & navGridMask),
	}, true
}

// cellAt returns the NavCell for a NavNode. (false) if the host grid is not
// loaded.
//
// Phase 14.6 M14.6.1: surface cells flagged NavInBuilding are reported
// inaccessible. The interior of a building can only be reached through a
// TransitionEdge (Door / Stairs) that lands the path on a Floor NavNode;
// pure surface expansion must go around the footprint.
func (s *NavService) cellAt(n components.NavNode, idx *TerrainChunkIndex, floors []levelRec) (components.NavCell, bool) {
	switch n.Kind {
	case components.NodeSurface:
		ent, ok := idx.Loaded[n.Chunk]
		if !ok {
			return components.NavCell{}, false
		}
		grid := s.navGridMap.Get(ent)
		if grid == nil {
			return components.NavCell{}, false
		}
		if n.I < 0 || n.I >= components.NavGridSide || n.J < 0 || n.J >= components.NavGridSide {
			return components.NavCell{}, false
		}
		cell := grid.Cells[int(n.J)*components.NavGridSide+int(n.I)]
		if cell.Flags&components.NavInBuilding != 0 {
			return cell, false
		}
		return cell, true
	case components.NodeLevel:
		for i := range floors {
			if floors[i].ent != n.Level {
				continue
			}
			fr := &floors[i]
			if n.I < 0 || n.I >= int16(fr.sizeX) || n.J < 0 || n.J >= int16(fr.sizeZ) {
				return components.NavCell{}, false
			}
			return fr.grid.Cells[int(n.J)*components.MaxLevelSide+int(n.I)], true
		}
	}
	return components.NavCell{}, false
}

// nodeWorldPos returns the centre-of-cell WorldPos for a NavNode. Used by the
// heuristic, path reconstruction, and the walker.
func (s *NavService) nodeWorldPos(n components.NavNode, floors []levelRec) components.WorldPos {
	switch n.Kind {
	case components.NodeSurface:
		gi := int32(n.Chunk.X)<<navGridShift + int32(n.I)
		gj := int32(n.Chunk.Z)<<navGridShift + int32(n.J)
		return cellToWorldPos(gi, gj)
	case components.NodeLevel:
		for i := range floors {
			if floors[i].ent != n.Level {
				continue
			}
			fr := &floors[i]
			lx := fr.originX + float32(n.I) + 0.5
			lz := fr.originZ + float32(n.J) + 0.5
			return components.WorldPos{
				Chunk: fr.chunk,
				Local: rl.Vector3{X: lx, Y: fr.aabb.MinY, Z: lz},
			}
		}
	}
	return components.WorldPos{}
}

// gridNeighbour - output of gridNeighbours, packs target NavNode + a flag
// telling A* whether the move is diagonal (sqrt2 step) or cardinal (1 m).
type gridNeighbour struct {
	node components.NavNode
	diag bool
}

// gridNeighbours enumerates the 8 cells around a node *inside the same grid*.
// Surface nodes overflow into neighbouring chunks via the global-cell math;
// floor nodes are clamped to the grid's SizeX x SizeZ block.
func (s *NavService) gridNeighbours(n components.NavNode) []gridNeighbour {
	var out [8]gridNeighbour
	offsets := [8][3]int{
		{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0},
		{1, 1, 1}, {1, -1, 1}, {-1, 1, 1}, {-1, -1, 1},
	}
	count := 0
	switch n.Kind {
	case components.NodeSurface:
		gi := int32(n.Chunk.X)<<navGridShift + int32(n.I)
		gj := int32(n.Chunk.Z)<<navGridShift + int32(n.J)
		for _, off := range offsets {
			ni := gi + int32(off[0])
			nj := gj + int32(off[1])
			nc := components.ChunkCoord{X: ni >> navGridShift, Z: nj >> navGridShift}
			out[count] = gridNeighbour{
				node: components.NavNode{
					Kind:  components.NodeSurface,
					Chunk: nc,
					I:     int16(ni & navGridMask),
					J:     int16(nj & navGridMask),
				},
				diag: off[2] == 1,
			}
			count++
		}
	case components.NodeLevel:
		for _, off := range offsets {
			ni := int16(int(n.I) + off[0])
			nj := int16(int(n.J) + off[1])
			out[count] = gridNeighbour{
				node: components.NavNode{
					Kind: components.NodeLevel, Level: n.Level, I: ni, J: nj,
				},
				diag: off[2] == 1,
			}
			count++
		}
	}
	return out[:count]
}

// nodeHeuristic - Chebyshev distance in WorldPos space, scaled by min cell
// cost (navCostRoad = 2). Admissible regardless of whether the two nodes
// live on the same grid.
func nodeHeuristic(a, b components.WorldPos) float32 {
	d := a.Sub(b)
	dx := d.X
	if dx < 0 {
		dx = -dx
	}
	dz := d.Z
	if dz < 0 {
		dz = -dz
	}
	cheb := dx
	if dz > cheb {
		cheb = dz
	}
	return cheb * float32(navCostRoad)
}

// nodeHeap - min-heap on f-score, keyed on NavNode payload.
type nodeHeapEntry struct {
	f    float32
	node components.NavNode
}

type nodeHeap struct {
	entries []nodeHeapEntry
}

func (h *nodeHeap) len() int { return len(h.entries) }

func (h *nodeHeap) push(e nodeHeapEntry) {
	h.entries = append(h.entries, e)
	h.siftUp(len(h.entries) - 1)
}

func (h *nodeHeap) pop() nodeHeapEntry {
	top := h.entries[0]
	last := len(h.entries) - 1
	h.entries[0] = h.entries[last]
	h.entries = h.entries[:last]
	if len(h.entries) > 0 {
		h.siftDown(0)
	}
	return top
}

func (h *nodeHeap) siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if h.entries[p].f <= h.entries[i].f {
			return
		}
		h.entries[p], h.entries[i] = h.entries[i], h.entries[p]
		i = p
	}
}

func (h *nodeHeap) siftDown(i int) {
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

// styleCellCost applies the PathStyle modifier to NavCell.Cost. PHASE-13.md
// P4 table:
//
//	Direct      x1.0
//	RoadPrefer  roadx0.5,  off-roadx1.5
//	RoadAvoid   roadx2.0,  off-roadx1.0 (cover-rich cells get an extra x0.8)
//	CoverSeek   CoverDistance<threshold x0.7, else x1.0
//
// Multipliers stay strictly positive so A* admissibility holds. Result is a
// float that the planner multiplies by step length (1 m NSEW, sqrt2 m diagonal).
func styleCellCost(cell components.NavCell, style components.PathStyle) float32 {
	base := float32(cell.Cost)
	onRoad := cell.Flags&components.NavOnRoad != 0
	switch style {
	case components.PathStyleRoadPrefer:
		if onRoad {
			return base * 0.5
		}
		return base * 1.5
	case components.PathStyleRoadAvoid:
		if onRoad {
			return base * 2.0
		}
		// Cover-rich cells inherit a small bonus so the unit naturally
		// drifts toward cover when avoiding roads.
		if cell.CoverDistance < components.CoverSeekThreshold {
			return base * 0.8
		}
		return base
	case components.PathStyleCoverSeek:
		if cell.CoverDistance < components.CoverSeekThreshold {
			return base * 0.7
		}
		return base
	default:
		return base
	}
}

// navGridShift / navGridMask - NavGridSide is fixed at 64 (1 m cells, 64 m
// chunk). Compile-time bit ops decode (gi, gj) into (chunk, local).
const (
	navGridShift = 6
	navGridMask  = components.NavGridSide - 1
)

// worldPosToCell - global (gi, gj) surface-cell index of a WorldPos.
func worldPosToCell(p components.WorldPos) (int32, int32) {
	gi := p.Chunk.X<<navGridShift + int32(math.Floor(float64(p.Local.X)))
	gj := p.Chunk.Z<<navGridShift + int32(math.Floor(float64(p.Local.Z)))
	return gi, gj
}

// cellToWorldPos - centre-of-cell WorldPos for a surface cell. Y is the
// procgen surface; GroundStickSystem clamps later.
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
