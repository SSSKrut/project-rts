package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// waterPropSpacing matches the PrimitivePlane size in PropTypeRegistry, so
// consecutive plates tile the river without overlap or gap.
const waterPropSpacing float32 = 4.0

// RiverSystem applies river-cut + water-prop pass to every pristine chunk
// that intersects a river polyline. Runs after terrain_load + terrain_gen
// (heightmap is filled), before prop_spawn (trees see the eventual water
// exclusion via nearestRiverDistance), and before terrain_mesh (cut is in
// the mesh on first build).
//
// Filter excludes Modified - player edits aren't re-stamped. RiverProcessed
// gates against re-running on the same chunk.
type RiverSystem struct {
	chunkFilter       *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	riversRes         ecs.Resource[components.Rivers]
	propIndexRes      ecs.Resource[PropChunkIndex]
	riverProcessedMap *ecs.Map[components.RiverProcessed]
	modifiedMap       *ecs.Map[components.Modified]
	posMap            *ecs.Map[components.WorldPos]
	propMap           *ecs.Map[components.Prop]
	lodRelevantMap    *ecs.Map[components.LODRelevant]
	stamper           *Stamper
}

func (sys *RiverSystem) InitUI(w *ecs.World) {
	sys.chunkFilter = ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.WorldPos](w).
		Without(
			ecs.C[components.RiverProcessed](),
			ecs.C[components.Modified](),
		)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.riverProcessedMap = ecs.NewMap[components.RiverProcessed](w)
	sys.modifiedMap = ecs.NewMap[components.Modified](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.propMap = ecs.NewMap[components.Prop](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.stamper = NewStamper(w)
}

func (RiverSystem) Name() string { return "river" }

func (RiverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

type pendingWater struct {
	cc    components.ChunkCoord
	local [3]float32
	yaw   float32
}

func (sys RiverSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	rivers := sys.riversRes.Get()
	if rivers == nil || len(rivers.Polylines) == 0 {
		return
	}
	propIndex := sys.propIndexRes.Get()
	if propIndex == nil {
		return
	}

	type plBBox struct {
		minX, minZ, maxX, maxZ float32
		idx                    int
	}
	bboxes := make([]plBBox, 0, len(rivers.Polylines))
	for i := range rivers.Polylines {
		minX, minZ, maxX, maxZ := polylineWorldBBox(rivers.Polylines[i])
		// Inflate by Width so the rejection test doesn't miss segments whose
		// strip clips into the chunk while their centre line is just outside.
		w := rivers.Polylines[i].Width
		bboxes = append(bboxes, plBBox{
			minX: minX - w, minZ: minZ - w,
			maxX: maxX + w, maxZ: maxZ + w,
			idx: i,
		})
	}

	type processedEnt struct {
		id ecs.Entity
		cc components.ChunkCoord
	}
	var processed []processedEnt
	var pendingWaters []pendingWater

	q := sys.chunkFilter.Query()
	for q.Next() {
		cc, _, _ := q.Get()
		ccVal := *cc
		ent := q.Entity()

		chunkMinX := float32(ccVal.X) * components.ChunkSize
		chunkMinZ := float32(ccVal.Z) * components.ChunkSize
		chunkMaxX := chunkMinX + components.ChunkSize
		chunkMaxZ := chunkMinZ + components.ChunkSize

		for _, b := range bboxes {
			if b.maxX < chunkMinX || b.minX > chunkMaxX ||
				b.maxZ < chunkMinZ || b.minZ > chunkMaxZ {
				continue
			}
			pl := &rivers.Polylines[b.idx]
			sys.stamper.RiverCut(ccVal, pl.Points, pl.Width, pl.Depth)

			for i := 0; i+1 < len(pl.Points); i++ {
				addWaterPropsForSegment(
					pl.Points[i], pl.Points[i+1],
					ccVal, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ,
					&pendingWaters,
				)
			}
		}

		// Mark even non-intersecting chunks - keeps the filter cheap on later
		// ticks. Without this we'd re-test every pristine chunk vs every river.
		processed = append(processed, processedEnt{id: ent, cc: ccVal})
	}

	for _, p := range processed {
		if !sys.riverProcessedMap.Has(p.id) {
			sys.riverProcessedMap.Add(p.id, &components.RiverProcessed{})
		}
	}
	for i := range pendingWaters {
		w := &pendingWaters[i]
		e := ctx.World.NewEntity()
		sys.posMap.Add(e, &components.WorldPos{
			Chunk: w.cc,
			Local: rl.Vector3{X: w.local[0], Y: w.local[1], Z: w.local[2]},
		})
		sys.propMap.Add(e, &components.Prop{
			Type:  components.PropWater,
			Yaw:   w.yaw,
			Scale: 1.0,
		})
		sys.lodRelevantMap.Add(e, &components.LODRelevant{})
		propIndex.Loaded[w.cc] = append(propIndex.Loaded[w.cc], e)
	}
}

// addWaterPropsForSegment walks a single river segment and emits water-prop
// pending-records for every sample point inside the chunk's XZ bbox. Sample
// step is waterPropSpacing.
//
// Sample Y is set just below the procgen surface (-0.4 m) so the placeholder
// plate reads as the floor of the cut. Works for shallow/wide rivers; deeper
// cuts can override per-river later.
func addWaterPropsForSegment(
	a, b components.WorldPos,
	cc components.ChunkCoord,
	minX, minZ, maxX, maxZ float32,
	out *[]pendingWater,
) {
	ax := float32(a.Chunk.X)*components.ChunkSize + a.Local.X
	az := float32(a.Chunk.Z)*components.ChunkSize + a.Local.Z
	bx := float32(b.Chunk.X)*components.ChunkSize + b.Local.X
	bz := float32(b.Chunk.Z)*components.ChunkSize + b.Local.Z
	dx := bx - ax
	dz := bz - az
	segLen := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if segLen <= 0 {
		return
	}
	// Atan2(dx, dz) - yaw around +Y so yaw=0 points along +Z (matches the
	// prop renderer's Rotatef-around-Y convention).
	yaw := float32(math.Atan2(float64(dx), float64(dz)))

	for d := float32(0); d <= segLen; d += waterPropSpacing {
		t := d / segLen
		wx := ax + dx*t
		wz := az + dz*t
		if wx < minX || wx >= maxX || wz < minZ || wz >= maxZ {
			continue
		}
		groundY := GroundHeight(wx, wz) - 0.4
		*out = append(*out, pendingWater{
			cc:    cc,
			local: [3]float32{wx - minX, groundY, wz - minZ},
			yaw:   yaw,
		})
	}
}
