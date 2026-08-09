package systems

import (
	"testing"

	"rts-go/components"
)

func TestYieldStepWaitsForSustainedPressure(t *testing.T) {
	// A brush lasting less than the threshold is not a reason to leave a slot.
	if _, _, ok := yieldStep(yieldPressureTime-0.01, 1.0/60, 1, 0); ok {
		t.Error("stepped aside before the pressure held")
	}
	if _, _, ok := yieldStep(0, 1.0/60, 1, 0); ok {
		t.Error("stepped aside with no pressure at all")
	}
}

func TestYieldStepMovesAlongEscapeNormal(t *testing.T) {
	const dt float32 = 1.0 / 60
	sx, sz, ok := yieldStep(yieldPressureTime, dt, 1, 0)
	if !ok {
		t.Fatal("no step once the pressure held")
	}
	if want := yieldSpeed * dt; !nearlyEqual(sx, want) {
		t.Errorf("step X = %f, want %f", sx, want)
	}
	if !nearlyEqual(sz, 0) {
		t.Errorf("step Z = %f, want 0", sz)
	}
	// The normal already points away from whoever is pressing.
	sx, _, _ = yieldStep(yieldPressureTime, dt, -1, 0)
	if sx >= 0 {
		t.Errorf("step X = %f, want the opposite side for a mirrored normal", sx)
	}
}

func TestYieldStepIgnoresDegenerateNormal(t *testing.T) {
	if _, _, ok := yieldStep(10, 1.0/60, 0, 0); ok {
		t.Error("stepped with no escape direction")
	}
}

func TestYieldStepScalesWithDelta(t *testing.T) {
	slow, _, _ := yieldStep(1, 1.0/60, 1, 0)
	fast, _, _ := yieldStep(1, 2.0/60, 1, 0)
	if !nearlyEqual(fast, 2*slow) {
		t.Errorf("step %f at 2x dt, want twice %f", fast, slow)
	}
}

func TestInsideAnyFootprint(t *testing.T) {
	foots := []components.AABB2D{
		{MinX: 0, MinZ: 0, MaxX: 10, MaxZ: 10},
		{MinX: 50, MinZ: 50, MaxX: 60, MaxZ: 60},
	}
	cases := []struct {
		name string
		x, z float32
		want bool
	}{
		{"inside the first", 5, 5, true},
		{"inside the second", 55, 55, true},
		{"on a corner", 10, 10, true},
		{"between them", 30, 30, false},
		{"outside everything", -5, -5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := insideAnyFootprint(tc.x, tc.z, foots); got != tc.want {
				t.Errorf("insideAnyFootprint(%v, %v) = %v, want %v", tc.x, tc.z, got, tc.want)
			}
		})
	}
	if insideAnyFootprint(5, 5, nil) {
		t.Error("an empty world reported a footprint hit")
	}
}
