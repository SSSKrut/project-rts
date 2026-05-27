package buildings

// HouseParams configures GenerateHouse. Defaults via DefaultHouseParams.
type HouseParams struct {
	Stories uint8
	SizeX   float32
	SizeZ   float32

	// DoorSide (legacy): 0=south, 1=east, 2=north, 3=west, any other = pick
	// from seed. Honoured only when DoorSides is nil.
	DoorSide uint8

	// DoorSides lists exterior walls that get an entrance ([0..3]).
	// Duplicates ignored. Empty falls back to DoorSide.
	DoorSides []uint8

	// Interior adds partition walls dividing each storey into 2..3 rooms.
	// Wall segments only — no Room metadata yet.
	Interior bool

	// Wings: additional rectangular volumes attached to the main footprint
	// (Compound generator). Empty = simple rectangle.
	Wings []WingSpec
}

type ConnectKind uint8

const (
	ConnectPassage ConnectKind = iota
	ConnectDoor
)

// WingSpec is one rectangular volume attached to the main house.
// OffsetX/OffsetZ are wing centre relative to main centre. Stories defaults
// to main when zero.
type WingSpec struct {
	OffsetX, OffsetZ float32
	SizeX, SizeZ     float32
	Stories          uint8
	ConnectVia       ConnectKind
}

func DefaultHouseParams() HouseParams {
	return HouseParams{
		Stories:  1,
		SizeX:    8,
		SizeZ:    8,
		DoorSide: 4,
	}
}
