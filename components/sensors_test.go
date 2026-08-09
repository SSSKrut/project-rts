package components

import (
	"math"
	"testing"
)

var allFalloffs = []FalloffKind{
	FalloffStep, FalloffLinear, FalloffInvSquare, FalloffExp, FalloffSigmoid,
}

func TestFalloffIsFullStrengthAtZeroDistance(t *testing.T) {
	for _, k := range allFalloffs {
		got := Falloff(k, 0, 100)
		if got < 0.99 || got > 1.0001 {
			t.Errorf("kind %d at d=0: %.4f, want ~1", k, got)
		}
	}
}

func TestFalloffDecaysWithDistance(t *testing.T) {
	for _, k := range allFalloffs {
		prev := float32(math.MaxFloat32)
		for d := float32(0); d <= 400; d += 10 {
			got := Falloff(k, d, 100)
			if got > prev+1e-4 {
				t.Fatalf("kind %d: strength rose at d=%.0f (%.4f after %.4f)", k, d, got, prev)
			}
			prev = got
		}
	}
}

func TestFalloffStaysInUnitRange(t *testing.T) {
	for _, k := range allFalloffs {
		for d := float32(0); d <= 1000; d += 7 {
			got := Falloff(k, d, 100)
			if got < -1e-6 || got > 1+1e-6 {
				t.Fatalf("kind %d at d=%.0f: %.4f outside [0, 1]", k, d, got)
			}
		}
	}
}

func TestFalloffRejectsBrokenRange(t *testing.T) {
	for _, k := range allFalloffs {
		if got := Falloff(k, 10, 0); got != 0 {
			t.Errorf("kind %d with r=0: %.4f, want 0", k, got)
		}
		if got := Falloff(k, 10, -5); got != 0 {
			t.Errorf("kind %d with r<0: %.4f, want 0", k, got)
		}
	}
}

func TestFalloffCurveShapesMatchTheirDocs(t *testing.T) {
	const r float32 = 100
	if got := Falloff(FalloffStep, r, r); got != 1 {
		t.Errorf("step at d=r: %.4f, want 1 (inclusive)", got)
	}
	if got := Falloff(FalloffStep, r+0.1, r); got != 0 {
		t.Errorf("step past r: %.4f, want 0", got)
	}
	if got := Falloff(FalloffLinear, r*0.5, r); math.Abs(float64(got-0.5)) > 1e-4 {
		t.Errorf("linear at half range: %.4f, want 0.5", got)
	}
	if got := Falloff(FalloffLinear, r, r); got != 0 {
		t.Errorf("linear at d=r: %.4f, want 0", got)
	}
	if got := Falloff(FalloffInvSquare, r, r); math.Abs(float64(got-0.5)) > 1e-3 {
		t.Errorf("inverse square at d=r: %.4f, want ~0.5", got)
	}
	if got := Falloff(FalloffExp, r, r); math.Abs(float64(got-0.3679)) > 1e-3 {
		t.Errorf("exponential at d=r: %.4f, want ~0.37", got)
	}
	if got := Falloff(FalloffSigmoid, r, r); math.Abs(float64(got-0.5)) > 1e-3 {
		t.Errorf("sigmoid at d=r: %.4f, want 0.5", got)
	}
}

func TestFalloffScalesWithRange(t *testing.T) {
	// The same relative distance must give the same strength at any range.
	for _, k := range allFalloffs {
		near := Falloff(k, 50, 100)
		far := Falloff(k, 250, 500)
		if math.Abs(float64(near-far)) > 1e-3 {
			t.Errorf("kind %d: half-range strength differs by scale (%.4f vs %.4f)", k, near, far)
		}
	}
}

func TestFacingMulHitsItsZoneValues(t *testing.T) {
	p := InfantryOpticalProfile
	if got := FacingMul(0, p); got != p.ForwardMul {
		t.Errorf("dead ahead: %.3f, want %.3f", got, p.ForwardMul)
	}
	if got := FacingMul(p.ForwardConeRad, p); got != p.ForwardMul {
		t.Errorf("forward cone edge: %.3f, want %.3f", got, p.ForwardMul)
	}
	if got := FacingMul(p.SideConeRad, p); math.Abs(float64(got-p.SideMul)) > 1e-4 {
		t.Errorf("side cone edge: %.3f, want %.3f", got, p.SideMul)
	}
	if got := FacingMul(math.Pi, p); math.Abs(float64(got-p.RearMul)) > 1e-4 {
		t.Errorf("dead astern: %.3f, want %.3f", got, p.RearMul)
	}
}

func TestFacingMulFallsOffMonotonically(t *testing.T) {
	for name, p := range map[string]FacingProfile{
		"infantry": InfantryOpticalProfile,
		"vehicle":  VehicleOpticalProfile,
	} {
		prev := float32(math.MaxFloat32)
		for a := float32(0); a <= math.Pi; a += 0.05 {
			got := FacingMul(a, p)
			if got > prev+1e-4 {
				t.Fatalf("%s: sensitivity rose at %.2f rad (%.4f after %.4f)", name, a, got, prev)
			}
			prev = got
		}
	}
}

func TestFacingMulClampsBeyondPi(t *testing.T) {
	p := InfantryOpticalProfile
	if got := FacingMul(4, p); math.Abs(float64(got-p.RearMul)) > 1e-4 {
		t.Errorf("angle past pi: %.3f, want the rear value %.3f", got, p.RearMul)
	}
	if got := FacingMul(-1, p); got != p.ForwardMul {
		t.Errorf("negative angle: %.3f, want the forward value %.3f", got, p.ForwardMul)
	}
}

// A profile is data. Collapsing a zone to zero width is a legal way to say
// "no side band" or "no rear band", and it must not divide by zero — an
// infinite multiplier is a sensor that sees the whole map.
func TestFacingMulSurvivesCollapsedZones(t *testing.T) {
	cases := []struct {
		name string
		p    FacingProfile
	}{
		{"no side band", FacingProfile{
			ForwardMul: 1, SideMul: 0.5, RearMul: 0.2,
			ForwardConeRad: math.Pi / 3, SideConeRad: math.Pi / 3,
		}},
		{"no rear band", FacingProfile{
			ForwardMul: 1, SideMul: 0.5, RearMul: 0.2,
			ForwardConeRad: math.Pi / 3, SideConeRad: math.Pi,
		}},
		{"all cones collapsed", FacingProfile{
			ForwardMul: 1, SideMul: 0.5, RearMul: 0.2,
		}},
		{"zero profile", FacingProfile{}},
	}
	for _, c := range cases {
		for a := float32(0); a <= math.Pi+0.5; a += 0.1 {
			got := FacingMul(a, c.p)
			if math.IsNaN(float64(got)) || math.IsInf(float64(got), 0) {
				t.Fatalf("%s at %.2f rad: %v", c.name, a, got)
			}
			lo := minMul(c.p)
			hi := maxMul(c.p)
			if got < lo-1e-4 || got > hi+1e-4 {
				t.Fatalf("%s at %.2f rad: %.4f outside the profile's own range [%.2f, %.2f]",
					c.name, a, got, lo, hi)
			}
		}
	}
}

func minMul(p FacingProfile) float32 {
	m := p.ForwardMul
	if p.SideMul < m {
		m = p.SideMul
	}
	if p.RearMul < m {
		m = p.RearMul
	}
	return m
}

func maxMul(p FacingProfile) float32 {
	m := p.ForwardMul
	if p.SideMul > m {
		m = p.SideMul
	}
	if p.RearMul > m {
		m = p.RearMul
	}
	return m
}

func TestShippedProfilesAreOrdered(t *testing.T) {
	for name, p := range map[string]FacingProfile{
		"infantry": InfantryOpticalProfile,
		"vehicle":  VehicleOpticalProfile,
	} {
		if !(p.ForwardMul >= p.SideMul && p.SideMul >= p.RearMul) {
			t.Errorf("%s: multipliers %.2f / %.2f / %.2f are not front-heavy",
				name, p.ForwardMul, p.SideMul, p.RearMul)
		}
		if !(p.ForwardConeRad > 0 && p.ForwardConeRad < p.SideConeRad && p.SideConeRad < math.Pi) {
			t.Errorf("%s: cones %.2f / %.2f do not nest inside pi",
				name, p.ForwardConeRad, p.SideConeRad)
		}
	}
}
