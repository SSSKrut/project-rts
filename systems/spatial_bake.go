package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 16.B.1.b debug: print first non-trivial Pass 4 bake to stdout so a
// failing nav setup is immediately diagnosable.
var bakeDebugReported bool

// SpatialBakeSystem is the Phase 6 oracle baker: per chunk, it walks the
// heightmap + props + walls + roads + trenches once and emits a NavGrid + a
// CoverMap, plus cover-slot entities for any cover-emitting host (prop /
// wall / corner) inside the chunk. The actual bake is split across four
// passes living in sibling files:
//
//	spatial_bake_nav.go         Pass 1 - NavGrid bake (gated Without[NavBaked])
//	spatial_bake_cover.go       Pass 2 - CoverMap + slots + CoverDistance
//	spatial_bake_level.go       Pass 3 - LevelNavGrid bake per Level entity
//	spatial_bake_transitions.go Pass 4 - TransitionRegistry rebuild
//
// Modified is *not* gated: bake reflects the live heightmap (potentially
// stamped) and props / buildings are spawned by their own systems before us.
// Modified-stale is an accepted Phase 6 trade-off (P13).
type SpatialBakeSystem struct {
	navFilter   *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	coverFilter *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	wallFilter  *ecs.Filter2[components.WorldPos, components.WallSegment]
	floorFilter *ecs.Filter2[components.WorldPos, components.Floor]
	// Phase 16.B.1.b: LevelNavGrid now hangs on Level entities, one per
	// interior volume. Pass 3 iterates levels (gated by LevelNavBaked);
	// Pass 4 needs the full set of baked levels to find transition endpoints,
	// so levelFilterAll skips the Without gate.
	levelFilter    *ecs.Filter2[components.Level, components.WorldPos]
	levelFilterAll *ecs.Filter2[components.Level, components.WorldPos]
	levelMap       *ecs.Map[components.Level]
	levelMemberMap *ecs.Map[components.LevelMember]
	stairLevelsMap *ecs.Map[components.StairLevels]
	// Phase 14.6 M14.6.1 - NavInBuilding bake reads Building roots (any
	// chunk, AlwaysActive) and stamps the flag onto surface cells whose XZ
	// falls inside the footprint.
	buildingFilter    *ecs.Filter1[components.Building]
	heightmapMap      *ecs.Map[components.Heightmap]
	navGridMap        *ecs.Map[components.NavGrid]
	coverMapMap       *ecs.Map[components.CoverMap]
	floorNavMap       *ecs.Map[components.LevelNavGrid]
	floorNavBakedMap  *ecs.Map[components.LevelNavBaked]
	navBakedMap       *ecs.Map[components.NavBaked]
	coverBakedMap     *ecs.Map[components.CoverBaked]
	doorMap           *ecs.Map[components.Door]
	posMap            *ecs.Map[components.WorldPos]
	propMap           *ecs.Map[components.Prop]
	memberMap         *ecs.Map[components.BuildingMember]
	coverDirMap       *ecs.Map[components.CoverDirection]
	coverSlotMap      *ecs.Map[components.CoverSlot]
	lodRelevantMap    *ecs.Map[components.LODRelevant]
	propIndexRes      ecs.Resource[PropChunkIndex]
	registryRes       ecs.Resource[components.PropTypeRegistry]
	roadGraphRes      ecs.Resource[components.RoadGraph]
	trenchRes         ecs.Resource[components.TrenchNetwork]
	riversRes         ecs.Resource[components.Rivers]
	chunkIndexRes     ecs.Resource[TerrainChunkIndex]
	coverSlotIndexRes ecs.Resource[CoverSlotIndex]
	buildingIndexRes  ecs.Resource[BuildingChildIndex]
	transitionRes     ecs.Resource[components.TransitionRegistry]
	stairsFilter      *ecs.Filter2[components.WorldPos, components.Stairs]
	floorComponentMap *ecs.Map[components.Floor]
	// Phase 13 M13.4: CoverDistance bake - query all cover-slot entities and
	// bucket by host chunk, then re-write NavCell.CoverDistance for cells
	// within scan radius of any slot in the 9-chunk window.
	coverSlotFilter *ecs.Filter2[components.WorldPos, components.CoverSlot]
}

func (sys *SpatialBakeSystem) InitUI(w *ecs.World) {
	sys.navFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(ecs.C[components.NavBaked]())
	sys.coverFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(ecs.C[components.CoverBaked]())
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.floorFilter = ecs.NewFilter2[components.WorldPos, components.Floor](w)
	sys.levelFilter = ecs.NewFilter2[components.Level, components.WorldPos](w).
		Without(ecs.C[components.LevelNavBaked]())
	sys.levelFilterAll = ecs.NewFilter2[components.Level, components.WorldPos](w)
	sys.levelMap = ecs.NewMap[components.Level](w)
	sys.levelMemberMap = ecs.NewMap[components.LevelMember](w)
	sys.stairLevelsMap = ecs.NewMap[components.StairLevels](w)
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.navGridMap = ecs.NewMap[components.NavGrid](w)
	sys.coverMapMap = ecs.NewMap[components.CoverMap](w)
	sys.floorNavMap = ecs.NewMap[components.LevelNavGrid](w)
	sys.floorNavBakedMap = ecs.NewMap[components.LevelNavBaked](w)
	sys.navBakedMap = ecs.NewMap[components.NavBaked](w)
	sys.coverBakedMap = ecs.NewMap[components.CoverBaked](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.propMap = ecs.NewMap[components.Prop](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.registryRes = ecs.NewResource[components.PropTypeRegistry](w)
	sys.roadGraphRes = ecs.NewResource[components.RoadGraph](w)
	sys.trenchRes = ecs.NewResource[components.TrenchNetwork](w)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
	sys.chunkIndexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.coverSlotIndexRes = ecs.NewResource[CoverSlotIndex](w)
	sys.buildingIndexRes = ecs.NewResource[BuildingChildIndex](w)
	sys.transitionRes = ecs.NewResource[components.TransitionRegistry](w)
	sys.stairsFilter = ecs.NewFilter2[components.WorldPos, components.Stairs](w)
	sys.floorComponentMap = ecs.NewMap[components.Floor](w)
	sys.coverSlotFilter = ecs.NewFilter2[components.WorldPos, components.CoverSlot](w)
	sys.memberMap = ecs.NewMap[components.BuildingMember](w)
	sys.coverDirMap = ecs.NewMap[components.CoverDirection](w)
	sys.coverSlotMap = ecs.NewMap[components.CoverSlot](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.buildingFilter = ecs.NewFilter1[components.Building](w)
}

func (SpatialBakeSystem) Name() string { return "spatial_bake" }

func (SpatialBakeSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// spatialBakeChunkRec is the per-todo record used by Pass 1 and Pass 2 to
// share the (entity, chunk-coord) pair after the filter snapshot.
type spatialBakeChunkRec struct {
	id ecs.Entity
	cc components.ChunkCoord
}

// wallEntry is a per-wall snapshot used inside one bake tick. Captures the
// passable-opening flag (door open) so the rasteriser doesn't need to peek
// into Door state again.
type wallEntry struct {
	local           rl.Vector3
	w               components.WallSegment
	openingPassable bool // true <-> open door; false otherwise (windows, closed doors, plain walls)
}

func (sys *SpatialBakeSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	sys.bakeNavPass(ctx)
	sys.bakeCoverPass(ctx)
	sys.bakeLevelNavPass(ctx)
	sys.bakeTransitionsPass(ctx)
}
