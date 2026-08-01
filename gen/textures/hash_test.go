package textures

import (
	"hash/fnv"
	"testing"
)

// The dry-soil surface is the approved reference look. Tuning any shared knob
// (dome profile, smoothing, occlusion) must leave it untouched — if this hash
// moves, the change reached further than intended.
const drySoilReference = 0x52b1bc32449633e

func TestDrySoilUnchanged(t *testing.T) {
	h := fnv.New64a()
	h.Write(Ground(DrySoilParams(512, 12345)))
	if got := h.Sum64(); got != drySoilReference {
		t.Fatalf("dry soil changed: %#x, expected %#x", got, drySoilReference)
	}
}
