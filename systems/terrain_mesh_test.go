package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func TestRiverMoistureFadesAwayFromBank(t *testing.T) {
	rivers := &components.Rivers{Polylines: []components.RiverPolyline{{
		Points: []components.WorldPos{{Local: rl.Vector3{}}, {Local: rl.Vector3{X: 10}}},
		Width:  4,
	}}}
	center := SurfaceBiomeAt(5, 0, rivers)
	bank := SurfaceBiomeAt(5, 3, rivers)
	away := SurfaceBiomeAt(5, 8, rivers)
	if center.Moisture != 1 || bank.Moisture <= 0 || bank.Moisture >= 1 || away.Moisture != 0 {
		t.Fatalf("unexpected moisture: center=%g bank=%g away=%g", center.Moisture, bank.Moisture, away.Moisture)
	}
}

func gridSampler(cc components.ChunkCoord, height func(wx, wz float32) float32) func(i, j int) float32 {
	return func(i, j int) float32 {
		step := components.ChunkSize / float32(components.ChunkResolution-1)
		wx := (float32(cc.X)*float32(components.ChunkResolution-1) + float32(i)) * step
		wz := (float32(cc.Z)*float32(components.ChunkResolution-1) + float32(j)) * step
		return height(wx, wz)
	}
}

func TestChunkNormalsMatchAcrossSeam(t *testing.T) {
	height := func(wx, wz float32) float32 {
		return float32(0.7*math.Sin(float64(wx)*0.09) + 0.4*math.Cos(float64(wz)*0.07))
	}
	left := buildChunkMeshFromSampler(gridSampler(components.ChunkCoord{}, height), components.ChunkResolution, nil)
	right := buildChunkMeshFromSampler(gridSampler(components.ChunkCoord{X: 1}, height), components.ChunkResolution, nil)
	const res = components.ChunkResolution
	for j := 0; j < res; j++ {
		li := (j*res + res - 1) * 3
		ri := (j * res) * 3
		for axis := 0; axis < 3; axis++ {
			if diff := math.Abs(float64(left.norms[li+axis] - right.norms[ri+axis])); diff > 1e-6 {
				t.Fatalf("seam normal row %d axis %d differs by %g", j, axis, diff)
			}
		}
	}
}

func TestDecimatedNormalsUseDestinationSpacing(t *testing.T) {
	height := func(wx, wz float32) float32 { return 2*wx - 3*wz }
	sampler := gridSampler(components.ChunkCoord{}, height)
	const dstRes = (components.ChunkResolution + 1) / 2
	mesh := buildDecimatedMeshFromSampler(sampler, components.ChunkResolution, dstRes, nil)
	invLen := float32(1 / math.Sqrt(14))
	want := [3]float32{-2 * invLen, invLen, 3 * invLen}
	for v := 0; v < dstRes*dstRes; v++ {
		for axis := 0; axis < 3; axis++ {
			if diff := math.Abs(float64(mesh.norms[v*3+axis] - want[axis])); diff > 1e-5 {
				t.Fatalf("vertex %d axis %d: got %g want %g", v, axis, mesh.norms[v*3+axis], want[axis])
			}
		}
	}
}

func TestSurfaceParamsFollowTerrainAmplitude(t *testing.T) {
	oldTerrain := terrainCfg
	oldSurface := surfaceCfg
	defer func() {
		SetTerrainParams(oldTerrain)
		surfaceCfg = oldSurface
	}()
	SetTerrainParams(TerrainParams{AmplitudeM: 16})
	SetSurfaceParams(SurfaceParams{})
	got := CurrentSurfaceParams()
	if got.SandBand != ([2]float32{-13, -6}) || got.RockBand != ([2]float32{11, 18}) || got.ReliefScale != 2 {
		t.Fatalf("unexpected scaled surface params: %+v", got)
	}
	SetSurfaceParams(SurfaceParams{SandBand: [2]float32{-2, -1}, ReliefScale: 3})
	got = CurrentSurfaceParams()
	if got.SandBand != ([2]float32{-2, -1}) || got.ReliefScale != 3 {
		t.Fatalf("map override not applied: %+v", got)
	}
}

func TestFloorGridChunkNegativeCoordinates(t *testing.T) {
	for _, tc := range []struct {
		v, want int
	}{{-129, -3}, {-65, -2}, {-64, -1}, {-1, -1}, {0, 0}, {64, 1}, {65, 1}} {
		if got := floorGridChunk(tc.v, 64); got != int32(tc.want) {
			t.Errorf("floorGridChunk(%d): got %d want %d", tc.v, got, tc.want)
		}
	}
}
