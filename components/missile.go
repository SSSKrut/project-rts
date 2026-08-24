package components

import "github.com/mlange-42/ark/ecs"

// Missile is a live round in flight (Phase 20 M2, P6). An entity rather than
// an instant resolution because only a round that takes TIME gives the target
// a window to answer — flares and the break turn are decisions precisely
// because the missile is still flying while they happen.
//
// Guidance is a velocity vector steered toward the aim point under a turn
// limit: a hard-turning target at close range beats the round with geometry,
// not dice. The single stochastic act is the decoy roll — once, in the
// terminal window, from a deterministic hash.
type Missile struct {
	Target  ecs.Entity
	VelX    float32
	VelY    float32
	VelZ    float32
	Speed   float32
	TurnRad float32 // max steering, rad/s
	Fuel    float32 // seconds of motor left
	Damage  float32
	Rolled  bool // the one decoy roll has happened
	Decoyed bool
	// Aim is the frozen point the round chases once decoyed or once the
	// target is gone. World-absolute metres.
	AimX, AimY, AimZ float32
}

// AirReflexKind — Break / Flare / Abort (Phase 20 P7), the mirror of
// VehicleReflexKind.
type AirReflexKind uint8

const (
	AirReflexNone AirReflexKind = iota
	// AirReflexBreak: hard turn away from the threat bearing, full speed,
	// down toward the deck — angular rate is the one quantity both the AA
	// gun's probability and a missile's chase geometry respect.
	AirReflexBreak
	// AirReflexFlare: Break plus decoys out — the window in which an IR
	// round may take the bait.
	AirReflexFlare
	// AirReflexAbort: the task is over; SendHome owns the rest.
	AirReflexAbort
)

// AircraftOverride is the always-present reactive state of an airframe.
// Mirror of VehicleOverride: every cross-tick datum lives here, so there is
// no PostLoad hook to forget.
type AircraftOverride struct {
	Kind      AirReflexKind
	Until     float32
	ThreatYaw float32
	LastAt    float32
}
