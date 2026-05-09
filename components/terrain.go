package components

import rl "github.com/gen2brain/raylib-go/raylib"

// Heightmap is row-major along +Z: Heights[j*ChunkResolution + i] is column i,
// row j. Vertex world position (assuming step = ChunkSize/(ChunkResolution-1)
// = 1 m) is (chunk.X*ChunkSize + i, height, chunk.Z*ChunkSize + j).
//
// Fixed-size array keeps Heightmap a plain ~17 KB value type so Ark packs it
// into the chunk archetype with no extra indirection.
type Heightmap struct {
	Heights [ChunkResolution * ChunkResolution]float32
}

// ChunkMesh holds the GPU-uploaded mesh for one terrain chunk.
//
// We deliberately store rl.Mesh (not rl.Model): raylib-go's UnloadModel calls
// C.free on mesh-data pointers — undefined behaviour for our Go-allocated
// buffers (we upload via rl.UploadMesh). UnloadMesh tracks Go-managed VAO IDs
// and skips the free; that's what teardown uses. Rendering goes through
// rl.DrawMesh + a shared material set up in main.go.
//
// Uploaded distinguishes "we own a live mesh that needs UnloadMesh" from "this
// slot has never been filled" — important on tier transitions and eviction.
type ChunkMesh struct {
	Mesh     rl.Mesh
	Uploaded bool
}

// HeightmapDirty: heights need initial fill. Set only at chunk creation;
// cleared by terrain_load (disk hit) or terrain_gen (procgen). Not used for
// in-place edits.
type HeightmapDirty struct{}

// MeshDirty: GPU mesh needs (re)build from Heightmap. Cleared after upload.
type MeshDirty struct{}

// Modified: heights diverge from pure procgen output. Set by Stamp and by
// terrain_load (a chunk loaded from disk diverged at some point). On eviction
// and shutdown, only Modified chunks are persisted; pristine chunks have zero
// disk footprint.
type Modified struct{}
