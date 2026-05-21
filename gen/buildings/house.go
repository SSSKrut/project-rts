package buildings

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// GenerateHouse emits a BuildingPlan for a small rectangular house with
// 1 or 2 storeys. Deterministic from `seed`.
//
// `pos` carries the building footprint centre in world-space: Pos.Chunk
// + Pos.Local.XZ. Pos.Local.Y must already hold the surface ground height
// for that footprint - the caller (main.go) computes it before invoking
// the generator. For BuildingBunker the floor sits BunkerDepth below
// Pos.Local.Y; the entrance stair connects the surface to the sunken
// level.
func GenerateHouse(seed uint64, p HouseParams, pos components.WorldPos, kind components.BuildingKind) *components.BuildingPlan {
	if p.Stories == 0 {
		p.Stories = 1
	}
	if p.Stories > 5 {
		p.Stories = 5
	}
	if p.SizeX <= 0 {
		p.SizeX = 8
	}
	if p.SizeZ <= 0 {
		p.SizeZ = 8
	}

	b := NewBuilder(pos, kind, 0, seed)
	b.SetSize(p.SizeX, p.SizeZ)
	b.SetStories(p.Stories)

	cx, cz := pos.Local.X, pos.Local.Z
	halfX := p.SizeX * 0.5
	halfZ := p.SizeZ * 0.5
	minX := cx - halfX
	maxX := cx + halfX
	minZ := cz - halfZ
	maxZ := cz + halfZ

	floorY := pos.Local.Y
	if kind == components.BuildingBunker {
		floorY = pos.Local.Y - components.BunkerDepth
	}

	chunkBaseX := float32(pos.Chunk.X) * components.ChunkSize
	chunkBaseZ := float32(pos.Chunk.Z) * components.ChunkSize
	worldMinX := minX + chunkBaseX
	worldMaxX := maxX + chunkBaseX
	worldMinZ := minZ + chunkBaseZ
	worldMaxZ := maxZ + chunkBaseZ

	levelRefs := make([]uint8, p.Stories)
	for s := uint8(0); s < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		ref := b.AddLevel(
			fmt.Sprintf("L%d", s),
			components.AABB3D{
				MinX: worldMinX, MinY: baseY, MinZ: worldMinZ,
				MaxX: worldMaxX, MaxY: baseY + components.FloorHeight, MaxZ: worldMaxZ,
			},
			s,
		)
		levelRefs[s] = ref
	}

	// Phase 17 M17.D.3: multi-entrance support. DoorSides list wins; falling
	// back to the legacy single DoorSide field (with seed pick for 4+) when
	// the slice is empty.
	doorSet := uint8(0)
	if len(p.DoorSides) > 0 {
		for _, side := range p.DoorSides {
			if side < 4 {
				doorSet |= 1 << side
			}
		}
	} else {
		doorSide := p.DoorSide
		if doorSide >= 4 {
			doorSide = uint8(seed % 4)
		}
		doorSet = 1 << doorSide
	}

	for s := uint8(0); s < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		for side := uint8(0); side < 4; side++ {
			from, to, outward := wallEndpoints(minX, minZ, maxX, maxZ, baseY, side)
			wallIdx := b.AddWall(from, to, components.FloorHeight, components.WallThickness, outward, levelRefs[s])

			length := vec2Dist(from, to)
			if s == 0 && doorSet&(1<<side) != 0 {
				b.SetOpening(wallIdx, components.OpeningDoor, 0.5, 1.2, 0, 2.2)
				continue
			}

			roll := splitMix64(seed ^ (uint64(side)*0x9E37 + uint64(s)*0x12B9))
			if roll&1 == 0 && length >= 3.0 {
				centerT := 0.4 + 0.2*hashFloatU64(splitMix64(roll^0xA5A5))
				b.SetOpening(wallIdx, components.OpeningWindow, centerT, 1.4, 1.0, 1.2)
			}
		}
	}

	for s := uint8(0); s < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		b.AddFloor(rl.Vector3{X: cx, Y: baseY, Z: cz}, p.SizeX, p.SizeZ, s, levelRefs[s])
	}

	// Stair placement: straight ladder for 2-storey buildings, switchback
	// cascade for 3+. Cascade footprint = StairsLength x 2*StairsWidth, which
	// fits comfortably in the SW corner of an 8x8 plate. Tucked at minX+inset
	// so the centre of the floor stays clear.
	const inset float32 = 0.5
	useCascade := p.Stories >= 3
	for s := uint8(0); s+1 < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		local := rl.Vector3{X: minX + inset, Y: baseY, Z: minZ + inset}
		if useCascade {
			b.AddCascadeStair(local, 0, levelRefs[s], levelRefs[s+1])
		} else {
			b.AddStraightStair(
				local, 0,
				components.StairsLength, components.StairsWidth, components.FloorHeight,
				s, s+1,
				levelRefs[s], levelRefs[s+1],
			)
		}
	}

	// Phase 17 M17.D.3: optional interior partition. Splits each storey with
	// a single N-S wall biased by seed, with a 1.2 m doorway in the centre.
	// Minimal pass - no Room metadata is emitted, see plan note.
	if p.Interior {
		for s := uint8(0); s < p.Stories; s++ {
			baseY := floorY + float32(s)*components.FloorHeight
			// Partition runs N-S at ~1/3 across the X axis. Doorway centred.
			partX := minX + p.SizeX*0.4
			from := rl.Vector3{X: partX, Y: baseY, Z: minZ + 0.1}
			to := rl.Vector3{X: partX, Y: baseY, Z: maxZ - 0.1}
			outward := rl.Vector3{X: 1, Y: 0, Z: 0}
			wallIdx := b.AddWall(from, to, components.FloorHeight, components.WallThickness, outward, levelRefs[s])
			b.SetOpening(wallIdx, components.OpeningDoor, 0.5, 1.0, 0, 2.0)
		}
	}

	if kind == components.BuildingBunker {
		local := rl.Vector3{X: maxX - inset - components.StairsLength, Y: floorY, Z: minZ + inset}
		end := rl.Vector3{X: local.X, Y: local.Y + components.BunkerDepth, Z: local.Z + components.StairsLength}
		mid := rl.Vector3{X: local.X, Y: 0.5 * (local.Y + end.Y), Z: 0.5 * (local.Z + end.Z)}
		// Both anchors point at ground-floor level: the exterior surface is
		// not represented as a Level in Phase 16.5 (it's open ground, not a
		// building volume). Phase 16.B may introduce an Exterior pseudo-level.
		b.AddCustomStair(local, components.Stairs{
			FromFloor: 0,
			ToFloor:   1,
			Yaw:       0,
			Length:    components.StairsLength,
			Width:     components.StairsWidth,
			Rise:      components.BunkerDepth,
		}, []rl.Vector3{local, mid, end}, []components.StairAnchor{
			{WpIndex: 0, LevelRef: levelRefs[0]},
			{WpIndex: 2, LevelRef: levelRefs[0]},
		})
	}

	return b.Plan()
}

// wallEndpoints returns "from", "to" chunk-local endpoints, and the outward
// XZ normal for one of the four rectangular sides. Side numbering matches
// the legacy generateBuildingLayout: 0=south, 1=east, 2=north, 3=west.
func wallEndpoints(minX, minZ, maxX, maxZ, y float32, side uint8) (rl.Vector3, rl.Vector3, rl.Vector3) {
	switch side {
	case 0:
		return rl.Vector3{X: minX, Y: y, Z: minZ}, rl.Vector3{X: maxX, Y: y, Z: minZ}, rl.Vector3{X: 0, Y: 0, Z: -1}
	case 1:
		return rl.Vector3{X: maxX, Y: y, Z: minZ}, rl.Vector3{X: maxX, Y: y, Z: maxZ}, rl.Vector3{X: 1, Y: 0, Z: 0}
	case 2:
		return rl.Vector3{X: maxX, Y: y, Z: maxZ}, rl.Vector3{X: minX, Y: y, Z: maxZ}, rl.Vector3{X: 0, Y: 0, Z: 1}
	default:
		return rl.Vector3{X: minX, Y: y, Z: maxZ}, rl.Vector3{X: minX, Y: y, Z: minZ}, rl.Vector3{X: -1, Y: 0, Z: 0}
	}
}
