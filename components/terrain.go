package components

import rl "github.com/gen2brain/raylib-go/raylib"

// Heightmap is a fixed-size grid of per-vertex heights for one chunk.
//
// Layout: row-major with rows along the +Z axis. Index (i, j) — where i is the
// X column and j is the Z row — lives at Heights[j*ChunkResolution + i]. The
// world-space coordinate of that vertex (assuming step = ChunkSize / (ChunkResolution-1) = 1 m)
// is (chunk.X*ChunkSize + i, height, chunk.Z*ChunkSize + j).
//
// The fixed-size array makes Heightmap a plain value type (~17 KB at 65×65),
// which lets Ark pack it directly into the chunk archetype with no extra
// indirection. Switching to a slice would force a heap allocation per chunk.
type Heightmap struct {
	Heights [ChunkResolution * ChunkResolution]float32
}

// ChunkMesh holds the GPU-uploaded mesh for one terrain chunk.
//
// We deliberately store rl.Mesh (not rl.Model) here: raylib-go's UnloadModel
// unconditionally calls C.free on the mesh-data pointers, which is undefined
// behaviour when those pointers were Go-allocated (as ours are, via
// rl.UploadMesh). raylib-go's UnloadMesh, by contrast, tracks
// Go-managed VAO IDs and skips the free for those — that's what we use on
// teardown. Rendering goes through rl.DrawMesh + a shared material set up in
// main.go.
//
// Uploaded distinguishes "we own a live mesh that needs UnloadMesh" from
// "this slot has never been filled" — important for the lifecycle on tier
// transitions and chunk eviction.
type ChunkMesh struct {
	Mesh     rl.Mesh
	Uploaded bool
}

// HeightmapDirty marks a chunk whose heights need to be (re)generated.
// TerrainGenSystem clears it after writing Heights.
type HeightmapDirty struct{}

// MeshDirty marks a chunk whose GPU mesh needs to be (re)built from its
// Heightmap. TerrainMeshSystem clears it after upload.
type MeshDirty struct{}
