package components

import "github.com/mlange-42/ark/ecs"

// NavGridSide is the per-side cell count of a chunk's NavGrid. ChunkSize / 1 m
// = 64; fixed at 1 m so it lines up with heightmap vertices and 1.2 m door
// openings resolve to >= 1 cell.
const NavGridSide = 64

// NavGridCells is the total cell count per chunk grid (64 * 64).
const NavGridCells = NavGridSide * NavGridSide

// NavFlags is a bitmask of per-cell semantic tags consulted by the pathfinder
// for cost overrides and tactical AI for terrain context.
type NavFlags uint8

const (
	NavOnRoad NavFlags = 1 << iota
	NavInBuilding
	NavInTrench
	NavNearWater
)

// NavCell is one A*-eligible square. Cost = 0 means impassable (slope, wall,
// closed door, deep water, blocking prop). Cost > 0 is a relative step cost
// applied as cost * stepLength (1 m NSEW, sqrt(2) m diagonal).
//
// CoverDistance is the distance in cells to the nearest cover slot in the
// 9-chunk window. 0 = on a slot, 255 = no slot within range
// (CoverDistanceFar). NavService.FindPath reads this when
// PathStyle=CoverSeek.
type NavCell struct {
	Cost          uint8
	Flags         NavFlags
	CoverDistance uint8
}

// CoverDistanceFar is the "no cover within scan radius" sentinel.
const CoverDistanceFar uint8 = 255

// CoverSeekThreshold is the cell-count distance below which CoverSeek
// PathStyle applies the cost multiplier. 8 cells = 8 m scan radius.
const CoverSeekThreshold uint8 = 8

// NavGrid is the per-chunk navigation bake. Packed into the chunk archetype
// with no extra indirection.
type NavGrid struct {
	Cells [NavGridCells]NavCell
}

// CoverCell records, per cell, which compass directions (N=bit0, clockwise:
// N, NE, E, SE, S, SW, W, NW) are blocked by a wall / steep terrain /
// LOS-blocking prop within the bake radius. BaseCover is the popcount-based
// debug overlay aggregate; tactical AI builds its own utility on top.
type CoverCell struct {
	BaseCover uint8
	DirMask   uint8
}

// CoverMap is the per-chunk cover bake. Same shape as NavGrid for cache
// alignment.
type CoverMap struct {
	Cells [NavGridCells]CoverCell
}

// Locomotion enumerates how an entity moves through the world. Vehicles will
// add wheeled / tracked / heavy variants; NavOpts already carries this.
type Locomotion uint8

const (
	LocomotionFoot Locomotion = iota
)

// NavBaked marks a chunk whose NavGrid pass has run. SpatialBakeSystem gates
// on Without[NavBaked]; cleared on chunk eviction.
type NavBaked struct{}

// CoverBaked marks a chunk whose CoverMap + cover slot pass has run.
type CoverBaked struct{}

// MaxLevelSide is the per-side cell count cap for a LevelNavGrid. 32 m,
// large enough for every placeholder building footprint.
const MaxLevelSide = 32

// MaxLevelCells is the total cell count of a LevelNavGrid.
const MaxLevelCells = MaxLevelSide * MaxLevelSide

// LevelNavGrid is the navigation bake for one interior level. Cells are
// 1 m * 1 m, indexed as cj*MaxLevelSide + ci. Origin is the level's
// chunk-local (0,0) corner; only the first SizeX * SizeZ block of Cells is
// valid.
//
// Walls of the same level (matched by WorldPos.Y) become Cost=0. Open doors
// punch passages. Windows and closed doors stay Cost=0. A level is a plane;
// slope is not computed.
//
// Phase 16.B.1.a: pure type rename from FloorNavGrid. The grid still lives
// on a Floor entity; M16.B.1.b will re-anchor it onto the Level entity
// (one grid per Level instead of per Floor).
type LevelNavGrid struct {
	SizeX, SizeZ uint8
	Origin       Vec3
	Cells        [MaxLevelCells]NavCell
}

// Vec3 is a small 3-float so LevelNavGrid stays import-free from rl.
type Vec3 struct {
	X, Y, Z float32
}

// LevelNavBaked marks an entity whose LevelNavGrid has been baked.
type LevelNavBaked struct{}

// NavNodeKind discriminates a NavNode reference. Surface = cell on a chunk's
// NavGrid; Level = cell on a Level's LevelNavGrid.
type NavNodeKind uint8

const (
	NodeSurface NavNodeKind = iota
	NodeLevel
)

// NavNode is the multi-graph cell identifier consumed by A*. For Surface,
// (Chunk, I, J) is the global cell coord. For Level, Level is the entity ID
// of the host Level (Phase 16.B.1.b: the grid lives on a Level entity, one
// per interior level volume).
type NavNode struct {
	Kind  NavNodeKind
	Chunk ChunkCoord
	Level ecs.Entity
	I, J  int16
}

// TransitionEdge is a directed link between two NavNodes. Cost is added to
// the A* gScore as a fixed integer step. Owner is the Door/Stairs entity
// that emitted the edge; TerrainStreamingSystem.evict drops edges whose
// owner has been despawned.
type TransitionEdge struct {
	From  NavNode
	To    NavNode
	Cost  uint8
	Owner ecs.Entity
}

// TransitionRegistry maps each NavNode to its outgoing transitions. Sparse:
// a node without entries falls back to in-grid neighbours.
type TransitionRegistry struct {
	Out map[NavNode][]TransitionEdge
}

func NewTransitionRegistry() TransitionRegistry {
	return TransitionRegistry{Out: make(map[NavNode][]TransitionEdge)}
}
