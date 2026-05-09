package systems

// BiomeKind selects which density mask to sample. Densities are independent
// across kinds — bushes can grow inside forests, rocks can sit in plains; the
// spawner unions independent rolls instead of choosing one biome per cell.
type BiomeKind uint8

const (
	BiomeForest   BiomeKind = iota // dense trees (Oak/Pine/Birch)
	BiomeBushland                  // open scrub (Bush)
	BiomeRocky                     // boulder fields (Rock)
	BiomePlains                    // mostly empty
)

// Biome fBm tuning. Lower frequency than terrain (1/96) → larger homogeneous
// patches: forests several chunks wide, not vertex-scale speckle. Two octaves
// = soft blobs, no detail.
const (
	biomeBaseFreq    = 1.0 / 256.0
	biomeFbmOctaves  = 2
	biomeFbmLac      = 2.0
	biomeFbmPersist  = 0.5
	biomeKindOffsetX = 1024.0
	biomeKindOffsetZ = 911.0
)

// BiomeDensity returns a [0, 1] mask value at (wx, wz) for the requested
// biome. Same Perlin permutation as GroundHeight, but with a per-kind
// coordinate offset so the four masks decorrelate (we don't have a runtime-
// keyable Perlin — perm table built once in init — so input-plane shifting is
// the standard trick).
//
// Result is shaped so ~half the world rolls > 0.5; spawner thresholds pick
// this off directly.
func BiomeDensity(seed int64, wx, wz float32, kind BiomeKind) float32 {
	kf := float64(kind+1) + float64(seed%97)
	ox := biomeKindOffsetX * kf
	oz := biomeKindOffsetZ * kf

	x := float64(wx)*biomeBaseFreq + ox
	z := float64(wz)*biomeBaseFreq + oz

	var sum, totalAmp float64
	freq := 1.0
	amp := 1.0
	for i := 0; i < biomeFbmOctaves; i++ {
		sum += perlin2(x*freq, z*freq) * amp
		totalAmp += amp
		freq *= biomeFbmLac
		amp *= biomeFbmPersist
	}
	if totalAmp == 0 {
		return 0
	}
	raw := sum / totalAmp
	v := float32(raw*0.5 + 0.5)
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	return v
}

// hashFloat is a deterministic per-coordinate RNG independent of any global
// rand state. SplitMix64 mixer over (seed, args...). Returns float32 in [0,1).
func hashFloat(seed int64, args ...int32) float32 {
	h := uint64(seed)
	h = mix64(h)
	for _, a := range args {
		h ^= uint64(uint32(a))
		h = mix64(h)
	}
	return float32(h>>40) / float32(1<<24)
}

func hashU32(seed int64, args ...int32) uint32 {
	h := uint64(seed)
	h = mix64(h)
	for _, a := range args {
		h ^= uint64(uint32(a))
		h = mix64(h)
	}
	return uint32(h >> 32)
}

// mix64 is the SplitMix64 finalizer.
func mix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z = z ^ (z >> 31)
	return z
}
