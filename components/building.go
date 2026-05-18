package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
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

// BuildingPlan is the hand-authored spec used at startup to instantiate
// Building entities. After startup nothing reads the plan list - the
// Building component carries everything systems need.
type BuildingPlan struct {
	Pos     WorldPos
	Kind    BuildingKind
	Stories uint8
	Size    rl.Vector2 // SizeX, SizeZ in metres
	Yaw     float32
	Seed    uint64
}

// BuildingPlanList is the singleton resource read once at startup.
type BuildingPlanList struct {
	Plans []BuildingPlan
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
