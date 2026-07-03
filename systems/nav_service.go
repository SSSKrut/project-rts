package systems

import (
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Debug flags: print first empty FindPath result and first successful one
// so we can distinguish empty-path bug from wrong-path bug.
var navDebugReported bool
var navOKReported bool
var navSnapshotReported bool

// NavService is the multi-graph A* pathfinder (not a System). Per-chunk
// surface NavGrids and per-Level LevelNavGrids share one A* expansion via
// the TransitionRegistry resource (door / stairs edges).
type NavService struct {
	indexRes      ecs.Resource[TerrainChunkIndex]
	transitionRes ecs.Resource[components.TransitionRegistry]
	buildingIndex ecs.Resource[BuildingChildIndex]
	navGridMap    *ecs.Map[components.NavGrid]
	floorNavMap   *ecs.Map[components.LevelNavGrid]
	levelMap      *ecs.Map[components.Level]
	posMap        *ecs.Map[components.WorldPos]
	heightmapMap  *ecs.Map[components.Heightmap]
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
		heightmapMap:  ecs.NewMap[components.Heightmap](w),
		levelFilter:   ecs.NewFilter2[components.Level, components.WorldPos](w),
	}
}

type NavOpts struct {
	Locomotion       components.Locomotion
	AvoidOpenedDoors bool // stealth stub; ignored
	// PathStyle scales NavCell.Cost via styleCellCost. Direct = ×1.0.
	PathStyle components.PathStyle
}

// navMaxIter caps the number of cells A* will expand per call.
const navMaxIter = 50000

// navArrivalRadius — agent pops a waypoint within this many metres.
const navArrivalRadius float32 = 0.5

// FindPath returns waypoints (cell centres) along a least-cost path. Empty
// result = no path; nil = both endpoints in the same cell.
func (s *NavService) FindPath(from, to components.WorldPos, opts NavOpts) []components.WorldPos {
	wps, _ := s.findPath(from, to, opts, true)
	return wps
}

// FindPathGates additionally reports which waypoints are transition
// endpoints (door / stairs / wing-junction edge ends): gates[i] pairs with
// waypoints[i]. Walkers must reach gate waypoints tightly before advancing
// — they thread wall openings.
func (s *NavService) FindPathGates(from, to components.WorldPos, opts NavOpts) ([]components.WorldPos, []bool) {
	return s.findPath(from, to, opts, true)
}

func (s *NavService) findPath(from, to components.WorldPos, opts NavOpts, allowFallback bool) ([]components.WorldPos, []bool) {
	idx := s.indexRes.Get()
	if idx == nil {
		return []components.WorldPos{}, nil
	}
	registry := s.transitionRes.Get()

	floors := s.snapshotLevels()
	// Diagnostic: RTS_NAV_PROBE="wx,wz" dumps a one-shot 13×13 surface-cell
	// window (cost / flags) around a world point on the first FindPath call.
	if !navSnapshotReported {
		if probe := os.Getenv("RTS_NAV_PROBE"); probe != "" {
			var px, pz float32
			if _, err := fmt.Sscanf(probe, "%f,%f", &px, &pz); err == nil {
				s.dumpSurfaceWindow(px, pz, 6, idx, floors)
			}
			for i, lr := range floors {
				fmt.Printf("[nav] snapshot[%d] ent=%v chunk=%v origin=(%.1f,%.1f) size=%dx%d aabbY=[%.1f..%.1f]\n",
					i, lr.ent, lr.chunk, lr.originX, lr.originZ, lr.sizeX, lr.sizeZ, lr.aabb.MinY, lr.aabb.MaxY)
			}
		}
		navSnapshotReported = true
	}

	fromNode, fromOK := s.resolveNode(from, idx, floors)
	toNode, toOK := s.resolveNode(to, idx, floors)
	if !fromOK || !toOK {
		if debugLog {
			fmt.Printf("[nav] EMPTY: resolve failed fromOK=%v toOK=%v to=(%.1f,%.1f)\n",
				fromOK, toOK,
				to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
				to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize)
		}
		return []components.WorldPos{}, nil
	}
	if fromNode == toNode {
		if debugLog {
			fmt.Printf("[nav] SAME node: kind=%d I=%d J=%d (from==to, no path needed)\n",
				fromNode.Kind, fromNode.I, fromNode.J)
		}
		return nil, nil
	}

	fromCell, fromCellOK := s.cellAt(fromNode, idx, floors)
	if !fromCellOK || fromCell.Cost == 0 {
		// Crowd pressure can wedge a walker onto a blocked cell (the
		// NavInBuilding footprint ring on the wall line, a prop circle).
		// Substitute the nearest walkable cell instead of giving up —
		// EMPTY here leaves the unit pathless, walking blind into the
		// wall until it pins.
		if alt, ok := s.nearestWalkable(fromNode, from, idx, floors); ok {
			fromNode = alt
		} else {
			if debugLog {
				fmt.Printf("[nav] EMPTY: from blocked kind=%d cost=%d flags=%d\n",
					fromNode.Kind, fromCell.Cost, fromCell.Flags)
			}
			return []components.WorldPos{}, nil
		}
	}
	toCell, toCellOK := s.cellAt(toNode, idx, floors)
	if !toCellOK || toCell.Cost == 0 {
		if debugLog {
			fmt.Printf("[nav] EMPTY: to blocked kind=%d cost=%d flags=%d to=(%.1f,%.1f)\n",
				toNode.Kind, toCell.Cost, toCell.Flags,
				to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
				to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize)
		}
		return []components.WorldPos{}, nil
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
		if allowFallback && registry != nil && toNode.Kind == components.NodeLevel {
			if entry, ok := s.closestSurfaceEntry(toNode.Level, registry, floors, from); ok {
				entryPos := s.nodeWorldPos(entry, floors)
				return s.findPath(from, entryPos, opts, false)
			}
		}
		if debugLog {
			surfaceClosed := 0
			levelClosed := 0
			levelMatchClosed := 0
			goalCellClosed := false
			for n := range closed {
				switch n.Kind {
				case components.NodeSurface:
					surfaceClosed++
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
			fmt.Printf("[nav] NOT FOUND closed=%d (surf=%d level=%d sameLevel=%d) openLen=%d goalClosed=%v\n",
				len(closed), surfaceClosed, levelClosed, levelMatchClosed,
				open.len(), goalCellClosed)
			fmt.Printf("[nav]     from=%+v to=%+v states=%d\n", fromNode, toNode, len(states))
		}
		return []components.WorldPos{}, nil
	}

	// Reconstruct goal→start, then reverse.
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
	n := len(nodes)
	waypoints := make([]components.WorldPos, 0, n)
	for i := n - 1; i >= 0; i-- {
		waypoints = append(waypoints, s.nodeWorldPos(nodes[i], floors))
	}
	// Transition edges (Kind or Level changes between consecutive steps —
	// grid neighbours never change either) mark the FAR endpoint as a gate:
	// the walker must actually cross the wall-opening plane between the
	// pair before steering at anything deeper inside.
	gates := make([]bool, n)
	for k := 1; k < n; k++ {
		a := nodes[n-k]   // waypoint k-1
		b := nodes[n-1-k] // waypoint k
		if a.Kind != b.Kind || (a.Kind == components.NodeLevel && a.Level != b.Level) {
			gates[k] = true
		}
	}
	// Drop the leading waypoint (start cell centre) — the agent is already
	// there; otherwise the walker spends its first metres correcting onto it.
	if len(waypoints) > 1 {
		waypoints = waypoints[1:]
		gates = gates[1:]
	}
	if debugLog {
		usesTransition := false
		for _, nd := range nodes {
			if nd.Kind == components.NodeLevel {
				usesTransition = true
				break
			}
		}
		fmt.Printf("[nav] OK len=%d via-level=%v to=(%.1f,%.1f) toKind=%d\n",
			len(waypoints), usesTransition,
			to.Local.X+float32(to.Chunk.X)*components.ChunkSize,
			to.Local.Z+float32(to.Chunk.Z)*components.ChunkSize,
			toNode.Kind)
	}
	return waypoints, gates
}

// dumpSurfaceWindow prints cost/flags for surface cells in a ±half window
// around world (px, pz), then the per-cell slope recomputed from the live
// chunk Heightmap vs pure procgen GroundHeight. Debug aid behind
// RTS_NAV_PROBE.
func (s *NavService) dumpSurfaceWindow(px, pz float32, half int32, idx *TerrainChunkIndex, floors []levelRec) {
	ci := int32(math.Floor(float64(px)))
	cj := int32(math.Floor(float64(pz)))
	fmt.Printf("[nav-probe] window around (%.1f, %.1f); rows z descending\n", px, pz)
	for gj := cj + half; gj >= cj-half; gj-- {
		fmt.Printf("[nav-probe] z=%4d |", gj)
		for gi := ci - half; gi <= ci+half; gi++ {
			n := components.NavNode{
				Kind:  components.NodeSurface,
				Chunk: components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift},
				I:     int16(gi & navGridMask),
				J:     int16(gj & navGridMask),
			}
			c, ok := s.cellAt(n, idx, floors)
			if !ok {
				fmt.Printf("  ?/--")
				continue
			}
			fmt.Printf(" %2d/%02x", c.Cost, c.Flags)
		}
		fmt.Println()
	}
	hmAt := func(gi, gj int32) (float32, bool) {
		cc := components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift}
		ent, ok := idx.Loaded[cc]
		if !ok {
			return 0, false
		}
		hm := s.heightmapMap.Get(ent)
		if hm == nil {
			return 0, false
		}
		li := int(gi & navGridMask)
		lj := int(gj & navGridMask)
		return hm.Heights[lj*components.ChunkResolution+li], true
	}
	for gj := cj + 2; gj >= cj-2; gj-- {
		fmt.Printf("[nav-probe-hm] z=%4d |", gj)
		for gi := ci - half; gi <= ci+half; gi++ {
			h00, ok0 := hmAt(gi, gj)
			h10, ok1 := hmAt(gi+1, gj)
			h01, ok2 := hmAt(gi, gj+1)
			h11, ok3 := hmAt(gi+1, gj+1)
			if !ok0 || !ok1 || !ok2 || !ok3 {
				fmt.Printf("    ?      ")
				continue
			}
			slope := maxF(maxF(absF(h00-h10), absF(h00-h01)),
				maxF(maxF(absF(h00-h11), absF(h10-h01)),
					maxF(absF(h10-h11), absF(h01-h11))))
			gen := GroundHeight(float32(gi), float32(gj))
			fmt.Printf(" %5.2f^%4.2f g%5.2f", h00, slope, gen)
		}
		fmt.Println()
	}
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// levelRec is a per-Level lookup record used during one FindPath call. The
// nav grid is anchored on a Level entity, one per interior volume; floors
// are render-only.
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

// snapshotLevels walks every Level with a baked LevelNavGrid. Level entities
// are AlwaysActive, so we filter directly (no BuildingChildIndex hop).
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

// resolveNode maps a WorldPos to a NavNode. Inside a Level's AABB
// (XZ + Y proximity) → NodeLevel; otherwise NodeSurface.
//
// Stacked storeys share a boundary plane (L0's MaxY == L1's MinY), so a
// point at a floor plate matches BOTH levels' padded Y bands. Pick the
// level whose floor (MinY) is nearest to the point's Y — first-match used
// to bind an upper-storey goal to the storey below, making "Occupy L1"
// resolve into the ground floor and the path degenerate to SAME-node.
func (s *NavService) resolveNode(wp components.WorldPos, idx *TerrainChunkIndex, levels []levelRec) (components.NavNode, bool) {
	worldX := wp.Local.X + float32(wp.Chunk.X)*components.ChunkSize
	worldZ := wp.Local.Z + float32(wp.Chunk.Z)*components.ChunkSize
	const yPad float32 = 0.6
	best := components.NavNode{}
	bestDY := float32(math.MaxFloat32)
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
		dy := absF(wp.Local.Y - lr.aabb.MinY)
		if dy < bestDY {
			bestDY = dy
			best = components.NavNode{Kind: components.NodeLevel, Level: lr.ent, I: ci, J: cj}
		}
	}
	if best.Kind == components.NodeLevel {
		return best, true
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

// cellAt returns the NavCell for a NavNode. ok=false if the host grid is
// not loaded.
//
// Surface cells flagged NavInBuilding are reported inaccessible: building
// interior must be reached through a TransitionEdge (Door / Stairs).
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
			// A surface cell stamped NavInBuilding by one building's
			// footprint can still be the outside side of another
			// building's door (compound case: wings overlap). When
			// TransitionRegistry reports outgoing edges, treat the cell
			// as walkable; force cost to a non-zero value so the
			// `cost == 0` guard doesn't skip it.
			if registry := s.transitionRes.Get(); registry != nil {
				if len(registry.Out[n]) > 0 {
					if cell.Cost == 0 {
						cell.Cost = 8
					}
					return cell, true
				}
			}
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

// nodeWorldPos returns the centre-of-cell WorldPos for a NavNode.
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

// nearestWalkable scans two neighbour rings around a blocked node and
// returns the walkable cell closest to the walker's true position. Used to
// recover a path start when the walker has been shoved onto a blocked cell.
func (s *NavService) nearestWalkable(n components.NavNode, from components.WorldPos, idx *TerrainChunkIndex, floors []levelRec) (components.NavNode, bool) {
	best := components.NavNode{}
	bestDist := float32(math.MaxFloat32)
	seen := map[components.NavNode]bool{n: true}
	consider := func(c components.NavNode) {
		if seen[c] {
			return
		}
		seen[c] = true
		cell, ok := s.cellAt(c, idx, floors)
		if !ok || cell.Cost == 0 {
			return
		}
		wp := s.nodeWorldPos(c, floors)
		d := wp.Sub(from)
		dist := d.X*d.X + d.Z*d.Z
		if dist < bestDist {
			bestDist = dist
			best = c
		}
	}
	ring1 := s.gridNeighbours(n)
	for _, nb := range ring1 {
		consider(nb.node)
	}
	if bestDist < float32(math.MaxFloat32) {
		return best, true
	}
	for _, nb := range ring1 {
		for _, nb2 := range s.gridNeighbours(nb.node) {
			consider(nb2.node)
		}
	}
	if bestDist < float32(math.MaxFloat32) {
		return best, true
	}
	return components.NavNode{}, false
}

// closestSurfaceEntry returns the surface NavNode closest to `from` that is
// reachable from the target Level via TransitionEdges (doors / stairs).
// Used as a fallback when A* fails to enter a building level directly.
func (s *NavService) closestSurfaceEntry(level ecs.Entity, registry *components.TransitionRegistry, floors []levelRec, from components.WorldPos) (components.NavNode, bool) {
	if registry == nil {
		return components.NavNode{}, false
	}
	queue := make([]components.NavNode, 0, 16)
	for n := range registry.Out {
		if n.Kind == components.NodeLevel && n.Level == level {
			queue = append(queue, n)
		}
	}
	if len(queue) == 0 {
		return components.NavNode{}, false
	}
	visited := map[components.NavNode]bool{}
	bestDist := float32(math.MaxFloat32)
	best := components.NavNode{}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if visited[n] {
			continue
		}
		visited[n] = true
		if n.Kind == components.NodeSurface {
			wp := s.nodeWorldPos(n, floors)
			d := wp.Sub(from)
			dist := d.X*d.X + d.Z*d.Z
			if dist < bestDist {
				bestDist = dist
				best = n
			}
			continue
		}
		for _, edge := range registry.Out[n] {
			if edge.Cost == 0 {
				continue
			}
			queue = append(queue, edge.To)
		}
	}
	if bestDist == float32(math.MaxFloat32) {
		return components.NavNode{}, false
	}
	return best, true
}

type gridNeighbour struct {
	node components.NavNode
	diag bool // true: sqrt2 step; false: 1 m cardinal
}

// gridNeighbours enumerates the 8 cells around a node inside the same grid.
// Surface nodes overflow into neighbouring chunks; floor nodes clamp to the
// grid's SizeX × SizeZ block.
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

// nodeHeuristic — Chebyshev distance scaled by min cell cost (navCostRoad).
// Admissible across heterogeneous grids.
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

// nodeHeap — min-heap on f-score, keyed on NavNode payload.
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

// styleCellCost applies the PathStyle modifier to NavCell.Cost:
//
//	Direct      ×1.0
//	RoadPrefer  road ×0.5,  off-road ×1.5
//	RoadAvoid   road ×2.0,  off-road ×1.0 (cover-rich cells ×0.8)
//	CoverSeek   CoverDistance<threshold ×0.7, else ×1.0
//
// Multipliers stay strictly positive so A* admissibility holds.
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

// NavGridSide is fixed at 64 (1 m cells, 64 m chunk). Compile-time bit ops
// decode (gi, gj) into (chunk, local).
const (
	navGridShift = 6
	navGridMask  = components.NavGridSide - 1
)

// worldPosToCell returns the global (gi, gj) surface-cell index of a WorldPos.
func worldPosToCell(p components.WorldPos) (int32, int32) {
	gi := p.Chunk.X<<navGridShift + int32(math.Floor(float64(p.Local.X)))
	gj := p.Chunk.Z<<navGridShift + int32(math.Floor(float64(p.Local.Z)))
	return gi, gj
}

// cellToWorldPos returns the centre-of-cell WorldPos for a surface cell. Y
// is the procgen surface; GroundStickSystem clamps later.
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
