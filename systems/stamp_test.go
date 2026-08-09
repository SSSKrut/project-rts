package systems

import (
	"math"
	"testing"

	"rts-go/components"
)

func TestCraterIsDeepestAtTheCentre(t *testing.T) {
	k := Crater(4, 10)
	if got := k(0, 0); math.Abs(float64(got+4)) > 1e-4 {
		t.Fatalf("centre delta %.4f, want -4", got)
	}
}

func TestCraterFadesToZeroAtTheRim(t *testing.T) {
	k := Crater(4, 10)
	if got := k(10, 0); got != 0 {
		t.Errorf("delta at the rim = %.4f, want 0", got)
	}
	if got := k(11, 0); got != 0 {
		t.Errorf("delta outside = %.4f, want 0", got)
	}
	if got := k(7.5, 7.5); got != 0 {
		t.Errorf("delta outside diagonally = %.4f, want 0", got)
	}
}

func TestCraterIsMonotonicAndRadial(t *testing.T) {
	k := Crater(4, 10)
	prev := k(0, 0)
	for d := float32(0.5); d < 10; d += 0.5 {
		got := k(d, 0)
		if got < prev-1e-4 {
			t.Fatalf("d=%.1f: delta %.4f deepened past %.4f", d, got, prev)
		}
		if diag := k(d*float32(math.Sqrt2/2), d*float32(math.Sqrt2/2)); math.Abs(float64(diag-got)) > 1e-3 {
			t.Fatalf("d=%.1f: axial %.4f vs diagonal %.4f — kernel is not radial", d, got, diag)
		}
		prev = got
	}
}

func TestCraterNeverRaisesGround(t *testing.T) {
	k := Crater(6, 8)
	for dx := float32(-12); dx <= 12; dx += 0.5 {
		for dz := float32(-12); dz <= 12; dz += 0.5 {
			if got := k(dx, dz); got > 0 {
				t.Fatalf("(%.1f, %.1f): delta %.4f raises the ground", dx, dz, got)
			}
		}
	}
}

func TestCraterHalfDepthAtHalfRadius(t *testing.T) {
	// cos(pi/2) = 0, so the bowl is exactly half depth midway out.
	k := Crater(4, 10)
	if got := k(5, 0); math.Abs(float64(got+2)) > 1e-3 {
		t.Fatalf("delta at half radius = %.4f, want -2", got)
	}
}

func TestCraterWithNoRadiusIsInert(t *testing.T) {
	k := Crater(4, 0)
	if got := k(0, 0); got != 0 {
		t.Fatalf("zero radius produced %.4f", got)
	}
	if got := Crater(4, -1)(0, 0); got != 0 {
		t.Fatalf("negative radius produced %.4f", got)
	}
}

func TestVertexRangeCoversTheSpan(t *testing.T) {
	const step float32 = 1
	cases := []struct {
		lo, hi   float32
		min, max int
	}{
		{0, 0, 0, 0},
		{0.2, 2.8, 0, 3},
		{10, 20, 10, 20},
		{-5, 3, 0, 3},
		{60, 200, 60, components.ChunkResolution - 1},
	}
	for _, c := range cases {
		min, max := vertexRange(c.lo, c.hi, step)
		if min != c.min || max != c.max {
			t.Errorf("[%.1f, %.1f]: got [%d, %d], want [%d, %d]", c.lo, c.hi, min, max, c.min, c.max)
		}
	}
}

// A span that misses the chunk must report empty (min > max) — the caller
// tests exactly that before looping.
func TestVertexRangeReportsEmptySpans(t *testing.T) {
	for _, c := range [][2]float32{{-100, -50}, {100, 200}, {-500, -400}} {
		min, max := vertexRange(c[0], c[1], 1)
		if min <= max {
			t.Errorf("[%.1f, %.1f]: got [%d, %d], want an empty range", c[0], c[1], min, max)
		}
	}
}

func TestVertexRangeNeverEscapesTheHeightmap(t *testing.T) {
	for lo := float32(-200); lo <= 200; lo += 7 {
		for _, span := range []float32{0, 1, 33, 400} {
			min, max := vertexRange(lo, lo+span, 1)
			if min < 0 || max < 0 || min >= components.ChunkResolution || max >= components.ChunkResolution {
				t.Fatalf("[%.1f, %.1f] -> [%d, %d]", lo, lo+span, min, max)
			}
		}
	}
}

func TestVertexRangeScalesWithStep(t *testing.T) {
	min, max := vertexRange(0, 10, 2)
	if min != 0 || max != 5 {
		t.Fatalf("step 2 over [0, 10] -> [%d, %d], want [0, 5]", min, max)
	}
}

func TestHashU32IsDeterministic(t *testing.T) {
	a := hashU32(42, 1, 2, 3)
	b := hashU32(42, 1, 2, 3)
	if a != b {
		t.Fatalf("same inputs gave %d and %d", a, b)
	}
	if hashU32(43, 1, 2, 3) == a {
		t.Error("the seed does not change the hash")
	}
	if hashU32(42, 1, 2, 4) == a {
		t.Error("the last argument does not change the hash")
	}
	if hashU32(42, 3, 2, 1) == a {
		t.Error("argument order does not change the hash")
	}
}

func TestHashFloatStaysInUnitRange(t *testing.T) {
	for x := int32(-50); x < 50; x++ {
		for z := int32(-50); z < 50; z++ {
			v := hashFloat(7, x, z)
			if v < 0 || v >= 1 {
				t.Fatalf("hashFloat(%d, %d) = %f, outside [0, 1)", x, z, v)
			}
		}
	}
}

func TestHashFloatSpreadsAcrossTheRange(t *testing.T) {
	var buckets [10]int
	for x := int32(0); x < 100; x++ {
		for z := int32(0); z < 100; z++ {
			b := int(hashFloat(3, x, z) * 10)
			if b > 9 {
				b = 9
			}
			buckets[b]++
		}
	}
	for i, n := range buckets {
		// 10 000 samples over 10 buckets: anything under half the expected
		// share means the hash is clumping.
		if n < 500 {
			t.Errorf("bucket %d holds only %d of 10000 samples", i, n)
		}
	}
}

func TestPickTreeTypeIsStableAndVaried(t *testing.T) {
	cc := components.ChunkCoord{X: 1, Z: -2}
	if a, b := pickTreeType(cc, 5, 7), pickTreeType(cc, 5, 7); a != b {
		t.Fatalf("same cell gave %v and %v", a, b)
	}
	seen := map[components.PropType]int{}
	for gx := int32(0); gx < 40; gx++ {
		for gz := int32(0); gz < 40; gz++ {
			seen[pickTreeType(cc, gx, gz)]++
		}
	}
	if len(seen) != 3 {
		t.Fatalf("got %d species over 1600 cells, want 3", len(seen))
	}
	for kind, n := range seen {
		if n < 300 {
			t.Errorf("species %v appeared only %d times in 1600 cells", kind, n)
		}
	}
}
