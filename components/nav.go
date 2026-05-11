package components

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
