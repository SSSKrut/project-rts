package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// 8×8 = 64 candidate cells per chunk; 8 m cell ≈ canopy radius of a mature
// oak, so neighbouring cells almost never collide visually.
const propCellsPerSide int32 = 8

const propCellSize = components.ChunkSize / float32(propCellsPerSide)

// Per-biome spawn probability per cell:
//
//	p = max(0, density - threshold) × rateScale × baseRate
const (
	forestDensityThreshold = 0.45
	forestRateScale        = 1.5
	forestBaseRate         = 0.85

	bushlandDensityThreshold = 0.30
	bushlandRateScale        = 1.2
	bushlandBaseRate         = 0.55

	rockyDensityThreshold = 0.55
	rockyRateScale        = 1.5
	rockyBaseRate         = 0.30
)

// minPropRiverDistance keeps trees / rocks / bushes clear of river polylines
// — props sit at procgen GroundHeight (not the lowered cut), so a tree in
// the riverbed would stand on a plinth above the water.
const minPropRiverDistance float32 = 3.0

const roadClearanceMargin float32 = 1.0
const minPropBuildingClearance float32 = 1.5
const minPropTrenchClearance float32 = 1.0

// Distinct salts keep parallel rolls de-correlated — otherwise rollTree and
// rollBush in the same cell would always be equal, visibly striping the
// world.
const (
	saltSpawnTree int32 = 1
	saltSpawnBush int32 = 2
	saltSpawnRock int32 = 3
	saltJitterX   int32 = 4
	saltJitterZ   int32 = 5
	saltType      int32 = 6
	saltYaw       int32 = 7
	saltScale     int32 = 8
)

// PropSpawnSystem walks every PropsDirty chunk and rolls procedural props
// (trees/bushes/rocks) via deterministic biome density + hash. Props share
// their owner-chunk's lifecycle.
type PropSpawnSystem struct {
	chunkFilter    *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.PropsDirty]
	propsDirtyMap  *ecs.Map[components.PropsDirty]
	posMap         *ecs.Map[components.WorldPos]
	propMap        *ecs.Map[components.Prop]
	lodRelevantMap *ecs.Map[components.LODRelevant]
	propIndexRes   ecs.Resource[PropChunkIndex]
	riversRes      ecs.Resource[components.Rivers]
	roadGraphRes   ecs.Resource[components.RoadGraph]
	trenchRes      ecs.Resource[components.TrenchNetwork]
	buildingFilter *ecs.Filter2[components.Building, components.WorldPos]
}

func (sys *PropSpawnSystem) InitUI(w *ecs.World) {
	sys.chunkFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.PropsDirty](w)
	sys.propsDirtyMap = ecs.NewMap[components.PropsDirty](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.propMap = ecs.NewMap[components.Prop](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
	sys.roadGraphRes = ecs.NewResource[components.RoadGraph](w)
	sys.trenchRes = ecs.NewResource[components.TrenchNetwork](w)
	sys.buildingFilter = ecs.NewFilter2[components.Building, components.WorldPos](w)
}

func (PropSpawnSystem) Name() string { return "prop_spawn" }

func (PropSpawnSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

type pendingProp struct {
	cc    components.ChunkCoord
	local rl.Vector3
	prop  components.Prop
}

func (sys PropSpawnSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	propIndex := sys.propIndexRes.Get()
	if propIndex == nil {
		return
	}
	rivers := sys.riversRes.Get()
	var riverPolylines []components.RiverPolyline
	if rivers != nil {
		riverPolylines = rivers.Polylines
	}
	graph := sys.roadGraphRes.Get()
	trenches := sys.trenchRes.Get()

	var buildings []components.AABB2D
	qb := sys.buildingFilter.Query()
	for qb.Next() {
		b, _ := qb.Get()
		buildings = append(buildings, b.Footprint)
	}

	var pending []pendingProp
	var clearedChunks []ecs.Entity

	q := sys.chunkFilter.Query()
	for q.Next() {
		cc, _, _ := q.Get()
		ccVal := *cc
		clearedChunks = append(clearedChunks, q.Entity())

		baseWX := float32(ccVal.X) * components.ChunkSize
		baseWZ := float32(ccVal.Z) * components.ChunkSize

		for gz := int32(0); gz < propCellsPerSide; gz++ {
			for gx := int32(0); gx < propCellsPerSide; gx++ {
				cellCenterWX := baseWX + (float32(gx)+0.5)*propCellSize
				cellCenterWZ := baseWZ + (float32(gz)+0.5)*propCellSize

				// Density at cell centre (not per-candidate) — soft cluster
				// boundaries with negligible aliasing at 8 m cell width.
				forestD := BiomeDensity(terrainSeed, cellCenterWX, cellCenterWZ, BiomeForest)
				bushlandD := BiomeDensity(terrainSeed, cellCenterWX, cellCenterWZ, BiomeBushland)
				rockyD := BiomeDensity(terrainSeed, cellCenterWX, cellCenterWZ, BiomeRocky)

				// Uniform jitter so neighbouring cells don't form a visible 8 m grid.
				jitterX := hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltJitterX) * propCellSize
				jitterZ := hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltJitterZ) * propCellSize
				wx := baseWX + float32(gx)*propCellSize + jitterX
				wz := baseWZ + float32(gz)*propCellSize + jitterZ

				if len(riverPolylines) > 0 &&
					nearestRiverDistance(wx, wz, riverPolylines) < minPropRiverDistance {
					continue
				}
				if graph != nil && tooCloseToRoad(graph, wx, wz) {
					continue
				}
				if len(buildings) > 0 && tooCloseToBuilding(buildings, wx, wz) {
					continue
				}
				if trenches != nil && tooCloseToTrench(trenches.Lines, wx, wz) {
					continue
				}

				// Priority: trees → bushes → rocks; at most one prop per cell.
				propType := components.PropNone

				if propType == components.PropNone && forestD > forestDensityThreshold {
					p := (forestD - forestDensityThreshold) * forestRateScale * forestBaseRate
					if hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltSpawnTree) < p {
						propType = pickTreeType(ccVal, gx, gz)
					}
				}
				if propType == components.PropNone && bushlandD > bushlandDensityThreshold {
					p := (bushlandD - bushlandDensityThreshold) * bushlandRateScale * bushlandBaseRate
					if hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltSpawnBush) < p {
						propType = components.PropBush
					}
				}
				if propType == components.PropNone && rockyD > rockyDensityThreshold {
					p := (rockyD - rockyDensityThreshold) * rockyRateScale * rockyBaseRate
					if hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltSpawnRock) < p {
						propType = components.PropRock
					}
				}

				if propType == components.PropNone {
					continue
				}

				yaw := hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltYaw) * 2.0 * math.Pi
				scale := 0.8 + 0.4*hashFloat(terrainSeed, ccVal.X, ccVal.Z, gx, gz, saltScale)

				// Y from procgen GroundHeight. Player-edited (Modified) chunks
				// may diverge — "tree on a plinth" caveat.
				groundY := GroundHeight(wx, wz)

				pending = append(pending, pendingProp{
					cc:    ccVal,
					local: rl.Vector3{X: wx - baseWX, Y: groundY, Z: wz - baseWZ},
					prop:  components.Prop{Type: propType, Yaw: yaw, Scale: scale},
				})
			}
		}
	}

	for i := range pending {
		p := &pending[i]
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: p.cc, Local: p.local})
		sys.propMap.Add(e, &p.prop)
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		propIndex.Loaded[p.cc] = append(propIndex.Loaded[p.cc], e)
	}

	for _, e := range clearedChunks {
		sys.propsDirtyMap.Remove(e)
	}
}

// tooCloseToBuilding rejects candidates inside a building footprint or within
// minPropBuildingClearance metres of its perimeter.
func tooCloseToBuilding(footprints []components.AABB2D, wx, wz float32) bool {
	for i := range footprints {
		if footprints[i].DistanceXZ(wx, wz) < minPropBuildingClearance {
			return true
		}
	}
	return false
}

// tooCloseToTrench rejects candidates within (Width/2 + margin) of any trench
// polyline segment.
func tooCloseToTrench(lines []components.Trench, wx, wz float32) bool {
	for i := range lines {
		t := &lines[i]
		threshold := t.Width*0.5 + minPropTrenchClearance
		for j := 0; j+1 < len(t.Points); j++ {
			ax, az := worldXZ(t.Points[j])
			bx, bz := worldXZ(t.Points[j+1])
			if pointToSegment2D(wx, wz, ax, az, bx, bz) < threshold {
				return true
			}
		}
	}
	return false
}

// tooCloseToRoad rejects when any edge's centre line passes within
// (Width/2 + margin). O(edges) per candidate; switch to a chunk-bucketed
// spatial index when the graph grows past a few thousand edges.
func tooCloseToRoad(g *components.RoadGraph, wx, wz float32) bool {
	for i := range g.Edges {
		e := &g.Edges[i]
		ax, az := worldXZ(g.Nodes[e.From].Pos)
		bx, bz := worldXZ(g.Nodes[e.To].Pos)
		threshold := e.Width*0.5 + roadClearanceMargin
		if pointToSegment2D(wx, wz, ax, az, bx, bz) < threshold {
			return true
		}
	}
	return false
}

// pickTreeType picks Oak/Pine/Birch deterministically. Hash is independent
// of biome rolls so species mix doesn't correlate with density.
func pickTreeType(cc components.ChunkCoord, gx, gz int32) components.PropType {
	switch hashU32(terrainSeed, cc.X, cc.Z, gx, gz, saltType) % 3 {
	case 0:
		return components.PropOak
	case 1:
		return components.PropPine
	default:
		return components.PropBirch
	}
}
