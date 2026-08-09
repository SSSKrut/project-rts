package systems

import (
	"math"
	"testing"

	"rts-go/components"
)

func flatChunk(h float32) []float32 {
	out := make([]float32, components.ChunkResolution*components.ChunkResolution)
	for i := range out {
		out[i] = h
	}
	return out
}

// rampChunk gives vertex (i, j) the height f(i, j) so bilinear sampling has
// something to interpolate.
func rampChunk(f func(i, j int) float32) []float32 {
	res := components.ChunkResolution
	out := make([]float32, res*res)
	for j := 0; j < res; j++ {
		for i := 0; i < res; i++ {
			out[j*res+i] = f(i, j)
		}
	}
	return out
}

func TestTerrainHeightAtReadsALoadedChunk(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{{X: 0, Z: 0}: flatChunk(12.5)}
	for _, p := range [][2]float32{{0, 0}, {31.5, 31.5}, {63.9, 0.1}} {
		if got := terrainHeightAt(hm, p[0], p[1]); math.Abs(float64(got-12.5)) > 1e-4 {
			t.Errorf("(%.2f, %.2f): height %.4f, want 12.5", p[0], p[1], got)
		}
	}
}

func TestTerrainHeightAtInterpolatesBilinearly(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 { return float32(i) }),
	}
	cases := []struct{ wx, wz, want float32 }{
		{10, 10, 10},
		{10.5, 10, 10.5},
		{10.25, 40, 10.25},
		{0.75, 0, 0.75},
	}
	for _, c := range cases {
		if got := terrainHeightAt(hm, c.wx, c.wz); math.Abs(float64(got-c.want)) > 1e-3 {
			t.Errorf("(%.2f, %.2f): height %.4f, want %.4f", c.wx, c.wz, got, c.want)
		}
	}
}

func TestTerrainHeightAtInterpolatesAlongZ(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 { return float32(j) * 2 }),
	}
	if got := terrainHeightAt(hm, 5, 3.5); math.Abs(float64(got-7)) > 1e-3 {
		t.Fatalf("height %.4f, want 7", got)
	}
}

func TestTerrainHeightAtHandlesNegativeWorldCoords(t *testing.T) {
	// (-0.5, -0.5) sits in chunk (-1, -1) at local column/row 63.
	hm := map[components.ChunkCoord][]float32{
		{X: -1, Z: -1}: rampChunk(func(i, j int) float32 {
			if i == 63 && j == 63 {
				return 100
			}
			return 0
		}),
	}
	got := terrainHeightAt(hm, -0.5, -0.5)
	if math.Abs(float64(got-25)) > 1e-3 {
		t.Fatalf("height %.4f, want 25 (quarter weight on the tall corner)", got)
	}
}

func TestTerrainHeightAtUsesTheSharedEdgeVertex(t *testing.T) {
	// Column 64 is the vertex shared with the next chunk; sampling at x=63.5
	// must reach it rather than run off the row.
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 {
			if i == 64 {
				return 8
			}
			return 0
		}),
	}
	if got := terrainHeightAt(hm, 63.5, 0); math.Abs(float64(got-4)) > 1e-3 {
		t.Fatalf("height %.4f, want 4", got)
	}
}

func TestTerrainHeightAtFallsBackToProcgen(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{}
	want := GroundHeight(120, -40)
	if got := terrainHeightAt(hm, 120, -40); got != want {
		t.Fatalf("height %.4f, want procgen %.4f", got, want)
	}
}

func TestTerrainHitTPassesOverFlatGround(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{{X: 0, Z: 0}: flatChunk(0)}
	if _, blocked := terrainHitT(hm, 2, 2, 1.5, 40, 2, 1.5); blocked {
		t.Fatal("flat ground blocked a sight line 1.5 m above it")
	}
}

func TestTerrainHitTStopsAtARidge(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 {
			if i >= 30 && i <= 34 {
				return 20
			}
			return 0
		}),
	}
	tHit, blocked := terrainHitT(hm, 2, 10, 1.5, 60, 10, 1.5)
	if !blocked {
		t.Fatal("a 20 m ridge did not block the line")
	}
	// The ridge starts around x = 30 on a 58 m span from x = 2.
	if tHit < 0.4 || tHit > 0.6 {
		t.Fatalf("hit at t = %.3f, expected the ridge around t ~ 0.48", tHit)
	}
}

func TestTerrainHitTRespectsClearance(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{{X: 0, Z: 0}: flatChunk(1.0)}
	// Sight line sits exactly on the surface: within losClearance, not a block.
	if _, blocked := terrainHitT(hm, 2, 2, 1.0, 40, 2, 1.0); blocked {
		t.Fatal("terrain at exactly eye height must not block")
	}
	if _, blocked := terrainHitT(hm, 2, 2, 0.9, 40, 2, 0.9); !blocked {
		t.Fatal("terrain 0.1 m above the line must block")
	}
}

func TestTerrainHitTFollowsTheVerticalSlope(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{{X: 0, Z: 0}: flatChunk(5)}
	// Shooter on the hill looking down at a target below: the line starts
	// above the surface and dips under it, so it is blocked partway.
	if _, blocked := terrainHitT(hm, 2, 2, 6, 40, 2, 0); !blocked {
		t.Fatal("a line dipping under the surface must block")
	}
	if _, blocked := terrainHitT(hm, 2, 2, 6, 40, 2, 6); blocked {
		t.Fatal("a level line above the surface must not block")
	}
}

// Documents the deliberate early-out: a sight line shorter than two sample
// steps is never tested against terrain at all.
func TestTerrainHitTIgnoresVeryShortLines(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{{X: 0, Z: 0}: flatChunk(50)}
	if _, blocked := terrainHitT(hm, 10, 10, 1, 11.5, 10, 1); blocked {
		t.Fatal("sub-2 m line should skip the terrain probe")
	}
	if _, blocked := terrainHitT(hm, 10, 10, 1, 40, 10, 1); !blocked {
		t.Fatal("a long line under a 50 m plateau must block")
	}
}

func TestTerrainBlocksLOSAgreesWithHitT(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 {
			if i > 20 {
				return 30
			}
			return 0
		}),
	}
	_, want := terrainHitT(hm, 1, 5, 1.5, 50, 5, 1.5)
	if got := terrainBlocksLOS(hm, 1, 5, 1.5, 50, 5, 1.5); got != want {
		t.Fatalf("terrainBlocksLOS = %v, terrainHitT = %v", got, want)
	}
	if !want {
		t.Fatal("expected the plateau to block")
	}
}

func TestTerrainHitTIsSymmetricEnough(t *testing.T) {
	hm := map[components.ChunkCoord][]float32{
		{X: 0, Z: 0}: rampChunk(func(i, j int) float32 {
			if i >= 30 && i <= 34 {
				return 20
			}
			return 0
		}),
	}
	_, fwd := terrainHitT(hm, 2, 10, 1.5, 60, 10, 1.5)
	_, back := terrainHitT(hm, 60, 10, 1.5, 2, 10, 1.5)
	if fwd != back {
		t.Fatalf("blocking is direction dependent: forward %v, backward %v", fwd, back)
	}
}
