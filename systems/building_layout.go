package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// floorHeight is the per-storey vertical span (metres). Slim placeholder; real
// building models in Phase 15 will dictate their own.
const floorHeight float32 = 3.0
const wallThickness float32 = 0.3

// bunkerDepth - how far below surface a Bunker building floor sits. Matches
// Stamper.RectCut depth applied by the BuildingSystem terrain pass.
const bunkerDepth float32 = 3.0
const bunkerFalloffWidth float32 = 4.0

const stairsLength float32 = 3.0
const stairsWidth float32 = 1.5

// childKind tags one entry in the layout output.
type childKind uint8

const (
	childWall childKind = iota
	childFloor
	childStairs
)

// childSpec is the discriminated-union output of the layout generator. Caller
// reads Kind, then the matching sub-struct. Pure data - no ECS handles - so
// the generator stays a pure function (deterministic from Building.Seed).
type childSpec struct {
	Kind childKind

	// Position is in chunk-local coords of the building's host chunk.
	Local rl.Vector3

	Wall   components.WallSegment
	Floor  components.Floor
	Stairs components.Stairs

	// Outward normal for walls - written into CoverDirection on the entity.
	OutwardNormal rl.Vector3
}

// generateBuildingLayout returns the spec list for one Building. Pure
// function: same Building -> same children, every spawn cycle. Children are
// expressed in chunk-local coordinates of the host chunk so the caller can
// drop them straight into WorldPos.Local.
func generateBuildingLayout(b components.Building, hostChunkBaseX, hostChunkBaseZ, surfaceY float32) []childSpec {
	out := make([]childSpec, 0, 16)

	fp := b.Footprint
	// Footprint corners in chunk-local space.
	minX := fp.MinX - hostChunkBaseX
	maxX := fp.MaxX - hostChunkBaseX
	minZ := fp.MinZ - hostChunkBaseZ
	maxZ := fp.MaxZ - hostChunkBaseZ
	cx := 0.5 * (minX + maxX)
	cz := 0.5 * (minZ + maxZ)
	sizeX := maxX - minX
	sizeZ := maxZ - minZ

	floorY := surfaceY
	if b.Kind == components.BuildingBunker {
		floorY = surfaceY - bunkerDepth
	}

	// Floors - one per storey. Bunkers also count from sunken Y up.
	for s := uint8(0); s < b.Stories; s++ {
		out = append(out, childSpec{
			Kind: childFloor,
			Local: rl.Vector3{
				X: cx,
				Y: floorY + float32(s)*floorHeight,
				Z: cz,
			},
			Floor: components.Floor{Level: s, SizeX: sizeX, SizeZ: sizeZ},
		})
	}

	// Walls - for each storey, four sides. Layout-seed picks which side gets
	// the door (storey 0 only) and which non-door sides get a window.
	doorSide := uint8(b.Seed % 4) // 0=south, 1=east, 2=north, 3=west

	for s := uint8(0); s < b.Stories; s++ {
		baseY := floorY + float32(s)*floorHeight
		for side := uint8(0); side < 4; side++ {
			fromX, fromZ, length, yaw, normX, normZ := wallEndpoints(minX, minZ, maxX, maxZ, side)
			opening := components.OpeningNone
			openingT := float32(0.5)
			openingW := float32(0)
			openingBottom := float32(0)
			openingH := float32(0)

			// Door only on storey 0 of the chosen side.
			if s == 0 && side == doorSide {
				opening = components.OpeningDoor
				openingW = 1.2
				openingBottom = 0
				openingH = 2.2
			} else {
				// 50/50 chance per side per storey for a window - deterministic.
				roll := splitMix64(b.Seed ^ (uint64(side)*0x9E37 + uint64(s)*0x12B9))
				if roll&1 == 0 && length >= 3.0 {
					opening = components.OpeningWindow
					openingW = 1.4
					openingBottom = 1.0
					openingH = 1.2
					// Centre-T jittered slightly so multiple walls don't all
					// stack windows at exact same fraction.
					openingT = 0.4 + 0.2*hashFloatU64(splitMix64(roll^0xA5A5))
				}
			}

			out = append(out, childSpec{
				Kind: childWall,
				Local: rl.Vector3{
					X: fromX,
					Y: baseY,
					Z: fromZ,
				},
				Wall: components.WallSegment{
					Length:         length,
					Yaw:            yaw,
					Height:         floorHeight,
					Thickness:      wallThickness,
					OpeningKind:    opening,
					OpeningCenterT: openingT,
					OpeningWidth:   openingW,
					OpeningBottom:  openingBottom,
					OpeningHeight:  openingH,
				},
				OutwardNormal: rl.Vector3{X: normX, Y: 0, Z: normZ},
			})
		}
	}

	// Stairs - placed in one corner per gap between adjacent storeys.
	for s := uint8(0); s+1 < b.Stories; s++ {
		baseY := floorY + float32(s)*floorHeight
		// Anchor at the (minX, minZ) corner with a small inset so the slab
		// doesn't poke through walls.
		inset := float32(0.5)
		out = append(out, childSpec{
			Kind: childStairs,
			Local: rl.Vector3{
				X: minX + inset,
				Y: baseY,
				Z: minZ + inset,
			},
			Stairs: components.Stairs{
				FromFloor: s,
				ToFloor:   s + 1,
				Yaw:       0,
				Length:    stairsLength,
				Width:     stairsWidth,
				Rise:      floorHeight,
			},
		})
	}

	// Bunker entrance: stairs from surface (above floorY+bunkerDepth) down to
	// the sunken floor 0. Placed at the (maxX, minZ) corner inset.
	if b.Kind == components.BuildingBunker {
		inset := float32(0.5)
		out = append(out, childSpec{
			Kind: childStairs,
			Local: rl.Vector3{
				X: maxX - inset - stairsLength,
				Y: floorY,
				Z: minZ + inset,
			},
			Stairs: components.Stairs{
				FromFloor: 0,
				ToFloor:   1, // virtual surface level
				Yaw:       0,
				Length:    stairsLength,
				Width:     stairsWidth,
				Rise:      bunkerDepth,
			},
		})
	}

	return out
}

// wallEndpoints returns the "from" corner, segment length along its Yaw axis,
// the Yaw itself, and the outward normal (X, Z). Side numbering is
// 0=south (+X dir), 1=east (+Z dir), 2=north (-X dir), 3=west (-Z dir).
func wallEndpoints(minX, minZ, maxX, maxZ float32, side uint8) (float32, float32, float32, float32, float32, float32) {
	switch side {
	case 0: // south wall: from (minX, minZ) to (maxX, minZ); along +X (yaw=π/2)
		return minX, minZ, maxX - minX, float32(math.Pi / 2), 0, -1
	case 1: // east wall: from (maxX, minZ) to (maxX, maxZ); along +Z (yaw=0)
		return maxX, minZ, maxZ - minZ, 0, 1, 0
	case 2: // north wall: from (maxX, maxZ) to (minX, maxZ); along -X (yaw=-π/2)
		return maxX, maxZ, maxX - minX, float32(-math.Pi / 2), 0, 1
	default: // west wall: from (minX, maxZ) to (minX, minZ); along -Z (yaw=π)
		return minX, maxZ, maxZ - minZ, float32(math.Pi), -1, 0
	}
}

// splitMix64 - same constants as the existing mix64 in biome.go; duplicated
// here so the layout generator stays self-contained on uint64 inputs (the
// other helpers run on int32 args).
func splitMix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func hashFloatU64(h uint64) float32 {
	return float32(h>>40) / float32(1<<24)
}
