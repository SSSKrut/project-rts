package buildings

import (
	"rts-go/components"
)

// GenerateCompound emits an L-shape multi-wing building as three adjacent
// BuildingPlans (main + east wing + north wing). The wings sit flush against
// the main rectangle, and each shared edge carries a doorway on both sides
// so units route between wings via NavService transitions just like they
// would between rooms.
//
// Phase 17 M17.D.5 closure-criteria template. Returned as a slice so callers
// (world_data.go, future tactical_sandbox) can append all three plans into
// BuildingPlanList in one step.
func GenerateCompound(seed uint64, centre components.WorldPos) []*components.BuildingPlan {
	const mainSize float32 = 12
	const wingSize float32 = 8

	mainPos := centre

	// East wing - centre shifted +X by (main/2 + wing/2).
	eastPos := centre
	eastPos.Local.X += mainSize*0.5 + wingSize*0.5
	eastPos = components.Normalize(eastPos)

	// North wing - centre shifted +Z.
	northPos := centre
	northPos.Local.Z += mainSize*0.5 + wingSize*0.5
	northPos = components.Normalize(northPos)

	main := GenerateHouse(seed, HouseParams{
		Stories:   2,
		SizeX:     mainSize,
		SizeZ:     mainSize,
		DoorSides: []uint8{0, 1, 2}, // south = compound entrance, east -> wing, north -> wing
	}, mainPos, components.BuildingHouse)

	east := GenerateHouse(seed^0xE0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{3}, // west - shared with main
	}, eastPos, components.BuildingHouse)

	north := GenerateHouse(seed^0xA0, HouseParams{
		Stories:   1,
		SizeX:     wingSize,
		SizeZ:     wingSize,
		DoorSides: []uint8{0}, // south - shared with main
	}, northPos, components.BuildingHouse)

	return []*components.BuildingPlan{main, east, north}
}
