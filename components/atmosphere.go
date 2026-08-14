package components

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
