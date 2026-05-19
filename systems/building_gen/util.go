package building_gen

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// splitMix64 mixes a uint64 seed into a deterministic uint64. Identical
// implementation to the systems package generator helper so output across
// pre- and post-Phase-16.5 binaries can be cross-checked.
func splitMix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// hashFloatU64 takes the high 24 bits of h and maps to [0, 1).
func hashFloatU64(h uint64) float32 {
	return float32(h>>40) / float32(1<<24)
}

// vec2Dist is the XZ-plane distance between two rl.Vector3 (Y ignored).
func vec2Dist(a, b rl.Vector3) float32 {
	dx := b.X - a.X
	dz := b.Z - a.Z
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}
