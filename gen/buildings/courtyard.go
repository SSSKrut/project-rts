package buildings

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// CourtyardParams configures GenerateCourtyard. Well* are the open shaft in
// the middle; the gallery is what is left between the two wall rings.
type CourtyardParams struct {
	Stories uint8
	SizeX   float32
	SizeZ   float32
	WellX   float32
	WellZ   float32
	// DoorSides are exterior entrances ([0..3], S/E/N/W). Empty = south.
	DoorSides []uint8
}

func DefaultCourtyardParams() CourtyardParams {
	return CourtyardParams{Stories: 2, SizeX: 22, SizeZ: 18, WellX: 10, WellZ: 8}
}

// MinGalleryWidth keeps the ring wide enough to hold a stair run and let a
// squad file around the loop; the well shrinks to make room.
const MinGalleryWidth float32 = 3.5

// GenerateCourtyard emits a toroidal building: an outer wall ring, an inner
// wall ring around an open well, and a floor made of four strips forming the
// gallery. The point is the loop — a unit can walk all the way around inside.
//
// Note on navigation: LevelNavGrid seeds every cell of a level's AABB as open
// and only WALLS carve it, floors are not consulted. The well is therefore
// blocked by the inner ring rather than by the absence of a floor plate, which
// is exactly why the inner ring is solid on every storey.
func GenerateCourtyard(seed uint64, p CourtyardParams, pos components.WorldPos) *components.BuildingPlan {
	if p.Stories == 0 {
		p.Stories = 1
	}
	if p.Stories > 5 {
		p.Stories = 5
	}
	if p.SizeX < 12 {
		p.SizeX = 12
	}
	if p.SizeZ < 12 {
		p.SizeZ = 12
	}
	// Clamp the well so the gallery keeps its minimum width on both axes.
	if maxWellX := p.SizeX - 2*MinGalleryWidth; p.WellX > maxWellX {
		p.WellX = maxWellX
	}
	if maxWellZ := p.SizeZ - 2*MinGalleryWidth; p.WellZ > maxWellZ {
		p.WellZ = maxWellZ
	}
	if p.WellX < 2 {
		p.WellX = 2
	}
	if p.WellZ < 2 {
		p.WellZ = 2
	}

	b := NewBuilder(pos, components.BuildingHouse, 0, seed)
	b.SetSize(p.SizeX, p.SizeZ)
	b.SetStories(p.Stories)

	cx, cz := pos.Local.X, pos.Local.Z
	minX, maxX := cx-p.SizeX*0.5, cx+p.SizeX*0.5
	minZ, maxZ := cz-p.SizeZ*0.5, cz+p.SizeZ*0.5
	wMinX, wMaxX := cx-p.WellX*0.5, cx+p.WellX*0.5
	wMinZ, wMaxZ := cz-p.WellZ*0.5, cz+p.WellZ*0.5
	floorY := pos.Local.Y

	chunkBaseX := float32(pos.Chunk.X) * components.ChunkSize
	chunkBaseZ := float32(pos.Chunk.Z) * components.ChunkSize

	levelRefs := make([]uint8, p.Stories)
	for s := uint8(0); s < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		levelRefs[s] = b.AddLevel(fmt.Sprintf("L%d", s), components.AABB3D{
			MinX: minX + chunkBaseX, MinY: baseY, MinZ: minZ + chunkBaseZ,
			MaxX: maxX + chunkBaseX, MaxY: baseY + components.FloorHeight, MaxZ: maxZ + chunkBaseZ,
		}, s)
	}

	doorSet := uint8(0)
	if len(p.DoorSides) > 0 {
		for _, side := range p.DoorSides {
			if side < 4 {
				doorSet |= 1 << side
			}
		}
	} else {
		doorSet = 1 // south
	}

	for s := uint8(0); s < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight

		// Outer ring: doors on the ground storey, windows rolled elsewhere.
		for side := uint8(0); side < 4; side++ {
			from, to, outward := wallEndpoints(minX, minZ, maxX, maxZ, baseY, side)
			idx := b.AddWall(from, to, components.FloorHeight, components.WallThickness, outward, levelRefs[s])
			if s == 0 && doorSet&(1<<side) != 0 {
				b.SetOpening(idx, components.OpeningDoor, 0.5, 1.2, 0, 2.2)
				continue
			}
			roll := splitMix64(seed ^ (uint64(side)*0x71C3 + uint64(s)*0x2F1B))
			if roll&1 == 0 && vec2Dist(from, to) >= 3.0 {
				centerT := 0.35 + 0.3*hashFloatU64(splitMix64(roll^0xC0DE))
				b.SetOpening(idx, components.OpeningWindow, centerT, 1.4, 1.0, 1.2)
			}
		}

		// Inner ring around the well. wallEndpoints hands back the normal
		// pointing away from the well's interior, i.e. INTO the gallery — but
		// the occupied side is the gallery, so cover faces the other way.
		for side := uint8(0); side < 4; side++ {
			from, to, outward := wallEndpoints(wMinX, wMinZ, wMaxX, wMaxZ, baseY, side)
			inward := rl.Vector3{X: -outward.X, Y: 0, Z: -outward.Z}
			idx := b.AddWall(from, to, components.FloorHeight, components.WallThickness, inward, levelRefs[s])
			// Windows onto the well: light for the gallery, and firing arcs
			// across the courtyard once ShootingArc gets a consumer.
			roll := splitMix64(seed ^ (uint64(side)*0x93D7 + uint64(s)*0x5AE1 + 0x9001))
			if roll&1 == 0 && vec2Dist(from, to) >= 3.0 {
				b.SetOpening(idx, components.OpeningWindow, 0.5, 1.2, 1.0, 1.2)
			}
		}

		// Gallery floor: four strips. South/north span the full width; west/
		// east fill only the well's depth so the strips do not overlap.
		addStrip := func(x0, z0, x1, z1 float32) {
			w, d := x1-x0, z1-z0
			if w <= 0.01 || d <= 0.01 {
				return
			}
			b.AddFloor(rl.Vector3{X: (x0 + x1) * 0.5, Y: baseY, Z: (z0 + z1) * 0.5}, w, d, s, levelRefs[s])
		}
		addStrip(minX, minZ, maxX, wMinZ)
		addStrip(minX, wMaxZ, maxX, maxZ)
		addStrip(minX, wMinZ, wMinX, wMaxZ)
		addStrip(wMaxX, wMinZ, maxX, wMaxZ)

		// The strips are the level's ROOMS too. Without them every room-less
		// fallback (pickRoomTarget, fillRoomSlots) aims at the level AABB
		// centre — the middle of the open well, a goal nav can never reach.
		addRoom := func(x0, z0, x1, z1 float32) {
			if x1-x0 <= 0.01 || z1-z0 <= 0.01 {
				return
			}
			b.AddRoom(levelRefs[s], components.AABB2D{
				MinX: x0 + chunkBaseX, MinZ: z0 + chunkBaseZ,
				MaxX: x1 + chunkBaseX, MaxZ: z1 + chunkBaseZ,
			})
		}
		addRoom(minX, minZ, maxX, wMinZ)
		addRoom(minX, wMaxZ, maxX, maxZ)
		addRoom(minX, wMinZ, wMinX, wMaxZ)
		addRoom(wMaxX, wMinZ, maxX, wMaxZ)
	}

	// Stairs live in the south gallery strip, running along +X so they stay
	// clear of both wall rings.
	galleryZ := wMinZ - minZ
	stairZ := minZ + (galleryZ-components.StairsWidth)*0.5
	for s := uint8(0); s+1 < p.Stories; s++ {
		baseY := floorY + float32(s)*components.FloorHeight
		b.AddStraightStair(
			rl.Vector3{X: minX + 1.0, Y: baseY, Z: stairZ},
			halfPi,
			components.StairsLength, components.StairsWidth, components.FloorHeight,
			s, s+1, levelRefs[s], levelRefs[s+1],
		)
	}

	// Ring roof: four flat strips matching the gallery, so the well stays open
	// to the sky. A single cap would defeat the whole shape.
	topY := floorY + float32(p.Stories)*components.FloorHeight
	top := levelRefs[p.Stories-1]
	addRoofStrip := func(x0, z0, x1, z1 float32) {
		w, d := x1-x0, z1-z0
		if w <= 0.01 || d <= 0.01 {
			return
		}
		b.AddRoofSized(rl.Vector3{X: (x0 + x1) * 0.5, Y: topY, Z: (z0 + z1) * 0.5},
			components.RoofFlat, w, d, 0.3, 0, top)
	}
	o := components.RoofOverhang
	addRoofStrip(minX-o, minZ-o, maxX+o, wMinZ)
	addRoofStrip(minX-o, wMaxZ, maxX+o, maxZ+o)
	addRoofStrip(minX-o, wMinZ, wMinX, wMaxZ)
	addRoofStrip(wMaxX, wMinZ, maxX+o, wMaxZ)

	return b.Plan()
}
