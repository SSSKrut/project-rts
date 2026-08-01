package textures

import (
	"math"
	"testing"
)

const testSize = 256

func presets() map[string]GroundParams {
	return map[string]GroundParams{
		"meadow": MeadowParams(testSize, 12345),
		"soil":   DrySoilParams(testSize, 12345),
		"gravel": GravelParams(testSize, 12345),
		"sand":   SandParams(testSize, 12345),
		"soft":   SoftGroundParams(testSize, 12345),
	}
}

// The tile is drawn edge-to-edge, so a discontinuity across the wrap shows up
// as a visible grid line. Compare the step across the seam with the typical
// interior step: anything much larger means a layer stopped wrapping.
func TestGroundSeamless(t *testing.T) {
	for name, p := range presets() {
		h, _ := groundFields(p)
		n := testSize

		var interior, seamX, seamZ float64
		for y := 0; y < n; y++ {
			for x := 1; x < n; x++ {
				interior += math.Abs(float64(h[y*n+x] - h[y*n+x-1]))
			}
			seamX += math.Abs(float64(h[y*n] - h[y*n+n-1]))
		}
		for x := 0; x < n; x++ {
			seamZ += math.Abs(float64(h[x] - h[(n-1)*n+x]))
		}
		interior /= float64(n * (n - 1))
		seamX /= float64(n)
		seamZ /= float64(n)

		if seamX > interior*2 {
			t.Errorf("%s: x seam discontinuous: %.5f vs interior %.5f", name, seamX, interior)
		}
		if seamZ > interior*2 {
			t.Errorf("%s: z seam discontinuous: %.5f vs interior %.5f", name, seamZ, interior)
		}
	}
}

// Slopes must average out to flat, or the whole surface tilts towards the sun
// on one side.
func TestGroundSlopeBalanced(t *testing.T) {
	for name, p := range presets() {
		px := Ground(p)
		var sumX, sumY float64
		for i := 0; i < len(px); i += 4 {
			sumX += float64(px[i])/255*2 - 1
			sumY += float64(px[i+1])/255*2 - 1
		}
		count := float64(len(px) / 4)
		if math.Abs(sumX/count) > 0.02 || math.Abs(sumY/count) > 0.02 {
			t.Errorf("%s: slope bias x=%.4f z=%.4f", name, sumX/count, sumY/count)
		}
	}
}

// Every surface has to actually carry relief: a flat AO channel means its
// layers cancelled or the occlusion pass lost them.
func TestGroundHasRelief(t *testing.T) {
	for name, p := range presets() {
		if name == "soft" {
			continue // broad undulation only, by design
		}
		px := Ground(p)
		minAO, maxAO := uint8(255), uint8(0)
		var strongSlopes int
		for i := 0; i < len(px); i += 4 {
			ao := px[i+2]
			if ao < minAO {
				minAO = ao
			}
			if ao > maxAO {
				maxAO = ao
			}
			if px[i] < 90 || px[i] > 165 {
				strongSlopes++
			}
		}
		// A soft surface legitimately has a narrow AO range — the check is that
		// the layers reached the output at all, not that they are harsh.
		if maxAO-minAO < 20 {
			t.Errorf("%s: AO range too flat: %d..%d", name, minAO, maxAO)
		}
		if frac := float64(strongSlopes) / float64(len(px)/4); frac < 0.05 {
			t.Errorf("%s: only %.1f%% of texels carry slope", name, frac*100)
		}
	}
}

// The surfaces have to be distinguishable — two presets collapsing onto the
// same look would make the blend pointless.
func TestGroundPresetsDiffer(t *testing.T) {
	all := presets()
	names := []string{"meadow", "soil", "gravel", "sand"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := Ground(all[names[i]]), Ground(all[names[j]])
			var diff float64
			for k := 0; k < len(a); k += 4 {
				diff += math.Abs(float64(a[k]) - float64(b[k]))
			}
			if diff /= float64(len(a) / 4); diff < 5 {
				t.Errorf("%s vs %s: mean slope difference only %.2f", names[i], names[j], diff)
			}
		}
	}
}

func TestGroundDeterministic(t *testing.T) {
	p := DrySoilParams(testSize, 12345)
	a := Ground(p)
	b := Ground(p)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("texel %d differs: %d vs %d", i, a[i], b[i])
		}
	}
}
