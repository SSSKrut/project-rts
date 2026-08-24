package components

import "math"

// SensorKind enumerates the perception channels a unit can have. Optical is
// the infantry eye; Radar is the active emitter; ESM is the receiver that
// hears one. Thermal / Acoustic are reserved slots.
type SensorKind uint8

const (
	SensorOptical SensorKind = iota
	SensorThermal
	SensorRadar
	SensorAcoustic
	SensorESM
)

// FalloffKind selects the d → strength curve a SensorChannel uses. All curves
// land on the [0..1] range at d=0 and decay toward 0 at d≫r. Detection passes
// threshold-test the post-multiplier strength against 0.5.
type FalloffKind uint8

const (
	FalloffStep      FalloffKind = iota // strength=1 if d≤r else 0  (legacy hard cutoff)
	FalloffLinear                       // max(0, 1 - d/r)
	FalloffInvSquare                    // r² / (r² + d²)             ≈0.5 at d=r
	FalloffExp                          // exp(-d/r)                  ≈0.37 at d=r
	FalloffSigmoid                      // 1 / (1 + exp((d-r)/k))     smooth around r
)

// DimensionMask is a bit-set of physical targets a SensorChannel can perceive.
// Optical detects everything; Radar omits Infantry (signal too small); etc.
type DimensionMask uint8

const (
	DimInfantry DimensionMask = 1 << iota
	DimVehicle
	DimAir
	DimNaval
)

const DimAll DimensionMask = DimInfantry | DimVehicle | DimAir | DimNaval

// FacingProfile encodes how a sensor's effective range degrades off-axis.
// Three zones with linear blend between them: Forward (≤ ForwardConeRad) at
// ForwardMul, Side (≤ SideConeRad) lerping to SideMul, Rear (> SideConeRad)
// lerping to RearMul at π. Pi-bound angles let `acos(dot)` feed directly.
type FacingProfile struct {
	ForwardMul     float32
	SideMul        float32
	RearMul        float32
	ForwardConeRad float32
	SideConeRad    float32
}

// SensorChannel is one perception path. Stored inline in Sensors[4] so DOD
// iteration stays cache-friendly.
//
// EmitRangeM > 0 makes the channel an ACTIVE emitter: it sees by its own light
// and is heard by any ESM receiver out to EmitRangeM. That range is meant to
// exceed BaseRangeM — an emitter heard no further than it sees costs nothing
// to leave running, and the whole dilemma collapses (Phase 20 P4).
type SensorChannel struct {
	Kind        SensorKind
	BaseRangeM  float32
	EmitRangeM  float32
	FalloffKind FalloffKind
	Facing      FacingProfile
	DetectMask  DimensionMask
}

// Sensors carries up to 4 channels per entity. Count gives the active prefix;
// only Channels[:Count] participate in detection.
//
// OffMask marks channels switched OFF, not on: the zero value has to mean
// "everything works", or every existing spawner goes blind the day the field
// appears.
type Sensors struct {
	Channels [4]SensorChannel
	Count    uint8
	OffMask  uint8
}

func (s *Sensors) ChannelOn(i uint8) bool { return s.OffMask&(1<<i) == 0 }

func (s *Sensors) SetChannel(i uint8, on bool) {
	if on {
		s.OffMask &^= 1 << i
	} else {
		s.OffMask |= 1 << i
	}
}

// EmitRangeM is the loudest live emission — how far away an ESM receiver can
// hear this entity at all. Zero when nothing is radiating.
func (s *Sensors) EmitRangeM() float32 {
	best := float32(0)
	for i := uint8(0); i < s.Count; i++ {
		if !s.ChannelOn(i) {
			continue
		}
		if r := s.Channels[i].EmitRangeM; r > best {
			best = r
		}
	}
	return best
}

// FindChannel returns the index of the first channel of a kind, or -1.
func (s *Sensors) FindChannel(k SensorKind) int {
	for i := uint8(0); i < s.Count; i++ {
		if s.Channels[i].Kind == k {
			return int(i)
		}
	}
	return -1
}

// Falloff dispatches a FalloffKind to its curve. Returns 0 when r ≤ 0 to
// safe-guard mis-configured channels.
func Falloff(kind FalloffKind, d, r float32) float32 {
	if r <= 0 {
		return 0
	}
	switch kind {
	case FalloffStep:
		if d <= r {
			return 1
		}
		return 0
	case FalloffLinear:
		if d >= r {
			return 0
		}
		return 1 - d/r
	case FalloffInvSquare:
		rr := r * r
		return rr / (rr + d*d)
	case FalloffExp:
		return float32(math.Exp(-float64(d / r)))
	case FalloffSigmoid:
		k := 0.15 * r // steepness — narrow band around r
		return float32(1 / (1 + math.Exp(float64((d-r)/k))))
	}
	return 0
}

// FacingMul evaluates the piecewise blend at angle (radians, 0..π).
func FacingMul(angle float32, p FacingProfile) float32 {
	if angle <= p.ForwardConeRad {
		return p.ForwardMul
	}
	if angle <= p.SideConeRad {
		t := (angle - p.ForwardConeRad) / (p.SideConeRad - p.ForwardConeRad)
		return p.ForwardMul + (p.SideMul-p.ForwardMul)*t
	}
	t := (angle - p.SideConeRad) / (float32(math.Pi) - p.SideConeRad)
	if t > 1 {
		t = 1
	}
	return p.SideMul + (p.RearMul-p.SideMul)*t
}

// InfantryOpticalProfile is the default infantry eye — 60° forward cone @ 1.0,
// 120° side @ 0.8, rear @ 0.5. Used by UnitFactory.Spawn for new units.
var InfantryOpticalProfile = FacingProfile{
	ForwardMul:     1.0,
	SideMul:        0.8,
	RearMul:        0.5,
	ForwardConeRad: float32(math.Pi / 3),     // 60°
	SideConeRad:    float32(2 * math.Pi / 3), // 120°
}

// VehicleOpticalProfile - restricted vision out of an armoured cab.
var VehicleOpticalProfile = FacingProfile{
	ForwardMul:     1.0,
	SideMul:        0.6,
	RearMul:        0.2,
	ForwardConeRad: float32(math.Pi / 6), // 30°
	SideConeRad:    float32(math.Pi / 2), // 90°
}

// OmniProfile — no off-axis penalty at all. A radar antenna sweeps and an ESM
// receiver listens in every direction; facing is a property of eyeballs.
var OmniProfile = FacingProfile{
	ForwardMul:     1.0,
	SideMul:        1.0,
	RearMul:        1.0,
	ForwardConeRad: float32(math.Pi),
	SideConeRad:    float32(math.Pi),
}

var sensorLabel = [...]string{"Optical", "Thermal", "Radar", "Acoustic", "ESM"}

func (k SensorKind) String() string {
	if int(k) < len(sensorLabel) {
		return sensorLabel[k]
	}
	return "?"
}

// AircraftOpticalProfile — a crew looking DOWN at open ground loses far less
// off-axis than one peering out of a hull: nothing occludes, and the whole
// point of putting an observer up there is the wide field. Facing still
// matters (the nose is where the sight and the gun point), just gently.
var AircraftOpticalProfile = FacingProfile{
	ForwardMul:     1.0,
	SideMul:        0.85,
	RearMul:        0.55,
	ForwardConeRad: float32(math.Pi / 3),     // 60°
	SideConeRad:    float32(5 * math.Pi / 6), // 150°
}
