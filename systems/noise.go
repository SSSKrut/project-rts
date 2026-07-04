package systems

import (
	"math"
	"math/rand"
)

// Terrain noise — single source of truth for ground height. Sampled in
// world coordinates so neighbouring chunks line up. Both procgen and the
// ground-stick controller call GroundHeight, keeping chunk seams continuous.

const terrainSeed int64 = 0x434F4C4457415221 // "COLD WAR!"

// TerrainParams parameterises procgen so maps can span flat plains to
// mountains. Startup-only: set before the world exists.
type TerrainParams struct {
	Seed        int64   `json:"seed"`
	WavelengthM float64 `json:"wavelengthM"`
	Octaves     int     `json:"octaves"`
	Lacunarity  float64 `json:"lacunarity"`
	Persistence float64 `json:"persistence"`
	AmplitudeM  float64 `json:"amplitudeM"`
}

func DefaultTerrainParams() TerrainParams {
	return TerrainParams{
		Seed:        terrainSeed,
		WavelengthM: 96,
		Octaves:     4,
		Lacunarity:  2.0,
		Persistence: 0.5,
		AmplitudeM:  8.0,
	}
}

var (
	terrainCfg = DefaultTerrainParams()
	noiseScale = 1.0 / 96.0
)

// SetTerrainParams reconfigures procgen; zero fields fall back to defaults.
func SetTerrainParams(p TerrainParams) {
	d := DefaultTerrainParams()
	if p.Seed == 0 {
		p.Seed = d.Seed
	}
	if p.WavelengthM <= 0 {
		p.WavelengthM = d.WavelengthM
	}
	if p.Octaves <= 0 {
		p.Octaves = d.Octaves
	}
	if p.Lacunarity <= 0 {
		p.Lacunarity = d.Lacunarity
	}
	if p.Persistence <= 0 {
		p.Persistence = d.Persistence
	}
	if p.AmplitudeM == 0 {
		p.AmplitudeM = d.AmplitudeM
	}
	terrainCfg = p
	noiseScale = 1.0 / p.WavelengthM
	buildPermTable(p.Seed)
}

// 256-entry permutation doubled to 512 to avoid wrap-around in the gradient
// lookup.
var permTable [512]int

func buildPermTable(seed int64) {
	r := rand.New(rand.NewSource(seed))
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

func init() {
	buildPermTable(terrainSeed)
}

// fade is Perlin's quintic ease curve: 6t⁵ - 15t⁴ + 10t³.
func fade(t float64) float64 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(a, b, t float64) float64 {
	return a + t*(b-a)
}

// grad2 picks one of 8 unit-ish gradients on the XY plane from hash and
// returns its dot with (x, y).
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

// perlin2 evaluates 2D Perlin noise at (x, y). Output ~[-1, 1].
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

// fbm2 sums octaves, scaling frequency by lacunarity / amplitude by
// persistence per octave. Normalised to ~[-1, 1] via cumulative amplitude.
func fbm2(x, y float64) float64 {
	var sum, amp, totalAmp float64
	freq := 1.0
	amp = 1.0
	for i := 0; i < terrainCfg.Octaves; i++ {
		sum += perlin2(x*freq, y*freq) * amp
		totalAmp += amp
		freq *= terrainCfg.Lacunarity
		amp *= terrainCfg.Persistence
	}
	if totalAmp == 0 {
		return 0
	}
	return sum / totalAmp
}

// GroundHeight returns the terrain height (Y, metres) at world (X, Z).
func GroundHeight(worldX, worldZ float32) float32 {
	h := fbm2(float64(worldX)*noiseScale, float64(worldZ)*noiseScale)
	return float32(h * terrainCfg.AmplitudeM)
}
