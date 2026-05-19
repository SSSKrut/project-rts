package building_gen

// HouseParams configures HouseTemplate. Defaults via DefaultHouseParams.
type HouseParams struct {
	Stories uint8   // 1 or 2
	SizeX   float32 // metres along world X
	SizeZ   float32 // metres along world Z
	// DoorSide picks which exterior wall hosts the door: 0=south, 1=east,
	// 2=north, 3=west. Any other value (e.g. 4) defers to seed.
	DoorSide uint8
}

// DefaultHouseParams returns the canonical small house: one storey, 8x8.
func DefaultHouseParams() HouseParams {
	return HouseParams{
		Stories:  1,
		SizeX:    8,
		SizeZ:    8,
		DoorSide: 4,
	}
}
