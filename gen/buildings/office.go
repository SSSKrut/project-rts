package buildings

import "rts-go/components"

// GenerateOffice emits a 3-storey rectangular Office plan: two entrances
// (south + east), central cascade stair, interior partition on each floor.
// Wraps GenerateHouse so wall / floor / window placement stays a single path.
func GenerateOffice(seed uint64, pos components.WorldPos) *components.BuildingPlan {
	return GenerateHouse(seed, HouseParams{
		Stories:   3,
		SizeX:     14,
		SizeZ:     10,
		DoorSides: []uint8{0, 1}, // south + east
		Interior:  true,
	}, pos, components.BuildingHouse)
}
