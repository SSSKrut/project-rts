package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// Shared geometric constants. Generator (Phase 16.5) and BuildingSystem
// reference these so the runtime layout stays consistent with what the
// loader / .glb meshes are expected to emit.
const (
	FloorHeight        float32 = 3.0
	WallThickness      float32 = 0.3
	BunkerDepth        float32 = 3.0
	BunkerFalloffWidth float32 = 4.0
	StairsLength       float32 = 3.0
	StairsWidth        float32 = 1.5
)

// BuildingKind drives layout (Bunker is sunken, House is surface) and
// downstream gameplay attributes.
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

// BuildingPlan is the per-building layout spec produced by either the
// Phase 16.A .glb loader or the Phase 16.5 generator. BuildingSystem reads
// the Walls / Floors / Stairs / Levels slices to spawn child entities.
//
// Pos / Size / Kind / Stories / Yaw / Seed are the legacy Phase 5 metadata
// (still consumed by main.go for Building root placement and by the map
// renderer for footprint rects). Empty Walls slice = "no full spec yet" -
// useful for headless / tests that build a Building root without a layout.
type BuildingPlan struct {
	Pos     WorldPos
	Kind    BuildingKind
	Stories uint8
	Size    rl.Vector2 // SizeX, SizeZ in metres - top-down footprint
	Yaw     float32
	Seed    uint64

	// Phase 16.A.3 / 16.5.A. World-space coords. All Local fields on child
	// specs are chunk-local of the building's host chunk (Pos.Chunk).
	Levels           []LevelSpec
	Walls            []WallSpec
	Floors           []FloorSpec
	Stairs           []StairSpec
	Furniture        []FurnitureSpec
	Markers          []MarkerSpec
	LevelTransitions []LevelTransitionSpec
}

// BuildingPlanList is the singleton resource read once at startup.
type BuildingPlanList struct {
	Plans []BuildingPlan
}

// NoLevelRef sentinel for spec LevelRef fields when the entity has no
// level association (rare - legacy plans, validator skip).
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
// away from the interior (or +X for internal partitions; either side is
// "outward"). LevelRefs are indices into BuildingPlan.Levels - a wall
// straddling two storeys carries both.
type WallSpec struct {
	Local         rl.Vector3
	Segment       WallSegment
	OutwardNormal rl.Vector3
	LevelRefs     []uint8
}

// FloorSpec is one Floor plate. LevelRef is the index of the Level this
// plate belongs to (mandatory in extended plans).
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

// MarkerKind tags marker_<kind>_<name> annotations from .glb / generator.
type MarkerKind uint8

const (
	MarkerSpawn MarkerKind = iota
	MarkerCapture
	MarkerSniperPerch
	MarkerEntry
)

// FurnitureSpec is one interior prop (sandbags / table / crate / ...).
// Distinct from outdoor PropSpawn: furniture lives as a BuildingMember
// child and despawns with the host chunk eviction.
type FurnitureSpec struct {
	Local    rl.Vector3
	Kind     PropType
	Yaw      float32
	LevelRef uint8
}

// MarkerSpec is one point of interest (spawn / capture / sniper perch).
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

// Level is the per-Level ECS component. One entity per LevelSpec is spawned
// by BuildingSystem alongside walls / floors / stairs. Phase 16.B reads
// this to bake LevelNavGrid + LevelCoverMap; Phase 16.C reads it for
// chip widgets and fog-of-war.
type Level struct {
	AABB         AABB3D
	Name         string
	DisplayOrder uint8
}

// LevelTransition is a per-door (or per-passage) link between two Level
// entities. Phase 16.B uses it to thread cross-level pathfinding.
type LevelTransition struct {
	LevelA ecs.Entity
	LevelB ecs.Entity
}

// Furniture is a sandbox / table / crate placed inside a building. Owned
// by the host building via BuildingMember; level association via the
// Level field (entity).
type Furniture struct {
	Kind  PropType
	Yaw   float32
	Level ecs.Entity
}

// Marker is a spawn / capture / sniperperch annotation attached to a level.
type Marker struct {
	Kind  MarkerKind
	Level ecs.Entity
}

// StairLevels names the two Level entities this stair connects. From is the
// lower / starting anchor, To is the upper / terminal anchor (or both equal
// for bunker entrances where the surface side has no Level entity yet -
// Phase 16.B.0+ treats those uniformly until exterior pseudo-level lands).
// Written by BuildingSystem.spawnBuilding from StairSpec.Anchors and read by
// SpatialBakeSystem to wire NavNode level endpoints without rescanning plans.
type StairLevels struct {
	From ecs.Entity
	To   ecs.Entity
}

// LevelMember tags a child entity (wall / floor / furniture / marker) with
// its owning Level entity. Resolved at spawn time from the spec's LevelRef
// (Phase 16.B.0+). Walls spanning multiple levels carry their lowest level
// here; multi-level cutaway logic queries the wall's bbox directly.
// Stairs deliberately have no LevelMember - they connect two levels.
type LevelMember struct {
	Level ecs.Entity
}

// WallRenderMode controls how external walls of the currently-viewed level
// render under cutaway. Phase 16.C.0 wires the All variant only; C.4 fills
// in CameraFacing alpha + Wireframe outline.
type WallRenderMode uint8

const (
	WallRenderAll          WallRenderMode = iota // default - all walls solid
	WallRenderCameraFacing                       // camera-facing walls become semi-transparent
	WallRenderWireframe                          // walls -> outline only
)

// LevelVisibility is Phase 16.C.2 fog-of-war state per Level entity.
// Discovered flips true the first time any friendly unit (or, in smoke,
// the camera anchor) enters the level's bbox. LastSeenAt is the session
// clock of the most recent friendly presence; renderers fog the level
// once (now - LastSeenAt) exceeds FogVisibleDuration.
type LevelVisibility struct {
	Discovered bool
	LastSeenAt float32
}

// FogVisibleDuration is how long after the last friendly presence a level
// stays "fresh" (full-colour render). Short for development; gameplay
// tuning later in Phase 16+.
const FogVisibleDuration float32 = 5.0

// BuildingViewMode lives on the building root. It drives the Sims cutaway:
// when InteriorOpen is true, the renderer hides every Level whose avgY is
// above CurrentLevel.avgY, exposing CurrentLevel's interior. Default state
// (InteriorOpen=false, WallMode=WallRenderAll) yields the regular building
// silhouette. CurrentLevel = ecs.Entity{} means "fall back to lowest level"
// at read time.
type BuildingViewMode struct {
	InteriorOpen bool
	CurrentLevel ecs.Entity
	WallMode     WallRenderMode
}

// BuildingMember marks a child entity (wall / floor / stairs / opening) as
// belonging to a parent building. Damage / destruction bubbles to the root.
type BuildingMember struct {
	Building ecs.Entity
}

// OpeningKind on a wall segment.
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

// Door state on the wall-segment-with-door entity. Sits beside WallSegment
// + (optionally) Occupancy / CoverDirection on the same entity.
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

// Window component on a wall-segment-with-window entity. BlocksLOS is
// intentionally absent - windows are LOS-transparent walls by design.
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

// Smart Object data - written at spawn time. CoverEvaluation and TacticalAI
// are the readers.
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
// applied. Gated by Without[Modified] - player edits win, the cut isn't
// re-applied on top.
type BuildingTerrainProcessed struct{}
