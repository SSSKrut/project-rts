package systems

import (
	"math"
	"math/rand"
)

// Terrain noise — single source of truth for ground height. Sampled in *world*
// coordinates so neighbouring chunks line up by construction. Both procgen
// and the ground-stick controller call GroundHeight, guaranteeing the anchor
// sits exactly on the surface and chunk seams stay continuous.

const terrainSeed int64 = 0x434F4C4457415221 // "COLD WAR!"

const (
	noiseScale       = 1.0 / 96.0 // ~96 m wavelength
	fbmOctaves       = 4
	fbmLacunarity    = 2.0
	fbmPersistence   = 0.5
	terrainAmplitude = 8.0 // peak-to-trough envelope, metres
)

// permTable: 256-entry permutation doubled to 512 to avoid wrap-around in the
// gradient lookup. Built once from terrainSeed at init.
var permTable [512]int

func init() {
	r := rand.New(rand.NewSource(terrainSeed))
	var p [256]int
	for i := 0; i < 256; i++ {
		p[i] = i
	}
	for i := 255; i > 0; i-- {
		j := r.Intn(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	for i := 0; i < 512; i++ {
		permTable[i] = p[i&255]
	}
}

// fade is Perlin's quintic ease curve: 6t^5 - 15t^4 + 10t^3.
func fade(t float64) float64 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(a, b, t float64) float64 {
	return a + t*(b-a)
}

// grad2 picks one of 8 unit-ish gradients on the XY plane based on the low
// bits of hash and returns its dot with (x, y). Standard Perlin trick:
// directions encoded in switch arms, no real lookup table.
func grad2(hash int, x, y float64) float64 {
	switch hash & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

// perlin2 evaluates classic 2D Perlin noise at (x, y). Output ~[-1, 1].
func perlin2(x, y float64) float64 {
	xi := int(math.Floor(x)) & 255
	yi := int(math.Floor(y)) & 255
	xf := x - math.Floor(x)
	yf := y - math.Floor(y)

	u := fade(xf)
	v := fade(yf)

	aa := permTable[permTable[xi]+yi]
	ab := permTable[permTable[xi]+yi+1]
	ba := permTable[permTable[xi+1]+yi]
	bb := permTable[permTable[xi+1]+yi+1]

	x1 := lerp(grad2(aa, xf, yf), grad2(ba, xf-1, yf), u)
	x2 := lerp(grad2(ab, xf, yf-1), grad2(bb, xf-1, yf-1), u)
	return lerp(x1, x2, v)
}

// fbm2 sums fbmOctaves octaves, doubling frequency / halving amplitude per
// octave. Result normalised to ~[-1, 1] by dividing by cumulative amplitude.
func fbm2(x, y float64) float64 {
	var sum, amp, totalAmp float64
	freq := 1.0
	amp = 1.0
	for i := 0; i < fbmOctaves; i++ {
		sum += perlin2(x*freq, y*freq) * amp
		totalAmp += amp
		freq *= fbmLacunarity
		amp *= fbmPersistence
	}
	if totalAmp == 0 {
		return 0
	}
	return sum / totalAmp
}

// GroundHeight returns the terrain height (Y, metres) at world (X, Z).
// Single source of truth for procgen and ground-stick.
func GroundHeight(worldX, worldZ float32) float32 {
	h := fbm2(float64(worldX)*noiseScale, float64(worldZ)*noiseScale)
	return float32(h * terrainAmplitude)
}
