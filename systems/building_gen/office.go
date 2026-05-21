package building_gen

import "rts-go/components"

// GenerateOffice emits a 3-storey rectangular Office plan. Two entrances
// (south + east), central cascade stair, interior partition on each floor.
// Wraps GenerateHouse with the office-shaped HouseParams so wall / floor /
// window placement stays a single code path.
//
// Phase 17 M17.D.4 closure-criteria template: used by tactical_sandbox to
// stage Garrison test cases where the player must pick the safer entrance.
func GenerateOffice(seed uint64, pos components.WorldPos) *components.BuildingPlan {
	return GenerateHouse(seed, HouseParams{
		Stories:   3,
		SizeX:     14,
		SizeZ:     10,
		DoorSides: []uint8{0, 1}, // south + east
		Interior:  true,
	}, pos, components.BuildingHouse)
}
