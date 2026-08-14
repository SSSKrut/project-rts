package components

import "math"

// Atmosphere is the single source of truth for sky, wind and visibility.
// Render reads it every frame (clouds, cloud shadows, aerial fog); sim-side
// consumers (particle advection, smoke drift — later sensor range and audio
// detect) read the same numbers, which is why it is a world resource and not
// render state. Wind is stored as a world-axis vector; the swirling component
// is NOT stored — it is derived per point by WindAt from four statistics
// knobs, so weather is authored, the field is computed.
type WeatherKind uint8

const (
	WeatherClear WeatherKind = iota
	WeatherScattered
	WeatherOvercast
	WeatherStorm
	WeatherFog
	WeatherCount
)

type Atmosphere struct {
	Weather     WeatherKind
	Coverage    float32 // cloud coverage 0..1
	WindX       float32 // mean wind, m/s, world axes
	WindZ       float32
	Gustiness   float32 // 0..1, slow modulation of wind strength
	Turbulence  float32 // m/s amplitude of the curl (swirl) field
	TurbScaleM  float32 // metres per swirl
	VisibilityM float32 // aerial fog: ~95% extinction at this distance
	CloudBase   float32 // metres
	CloudTop    float32
}

// FogK converts visibility to the exponential extinction coefficient
// (3 ≈ -ln(0.05): 95% haze at VisibilityM).
func (a *Atmosphere) FogK() float32 {
	v := a.VisibilityM
	if v < 100 {
		v = 100
	}
	return 3.0 / v
}

type WeatherSpec struct {
	Label string
	Atmo  Atmosphere
}

var WeatherSpecs = [WeatherCount]WeatherSpec{
	WeatherClear: {"Clear", Atmosphere{Weather: WeatherClear,
		Coverage: 0.15, WindX: 4, WindZ: 2, Gustiness: 0.2, Turbulence: 0.6,
		TurbScaleM: 60, VisibilityM: 16000, CloudBase: 380, CloudTop: 760}},
	WeatherScattered: {"Scattered", Atmosphere{Weather: WeatherScattered,
		Coverage: 0.62, WindX: 9, WindZ: 4, Gustiness: 0.35, Turbulence: 1.2,
		TurbScaleM: 45, VisibilityM: 13600, CloudBase: 350, CloudTop: 800}},
	WeatherOvercast: {"Overcast", Atmosphere{Weather: WeatherOvercast,
		Coverage: 0.85, WindX: 7, WindZ: 3, Gustiness: 0.3, Turbulence: 1.0,
		TurbScaleM: 50, VisibilityM: 9000, CloudBase: 320, CloudTop: 700}},
	WeatherStorm: {"Storm", Atmosphere{Weather: WeatherStorm,
		Coverage: 0.95, WindX: 16, WindZ: 7, Gustiness: 0.7, Turbulence: 3.5,
		TurbScaleM: 35, VisibilityM: 5000, CloudBase: 280, CloudTop: 900}},
	WeatherFog: {"Fog", Atmosphere{Weather: WeatherFog,
		Coverage: 0.3, WindX: 1.5, WindZ: 0.7, Gustiness: 0.1, Turbulence: 0.4,
		TurbScaleM: 30, VisibilityM: 900, CloudBase: 350, CloudTop: 700}},
}

func DefaultAtmosphere() Atmosphere { return WeatherSpecs[WeatherScattered].Atmo }

// DayClock anchors the time of day to the sim clock: hours are DERIVED as
// Start + simNow*rate (no accumulator), so time compresses with TimeScale,
// stops on pause, and stays deterministic for future sim consumers (night
// sensor penalties). Render reads it every frame for the sun path. Weather
// presets never touch it — swapping to Storm must not move the clock.
type DayClock struct {
	StartHours  float32 // hours at simNow = 0
	HoursPerSec float32 // game-hours per sim-second; 0 freezes the clock
}

// DayClockRate: full day in 48 min at 1x speed (1.5 min at 32x).
const DayClockRate float32 = 1.0 / 120.0

func (d *DayClock) HoursAt(simNow float64) float32 {
	h := float32(math.Mod(float64(d.StartHours)+simNow*float64(d.HoursPerSec), 24))
	if h < 0 {
		h += 24
	}
	return h
}

// Anchor re-bases StartHours so HoursAt(simNow) == hours — rate changes and
// time jumps stay continuous.
func (d *DayClock) Anchor(hours float32, simNow float64) {
	d.StartHours = hours - float32(simNow)*d.HoursPerSec
}

func DefaultDayClock() DayClock {
	return DayClock{StartHours: 10.5, HoursPerSec: DayClockRate}
}
