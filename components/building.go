package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

const (
	FloorHeight        float32 = 3.0
	WallThickness      float32 = 0.3
	BunkerDepth        float32 = 3.0
	BunkerFalloffWidth float32 = 4.0
	StairsLength       float32 = 3.0
	StairsWidth        float32 = 1.5
	// BuildingLevelingSkirtWidth: cosine-blend distance (metres) outside a
	// surface building's footprint where the heightmap fades from the
	// building's floor Y back to the natural terrain. Keeps door thresholds
	// flush with ground so units can walk in without clipping the wall.
	//
	// Must span several heightmap cells (grid step = 1 m): a 1 m skirt
	// touches zero vertices (d >= falloffWidth cuts exactly at the first
	// ring), leaving a sheer step around buildings on sloped terrain. The
	// step reads as slope >= 0.60 in the nav bake, which walls off the
	// door approach cells and makes the whole building unreachable. 4 m
	// keeps the worst ring-to-ring delta near 0.39 x total-step, walkable
	// for pads up to ~1.5 m above/below natural terrain.
	BuildingLevelingSkirtWidth float32 = 4.0
	// BuildingLevelingDepthOffset: how far below the building's floor Y the
	// leveled terrain plate sits. Prevents Z-fighting between the terrain mesh
	// and the concrete floor.
	BuildingLevelingDepthOffset float32 = 0.05
)

type BuildingKind uint8

const (
	BuildingHouse BuildingKind = iota
	BuildingWarehouse
	BuildingBunker
	BuildingFortification
)

// Building is the root component on a building entity. Child entities (walls
// / floors / stairs / doors / windows) are spawned per-chunk-life by
// BuildingSystem and indexed via BuildingChildIndex. The root carries
// AlwaysActive - it survives chunk eviction so cross-chunk queries keep
// working.
type Building struct {
	Kind      BuildingKind
	Stories   uint8
	Yaw       float32 // radians around +Y
	Footprint AABB2D
	Seed      uint64
}

// BuildingPlan is the per-building layout spec produced by either the .glb
// loader or the generator. BuildingSystem reads the Walls / Floors / Stairs /
// Levels slices to spawn child entities. Empty Walls slice = "no full spec
// yet" - useful for headless / tests that build a Building root without a
// layout.
type BuildingPlan struct {
	Pos     WorldPos
	Kind    BuildingKind
	Stories uint8
	Size    rl.Vector2
	Yaw     float32
	Seed    uint64

	// World-space coords. All Local fields on child specs are chunk-local of
	// the building's host chunk (Pos.Chunk).
	Levels           []LevelSpec
	Walls            []WallSpec
	Floors           []FloorSpec
	Stairs           []StairSpec
	Furniture        []FurnitureSpec
	Markers          []MarkerSpec
	LevelTransitions []LevelTransitionSpec
}

type BuildingPlanList struct {
	Plans []BuildingPlan
}

// NoLevelRef sentinel for spec LevelRef fields when the entity has no
// level association.
const NoLevelRef uint8 = 0xFF

// LevelSpec is one level volume in a BuildingPlan. AABB is world-space.
// DisplayOrder is an optional override for UI chip ordering - 0 means
// "use avgY ascending + alphabetical tiebreak".
type LevelSpec struct {
	Name         string
	AABB         AABB3D
	DisplayOrder uint8
}

// WallSpec is one external or internal wall. Local is the "from" endpoint
// in chunk-local coords of the building's host chunk. OutwardNormal points
// away from the interior (or +X for internal partitions). LevelRefs are
// indices into BuildingPlan.Levels - a wall straddling two storeys carries
// both.
type WallSpec struct {
	Local         rl.Vector3
	Segment       WallSegment
	OutwardNormal rl.Vector3
	LevelRefs     []uint8
}

type FloorSpec struct {
	Local    rl.Vector3
	Floor    Floor
	LevelRef uint8
}

// StairAnchor connects one waypoint of a stair to a Level via LevelRef.
// Multi-segment stairs (switchback / spiral) carry multiple anchors -
// usually 0 and len(Waypoints)-1, but mid-anchors are allowed.
type StairAnchor struct {
	WpIndex  uint8
	LevelRef uint8
}

// StairSpec is one stair entity + a waypoint chain in chunk-local coords +
// level anchors at the endpoints. NavService consumes Waypoints + Anchors
// to emit NodeStairWp transition edges.
type StairSpec struct {
	Local     rl.Vector3
	Stairs    Stairs
	Waypoints []rl.Vector3
	Anchors   []StairAnchor
}

type MarkerKind uint8

const (
	MarkerSpawn MarkerKind = iota
	MarkerCapture
	MarkerSniperPerch
	MarkerEntry
)

// FurnitureSpec is one interior prop. Distinct from outdoor PropSpawn:
// furniture lives as a BuildingMember child and despawns with the host chunk.
type FurnitureSpec struct {
	Local    rl.Vector3
	Kind     PropType
	Yaw      float32
	LevelRef uint8
}

type MarkerSpec struct {
	Local    rl.Vector3
	Kind     MarkerKind
	LevelRef uint8
}

// LevelTransitionSpec describes a door_<A>-<B> connection between two
// levels. ViaWall is the index into BuildingPlan.Walls of the wall
// carrying the door; -1 marks transitions via stairs / passages.
type LevelTransitionSpec struct {
	LevelA  uint8
	LevelB  uint8
	ViaWall int16
}

type Level struct {
	AABB         AABB3D
	Name         [8]byte // zero-padded label; keep POD for snapshots
	DisplayOrder uint8
}

func LevelName(s string) (out [8]byte) {
	copy(out[:], s)
	return out
}

func (l *Level) Label() string {
	n := 0
	for n < len(l.Name) && l.Name[n] != 0 {
		n++
	}
	return string(l.Name[:n])
}

type LevelTransition struct {
	LevelA ecs.Entity
	LevelB ecs.Entity
}

type Furniture struct {
	Kind  PropType
	Yaw   float32
	Level ecs.Entity
}

type Marker struct {
	Kind  MarkerKind
	Level ecs.Entity
}

// StairLevels names the two Level entities this stair connects. From is the
// lower / starting anchor, To is the upper / terminal anchor (or both equal
// for bunker entrances where the surface side has no Level entity yet).
// Written by BuildingSystem from StairSpec.Anchors and read by
// SpatialBakeSystem to wire NavNode level endpoints without rescanning plans.
type StairLevels struct {
	From ecs.Entity
	To   ecs.Entity
}

// LevelMember tags a child entity with its owning Level entity. Walls
// spanning multiple levels carry their lowest level here; multi-level
// cutaway logic queries the wall's bbox directly. Stairs deliberately have
// no LevelMember - they connect two levels.
type LevelMember struct {
	Level ecs.Entity
}

// WallRenderMode controls how external walls of the currently-viewed level
// render under cutaway.
type WallRenderMode uint8

const (
	WallRenderAll          WallRenderMode = iota // all walls solid
	WallRenderCameraFacing                       // camera-facing walls become semi-transparent
	WallRenderWireframe                          // walls -> outline only
)

// LevelVisibility is fog-of-war state per Level entity. Discovered flips
// true the first time any friendly unit enters the level's bbox. LastSeenAt
// is the session clock of the most recent friendly presence; renderers fog
// the level once (now - LastSeenAt) exceeds FogVisibleDuration.
type LevelVisibility struct {
	Discovered bool
	LastSeenAt float32
}

const FogVisibleDuration float32 = 5.0

// BuildingViewMode lives on the building root. When InteriorOpen is true,
// the renderer hides every Level whose avgY is above CurrentLevel.avgY,
// exposing CurrentLevel's interior. CurrentLevel = ecs.Entity{} means "fall
// back to lowest level" at read time.
type BuildingViewMode struct {
	InteriorOpen bool
	CurrentLevel ecs.Entity
	WallMode     WallRenderMode
}

// BuildingMember marks a child entity as belonging to a parent building.
// Damage / destruction bubbles to the root.
type BuildingMember struct {
	Building ecs.Entity
}

type OpeningKind uint8

const (
	OpeningNone OpeningKind = iota
	OpeningDoor
	OpeningWindow
)

// WallSegment is one piece of building outer wall. WorldPos = "from"
// endpoint (chunk-relative), Length runs along Yaw, Height extends upward,
// Thickness is centred on the line. At most one opening per segment;
// multi-opening walls split at layout time.
type WallSegment struct {
	Length    float32
	Yaw       float32
	Height    float32
	Thickness float32

	OpeningKind    OpeningKind
	OpeningCenterT float32 // [0, 1] along Length
	OpeningWidth   float32
	OpeningBottom  float32 // sill height (= 0 for door)
	OpeningHeight  float32
}

type Door struct {
	State               DoorState
	Material            DoorMaterial
	BlocksLOSWhenClosed bool
}

type DoorState uint8

const (
	DoorClosed DoorState = iota
	DoorOpen
)

type DoorMaterial uint8

const (
	DoorWood DoorMaterial = iota
	DoorMetal
)

// Window: BlocksLOS is intentionally absent - windows are LOS-transparent
// walls by design.
type Window struct {
	Glass bool
}

// Floor is one storey's horizontal plate. WorldPos.Y holds the floor surface
// height; SizeX/SizeZ are the plate extents. For a bunker, Y is below ground.
type Floor struct {
	Level uint8
	SizeX float32
	SizeZ float32
}

// Stairs connects FromFloor and ToFloor along Yaw. Length is the horizontal
// run, Width is the tread width, Rise is the vertical climb.
type Stairs struct {
	FromFloor uint8
	ToFloor   uint8
	Yaw       float32
	Length    float32
	Width     float32
	Rise      float32
}

type Occupancy struct {
	Max     uint8
	Current uint8
}

type CoverDirection struct {
	Dir rl.Vector3 // unit vector pointing OUTward (away from interior)
}

type ShootingArc struct {
	Forward      rl.Vector3
	HalfAngleRad float32
}

// BuildingsProcessed marks a chunk whose child entities have been spawned.
// Independent of Modified - children live in entity space, not on the heightmap.
type BuildingsProcessed struct{}

// BuildingTerrainProcessed marks a chunk whose bunker RectCut has been
// applied. Gated by Without[Modified] - player edits win.
type BuildingTerrainProcessed struct{}
