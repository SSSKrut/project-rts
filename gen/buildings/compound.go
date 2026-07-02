package buildings

import (
	"rts-go/components"
)

// GenerateCompound emits an L-shape multi-wing building as three adjacent
// BuildingPlans (main + east wing + north wing). Wings sit flush against the
// main rectangle; shared edges carry a doorway on both sides so units route
// between wings via NavService transitions just like between rooms.
func GenerateCompound(seed uint64, centre components.WorldPos) []*components.BuildingPlan {
	const mainSize float32 = 12
	const wingSize float32 = 8

	mainPos := centre

	eastPos := centre
	eastPos.Local.X += mainSize*0.5 + wingSize*0.5
	eastPos = components.Normalize(eastPos)

	northPos := centre
	northPos.Local.Z += mainSize*0.5 + wingSize*0.5
	northPos = components.Normalize(northPos)

	main := GenerateHouse(seed, HouseParams{
		Stories:   2,
		SizeX:     mainSize,
		SizeZ:     mainSize,
		DoorSides: []uint8{0, 1, 2}, // south = compound entrance, east+north -> wings
	}, mainPos, components.BuildingHouse)

	east := GenerateHouse(seed^0xE0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{3}, // west — shared with main
	}, eastPos, components.BuildingHouse)

	north := GenerateHouse(seed^0xA0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{0}, // south — shared with main
	}, northPos, components.BuildingHouse)

	return []*components.BuildingPlan{main, east, north}
}

// GenerateCompoundPlus emits a plus-shaped compound: main + 4 wings
// (east/west/north/south). Each wing connects to the main volume through
// a doorway on the shared wall, giving a higher-wing-count interior test.
func GenerateCompoundPlus(seed uint64, centre components.WorldPos) []*components.BuildingPlan {
	const mainSize float32 = 12
	const wingSize float32 = 8

	mainPos := centre

	eastPos := centre
	eastPos.Local.X += mainSize*0.5 + wingSize*0.5
	eastPos = components.Normalize(eastPos)

	westPos := centre
	westPos.Local.X -= mainSize*0.5 + wingSize*0.5
	westPos = components.Normalize(westPos)

	northPos := centre
	northPos.Local.Z += mainSize*0.5 + wingSize*0.5
	northPos = components.Normalize(northPos)

	southPos := centre
	southPos.Local.Z -= mainSize*0.5 + wingSize*0.5
	southPos = components.Normalize(southPos)

	main := GenerateHouse(seed, HouseParams{
		Stories:   2,
		SizeX:     mainSize,
		SizeZ:     mainSize,
		DoorSides: []uint8{0, 1, 2, 3}, // all sides connect to wings
	}, mainPos, components.BuildingHouse)

	// Every main-volume door opens into a wing, so each wing must carry an
	// exterior door of its own — without one the compound has zero
	// surface↔level transitions and units cannot enter it at all.
	east := GenerateHouse(seed^0xE0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{3, 1}, // west — shared with main; east — exterior
	}, eastPos, components.BuildingHouse)

	west := GenerateHouse(seed^0xB0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{1, 3}, // east — shared with main; west — exterior
	}, westPos, components.BuildingHouse)

	north := GenerateHouse(seed^0xA0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{0, 2}, // south — shared with main; north — exterior
	}, northPos, components.BuildingHouse)

	south := GenerateHouse(seed^0xC0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{2, 0}, // north — shared with main; south — exterior
	}, southPos, components.BuildingHouse)

	return []*components.BuildingPlan{main, east, west, north, south}
}
