package components

// AircraftKind indexes AircraftSpecs. Phase 20 flies rotary wing only; fixed
// wing arrives with off-map sortie requests, which need a different control
// surface (no loiter, no hover, a run-in geometry instead of a route).
type AircraftKind uint8

const (
	AircraftHeliAttack AircraftKind = iota
	AircraftHeliTransport
	AircraftKindCount
)

// AltRef says what AltSet is measured from. AGL follows the terrain, which is
// what nap-of-the-earth flight IS; AMSL is a flat shelf for transit. Making
// this a reference plus a number keeps altitude continuous — bands are read
// off the result (see AltBandOf), never stored, or masked flight becomes
// inexpressible and every band change becomes a teleport.
type AltRef uint8

const (
	AltAGL AltRef = iota
	AltAMSL
)

// AltBand classifies a height for the only three questions that need it: who
// can see the airframe, who can reach it, and whether terrain masks it.
type AltBand uint8

const (
	AltBandNOE AltBand = iota
	AltBandLow
	AltBandMedium
	AltBandTransit
)

// Band edges in metres AGL.
const (
	altNOECeil    float32 = 40
	altLowCeil    float32 = 250
	altMediumCeil float32 = 1500
)

func AltBandOf(agl float32) AltBand {
	switch {
	case agl <= altNOECeil:
		return AltBandNOE
	case agl <= altLowCeil:
		return AltBandLow
	case agl <= altMediumCeil:
		return AltBandMedium
	}
	return AltBandTransit
}

var altBandLabel = [...]string{"NOE", "Low", "Medium", "Transit"}

func (b AltBand) String() string {
	if int(b) < len(altBandLabel) {
		return altBandLabel[b]
	}
	return "?"
}

// Aircraft is the airborne locomotion state. The airframe deliberately carries
// no OnGround marker: GroundStick leaves its Y alone and AirDriverSystem owns
// the vertical axis outright.
//
// AltSet and SpeedSet are the player's DIALS, not order parameters — they are
// sticky and outlive every waypoint. That is the whole control model (P1): a
// route says where, the dials say how, and neither is re-issued per tick.
//
// Exit is where this airframe leaves the map when its work is done. It is
// stamped at release time by whatever made the airframe available — a
// scenario arrival now, a pad later (P9) — so egress needs no second lookup.
type Aircraft struct {
	Kind      AircraftKind
	AltRef    AltRef
	AltSet    float32
	SpeedSet  float32
	Fuel      float32 // seconds of flight left
	Exit      WorldPos
	Egressing bool
}

// AirArrival is one scheduled appearance, held as an ordinary entity so it
// saves, hashes and despawns like everything else. AirTrafficSystem releases
// it when the clock passes At and removes it.
//
// Schedule and pad are meant to be two SOURCES of one release, not two
// subsystems: both end at AircraftFactory.Spawn. Keeping that seam is the
// whole reason the arrival is data rather than a branch in a spawner.
type AirArrival struct {
	At         float32
	Kind       AircraftKind
	FactionID  uint8
	Controller uint8
	Entry      WorldPos
	Exit       WorldPos
	AltSet     float32
	AltRef     AltRef
	SpeedSet   float32
}
