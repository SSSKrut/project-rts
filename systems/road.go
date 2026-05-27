package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Per-chunk spawn step along an edge; matches PrimitivePlane meta length so
// consecutive surface plates tile end-to-end.
const roadPropSpacing float32 = 4.0

// Lift road-surface props above the flattened heightmap to avoid z-fighting;
// bridges sit higher (above the river cut).
const roadSurfaceYOffset float32 = 0.05
const bridgeYOffset float32 = 1.5

// junctionYOffset stacks junction plates atop surface plates at the same node.
const junctionYOffset float32 = 0.06

// RoadSystem runs two passes per tick: (1) flatten heightmap for non-bridge
// edges (gated by Without[Modified] + Without[RoadProcessed]); (2) spawn
// road / bridge / junction props (gated by Without[RoadPropsSpawned] only —
// player-edited chunks keep their road visual).
type RoadSystem struct {
	flattenFilter       *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	spawnFilter         *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	graphRes            ecs.Resource[components.RoadGraph]
	propIndexRes        ecs.Resource[PropChunkIndex]
	roadProcessedMap    *ecs.Map[components.RoadProcessed]
	roadPropsSpawnedMap *ecs.Map[components.RoadPropsSpawned]
	posMap              *ecs.Map[components.WorldPos]
	propMap             *ecs.Map[components.Prop]
	lodRelevantMap      *ecs.Map[components.LODRelevant]
	stamper             *Stamper
}

func (sys *RoadSystem) InitUI(w *ecs.World) {
	sys.flattenFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.Modified](),
			ecs.C[components.RoadProcessed](),
		)
	// Modified excluded only from flatten — props live in entity-space, not
	// on the heightmap.
	sys.spawnFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(ecs.C[components.RoadPropsSpawned]())
	sys.graphRes = ecs.NewResource[components.RoadGraph](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.roadProcessedMap = ecs.NewMap[components.RoadProcessed](w)
	sys.roadPropsSpawnedMap = ecs.NewMap[components.RoadPropsSpawned](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.propMap = ecs.NewMap[components.Prop](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.stamper = NewStamper(w)
}

func (RoadSystem) Name() string { return "road" }

func (RoadSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

type roadPendingProp struct {
	cc    components.ChunkCoord
	local rl.Vector3
	prop  components.Prop
}

type edgeInfo struct {
	idx                    int
	ax, az, bx, bz         float32
	yaw                    float32
	segLen                 float32
	minX, minZ, maxX, maxZ float32
}

func (sys RoadSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	graph := sys.graphRes.Get()
	if graph == nil || len(graph.Edges) == 0 {
		return
	}
	propIndex := sys.propIndexRes.Get()
	if propIndex == nil {
		return
	}

	// Per-edge geometry + per-node ground height; reused across every chunk
	// this tick.
	nodeY := make([]float32, len(graph.Nodes))
	for i := range graph.Nodes {
		wx, wz := worldXZ(graph.Nodes[i].Pos)
		nodeY[i] = GroundHeight(wx, wz)
	}
	edges := make([]edgeInfo, len(graph.Edges))
	for i := range graph.Edges {
		e := &graph.Edges[i]
		ax, az := worldXZ(graph.Nodes[e.From].Pos)
		bx, bz := worldXZ(graph.Nodes[e.To].Pos)
		minX, maxX := ax, bx
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		minZ, maxZ := az, bz
		if minZ > maxZ {
			minZ, maxZ = maxZ, minZ
		}
		w := e.Width
		segLen := float32(math.Sqrt(float64((bx-ax)*(bx-ax) + (bz-az)*(bz-az))))
		edges[i] = edgeInfo{
			idx:    i,
			ax:     ax, az: az, bx: bx, bz: bz,
			yaw:    float32(math.Atan2(float64(bx-ax), float64(bz-az))),
			segLen: segLen,
			minX:   minX - w, minZ: minZ - w, maxX: maxX + w, maxZ: maxZ + w,
		}
	}

	// Max width of incident edges per node — sizes junction props.
	nodeMaxW := make([]float32, len(graph.Nodes))
	for i := range graph.Edges {
		e := &graph.Edges[i]
		if e.Width > nodeMaxW[e.From] {
			nodeMaxW[e.From] = e.Width
		}
		if e.Width > nodeMaxW[e.To] {
			nodeMaxW[e.To] = e.Width
		}
	}

	type processedEnt struct {
		id ecs.Entity
	}
	var flattened []processedEnt

	qF := sys.flattenFilter.Query()
	for qF.Next() {
		cc, _, _ := qF.Get()
		ccVal := *cc
		chunkMinX := float32(ccVal.X) * components.ChunkSize
		chunkMinZ := float32(ccVal.Z) * components.ChunkSize
		chunkMaxX := chunkMinX + components.ChunkSize
		chunkMaxZ := chunkMinZ + components.ChunkSize

		for ei := range edges {
			ed := &edges[ei]
			if ed.maxX < chunkMinX || ed.minX > chunkMaxX ||
				ed.maxZ < chunkMinZ || ed.minZ > chunkMaxZ {
				continue
			}
			e := &graph.Edges[ed.idx]
			if e.Kind == components.RoadBridge {
				continue
			}
			sys.stamper.RoadFlatten(ccVal,
				ed.ax, ed.az, nodeY[e.From],
				ed.bx, ed.bz, nodeY[e.To],
				e.Width)
		}
		flattened = append(flattened, processedEnt{qF.Entity()})
	}

	for _, p := range flattened {
		if !sys.roadProcessedMap.Has(p.id) {
			sys.roadProcessedMap.Add(p.id, &components.RoadProcessed{})
		}
	}

	var pendingProps []roadPendingProp
	var spawned []processedEnt

	qS := sys.spawnFilter.Query()
	for qS.Next() {
		cc, _, _ := qS.Get()
		ccVal := *cc
		chunkMinX := float32(ccVal.X) * components.ChunkSize
		chunkMinZ := float32(ccVal.Z) * components.ChunkSize
		chunkMaxX := chunkMinX + components.ChunkSize
		chunkMaxZ := chunkMinZ + components.ChunkSize

		for ei := range edges {
			ed := &edges[ei]
			if ed.maxX < chunkMinX || ed.minX > chunkMaxX ||
				ed.maxZ < chunkMinZ || ed.minZ > chunkMaxZ {
				continue
			}
			e := &graph.Edges[ed.idx]
			if ed.segLen <= 0 {
				continue
			}

			tStart, tEnd, ok := segmentBBoxClipXZ(
				ed.ax, ed.az, ed.bx, ed.bz,
				chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ)
			if !ok {
				continue
			}

			propType := propTypeForKind(e.Kind)
			yOffset := roadSurfaceYOffset
			if e.Kind == components.RoadBridge {
				yOffset = bridgeYOffset
			}
			tStep := roadPropSpacing / ed.segLen
			if tStep <= 0 {
				continue
			}
			for t := tStart; t <= tEnd+1e-4; t += tStep {
				wx := ed.ax + t*(ed.bx-ed.ax)
				wz := ed.az + t*(ed.bz-ed.az)
				if wx < chunkMinX || wx >= chunkMaxX || wz < chunkMinZ || wz >= chunkMaxZ {
					continue
				}
				targetY := nodeY[e.From] + t*(nodeY[e.To]-nodeY[e.From])
				pendingProps = append(pendingProps, roadPendingProp{
					cc: ccVal,
					local: rl.Vector3{
						X: wx - chunkMinX,
						Y: targetY + yOffset,
						Z: wz - chunkMinZ,
					},
					prop: components.Prop{Type: propType, Yaw: ed.yaw, Scale: 1.0},
				})
			}
		}

		// One junction per node whose Pos.Chunk == ccVal — avoids duplication
		// when a node sits on a chunk boundary (Pos.Chunk = floor-rounded
		// owner, exactly one chunk wins).
		for ni := range graph.Nodes {
			n := &graph.Nodes[ni]
			if n.Pos.Chunk != ccVal {
				continue
			}
			if nodeMaxW[ni] <= 0 {
				continue
			}
			pendingProps = append(pendingProps, roadPendingProp{
				cc: ccVal,
				local: rl.Vector3{
					X: n.Pos.Local.X,
					Y: nodeY[ni] + junctionYOffset,
					Z: n.Pos.Local.Z,
				},
				prop: components.Prop{
					Type:  components.PropJunction,
					Yaw:   0,
					Scale: nodeMaxW[ni] * 1.5,
				},
			})
		}

		spawned = append(spawned, processedEnt{qS.Entity()})
	}

	for i := range pendingProps {
		p := &pendingProps[i]
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{Chunk: p.cc, Local: p.local})
		sys.propMap.Add(e, &p.prop)
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		propIndex.Loaded[p.cc] = append(propIndex.Loaded[p.cc], e)
	}
	for _, p := range spawned {
		if !sys.roadPropsSpawnedMap.Has(p.id) {
			sys.roadPropsSpawnedMap.Add(p.id, &components.RoadPropsSpawned{})
		}
	}
}

func propTypeForKind(k components.RoadKind) components.PropType {
	switch k {
	case components.RoadHighway:
		return components.PropRoadHighway
	case components.RoadLocal:
		return components.PropRoadLocal
	case components.RoadDirtTrack:
		return components.PropRoadDirt
	case components.RoadBridge:
		return components.PropBridge
	}
	return components.PropRoadLocal
}

// segmentBBoxClipXZ does Liang-Barsky clip of segment (ax,az)→(bx,bz)
// against rectangle [minX, maxX]×[minZ, maxZ]. Returns the inside t-range,
// or ok=false if disjoint.
func segmentBBoxClipXZ(ax, az, bx, bz, minX, minZ, maxX, maxZ float32) (float32, float32, bool) {
	dx := bx - ax
	dz := bz - az
	tStart := float32(0)
	tEnd := float32(1)

	clip := func(p, q float32) bool {
		if p == 0 {
			return q >= 0
		}
		t := q / p
		if p < 0 {
			if t > tEnd {
				return false
			}
			if t > tStart {
				tStart = t
			}
		} else {
			if t < tStart {
				return false
			}
			if t < tEnd {
				tEnd = t
			}
		}
		return true
	}
	if !clip(-dx, ax-minX) {
		return 0, 0, false
	}
	if !clip(dx, maxX-ax) {
		return 0, 0, false
	}
	if !clip(-dz, az-minZ) {
		return 0, 0, false
	}
	if !clip(dz, maxZ-az) {
		return 0, 0, false
	}
	return tStart, tEnd, tEnd > tStart
}
