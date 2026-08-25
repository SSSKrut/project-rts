package components

import "github.com/mlange-42/ark/ecs"

// Comms is the lite game's core resource: a commander acts on the player's
// orders only while its radio net reaches it. Everything here is POD and
// coefficient-driven — no propagation model, no terrain, no antenna height
// (block A P9). Distance and one multiplier.

// CommsBand is the readable classification of Quality. Green is the ZERO
// VALUE on purpose: anything that never joins the net (a prop, a scene fixture,
// an entity from an older save) must read as "works", or the day the component
// lands every commander goes silent at once.
type CommsBand uint8

const (
	CommsGreen CommsBand = iota // orders arrive at once, shared sightings work
	CommsAmber                  // orders arrive late, no shared sightings
	CommsRed                    // new orders do not arrive; leash 150 m
	CommsDark                   // as Red, leash 60 m, own marker goes stale
)

// Floor of each band, indexed by CommsBand. Descending by construction.
var commsBandFloor = [...]float32{0.70, 0.40, 0.15, 0}

// commsHysteresis: a band change has to overshoot the boundary. Flicker here
// is flicker in whether orders arrive, and no player could read that.
const commsHysteresis float32 = 0.03

// Leash is how far from CommsState.Anchor a commander will still act on its
// own. Zero means unleashed.
var commsBandLeash = [...]float32{0, 0, 150, 60}

var commsBandLabel = [...]string{"Comms", "Degraded", "Cut", "Silent"}

func (b CommsBand) String() string {
	if int(b) < len(commsBandLabel) {
		return commsBandLabel[b]
	}
	return "?"
}

func (b CommsBand) LeashM() float32 {
	if int(b) < len(commsBandLeash) {
		return commsBandLeash[b]
	}
	return 0
}

// BandFor classifies a quality, refusing to move off `cur` until the boundary
// is cleared by commsHysteresis in the direction of travel.
func BandFor(q float32, cur CommsBand) CommsBand {
	raw := CommsDark
	for i := range commsBandFloor {
		if q >= commsBandFloor[i] {
			raw = CommsBand(i)
			break
		}
	}
	if raw == cur {
		return cur
	}
	if raw < cur { // improving — a lower index is a better band
		if q < commsBandFloor[raw]+commsHysteresis {
			return cur
		}
	} else if q > commsBandFloor[cur]-commsHysteresis {
		return cur
	}
	return raw
}

// CommsState lives on whatever can hold an order of its own: a squad, or a
// soloist vehicle / airframe that commands itself (Phase 20.7 L0). One
// component for every commander, so nothing has to ask "which kind is this".
type CommsState struct {
	Quality float32
	Band    CommsBand
	// Anchor is the target of the last DELIVERED order — the point a
	// disconnected commander stays within LeashM of. Anchored to the order,
	// not to the body: a leash measured from the current position travels
	// with it and constrains nothing.
	Anchor WorldPos
	// Undelivered / DeliverAt are block A M1; M0 leaves them zero.
	Undelivered ecs.Entity
	DeliverAt   float32
}

// Relay is a node of the net: a faction spawn, a captured control point
// (block B), or a command vehicle. Active is what a contested point clears.
type Relay struct {
	RangeM float32
	Active bool
}

// Comms tuning. Ranges live in the relay's own spec; these are the shared
// multipliers.
const (
	// A commander with no working radio still hears at half strength — close
	// to a relay that is Amber, which turns a dead radioman into "fall back to
	// the point", not into a mute unit.
	CommsNoRadioFactor float32 = 0.5
	// Altitude is line of sight, and line of sight is range. One number
	// instead of a propagation model, and it keeps aircraft inside the same
	// mechanic instead of exempting them.
	CommsAirRelayMul float32 = 3.0
	// How far a working set is HEARD. Larger than any relay range on purpose:
	// the net that lets you command is also the net that gives you away.
	RadioEmitDefaultM float32 = 900
	// Relay reach by source. The spawn is the deepest node of the net; a
	// command vehicle is the mobile one; a captured control point (block B)
	// sits between them.
	RelayRangeSpawnM   float32 = 800
	RelayRangePointM   float32 = 450
	RelayRangeCommandM float32 = 600
)
