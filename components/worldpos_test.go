package components

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func wp(cx, cz int32, x, y, z float32) WorldPos {
	return WorldPos{Chunk: ChunkCoord{X: cx, Z: cz}, Local: rl.Vector3{X: x, Y: y, Z: z}}
}

func TestNormalizeLeavesValidPositions(t *testing.T) {
	p := wp(3, -2, 0, 12, 63.9)
	if got := Normalize(p); got != p {
		t.Fatalf("normalize mutated a valid pos: %+v -> %+v", p, got)
	}
}

func TestNormalizeFoldsPositiveOverflow(t *testing.T) {
	got := Normalize(wp(0, 0, 70, 5, 130))
	if got.Chunk != (ChunkCoord{X: 1, Z: 2}) {
		t.Fatalf("chunk = %+v, want {1 2}", got.Chunk)
	}
	if got.Local.X != 6 || got.Local.Z != 2 {
		t.Fatalf("local = (%.2f, %.2f), want (6, 2)", got.Local.X, got.Local.Z)
	}
	if got.Local.Y != 5 {
		t.Fatalf("Y must survive untouched, got %.2f", got.Local.Y)
	}
}

func TestNormalizeFoldsNegativeIntoLowerChunk(t *testing.T) {
	got := Normalize(wp(0, 0, -1, 0, -65))
	if got.Chunk != (ChunkCoord{X: -1, Z: -2}) {
		t.Fatalf("chunk = %+v, want {-1 -2}", got.Chunk)
	}
	if got.Local.X != 63 || got.Local.Z != 63 {
		t.Fatalf("local = (%.2f, %.2f), want (63, 63)", got.Local.X, got.Local.Z)
	}
}

// A tiny negative local is the case float32 cannot fold cleanly: -1e-7 + 64
// rounds to exactly 64, which is outside the half-open chunk range.
func TestNormalizeTinyNegativeStaysInsideTheChunk(t *testing.T) {
	for _, eps := range []float32{-1e-7, -1e-8, -1e-30} {
		got := Normalize(wp(4, 4, eps, 0, eps))
		if got.Local.X < 0 || got.Local.X >= ChunkSize {
			t.Fatalf("eps=%g: Local.X = %v, outside [0, %v)", eps, got.Local.X, ChunkSize)
		}
		if got.Local.Z < 0 || got.Local.Z >= ChunkSize {
			t.Fatalf("eps=%g: Local.Z = %v, outside [0, %v)", eps, got.Local.Z, ChunkSize)
		}
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	cases := []WorldPos{
		wp(0, 0, -1e-7, 0, -1e-7),
		wp(0, 0, 64, 0, 64),
		wp(-3, 7, -200.5, 3, 511.25),
		wp(1, 1, 0, 0, 0),
	}
	for _, c := range cases {
		once := Normalize(c)
		if twice := Normalize(once); twice != once {
			t.Fatalf("%+v: not idempotent, %+v -> %+v", c, once, twice)
		}
	}
}

func TestNormalizePreservesWorldPosition(t *testing.T) {
	cases := []WorldPos{
		wp(0, 0, 70, 0, 130),
		wp(2, -5, -1, 0, -65),
		wp(-1, 3, 200.25, 0, -0.75),
	}
	for _, c := range cases {
		n := Normalize(c)
		wantX := float64(c.Chunk.X)*float64(ChunkSize) + float64(c.Local.X)
		wantZ := float64(c.Chunk.Z)*float64(ChunkSize) + float64(c.Local.Z)
		gotX := float64(n.Chunk.X)*float64(ChunkSize) + float64(n.Local.X)
		gotZ := float64(n.Chunk.Z)*float64(ChunkSize) + float64(n.Local.Z)
		if math.Abs(gotX-wantX) > 1e-3 || math.Abs(gotZ-wantZ) > 1e-3 {
			t.Fatalf("%+v: world pos moved (%.4f,%.4f) -> (%.4f,%.4f)", c, wantX, wantZ, gotX, gotZ)
		}
	}
}

func TestAddCrossesChunkBoundary(t *testing.T) {
	got := wp(0, 0, 60, 10, 60).Add(rl.Vector3{X: 8, Y: 1, Z: 8})
	if got.Chunk != (ChunkCoord{X: 1, Z: 1}) {
		t.Fatalf("chunk = %+v, want {1 1}", got.Chunk)
	}
	if got.Local.X != 4 || got.Local.Z != 4 || got.Local.Y != 11 {
		t.Fatalf("local = %+v, want (4, 11, 4)", got.Local)
	}
}

func TestSubSpansChunks(t *testing.T) {
	d := wp(1, 0, 2, 7, 0).Sub(wp(0, 0, 2, 3, 0))
	if d.X != ChunkSize {
		t.Fatalf("dx = %.2f, want %.2f", d.X, ChunkSize)
	}
	if d.Y != 4 {
		t.Fatalf("dy = %.2f, want 4", d.Y)
	}
}

func TestSubIsAntisymmetric(t *testing.T) {
	a, b := wp(-2, 5, 10, 1, 20), wp(3, -1, 40, 9, 7)
	ab, ba := a.Sub(b), b.Sub(a)
	if ab.X != -ba.X || ab.Y != -ba.Y || ab.Z != -ba.Z {
		t.Fatalf("a-b = %+v, b-a = %+v", ab, ba)
	}
}

func TestAddSubRoundTrip(t *testing.T) {
	base := wp(2, -3, 10, 4, 50)
	v := rl.Vector3{X: -140.5, Y: 3, Z: 77.25}
	back := base.Add(v).Sub(base)
	if math.Abs(float64(back.X-v.X)) > 1e-3 || math.Abs(float64(back.Z-v.Z)) > 1e-3 {
		t.Fatalf("round trip lost the vector: %+v -> %+v", v, back)
	}
}

func TestDistanceFoldsAllThreeAxes(t *testing.T) {
	a := wp(0, 0, 0, 0, 0)
	b := wp(0, 0, 3, 4, 0)
	if d := Distance(a, b); math.Abs(float64(d-5)) > 1e-4 {
		t.Fatalf("distance = %.4f, want 5", d)
	}
	if d := DistanceSquared(a, b); math.Abs(float64(d-25)) > 1e-3 {
		t.Fatalf("distance^2 = %.4f, want 25", d)
	}
}

func TestToRenderSpaceIsRelativeToOrigin(t *testing.T) {
	p := wp(2, -1, 10, 3, 20)
	got := p.ToRenderSpace(ChunkCoord{X: 2, Z: -1})
	if got.X != 10 || got.Z != 20 {
		t.Fatalf("own chunk should be identity, got %+v", got)
	}
	got = p.ToRenderSpace(ChunkCoord{X: 1, Z: -1})
	if got.X != 10+ChunkSize {
		t.Fatalf("X = %.2f, want %.2f", got.X, 10+ChunkSize)
	}
}
