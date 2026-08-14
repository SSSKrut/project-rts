package main

import (
	"flag"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Daylight: one CPU-side palette per frame drives every shader. The sun runs
// east -> south -> west (sunrise 06:00, sunset 18:00, N = -Z); the moon rides
// the same path 12 h out of phase, so night keeps a directional light. All
// color ramps key on the SUN's elevation (night -> dusk -> day stops), while
// the direct term fades to zero at either light's horizon — the 180-degree
// direction swap at 06:00/18:00 happens with no light to show it.
var hourFlag = flag.Float64("hour", -1, "dev: fix time of day (hours 0..24, freezes the clock)")

const (
	maxSunElevRad  = 52 * math.Pi / 180
	maxMoonElevRad = 40 * math.Pi / 180
)

type skyPalette struct {
	lightDir  [3]float32 // toward the light: sun by day, moon by night
	lightCol  [3]float32
	ambSky    [3]float32 // hemisphere ambient, upper half (worldFS uSkyColor)
	ambBounce [3]float32 // lower half
	zenith    [3]float32
	horizon   [3]float32
	cloudAmbB [3]float32 // cloud-march ambient at layer base / top
	cloudAmbT [3]float32
	shadowDir [3]float32 // y-clamped light for the cloud-shadow projection
}

// Ramp stops. Day values are the pre-M4 constants, so noon reproduces the
// look every earlier screenshot was tuned against.
var (
	daySunCol  = [3]float32{0.82, 0.78, 0.68}
	duskSunCol = [3]float32{0.90, 0.44, 0.20}
	moonCol    = [3]float32{0.10, 0.12, 0.18}

	ambSkyStops    = [3][3]float32{{0.045, 0.055, 0.095}, {0.30, 0.25, 0.27}, {0.55, 0.58, 0.63}}
	ambBounceStops = [3][3]float32{{0.020, 0.024, 0.040}, {0.15, 0.11, 0.10}, {0.34, 0.33, 0.29}}
	zenithStops    = [3][3]float32{{0.013, 0.022, 0.050}, {0.15, 0.19, 0.36}, {0.32, 0.49, 0.72}}
	horizonStops   = [3][3]float32{{0.035, 0.048, 0.090}, {0.93, 0.49, 0.26}, {0.80, 0.84, 0.89}}
	cloudAmbBStops = [3][3]float32{{0.045, 0.055, 0.085}, {0.40, 0.29, 0.29}, {0.50, 0.54, 0.62}}
	cloudAmbTStops = [3][3]float32{{0.085, 0.095, 0.145}, {0.86, 0.64, 0.56}, {0.98, 1.00, 1.04}}
)

func daylightPalette(hours float32) skyPalette {
	sunD := celestialDir(hours, maxSunElevRad)
	moonD := celestialDir(hours-12, maxMoonElevRad)

	// night -> dusk -> day, keyed on the sun alone.
	duskUp := smooth01(-0.14, 0.02, sunD[1])
	dayUp := smooth01(0.06, 0.38, sunD[1])
	ramp := func(stops [3][3]float32) [3]float32 {
		return lerp3(lerp3(stops[0], stops[1], duskUp), stops[2], dayUp)
	}

	p := skyPalette{
		ambSky:    ramp(ambSkyStops),
		ambBounce: ramp(ambBounceStops),
		zenith:    ramp(zenithStops),
		horizon:   ramp(horizonStops),
		cloudAmbB: ramp(cloudAmbBStops),
		cloudAmbT: ramp(cloudAmbTStops),
	}

	if sunD[1] >= moonD[1] {
		p.lightDir = sunD
		hue := lerp3(duskSunCol, daySunCol, dayUp)
		p.lightCol = scale3(hue, smooth01(0.02, 0.14, sunD[1]))
	} else {
		p.lightDir = moonD
		p.lightCol = scale3(moonCol, smooth01(0.02, 0.14, moonD[1]))
	}

	p.shadowDir = p.lightDir
	if p.shadowDir[1] < 0.30 {
		p.shadowDir[1] = 0.30
		p.shadowDir = normalize3(p.shadowDir)
	}
	return p
}

// celestialDir: unit vector toward the body for hour-of-day h. t=0 rise in
// the east, t=0.5 culmination in the south (+Z), t=1 set in the west;
// elevation goes negative outside [0,1] so "below horizon" needs no branch.
func celestialDir(h, maxElev float32) [3]float32 {
	t := float64(h-6) / 12
	for t < -1 {
		t += 2
	}
	for t > 1 {
		t -= 2
	}
	elev := math.Sin(t*math.Pi) * float64(maxElev)
	az := (0.5 + t) * math.Pi // from north, clockwise
	ce := math.Cos(elev)
	return normalize3([3]float32{
		float32(math.Sin(az) * ce),
		float32(math.Sin(elev)),
		float32(-math.Cos(az) * ce),
	})
}

// setVec3 copies the array into a fresh slice: passing &struct-field memory
// to cgo trips the pointer check when the enclosing allocation holds Go
// pointers (the palette lives inside Game).
func setVec3(shader rl.Shader, loc int32, v [3]float32) {
	rl.SetShaderValue(shader, loc, []float32{v[0], v[1], v[2]}, rl.ShaderUniformVec3)
}

func smooth01(a, b, x float32) float32 {
	t := (x - a) / (b - a)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

func lerp3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

func scale3(v [3]float32, s float32) [3]float32 {
	return [3]float32{v[0] * s, v[1] * s, v[2] * s}
}
