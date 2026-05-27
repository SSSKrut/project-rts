package systems

import (
	"math"
	"math/bits"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// CoverMap raycast tunables:
//
//	Length: 8 m fixed window
//	Step:   1 m, 8 sample points per ray
//	Observer eye height: 1.2 m (crouching infantryman)
//	Terrain block threshold: 0.5 m above observer ⇒ LOS blocked.
//
// 8 directions starting at -Z (notional "north"), clockwise. Encoding into
// DirMask bit position lets tactical AI ask "is this cell defended from the
// direction of the threat?" with a 1-bit lookup.
const (
	coverRayLen     float32 = 8.0
	coverRayStep    float32 = 1.0
	coverObsHeight  float32 = 1.2
	coverTerrainPad float32 = 0.5
)

var coverDirs = [8][2]float32{
	{0, -1},                    // 0: N  (-Z)
	{0.70710678, -0.70710678},  // 1: NE
	{1, 0},                     // 2: E  (+X)
	{0.70710678, 0.70710678},   // 3: SE
	{0, 1},                     // 4: S  (+Z)
	{-0.70710678, 0.70710678},  // 5: SW
	{-1, 0},                    // 6: W  (-X)
	{-0.70710678, -0.70710678}, // 7: NW
}

// losWall is a per-bake LOS test record built from a WallSegment + Door
// state. World coords; trig pre-computed; openingTransparent collapses
// "is this opening passable for a sight ray" into a single boolean.
type losWall struct {
	fromX, fromZ       float32
	sa, ca             float32
	length             float32
	openStart, openEnd float32

	openingPresent     bool
	openingTransparent bool // window OR open door
}

// losProp is the LOS-blocking variant of a Prop in world coords with its
// effective XZ radius (scale folded in).
type losProp struct {
	x, z, r float32
}

// bakeCoverPass is Pass 2: CoverMap + cover slots bake. Walks coverFilter
// (chunks without CoverBaked), builds LOS wall / prop buckets for the
// 9-chunk window, produces CoverMap.Cells + CoverCell.DirMask, spawns
// cover-slot entities, and re-walks every coverTodo chunk to write
// NavCell.CoverDistance.
func (sys *SpatialBakeSystem) bakeCoverPass(ctx core.UpdateContext) {
	var coverTodo []spatialBakeChunkRec
	qC := sys.coverFilter.Query()
	for qC.Next() {
		cc, _, _ := qC.Get()
		coverTodo = append(coverTodo, spatialBakeChunkRec{id: qC.Entity(), cc: *cc})
	}
	if len(coverTodo) == 0 {
		return
	}
	chunkIdx := sys.chunkIndexRes.Get()

	// Bucket LOS-walls by host chunk once per tick; reused for every chunk.
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
		// Walls / props within ray-cast reach can sit in any of the 8
		// surrounding chunks.
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

	// Done here in Pass 2 so newly spawned cover slots are visible to the
	// distance scan.
	sys.bakeCoverDistance(coverTodo)
}

// spawnCoverSlots emits cover-slot entities for one chunk:
//
//   - Per prop with Cover > 0: 8 radial slots (propCoverSlots).
//   - Per WallSegment with OpeningWindow: 1 outward-facing slot.
//   - Per pair of walls of one building meeting at a corner: 1 corner slot
//     (deduped within the building).
//
// Slots are dual-indexed:
//   - PropChunkIndex / BuildingChildIndex — chunk-life ownership; eviction
//     tears them down with the host.
//   - CoverSlotIndex.ByHost[host] — host-keyed for destruction.
//
// Both indices must agree or eviction leaves dangling slots.
func (sys *SpatialBakeSystem) spawnCoverSlots(
	world *ecs.World,
	cc components.ChunkCoord,
	propIdx *PropChunkIndex,
	registry *components.PropTypeRegistry,
	coverIdx *CoverSlotIndex,
	buildingIdx *BuildingChildIndex,
) {
	if propIdx != nil && registry != nil && coverIdx != nil {
		// Snapshot — we'll append slot entities to the same bucket and don't
		// want to iterate the new slots as if they were hosts.
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

	if coverIdx == nil || buildingIdx == nil {
		return
	}

	// Ark forbids archetype mutations during a live query; snapshot first
	// then spawn after closing.
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
	// Normalize through Add — a slot near a chunk edge may have Local just
	// outside [0, 64); fold into the neighbouring chunk for WorldPos invariant.
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
		// Windows = LOS-transparent walls.
		wl.openingTransparent = true
	case components.OpeningDoor:
		if doorState == components.DoorOpen {
			wl.openingTransparent = true
		}
	}
	return wl
}

// bakeCoverCells fires 8 raycasts of 8 m at 1 m step per cell and emits
// CoverCell{BaseCover, DirMask}. Heightmap is sampled with bilinear
// interpolation across chunk seams (missing neighbour = "open").
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

// sampleHeightmapBilinear returns the bilinearly-interpolated height at
// world XZ. (false, _) when the host chunk isn't loaded.
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

// anyLosPropBlocks: LOS segment crosses (within radius) any LOS-blocking
// prop's column. Cylinder approximation at observer height.
func anyLosPropBlocks(props []losProp, ax, az, bx, bz float32) bool {
	for i := range props {
		if pointToSegment2D(props[i].x, props[i].z, ax, az, bx, bz) <= props[i].r {
			return true
		}
	}
	return false
}

// segmentSegmentIntersect2D — parametric line-line crossing. Caller must
// clamp t1, t2 to [0, 1] for actual segment intersection.
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

// bakeCoverDistance writes NavCell.CoverDistance using cover-slot entities
// in the 9-chunk window. Scan radius = CoverSeekThreshold; slots beyond
// stay at CoverDistanceFar. Brute-force O(cells × slots); fits the budget
// at current populations.
func (sys *SpatialBakeSystem) bakeCoverDistance(coverTodo []spatialBakeChunkRec) {
	if len(coverTodo) == 0 {
		return
	}
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
				originDX := float32(dx) * components.ChunkSize
				originDZ := float32(dz) * components.ChunkSize
				for si := range slots {
					sx := slots[si].x + originDX
					sz := slots[si].z + originDZ
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
