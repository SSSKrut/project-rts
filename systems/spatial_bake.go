package systems

import (
	"math"
	"math/bits"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Slope thresholds for NavCell.Cost (foot locomotion, Phase 6 P5):
//
//	slope < 0.30  (~17°)  ->  Cost = navCostOpen
//	0.30 <= slope < 0.60   ->  Cost = navCostRough
//	slope >= 0.60  (~31°)  ->  Cost = 0    (impassable)
//
// Slope = max pairwise corner-height difference per 1 m cell (ChunkResolution
// step is exactly 1 m). Diagonals are not divided by √2 - the bake takes the
// raw max so a sharp ridge-on-diagonal still flags as steep.
const (
	navSlopeOpen  float32 = 0.30
	navSlopeRough float32 = 0.60

	navCostOpen   uint8 = 4
	navCostRough  uint8 = 8
	navCostRoad   uint8 = 2
	navCostTrench uint8 = 16
)

// SpatialBakeSystem is the Phase 6 oracle baker: per chunk, it walks the
// heightmap + props + walls + roads + trenches once and emits a NavGrid + a
// CoverMap, plus cover-slot entities for any cover-emitting host (prop /
// wall / corner) inside the chunk. Two independent passes gated by two
// markers, mirroring RoadSystem / BuildingSystem:
//
//  1. NavGrid pass - gated by Without[NavBaked]. Slope-from-heightmap base,
//     then wall / opening / prop / road / trench overrides.
//  2. CoverMap + slots pass - gated by Without[CoverBaked]. Ray-casts per
//     cell + slot generation per host inside this chunk.
//
// Modified is *not* gated: bake reflects the live heightmap (potentially
// stamped) and props / buildings are spawned by their own systems before
// us. Modified-stale is an accepted Phase 6 trade-off (P13).
type SpatialBakeSystem struct {
	navFilter         *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	coverFilter       *ecs.Filter3[components.ChunkCoord, components.Heightmap, components.WorldPos]
	wallFilter        *ecs.Filter2[components.WorldPos, components.WallSegment]
	floorFilter       *ecs.Filter2[components.WorldPos, components.Floor]
	// Phase 14.6 M14.6.1 - NavInBuilding bake reads Building roots (any
	// chunk, AlwaysActive) and stamps the flag onto surface cells whose XZ
	// falls inside the footprint.
	buildingFilter *ecs.Filter1[components.Building]
	heightmapMap      *ecs.Map[components.Heightmap]
	navGridMap        *ecs.Map[components.NavGrid]
	coverMapMap       *ecs.Map[components.CoverMap]
	floorNavMap       *ecs.Map[components.FloorNavGrid]
	floorNavBakedMap  *ecs.Map[components.FloorNavBaked]
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
	sys.floorFilter = ecs.NewFilter2[components.WorldPos, components.Floor](w).
		Without(ecs.C[components.FloorNavBaked]())
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.navGridMap = ecs.NewMap[components.NavGrid](w)
	sys.coverMapMap = ecs.NewMap[components.CoverMap](w)
	sys.floorNavMap = ecs.NewMap[components.FloorNavGrid](w)
	sys.floorNavBakedMap = ecs.NewMap[components.FloorNavBaked](w)
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

type spatialBakeChunkRec struct {
	id ecs.Entity
	cc components.ChunkCoord
}

// wallEntry - a per-wall snapshot used inside one bake tick. Captures the
// passable-opening flag (door open) so the rasterizer doesn't need to peek
// into Door state again.
type wallEntry struct {
	local            rl.Vector3
	w                components.WallSegment
	openingPassable  bool // true ⇔ open door; false otherwise (windows, closed doors, plain walls)
}

func (sys SpatialBakeSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	// ── Pass 1: NavGrid bake ──
	var navTodo []spatialBakeChunkRec
	qN := sys.navFilter.Query()
	for qN.Next() {
		cc, _, _ := qN.Get()
		navTodo = append(navTodo, spatialBakeChunkRec{id: qN.Entity(), cc: *cc})
	}
	if len(navTodo) > 0 {
		// Bucket walls by host chunk in one pass - avoids quadratic
		// re-scanning when several pristine chunks bake in the same tick.
		wallsByChunk := map[components.ChunkCoord][]wallEntry{}
		qW := sys.wallFilter.Query()
		for qW.Next() {
			pos, w := qW.Get()
			passable := false
			if w.OpeningKind == components.OpeningDoor {
				if d := sys.doorMap.Get(qW.Entity()); d != nil && d.State == components.DoorOpen {
					passable = true
				}
			}
			wallsByChunk[pos.Chunk] = append(wallsByChunk[pos.Chunk], wallEntry{
				local:           pos.Local,
				w:               *w,
				openingPassable: passable,
			})
		}

		propIdx := sys.propIndexRes.Get()
		registry := sys.registryRes.Get()
		graph := sys.roadGraphRes.Get()
		trenches := sys.trenchRes.Get()
		rivers := sys.riversRes.Get()

		// Phase 14.6 M14.6.1 - snapshot every Building's Footprint once for
		// the NavInBuilding stamp pass. Building roots carry AlwaysActive,
		// so a single filter sweep covers the whole world.
		var footprints []components.AABB2D
		qB := sys.buildingFilter.Query()
		for qB.Next() {
			b := qB.Get()
			footprints = append(footprints, b.Footprint)
		}

		// Bake order is calibrated for the cost-priority hierarchy in P5:
		//
		//   slope  <  OnRoad  <  InTrench  <  NearWater  <  Walls/Props
		//
		// Apply lower priority first; later passes overwrite.  Two intentional
		// twists:
		//
		//   - The river-block pass leaves Cost untouched on cells that are
		//     already OnRoad - this is how a bridge keeps Cost=2 over the
		//     river bed instead of falling back to "water = impassable".
		//   - Walls/props always win at the end with hard Cost=0; flags from
		//     earlier passes are kept so AI can see e.g. "this blocked cell
		//     is also on a road".
		for _, rec := range navTodo {
			hm := sys.heightmapMap.Get(rec.id)
			if hm == nil {
				continue
			}
			var grid components.NavGrid
			bakeNavSlope(&grid, hm)

			applyNavRoads(&grid, rec.cc, graph)
			applyNavTrenches(&grid, rec.cc, trenches)
			applyNavRiverBlock(&grid, rec.cc, rivers)

			for i := range wallsByChunk[rec.cc] {
				rasterizeWall(&grid, wallsByChunk[rec.cc][i])
			}
			if propIdx != nil && registry != nil {
				for _, propEnt := range propIdx.Loaded[rec.cc] {
					prop := sys.propMap.Get(propEnt)
					propPos := sys.posMap.Get(propEnt)
					if prop == nil || propPos == nil {
						continue
					}
					meta := registry.Metas[prop.Type]
					if !meta.BlocksMove {
						continue
					}
					rasterizePropCircle(&grid, propPos.Local.X, propPos.Local.Z,
						meta.BBoxRadius*prop.Scale)
				}
			}

			// Phase 14.6 M14.6.1 - flag every cell whose centre falls inside
			// any Building.Footprint. NavService.cellAt then refuses surface
			// expansion into them; access remains only through TransitionEdges
			// (Door/Stairs) that route into Floor NavNodes.
			applyNavBuildings(&grid, rec.cc, footprints)

			if existing := sys.navGridMap.Get(rec.id); existing != nil {
				existing.Cells = grid.Cells
			} else {
				sys.navGridMap.Add(rec.id, &grid)
			}
			if !sys.navBakedMap.Has(rec.id) {
				sys.navBakedMap.Add(rec.id, &components.NavBaked{})
			}
		}
	}

	// ── Pass 2: CoverMap + cover slots bake ──
	var coverTodo []spatialBakeChunkRec
	qC := sys.coverFilter.Query()
	for qC.Next() {
		cc, _, _ := qC.Get()
		coverTodo = append(coverTodo, spatialBakeChunkRec{id: qC.Entity(), cc: *cc})
	}
	if len(coverTodo) > 0 {
		chunkIdx := sys.chunkIndexRes.Get()

		// World-space LOS-walls bucketed by host chunk. Pre-computed once per
		// tick and re-used for every chunk being baked - saves the rebuild on
		// every coverTodo iteration.
		losWallsByChunk := map[components.ChunkCoord][]losWall{}
		qLW := sys.wallFilter.Query()
		for qLW.Next() {
			pos, w := qLW.Get()
			doorState := components.DoorClosed
			if d := sys.doorMap.Get(qLW.Entity()); d != nil {
				doorState = d.State
			}
			losWallsByChunk[pos.Chunk] = append(losWallsByChunk[pos.Chunk],
				makeLosWall(*pos, *w, doorState))
		}

		// LOS-blocking props (BlocksLOS=true in registry) by host chunk.
		losPropsByChunk := map[components.ChunkCoord][]losProp{}
		propIdx := sys.propIndexRes.Get()
		registry := sys.registryRes.Get()
		if propIdx != nil && registry != nil {
			for cc, ents := range propIdx.Loaded {
				baseX := float32(cc.X) * components.ChunkSize
				baseZ := float32(cc.Z) * components.ChunkSize
				for _, ent := range ents {
					prop := sys.propMap.Get(ent)
					pos := sys.posMap.Get(ent)
					if prop == nil || pos == nil {
						continue
					}
					meta := registry.Metas[prop.Type]
					if !meta.BlocksLOS {
						continue
					}
					losPropsByChunk[cc] = append(losPropsByChunk[cc], losProp{
						x: baseX + pos.Local.X,
						z: baseZ + pos.Local.Z,
						r: meta.BBoxRadius * prop.Scale,
					})
				}
			}
		}

		coverIdx := sys.coverSlotIndexRes.Get()
		buildingIdx := sys.buildingIndexRes.Get()

		for _, rec := range coverTodo {
			// Walls / props within ray-cast reach can sit in the eight
			// surrounding chunks; pull all nine into the per-chunk working set.
			var walls []losWall
			var props []losProp
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					nb := components.ChunkCoord{X: rec.cc.X + dx, Z: rec.cc.Z + dz}
					walls = append(walls, losWallsByChunk[nb]...)
					props = append(props, losPropsByChunk[nb]...)
				}
			}

			var coverMap components.CoverMap
			bakeCoverCells(&coverMap, rec.cc, chunkIdx, sys.heightmapMap, walls, props)
			if existing := sys.coverMapMap.Get(rec.id); existing != nil {
				existing.Cells = coverMap.Cells
			} else {
				sys.coverMapMap.Add(rec.id, &coverMap)
			}
			if !sys.coverBakedMap.Has(rec.id) {
				sys.coverBakedMap.Add(rec.id, &components.CoverBaked{})
			}

			sys.spawnCoverSlots(ctx.World, rec.cc, propIdx, registry, coverIdx, buildingIdx)
		}

		// Phase 13 M13.4: CoverDistance bake for every chunk whose NavGrid is
		// already built (chunks that completed Pass 1 in some earlier tick).
		// Done here in Pass 2 so newly spawned cover slots are visible to the
		// distance scan.
		sys.bakeCoverDistance(coverTodo)
	}

	// ── Pass 3: FloorNavGrid bake ──
	//
	// Per Phase 7 P2: one grid per Floor entity. Walls of the same storey
	// (matched by WorldPos.Y ≈ floor.Y) become Cost=0; open-door openings are
	// punched through; windows and closed doors stay blocked. Floor footprint
	// must fit MaxFloorSide (<=32 m); Phase 5's placeholder buildings all do.
	type floorRec struct {
		ent     ecs.Entity
		pos     components.WorldPos
		f       components.Floor
	}
	var floorTodo []floorRec
	qF := sys.floorFilter.Query()
	for qF.Next() {
		pos, f := qF.Get()
		floorTodo = append(floorTodo, floorRec{ent: qF.Entity(), pos: *pos, f: *f})
	}
	if len(floorTodo) == 0 {
		return
	}

	// Snapshot every wall once - Phase 7 has at most a few buildings loaded;
	// the per-floor filter is cheap.
	type wallSnap struct {
		pos             components.WorldPos
		w               components.WallSegment
		openingPassable bool
	}
	var wallSnaps []wallSnap
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		passable := false
		if w.OpeningKind == components.OpeningDoor {
			if d := sys.doorMap.Get(qW.Entity()); d != nil && d.State == components.DoorOpen {
				passable = true
			}
		}
		wallSnaps = append(wallSnaps, wallSnap{pos: *pos, w: *w, openingPassable: passable})
	}

	for _, fr := range floorTodo {
		// Footprint centre in chunk-local space -> corners.
		sx := fr.f.SizeX
		sz := fr.f.SizeZ
		if sx <= 0 || sz <= 0 {
			continue
		}
		szi := uint8(math.Ceil(float64(sx)))
		szj := uint8(math.Ceil(float64(sz)))
		if szi > components.MaxFloorSide {
			szi = components.MaxFloorSide
		}
		if szj > components.MaxFloorSide {
			szj = components.MaxFloorSide
		}

		originX := fr.pos.Local.X - sx*0.5
		originZ := fr.pos.Local.Z - sz*0.5
		var grid components.FloorNavGrid
		grid.SizeX = szi
		grid.SizeZ = szj
		grid.Origin = components.Vec3{X: originX, Y: fr.pos.Local.Y, Z: originZ}

		// Seed all valid cells as open (Cost=4 - same as NavCostOpen for
		// chunk-NavGrid; A* uses the same scale).
		for cj := uint8(0); cj < szj; cj++ {
			for ci := uint8(0); ci < szi; ci++ {
				grid.Cells[int(cj)*components.MaxFloorSide+int(ci)] = components.NavCell{
					Cost:  navCostOpen,
					Flags: components.NavInBuilding,
				}
			}
		}

		// Rasterise walls that belong to this storey. "Same storey" =
		// |wall.Y - floor.Y| < floorHeight/2 AND same host chunk.
		for _, ws := range wallSnaps {
			if ws.pos.Chunk != fr.pos.Chunk {
				continue
			}
			if absDelta(ws.pos.Local.Y, fr.pos.Local.Y) > floorHeight*0.5 {
				continue
			}
			rasterizeFloorWall(&grid, ws.pos.Local, ws.w, ws.openingPassable, originX, originZ)
		}

		if existing := sys.floorNavMap.Get(fr.ent); existing != nil {
			*existing = grid
		} else {
			sys.floorNavMap.Add(fr.ent, &grid)
		}
		if !sys.floorNavBakedMap.Has(fr.ent) {
			sys.floorNavBakedMap.Add(fr.ent, &components.FloorNavBaked{})
		}
	}

	// ── Pass 4: TransitionRegistry edges ──
	//
	// Doors / Stairs / bunker entrances connect surface↔floor and floor↔floor
	// NavNodes. We rebuild every floor's outgoing edges whenever a Floor is
	// (re-)baked above; existing edges for the same owner are replaced. Phase 7
	// scope: every Floor in the world is touched whenever ANY chunk bakes,
	// which is fine on the placeholder scene; Phase 8+ may want per-owner
	// invalidation if rebuild cost becomes visible.
	registry := sys.transitionRes.Get()
	if registry == nil {
		return
	}

	// Drop every edge owned by an entity in the current floorTodo set so the
	// re-bake doesn't duplicate. (Phase 7 keeps the floor-owner association
	// loose: every transition is owned by either a Door/Stairs entity or by
	// the Floor itself; the latter happens for "implicit" edges that don't
	// have a dedicated source.) Simpler approach: wipe the entire registry
	// since the test scene has at most ~20 edges.
	for k := range registry.Out {
		delete(registry.Out, k)
	}

	// Snapshot every Floor entity we can address: walk every building's
	// child entities, filter by "has Floor component", and require a baked
	// FloorNavGrid so we know origin/size are valid.
	var floors []floorSnapshot
	bIdx := sys.buildingIndexRes.Get()
	if bIdx != nil {
		for _, children := range bIdx.Loaded {
			for _, c := range children {
				fComp := sys.floorComponentMap.Get(c)
				if fComp == nil {
					continue
				}
				if sys.floorNavMap.Get(c) == nil {
					continue
				}
				pos := sys.posMap.Get(c)
				if pos == nil {
					continue
				}
				floors = append(floors, floorSnapshot{ent: c, pos: *pos, f: *fComp})
			}
		}
	}
	if len(floors) == 0 {
		return
	}

	// Snapshot door walls (open doors only - closed = blocked, no edge).
	type doorSnap struct {
		ent     ecs.Entity
		pos     components.WorldPos
		w       components.WallSegment
		outward rl.Vector3
	}
	var doors []doorSnap
	qDW := sys.wallFilter.Query()
	for qDW.Next() {
		pos, w := qDW.Get()
		if w.OpeningKind != components.OpeningDoor {
			continue
		}
		// Phase 7: include all doors regardless of state (closed = Cost=0
		// edge, kept for forward-compat with Phase 12 open/close logic).
		_ = sys.doorMap.Get(qDW.Entity())
		var outward rl.Vector3
		if cd := sys.coverDirMap.Get(qDW.Entity()); cd != nil {
			outward = cd.Dir
		}
		doors = append(doors, doorSnap{ent: qDW.Entity(), pos: *pos, w: *w, outward: outward})
	}

	// Snapshot stairs.
	type stairsSnap struct {
		ent ecs.Entity
		pos components.WorldPos
		s   components.Stairs
	}
	var stairs []stairsSnap
	qS := sys.stairsFilter.Query()
	for qS.Next() {
		pos, s := qS.Get()
		stairs = append(stairs, stairsSnap{ent: qS.Entity(), pos: *pos, s: *s})
	}

	addEdge := func(from, to components.NavNode, cost uint8, owner ecs.Entity) {
		registry.Out[from] = append(registry.Out[from], components.TransitionEdge{
			From: from, To: to, Cost: cost, Owner: owner,
		})
	}

	// Doors -> 1 bidirectional edge between surface cell outside and floor
	// cell inside. Outside = surface cell at door centre + outward * 0.7 m.
	// Inside = floor cell at door centre - outward * 0.7 m.
	for _, d := range doors {
		// Door centre in world XZ.
		sa := float32(math.Sin(float64(d.w.Yaw)))
		ca := float32(math.Cos(float64(d.w.Yaw)))
		centreT := d.w.OpeningCenterT * d.w.Length
		baseX := float32(d.pos.Chunk.X) * components.ChunkSize
		baseZ := float32(d.pos.Chunk.Z) * components.ChunkSize
		cx := baseX + d.pos.Local.X + sa*centreT
		cz := baseZ + d.pos.Local.Z + ca*centreT
		// Find the floor entity hosting this door (same chunk, |Y - door.Y|
		// minimal). Doors live at storey-0 in Phase 5 placeholder buildings.
		fr := findFloorAt(floors, d.pos.Chunk, d.pos.Local.Y)
		if fr == nil {
			continue
		}
		// Floor-side cell.
		floorOriginX := float32(fr.pos.Chunk.X)*components.ChunkSize + fr.pos.Local.X - fr.f.SizeX*0.5
		floorOriginZ := float32(fr.pos.Chunk.Z)*components.ChunkSize + fr.pos.Local.Z - fr.f.SizeZ*0.5
		insideX := cx - d.outward.X*0.7
		insideZ := cz - d.outward.Z*0.7
		fi := int16(math.Floor(float64(insideX - floorOriginX)))
		fj := int16(math.Floor(float64(insideZ - floorOriginZ)))
		if fi < 0 || fi >= int16(fr.f.SizeX) || fj < 0 || fj >= int16(fr.f.SizeZ) {
			continue
		}
		floorNode := components.NavNode{Kind: components.NodeFloor, Floor: fr.ent, I: fi, J: fj}

		// Surface side. Convert outside world XZ to global (gi, gj).
		outsideX := cx + d.outward.X*0.7
		outsideZ := cz + d.outward.Z*0.7
		sgi := int32(math.Floor(float64(outsideX)))
		sgj := int32(math.Floor(float64(outsideZ)))
		surfChunk := components.ChunkCoord{X: sgi >> 6, Z: sgj >> 6}
		surfI := int16(sgi & 63)
		surfJ := int16(sgj & 63)
		surfNode := components.NavNode{Kind: components.NodeSurface, Chunk: surfChunk, I: surfI, J: surfJ}

		var cost uint8 = 3
		if dc := sys.doorMap.Get(d.ent); dc != nil && dc.State == components.DoorClosed {
			cost = 0 // closed door blocks
		}
		addEdge(surfNode, floorNode, cost, d.ent)
		addEdge(floorNode, surfNode, cost, d.ent)
	}

	// Stairs -> 1 bidirectional edge between two floor cells (or surface↔floor
	// for bunker entrance). FromFloor==0, ToFloor==1 with rise == bunkerDepth
	// is the bunker case - top "floor" doesn't exist as an entity, so we
	// emit a surface edge.
	for _, s := range stairs {
		// Bottom (FromFloor) is the floor whose Y matches stairs.Y in the same
		// chunk.
		fromFloor := findFloorAt(floors, s.pos.Chunk, s.pos.Local.Y)
		if fromFloor == nil {
			continue
		}
		// Top - Y at fromFloor.Y + rise.
		topY := s.pos.Local.Y + s.s.Rise
		// Stairs centre (XZ).
		sa := float32(math.Sin(float64(s.s.Yaw)))
		ca := float32(math.Cos(float64(s.s.Yaw)))
		bottomX := s.pos.Local.X
		bottomZ := s.pos.Local.Z
		topX := bottomX + sa*s.s.Length
		topZ := bottomZ + ca*s.s.Length

		// Floor cell on the bottom (centre near the foot of the stairs).
		fromOriginX := fromFloor.pos.Local.X - fromFloor.f.SizeX*0.5
		fromOriginZ := fromFloor.pos.Local.Z - fromFloor.f.SizeZ*0.5
		fI := int16(math.Floor(float64(bottomX - fromOriginX)))
		fJ := int16(math.Floor(float64(bottomZ - fromOriginZ)))
		if fI < 0 || fI >= int16(fromFloor.f.SizeX) || fJ < 0 || fJ >= int16(fromFloor.f.SizeZ) {
			continue
		}
		fromNode := components.NavNode{Kind: components.NodeFloor, Floor: fromFloor.ent, I: fI, J: fJ}

		// Find top end - a floor at topY, or surface (bunker entrance).
		toFloor := findFloorAt(floors, s.pos.Chunk, topY)
		if toFloor != nil {
			toOriginX := toFloor.pos.Local.X - toFloor.f.SizeX*0.5
			toOriginZ := toFloor.pos.Local.Z - toFloor.f.SizeZ*0.5
			tI := int16(math.Floor(float64(topX - toOriginX)))
			tJ := int16(math.Floor(float64(topZ - toOriginZ)))
			if tI < 0 || tI >= int16(toFloor.f.SizeX) || tJ < 0 || tJ >= int16(toFloor.f.SizeZ) {
				continue
			}
			toNode := components.NavNode{Kind: components.NodeFloor, Floor: toFloor.ent, I: tI, J: tJ}
			addEdge(fromNode, toNode, 4, s.ent)
			addEdge(toNode, fromNode, 4, s.ent)
		} else {
			// Bunker entrance: top is the surface cell above the stairs head.
			baseX := float32(s.pos.Chunk.X) * components.ChunkSize
			baseZ := float32(s.pos.Chunk.Z) * components.ChunkSize
			tx := baseX + topX
			tz := baseZ + topZ
			tgi := int32(math.Floor(float64(tx)))
			tgj := int32(math.Floor(float64(tz)))
			surfChunk := components.ChunkCoord{X: tgi >> 6, Z: tgj >> 6}
			toNode := components.NavNode{
				Kind:  components.NodeSurface,
				Chunk: surfChunk,
				I:     int16(tgi & 63),
				J:     int16(tgj & 63),
			}
			addEdge(fromNode, toNode, 4, s.ent)
			addEdge(toNode, fromNode, 4, s.ent)
		}
	}
}

// floorSnapshot - per-floor record used by the TransitionRegistry pass.
type floorSnapshot struct {
	ent ecs.Entity
	pos components.WorldPos
	f   components.Floor
}

// findFloorAt returns the floor whose pos.Chunk matches `chunk` and whose Y
// is closest to `y` within 0.5 m. Returns nil if no floor qualifies.
func findFloorAt(floors []floorSnapshot, chunk components.ChunkCoord, y float32) *floorSnapshot {
	var best *floorSnapshot
	bestD := float32(0.5)
	for i := range floors {
		fl := &floors[i]
		if fl.pos.Chunk != chunk {
			continue
		}
		d := absDelta(fl.pos.Local.Y, y)
		if d <= bestD {
			bestD = d
			best = fl
		}
	}
	return best
}

// bakeNavSlope writes per-cell Cost from the heightmap. Each cell maps 1:1 to
// one heightmap quad (NavGridSide == ChunkResolution-1). The Heightmap is
// row-major along +Z (Heights[j*ChunkResolution + i] = column i, row j); same
// row-major layout is mirrored on NavGrid.Cells for cache-coherent A*.
//
// "Slope" here is max pairwise corner-height delta (1 m cell, raw deltas).
// Diagonals are not divided by √2 - a sharp ridge on a diagonal still flags
// as steep, which is the conservative choice for movement.
func bakeNavSlope(grid *components.NavGrid, hm *components.Heightmap) {
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			slope := maxPairwiseAbs4(h00, h10, h01, h11)

			var cost uint8
			switch {
			case slope < navSlopeOpen:
				cost = navCostOpen
			case slope < navSlopeRough:
				cost = navCostRough
			default:
				cost = 0
			}
			// Phase 13 M13.4: CoverDistance defaults to "no cover within
			// scan radius"; Pass 2 (CoverDistance bake, after cover slot
			// spawn) overwrites this for cells near slots.
			grid.Cells[cj*components.NavGridSide+ci] = components.NavCell{
				Cost:          cost,
				CoverDistance: components.CoverDistanceFar,
			}
		}
	}
}

func maxPairwiseAbs4(a, b, c, d float32) float32 {
	best := absDelta(a, b)
	if v := absDelta(a, c); v > best {
		best = v
	}
	if v := absDelta(a, d); v > best {
		best = v
	}
	if v := absDelta(b, c); v > best {
		best = v
	}
	if v := absDelta(b, d); v > best {
		best = v
	}
	if v := absDelta(c, d); v > best {
		best = v
	}
	return best
}

func absDelta(a, b float32) float32 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}

// rasterizeWall marks every NavCell whose centre lies inside the wall's
// oriented rectangle (length × thickness, rotated by Yaw) as Cost=0. If the
// wall has a passable opening (only OpeningDoor + Door.State == DoorOpen), the
// opening segment along the wall axis is left untouched. Closed doors and
// windows leave the opening at Cost=0 (windows block movement; closed doors
// also block).
//
// Wall axis (after Yaw rotation around +Y): a = (sin Yaw, 0, cos Yaw); the
// perpendicular thickness axis is p = (cos Yaw, 0, -sin Yaw). Both unit and
// orthogonal - so a 2D point-in-rectangle test reduces to two orthogonal
// projections clamped against [0, Length] and [-Thickness/2, Thickness/2].
func rasterizeWall(grid *components.NavGrid, e wallEntry) {
	fromX, fromZ := e.local.X, e.local.Z
	yaw := e.w.Yaw
	length := e.w.Length
	halfT := e.w.Thickness * 0.5
	if length <= 0 || halfT <= 0 {
		return
	}

	sa := float32(math.Sin(float64(yaw)))
	ca := float32(math.Cos(float64(yaw)))

	// Wall-rectangle corners in chunk-local XZ. axis = (sa, ca), perp = (ca, -sa).
	ex := length * sa
	ez := length * ca
	px := halfT * ca
	pz := halfT * (-sa)
	corners := [4][2]float32{
		{fromX - px, fromZ - pz},
		{fromX + px, fromZ + pz},
		{fromX + ex - px, fromZ + ez - pz},
		{fromX + ex + px, fromZ + ez + pz},
	}
	minX, maxX := corners[0][0], corners[0][0]
	minZ, maxZ := corners[0][1], corners[0][1]
	for i := 1; i < 4; i++ {
		if corners[i][0] < minX {
			minX = corners[i][0]
		}
		if corners[i][0] > maxX {
			maxX = corners[i][0]
		}
		if corners[i][1] < minZ {
			minZ = corners[i][1]
		}
		if corners[i][1] > maxZ {
			maxZ = corners[i][1]
		}
	}
	iMin, iMax := clampCellRange(minX, maxX)
	jMin, jMax := clampCellRange(minZ, maxZ)
	if iMin >= iMax || jMin >= jMax {
		return
	}

	// Opening interval along the wall axis (only used if passable).
	openCenter := e.w.OpeningCenterT * length
	openStart := openCenter - e.w.OpeningWidth*0.5
	openEnd := openCenter + e.w.OpeningWidth*0.5

	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			cx := float32(ci) + 0.5
			cz := float32(cj) + 0.5
			dx := cx - fromX
			dz := cz - fromZ
			t := dx*sa + dz*ca
			n := dx*ca - dz*sa
			if t < 0 || t > length || n < -halfT || n > halfT {
				continue
			}
			if e.openingPassable && e.w.OpeningWidth > 0 {
				if t >= openStart && t <= openEnd {
					continue
				}
			}
			grid.Cells[cj*components.NavGridSide+ci].Cost = 0
		}
	}
}

// rasterizeFloorWall marks cells of a FloorNavGrid as Cost=0 inside the wall's
// oriented rectangle, mirroring rasterizeWall for chunk-NavGrids but with a
// (sizeX, sizeZ, origin) sub-block instead of a full 64×64 grid. Passable
// opening (open door) carves a gap along the wall axis. Windows and closed
// doors leave Cost=0 over the opening.
func rasterizeFloorWall(grid *components.FloorNavGrid, wallLocal rl.Vector3,
	w components.WallSegment, openingPassable bool,
	originX, originZ float32) {
	yaw := w.Yaw
	length := w.Length
	halfT := w.Thickness * 0.5
	if length <= 0 || halfT <= 0 {
		return
	}

	sa := float32(math.Sin(float64(yaw)))
	ca := float32(math.Cos(float64(yaw)))

	fromX := wallLocal.X - originX
	fromZ := wallLocal.Z - originZ

	ex := length * sa
	ez := length * ca
	px := halfT * ca
	pz := halfT * (-sa)
	corners := [4][2]float32{
		{fromX - px, fromZ - pz},
		{fromX + px, fromZ + pz},
		{fromX + ex - px, fromZ + ez - pz},
		{fromX + ex + px, fromZ + ez + pz},
	}
	minX, maxX := corners[0][0], corners[0][0]
	minZ, maxZ := corners[0][1], corners[0][1]
	for i := 1; i < 4; i++ {
		if corners[i][0] < minX {
			minX = corners[i][0]
		}
		if corners[i][0] > maxX {
			maxX = corners[i][0]
		}
		if corners[i][1] < minZ {
			minZ = corners[i][1]
		}
		if corners[i][1] > maxZ {
			maxZ = corners[i][1]
		}
	}
	iMin := int(math.Floor(float64(minX)))
	iMax := int(math.Ceil(float64(maxX)))
	jMin := int(math.Floor(float64(minZ)))
	jMax := int(math.Ceil(float64(maxZ)))
	if iMin < 0 {
		iMin = 0
	}
	if jMin < 0 {
		jMin = 0
	}
	if iMax > int(grid.SizeX) {
		iMax = int(grid.SizeX)
	}
	if jMax > int(grid.SizeZ) {
		jMax = int(grid.SizeZ)
	}
	if iMin >= iMax || jMin >= jMax {
		return
	}

	openCenter := w.OpeningCenterT * length
	openStart := openCenter - w.OpeningWidth*0.5
	openEnd := openCenter + w.OpeningWidth*0.5

	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			cx := float32(ci) + 0.5
			cz := float32(cj) + 0.5
			dx := cx - fromX
			dz := cz - fromZ
			t := dx*sa + dz*ca
			n := dx*ca - dz*sa
			if t < 0 || t > length || n < -halfT || n > halfT {
				continue
			}
			if openingPassable && w.OpeningWidth > 0 {
				if t >= openStart && t <= openEnd {
					continue
				}
			}
			grid.Cells[cj*components.MaxFloorSide+ci].Cost = 0
		}
	}
}

// rasterizePropCircle marks every NavCell within radius of (cx, cz) as Cost=0.
// Used for tree-stems / rocks / any prop with PropMeta.BlocksMove. BBoxRadius
// is a flat XZ approximation - fine for placeholder primitives, will be replaced
// by per-prop swept bounds when real meshes land in Phase 15.
func rasterizePropCircle(grid *components.NavGrid, cx, cz, r float32) {
	if r <= 0 {
		return
	}
	iMin, iMax := clampCellRange(cx-r, cx+r)
	jMin, jMax := clampCellRange(cz-r, cz+r)
	r2 := r * r
	for cj := jMin; cj < jMax; cj++ {
		for ci := iMin; ci < iMax; ci++ {
			ccx := float32(ci) + 0.5
			ccz := float32(cj) + 0.5
			dx := ccx - cx
			dz := ccz - cz
			if dx*dx+dz*dz > r2 {
				continue
			}
			grid.Cells[cj*components.NavGridSide+ci].Cost = 0
		}
	}
}

// applyNavRoads marks every cell whose centre lies within edge.Width/2 of any
// edge centre line as OnRoad with Cost=navCostRoad. Bridge-edges are treated
// the same as other roads - that's how the river-block pass later knows to
// leave them passable.
func applyNavRoads(grid *components.NavGrid, cc components.ChunkCoord, g *components.RoadGraph) {
	if g == nil || len(g.Edges) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for ei := range g.Edges {
		e := &g.Edges[ei]
		ax, az := worldXZ(g.Nodes[e.From].Pos)
		bx, bz := worldXZ(g.Nodes[e.To].Pos)
		halfW := e.Width * 0.5
		minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
		minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
		if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
			continue
		}
		stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
			func(idx int) {
				grid.Cells[idx].Flags |= components.NavOnRoad
				grid.Cells[idx].Cost = navCostRoad
			})
	}
}

// applyNavTrenches marks every cell within trench.Width/2 of any trench
// segment as InTrench with Cost=navCostTrench. Trenches are not impassable -
// just expensive - so AI prefers to skirt them on the open ground.
func applyNavTrenches(grid *components.NavGrid, cc components.ChunkCoord, tn *components.TrenchNetwork) {
	if tn == nil || len(tn.Lines) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for ti := range tn.Lines {
		t := &tn.Lines[ti]
		halfW := t.Width * 0.5
		for si := 0; si+1 < len(t.Points); si++ {
			ax, az := worldXZ(t.Points[si])
			bx, bz := worldXZ(t.Points[si+1])
			minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
			minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
			if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
				continue
			}
			stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
				func(idx int) {
					grid.Cells[idx].Flags |= components.NavInTrench
					grid.Cells[idx].Cost = navCostTrench
				})
		}
	}
}

// applyNavRiverBlock marks river cells as NearWater. Cost is forced to 0 only
// when the cell is *not* already OnRoad - i.e. only river cells with no bridge
// above become impassable. Bridge cells keep Cost=2 from the road pass and gain
// the NearWater flag for downstream consumers ("we are over water").
func applyNavRiverBlock(grid *components.NavGrid, cc components.ChunkCoord, rivers *components.Rivers) {
	if rivers == nil || len(rivers.Polylines) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for pi := range rivers.Polylines {
		pl := &rivers.Polylines[pi]
		halfW := pl.Width * 0.5
		for si := 0; si+1 < len(pl.Points); si++ {
			ax, az := worldXZ(pl.Points[si])
			bx, bz := worldXZ(pl.Points[si+1])
			minX, maxX := minF32(ax, bx)-halfW, maxF32(ax, bx)+halfW
			minZ, maxZ := minF32(az, bz)-halfW, maxF32(az, bz)+halfW
			if maxX < chunkMinX || minX > chunkMaxX || maxZ < chunkMinZ || minZ > chunkMaxZ {
				continue
			}
			stripCellPass(grid, cc, minX, maxX, minZ, maxZ, ax, az, bx, bz, halfW,
				func(idx int) {
					grid.Cells[idx].Flags |= components.NavNearWater
					if grid.Cells[idx].Flags&components.NavOnRoad == 0 {
						grid.Cells[idx].Cost = 0
					}
				})
		}
	}
}

// applyNavBuildings stamps NavInBuilding onto every surface cell whose centre
// (XZ world coord) lies inside any building Footprint that intersects the
// chunk. Cost is left alone so wall-rasterised Cost=0 cells stay impassable
// and open cells keep their slope-derived Cost - the flag is the bit that
// NavService.cellAt reads to refuse surface expansion through the interior.
// Access to the inside is reserved for TransitionEdges (Door / Stairs) that
// route into Floor NavNodes; pure surface paths must skirt the footprint.
func applyNavBuildings(grid *components.NavGrid, cc components.ChunkCoord, footprints []components.AABB2D) {
	if len(footprints) == 0 {
		return
	}
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	chunkMaxX := chunkMinX + components.ChunkSize
	chunkMaxZ := chunkMinZ + components.ChunkSize

	for fi := range footprints {
		fp := footprints[fi]
		if fp.MaxX <= chunkMinX || fp.MinX >= chunkMaxX ||
			fp.MaxZ <= chunkMinZ || fp.MinZ >= chunkMaxZ {
			continue
		}
		cellMinX, cellMaxX := clampCellRange(fp.MinX-chunkMinX, fp.MaxX-chunkMinX)
		cellMinZ, cellMaxZ := clampCellRange(fp.MinZ-chunkMinZ, fp.MaxZ-chunkMinZ)
		if cellMinX >= cellMaxX || cellMinZ >= cellMaxZ {
			continue
		}
		for cj := cellMinZ; cj < cellMaxZ; cj++ {
			cz := chunkMinZ + float32(cj) + 0.5
			if cz < fp.MinZ || cz > fp.MaxZ {
				continue
			}
			for ci := cellMinX; ci < cellMaxX; ci++ {
				cx := chunkMinX + float32(ci) + 0.5
				if cx < fp.MinX || cx > fp.MaxX {
					continue
				}
				grid.Cells[cj*components.NavGridSide+ci].Flags |= components.NavInBuilding
			}
		}
	}
}

// stripCellPass - common driver for the road / trench / river polyline-strip
// passes. Iterates exactly the cells whose AABB intersects the inflated edge
// bbox, runs a point-to-segment distance test, and invokes mark(idx) for any
// cell whose centre is within halfW of the segment.
func stripCellPass(grid *components.NavGrid, cc components.ChunkCoord,
	worldMinX, worldMaxX, worldMinZ, worldMaxZ float32,
	ax, az, bx, bz, halfW float32, mark func(idx int)) {
	chunkMinX := float32(cc.X) * components.ChunkSize
	chunkMinZ := float32(cc.Z) * components.ChunkSize
	cellMinX, cellMaxX := clampCellRange(worldMinX-chunkMinX, worldMaxX-chunkMinX)
	cellMinZ, cellMaxZ := clampCellRange(worldMinZ-chunkMinZ, worldMaxZ-chunkMinZ)
	if cellMinX >= cellMaxX || cellMinZ >= cellMaxZ {
		return
	}
	for cj := cellMinZ; cj < cellMaxZ; cj++ {
		for ci := cellMinX; ci < cellMaxX; ci++ {
			wx := chunkMinX + float32(ci) + 0.5
			wz := chunkMinZ + float32(cj) + 0.5
			if pointToSegment2D(wx, wz, ax, az, bx, bz) > halfW {
				continue
			}
			mark(cj*components.NavGridSide + ci)
		}
	}
}

// spawnCoverSlots emits cover-slot entities for one chunk:
//
//   - Per prop with Cover > 0 -> 8 radial slots (propCoverSlots).
//   - Per WallSegment with OpeningWindow -> 1 outward-facing slot (windowCoverSlots).
//   - Per pair of walls of one building meeting at a corner -> 1 corner slot
//     (wallCornerCoverSlots, deduped within the building).
//
// Slots are dual-indexed:
//
//   - PropChunkIndex / BuildingChildIndex - chunk-life ownership; eviction
//     tears them down with the host.
//   - CoverSlotIndex.ByHost[host] - host-keyed for Phase 11 destruction.
//
// Both indices must agree, otherwise eviction would leave dangling slots.
func (sys *SpatialBakeSystem) spawnCoverSlots(
	world *ecs.World,
	cc components.ChunkCoord,
	propIdx *PropChunkIndex,
	registry *components.PropTypeRegistry,
	coverIdx *CoverSlotIndex,
	buildingIdx *BuildingChildIndex,
) {
	// ── Prop slots ──
	if propIdx != nil && registry != nil && coverIdx != nil {
		// Snapshot the prop list - we'll be appending slot entities to the
		// same bucket and don't want to iterate the new slots as if they were
		// hosts.
		hostProps := append([]ecs.Entity(nil), propIdx.Loaded[cc]...)
		for _, propEnt := range hostProps {
			prop := sys.propMap.Get(propEnt)
			pos := sys.posMap.Get(propEnt)
			if prop == nil || pos == nil {
				continue
			}
			meta := registry.Metas[prop.Type]
			if meta.Cover <= 0 {
				continue
			}
			specs := propCoverSlots(propEnt, pos.Local, meta, prop.Scale)
			for _, sp := range specs {
				slotEnt := sys.spawnOneSlot(world, cc, sp)
				propIdx.Loaded[cc] = append(propIdx.Loaded[cc], slotEnt)
				coverIdx.ByHost[sp.Host] = append(coverIdx.ByHost[sp.Host], slotEnt)
			}
		}
	}

	// ── Wall-based slots (windows + corners) ──
	if coverIdx == nil || buildingIdx == nil {
		return
	}

	// Ark forbids archetype mutations while a query iterates; the spawn step
	// adds entities and components, so we MUST snapshot the wall set first
	// and only spawn after closing the query.
	type wallSnap struct {
		ent     ecs.Entity
		pos     components.WorldPos
		w       components.WallSegment
		root    ecs.Entity
		outward rl.Vector3
	}
	var wallSnaps []wallSnap
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		if pos.Chunk != cc {
			continue
		}
		ent := qW.Entity()
		member := sys.memberMap.Get(ent)
		if member == nil {
			continue
		}
		var outward rl.Vector3
		if cd := sys.coverDirMap.Get(ent); cd != nil {
			outward = cd.Dir
		}
		wallSnaps = append(wallSnaps, wallSnap{
			ent:     ent,
			pos:     *pos,
			w:       *w,
			root:    member.Building,
			outward: outward,
		})
	}

	wallsByBuilding := map[ecs.Entity][]wallCornerSrc{}
	type pendingSlot struct {
		spec coverSlotSpec
		root ecs.Entity
	}
	var pending []pendingSlot

	for i := range wallSnaps {
		s := &wallSnaps[i]
		if s.w.OpeningKind == components.OpeningWindow {
			for _, sp := range windowCoverSlots(s.ent, s.pos.Local, s.w, s.outward) {
				pending = append(pending, pendingSlot{spec: sp, root: s.root})
			}
		}
		wallsByBuilding[s.root] = append(wallsByBuilding[s.root],
			makeWallCornerSrc(s.ent, s.pos, s.w, s.outward))
	}

	for root, ws := range wallsByBuilding {
		for _, sp := range wallCornerCoverSlots(ws) {
			pending = append(pending, pendingSlot{spec: sp, root: root})
		}
	}

	for _, p := range pending {
		slotEnt := sys.spawnOneSlot(world, cc, p.spec)
		buildingIdx.Loaded[p.root] = append(buildingIdx.Loaded[p.root], slotEnt)
		coverIdx.ByHost[p.spec.Host] = append(coverIdx.ByHost[p.spec.Host], slotEnt)
	}
}

func (sys *SpatialBakeSystem) spawnOneSlot(world *ecs.World, cc components.ChunkCoord, sp coverSlotSpec) ecs.Entity {
	e := world.NewEntity()
	// Normalize through Add - a slot generated near a chunk edge can have a
	// Local component just outside [0, 64) and needs to fold into the
	// neighbouring chunk to keep the WorldPos invariant.
	pos := (components.WorldPos{Chunk: cc}).Add(sp.Local)
	sys.posMap.Add(e, &pos)
	sys.coverSlotMap.Add(e, &components.CoverSlot{
		Host:      sp.Host,
		HostKind:  sp.HostKind,
		OriginDir: sp.OriginDir,
		Quality:   sp.Quality,
		Stance:    sp.Stance,
	})
	sys.lodRelevantMap.Add(e, &components.LODRelevant{})
	return e
}

func makeWallCornerSrc(ent ecs.Entity, pos components.WorldPos, w components.WallSegment, outward rl.Vector3) wallCornerSrc {
	baseX := float32(pos.Chunk.X) * components.ChunkSize
	baseZ := float32(pos.Chunk.Z) * components.ChunkSize
	startX := baseX + pos.Local.X
	startZ := baseZ + pos.Local.Z
	sa := float32(math.Sin(float64(w.Yaw)))
	ca := float32(math.Cos(float64(w.Yaw)))
	return wallCornerSrc{
		entity: ent,
		startX: startX,
		startZ: startZ,
		endX:   startX + sa*w.Length,
		endZ:   startZ + ca*w.Length,
		normX:  outward.X,
		normZ:  outward.Z,
		baseY:  pos.Local.Y,
		chunk:  pos.Chunk,
	}
}

// CoverMap raycast tunables. Plan P10:
//
//	Length: 8 m fixed window
//	Step:   1 m, 8 sample points per ray
//	Observer eye height above terrain: 1.2 m (crouching infantryman, see DESIGN)
//	Terrain block threshold: 0.5 m above observer = LOS blocked.
//
// 8 directions on the compass starting at -Z (notional "north", though the
// world has no fixed compass) and going clockwise. Encoding into the DirMask
// bit position lets future tactical AI ask "is this cell defended from the
// direction of the threat?" with a 1-bit lookup.
const (
	coverRayLen     float32 = 8.0
	coverRayStep    float32 = 1.0
	coverObsHeight  float32 = 1.2
	coverTerrainPad float32 = 0.5
)

var coverDirs = [8][2]float32{
	{0, -1},                               // 0: N  (-Z)
	{0.70710678, -0.70710678},             // 1: NE
	{1, 0},                                // 2: E  (+X)
	{0.70710678, 0.70710678},              // 3: SE
	{0, 1},                                // 4: S  (+Z)
	{-0.70710678, 0.70710678},             // 5: SW
	{-1, 0},                               // 6: W  (-X)
	{-0.70710678, -0.70710678},            // 7: NW
}

// losWall is a per-bake LOS test record built once from a WallSegment + Door
// state. World coords; trig pre-computed; openingTransparent collapses
// "is this opening passable for a sight ray" into a single boolean.
type losWall struct {
	fromX, fromZ      float32
	sa, ca            float32
	length            float32
	openStart, openEnd float32

	openingPresent     bool
	openingTransparent bool // window OR open door - sight ray passes
}

// losProp is the LOS-blocking variant of a Prop in world coords with its
// effective XZ radius (scale already folded in).
type losProp struct {
	x, z, r float32
}

func makeLosWall(pos components.WorldPos, w components.WallSegment, doorState components.DoorState) losWall {
	baseX := float32(pos.Chunk.X) * components.ChunkSize
	baseZ := float32(pos.Chunk.Z) * components.ChunkSize
	sa := float32(math.Sin(float64(w.Yaw)))
	ca := float32(math.Cos(float64(w.Yaw)))
	wl := losWall{
		fromX:          baseX + pos.Local.X,
		fromZ:          baseZ + pos.Local.Z,
		sa:             sa,
		ca:             ca,
		length:         w.Length,
		openingPresent: w.OpeningKind != components.OpeningNone,
		openStart:      w.OpeningCenterT*w.Length - w.OpeningWidth*0.5,
		openEnd:        w.OpeningCenterT*w.Length + w.OpeningWidth*0.5,
	}
	switch w.OpeningKind {
	case components.OpeningWindow:
		// Plan: windows = LOS-transparent walls. Even at observer eye height
		// the line passes through the glass.
		wl.openingTransparent = true
	case components.OpeningDoor:
		// Closed = block both LOS and movement; open = pass.
		if doorState == components.DoorOpen {
			wl.openingTransparent = true
		}
	}
	return wl
}

// bakeCoverCells walks every cell of one chunk, fires 8 raycasts of 8 m at
// 1 m step, and emits CoverCell{BaseCover, DirMask}. Heightmap is sampled
// with bilinear interpolation across chunk seams (neighbour chunk pulled
// through chunkIdx; missing neighbour = "open" per plan).
func bakeCoverCells(cov *components.CoverMap, cc components.ChunkCoord,
	chunkIdx *TerrainChunkIndex, hmMap *ecs.Map[components.Heightmap],
	walls []losWall, props []losProp) {

	chunkBaseX := float32(cc.X) * components.ChunkSize
	chunkBaseZ := float32(cc.Z) * components.ChunkSize
	stepCount := int(coverRayLen / coverRayStep)

	for cj := 0; cj < components.NavGridSide; cj++ {
		for ci := 0; ci < components.NavGridSide; ci++ {
			cx := chunkBaseX + float32(ci) + 0.5
			cz := chunkBaseZ + float32(cj) + 0.5
			groundY, _ := sampleHeightmapBilinear(chunkIdx, hmMap, cx, cz)
			obsY := groundY + coverObsHeight

			var dirMask uint8
			for d := 0; d < 8; d++ {
				dx := coverDirs[d][0]
				dz := coverDirs[d][1]
				blocked := false

				// Terrain step-by-step. A missing-chunk sample is "open" so
				// chunks at the loaded-zone boundary slightly underestimate
				// cover - accepted in P10/edge-band notes.
				for s := 1; s <= stepCount; s++ {
					dist := float32(s) * coverRayStep
					gy, ok := sampleHeightmapBilinear(chunkIdx, hmMap, cx+dx*dist, cz+dz*dist)
					if !ok {
						continue
					}
					if gy+coverTerrainPad >= obsY {
						blocked = true
						break
					}
				}

				if !blocked {
					endX := cx + dx*coverRayLen
					endZ := cz + dz*coverRayLen
					if anyLosWallBlocks(walls, cx, cz, endX, endZ) {
						blocked = true
					} else if anyLosPropBlocks(props, cx, cz, endX, endZ) {
						blocked = true
					}
				}

				if blocked {
					dirMask |= 1 << d
				}
			}

			pop := bits.OnesCount8(dirMask)
			base := uint8(0)
			v := pop * 32
			if v > 255 {
				v = 255
			}
			base = uint8(v)
			cov.Cells[cj*components.NavGridSide+ci] = components.CoverCell{
				BaseCover: base,
				DirMask:   dirMask,
			}
		}
	}
}

// sampleHeightmapBilinear returns the bilinearly-interpolated height at a
// world XZ coord, pulling the appropriate chunk's Heightmap via chunkIdx.
// (false, _) when the host chunk isn't loaded.
//
// Vertices live on integer world coords (ChunkResolution=65, step=1 m). Inside
// a chunk we have 64×64 quads; outside the chunk we'd need the neighbour for
// the (i=64, j) edge - fetched if loaded, otherwise we clamp to the chunk's
// own edge so the function still returns a value rather than failing on the
// boundary.
func sampleHeightmapBilinear(chunkIdx *TerrainChunkIndex, hmMap *ecs.Map[components.Heightmap], wx, wz float32) (float32, bool) {
	if chunkIdx == nil {
		return 0, false
	}
	cc := components.ChunkCoord{
		X: int32(math.Floor(float64(wx / components.ChunkSize))),
		Z: int32(math.Floor(float64(wz / components.ChunkSize))),
	}
	ent, ok := chunkIdx.Loaded[cc]
	if !ok {
		return 0, false
	}
	hm := hmMap.Get(ent)
	if hm == nil {
		return 0, false
	}

	lx := wx - float32(cc.X)*components.ChunkSize
	lz := wz - float32(cc.Z)*components.ChunkSize
	if lx < 0 {
		lx = 0
	}
	if lz < 0 {
		lz = 0
	}
	maxIdx := float32(components.ChunkResolution - 1)
	if lx > maxIdx {
		lx = maxIdx
	}
	if lz > maxIdx {
		lz = maxIdx
	}
	i0 := int(math.Floor(float64(lx)))
	j0 := int(math.Floor(float64(lz)))
	if i0 >= components.ChunkResolution-1 {
		i0 = components.ChunkResolution - 2
	}
	if j0 >= components.ChunkResolution-1 {
		j0 = components.ChunkResolution - 2
	}
	fx := lx - float32(i0)
	fz := lz - float32(j0)
	row0 := j0 * components.ChunkResolution
	row1 := row0 + components.ChunkResolution
	h00 := hm.Heights[row0+i0]
	h10 := hm.Heights[row0+i0+1]
	h01 := hm.Heights[row1+i0]
	h11 := hm.Heights[row1+i0+1]
	hx0 := h00*(1-fx) + h10*fx
	hx1 := h01*(1-fx) + h11*fx
	return hx0*(1-fz) + hx1*fz, true
}

// anyLosWallBlocks returns true if any wall segment intersects the LOS
// segment in XZ AND the intersection isn't inside a transparent opening
// (window / open door).
func anyLosWallBlocks(walls []losWall, ax, az, bx, bz float32) bool {
	for i := range walls {
		w := &walls[i]
		toX := w.fromX + w.sa*w.length
		toZ := w.fromZ + w.ca*w.length
		t1, t2, ok := segmentSegmentIntersect2D(ax, az, bx, bz, w.fromX, w.fromZ, toX, toZ)
		if !ok {
			continue
		}
		if t1 < 0 || t1 > 1 || t2 < 0 || t2 > 1 {
			continue
		}
		if w.openingPresent {
			wallT := t2 * w.length
			if wallT >= w.openStart && wallT <= w.openEnd {
				if w.openingTransparent {
					continue
				}
				// Closed door case - fall through to "blocked".
			}
		}
		return true
	}
	return false
}

// anyLosPropBlocks: the LOS segment crosses (within radius) any LOS-blocking
// prop's column. Cylinder approximation - walls/columns are vertical cylinders
// at observer height for Phase 6.
func anyLosPropBlocks(props []losProp, ax, az, bx, bz float32) bool {
	for i := range props {
		if pointToSegment2D(props[i].x, props[i].z, ax, az, bx, bz) <= props[i].r {
			return true
		}
	}
	return false
}

// segmentSegmentIntersect2D - parametric line-line crossing test; t1 along
// segment 1 (p1->p2), t2 along segment 2 (p3->p4). Caller must clamp both to
// [0, 1] for actual segment intersection. (false, _, _) on parallel/colinear.
func segmentSegmentIntersect2D(p1x, p1z, p2x, p2z, p3x, p3z, p4x, p4z float32) (float32, float32, bool) {
	d1x := p2x - p1x
	d1z := p2z - p1z
	d2x := p4x - p3x
	d2z := p4z - p3z
	denom := d1x*d2z - d1z*d2x
	if math.Abs(float64(denom)) < 1e-9 {
		return 0, 0, false
	}
	dx := p3x - p1x
	dz := p3z - p1z
	t1 := (dx*d2z - dz*d2x) / denom
	t2 := (dx*d1z - dz*d1x) / denom
	return t1, t2, true
}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// bakeCoverDistance writes NavCell.CoverDistance for every chunk in coverTodo
// using cover-slot entities in the 9-chunk window. Scan radius is the same
// as CoverSeekThreshold - slots beyond stay at CoverDistanceFar.
//
// Phase 13 M13.4: brute-force O(cells × slots-in-9-chunks) per chunk. On the
// placeholder scene (50 slots / chunk × 4096 cells × 9 chunks ≈ 1.8 M ops) it
// fits in the spatial_bake budget; if Phase 14+ grows the slot population a
// KD-tree / spatial-hash pass would replace this.
func (sys *SpatialBakeSystem) bakeCoverDistance(coverTodo []spatialBakeChunkRec) {
	if len(coverTodo) == 0 {
		return
	}
	// Snapshot every cover slot in the world once, bucketed by host chunk.
	// Position is captured as chunk-local XZ - the scan converts to world
	// coords lazily per neighbour.
	type slotRec struct {
		x, z float32 // chunk-local XZ
	}
	slotsByChunk := map[components.ChunkCoord][]slotRec{}
	qS := sys.coverSlotFilter.Query()
	for qS.Next() {
		pos, _ := qS.Get()
		slotsByChunk[pos.Chunk] = append(slotsByChunk[pos.Chunk], slotRec{x: pos.Local.X, z: pos.Local.Z})
	}
	if len(slotsByChunk) == 0 {
		// No slots anywhere - nothing to do. CoverDistance stays at Far for
		// every cell, which is the correct "no cover bias" answer.
		return
	}

	scan := float32(components.CoverSeekThreshold)
	scanSq := scan * scan
	for _, rec := range coverTodo {
		grid := sys.navGridMap.Get(rec.id)
		if grid == nil {
			continue
		}
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				nb := components.ChunkCoord{X: rec.cc.X + dx, Z: rec.cc.Z + dz}
				slots, ok := slotsByChunk[nb]
				if !ok {
					continue
				}
				// Slot positions are chunk-local; translate to "relative to
				// rec.cc local origin" so cells (ci+0.5, cj+0.5) can be
				// compared directly without world-coord conversion.
				originDX := float32(dx) * components.ChunkSize
				originDZ := float32(dz) * components.ChunkSize
				for si := range slots {
					sx := slots[si].x + originDX
					sz := slots[si].z + originDZ
					// AABB cull around the slot.
					iMin, iMax := clampCellRange(sx-scan, sx+scan)
					jMin, jMax := clampCellRange(sz-scan, sz+scan)
					if iMin >= iMax || jMin >= jMax {
						continue
					}
					for cj := jMin; cj < jMax; cj++ {
						for ci := iMin; ci < iMax; ci++ {
							cx := float32(ci) + 0.5
							cz := float32(cj) + 0.5
							ddx := cx - sx
							ddz := cz - sz
							dSq := ddx*ddx + ddz*ddz
							if dSq > scanSq {
								continue
							}
							d := uint8(math.Sqrt(float64(dSq)))
							cell := &grid.Cells[cj*components.NavGridSide+ci]
							if d < cell.CoverDistance {
								cell.CoverDistance = d
							}
						}
					}
				}
			}
		}
	}
}

// clampCellRange - half-open [iMin, iMax) NavGrid cell indices that cover the
// world-XZ range [a, b]. Returns an empty range when the input is fully
// outside the chunk.
func clampCellRange(a, b float32) (int, int) {
	if a > b {
		a, b = b, a
	}
	iMin := int(math.Floor(float64(a)))
	iMax := int(math.Ceil(float64(b)))
	if iMin < 0 {
		iMin = 0
	}
	if iMax > components.NavGridSide {
		iMax = components.NavGridSide
	}
	if iMin > components.NavGridSide {
		iMin = components.NavGridSide
	}
	if iMax < 0 {
		iMax = 0
	}
	return iMin, iMax
}
