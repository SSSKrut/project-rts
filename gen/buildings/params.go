package buildings

// HouseParams configures GenerateHouse. Defaults via DefaultHouseParams.
//
// Phase 17 M17.D.1: Stories raised to 1..5; DoorSides slice replaces the
// single DoorSide knob (back-compat - empty defers to seed-derived single
// door, the legacy DoorSide field is still honoured when DoorSides is nil).
// Interior toggles partition walls inside the footprint; Wings drives
// multi-rectangle layouts (compound generator).
type HouseParams struct {
	Stories uint8   // 1..5
	SizeX   float32 // metres along world X
	SizeZ   float32 // metres along world Z

	// DoorSide (legacy): 0=south, 1=east, 2=north, 3=west, any other = pick
	// from seed. Used only when DoorSides is nil (back-compat with Phase 16.5
	// callsites). New code should populate DoorSides instead.
	DoorSide uint8

	// DoorSides lists the exterior walls that get an entrance. Empty falls
	// back to DoorSide (single entrance). Each value in [0..3]; duplicates
	// are ignored.
	DoorSides []uint8

	// Interior, when true, asks GenerateHouse to add light partition walls
	// dividing each storey into 2..3 rooms. Phase 17 M17.D ships the wall
	// segments; Room metadata (entity + AABB) is deferred until a consumer
	// lands in Phase 21.
	Interior bool

	// Wings describes additional rectangular volumes attached to the main
	// footprint - used by the Compound generator. Empty = simple rectangle.
	Wings []WingSpec
}

// ConnectKind names how a Wing attaches to the main footprint.
type ConnectKind uint8

const (
	// ConnectPassage = open opening in the shared wall, no door.
	ConnectPassage ConnectKind = iota
	// ConnectDoor = a closed door in the shared wall (Phase 17 ships the
	// passage variant; door is reserved).
	ConnectDoor
)

// WingSpec - one additional rectangular volume attached to the main house.
// OffsetX / OffsetZ are the wing centre relative to the main centre, in
// metres. SizeX / SizeZ are the wing footprint. Stories defaults to the
// main building's Stories when zero. ConnectVia selects how the shared
// wall is breached.
type WingSpec struct {
	OffsetX, OffsetZ float32
	SizeX, SizeZ     float32
	Stories          uint8
	ConnectVia       ConnectKind
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
