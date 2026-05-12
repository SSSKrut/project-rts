package components

import "github.com/mlange-42/ark/ecs"

// NavGridSide is the per-side cell count of a chunk's NavGrid. ChunkSize / 1 m
// = 64 — fixed at 1 m so it lines up with heightmap vertices and so 1.2 m door
// openings still resolve to ≥ 1 cell.
const NavGridSide = 64

// NavGridCells is the total cell count per chunk grid (64 × 64 = 4096).
const NavGridCells = NavGridSide * NavGridSide

// NavFlags is a bitmask of per-cell semantic tags consulted by the pathfinder
// for cost overrides and by tactical AI for "what kind of terrain is this".
// Phase 6 ships four bits; Phase 8 will widen to differentiated road kinds.
type NavFlags uint8

const (
	NavOnRoad NavFlags = 1 << iota
	NavInBuilding
	NavInTrench
	NavNearWater
)

// NavCell is one A*-eligible square. Cost = 0 means "impassable" (slope, wall,
// closed door, deep water, blocking prop). Cost > 0 is a relative step cost
// applied as cost × stepLength (1 m NSEW, √2 m diagonal) by the planner.
type NavCell struct {
	Cost  uint8
	Flags NavFlags
}

// NavGrid is the per-chunk navigation bake. ~8 KB by value, packed into the
// chunk archetype with no extra indirection.
type NavGrid struct {
	Cells [NavGridCells]NavCell
}

// CoverCell records, per cell, which compass directions (8 bits, N=bit0,
// going clockwise: N, NE, E, SE, S, SW, W, NW) are blocked by a wall / steep
// terrain / LOS-blocking prop within the bake radius (8 m). BaseCover is a
// cheap aggregate (popcount × 32) used by the debug overlay; Phase 10 builds
// the real utility scoring.
type CoverCell struct {
	BaseCover uint8
	DirMask   uint8
}

// CoverMap is the per-chunk cover bake. Same shape as NavGrid for cache
// alignment and cell-coordinate parity.
type CoverMap struct {
	Cells [NavGridCells]CoverCell
}

// Locomotion enumerates how an entity moves through the world. Phase 6 only
// ships LocomotionFoot; Phase 8 (vehicles) will add wheeled / tracked / heavy
// variants and a per-class cost table. NavService.FindPath already takes this
// in NavOpts so the API is forward-compatible.
type Locomotion uint8

const (
	LocomotionFoot Locomotion = iota
)

// NavBaked marks a chunk whose NavGrid pass has run. SpatialBakeSystem gates
// on Without[NavBaked]; cleared on chunk eviction (the entity is removed and
// re-spawns pristine).
type NavBaked struct{}

// CoverBaked marks a chunk whose CoverMap + cover slot pass has run. Same
// gating pattern as NavBaked.
type CoverBaked struct{}

// MaxFloorSide is the per-side cell count cap for a FloorNavGrid. 32 cells ×
// 1 m = 32 m, large enough to cover every placeholder building footprint
// (Phase 5: 8×8 / 12×10 / 10×10).
const MaxFloorSide = 32

// MaxFloorCells is the total cell count of a FloorNavGrid. 32² = 1024.
const MaxFloorCells = MaxFloorSide * MaxFloorSide

// FloorNavGrid is the per-Floor navigation bake. Lives as a component on
// Floor-entities (one grid per storey). Cells are 1 m × 1 m, layout indexed
// as cj*MaxFloorSide + ci. Origin is the floor's chunk-local (0,0) corner in
// XZ; only the first SizeX × SizeZ block of Cells is valid.
//
// Walls of the same floor (matched by WorldPos.Y) become Cost=0. Open doors
// punch passages. Windows + closed doors stay Cost=0 (windows block
// movement; closed doors block both). Slope is not computed — a floor is
// a plane.
type FloorNavGrid struct {
	SizeX, SizeZ uint8
	Origin       Vec3
	Cells        [MaxFloorCells]NavCell
}

// Vec3 — tiny re-declaration of a 3-float vec so FloorNavGrid stays
// import-free from rl. The field carries chunk-local XZ + Y of the floor
// surface.
type Vec3 struct {
	X, Y, Z float32
}

// FloorNavBaked marks a Floor entity whose FloorNavGrid has been baked. Same
// pattern as NavBaked but on the Floor child rather than the chunk.
type FloorNavBaked struct{}

// NavNodeKind discriminates a NavNode reference. Surface = cell on a chunk's
// NavGrid; Floor = cell on a Floor's FloorNavGrid.
type NavNodeKind uint8

const (
	NodeSurface NavNodeKind = iota
	NodeFloor
)

// NavNode is the multi-graph cell identifier consumed by A*. For surface
// cells, (Chunk, I, J) is the global cell coord. For floor cells, Floor is
// the entity ID of the host Floor and (I, J) are local cell indices in the
// FloorNavGrid.
type NavNode struct {
	Kind  NavNodeKind
	Chunk ChunkCoord
	Floor ecs.Entity
	I, J  int16
}

// TransitionEdge is a directed link between two NavNodes. Cost is added to
// the A* gScore as a fixed integer step (no √2 scaling — transitions are
// short). Owner is the Door/Stairs entity that emitted the edge — used by
// TerrainStreamingSystem.evict to garbage-collect edges when their owner is
// despawned.
type TransitionEdge struct {
	From  NavNode
	To    NavNode
	Cost  uint8
	Owner ecs.Entity
}

// TransitionRegistry maps each NavNode to its outgoing transitions. Sparse —
// a node without entries has no transitions (the A* loop just falls back to
// the 8 in-grid neighbours).
type TransitionRegistry struct {
	Out map[NavNode][]TransitionEdge
}

func NewTransitionRegistry() TransitionRegistry {
	return TransitionRegistry{Out: make(map[NavNode][]TransitionEdge)}
}
