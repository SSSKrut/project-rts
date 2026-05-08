package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// skirtColor is the flat tint used for chunk skirts (the perimeter strips
// dropped below the surface to hide LOD seams). A muted earth tone so the
// underground geometry doesn't compete visually with the surface relief.
var skirtColor = [4]uint8{55, 45, 35, 255}

// skirtDrop is how far below a border vertex the skirt vertex is placed.
// 2 m is enough to hide the height-mismatch seam between Active and Relevant
// chunks (which use different vertex resolutions and therefore interpolate
// the surface differently along their shared edge).
const skirtDrop float32 = 2.0

// reliefStops are the height/colour control points used by reliefColor below.
// Heights are tuned for terrainAmplitude = 8 (so vertex Y typically falls in
// roughly ±8 m); adjust together if amplitude changes meaningfully. Order
// must be ascending in H.
var reliefStops = [...]struct {
	H       float32
	R, G, B float32
}{
	{-8, 55, 80, 50},     // wet lowland / shadowed dip
	{-3, 90, 130, 70},    // dark grass
	{0, 125, 165, 90},    // grass
	{3, 160, 160, 100},   // dry grass / tan
	{6, 165, 140, 105},   // earth
	{9, 185, 175, 165},   // exposed rock
	{14, 220, 215, 205},  // peak / snow-ish
}

// reliefColor maps a vertex (height y, normal-Y component ny) to an RGB tint.
// Linearly blended between reliefStops by height; multiplied by a slope-based
// shade (steep faces darken) so the form reads on flat-shaded geometry,
// without depending on a runtime light/shader. ny is clamped to [0, 1].
func reliefColor(y, ny float32) (uint8, uint8, uint8) {
	var rr, gg, bb float32
	switch {
	case y <= reliefStops[0].H:
		s := reliefStops[0]
		rr, gg, bb = s.R, s.G, s.B
	case y >= reliefStops[len(reliefStops)-1].H:
		s := reliefStops[len(reliefStops)-1]
		rr, gg, bb = s.R, s.G, s.B
	default:
		for i := 0; i < len(reliefStops)-1; i++ {
			a, b := reliefStops[i], reliefStops[i+1]
			if y >= a.H && y <= b.H {
				t := (y - a.H) / (b.H - a.H)
				rr = a.R + t*(b.R-a.R)
				gg = a.G + t*(b.G-a.G)
				bb = a.B + t*(b.B-a.B)
				break
			}
		}
	}
	if ny < 0 {
		ny = 0
	} else if ny > 1 {
		ny = 1
	}
	// Slope shade: 0.45 darkest (vertical wall) → 1.0 brightest (flat).
	shade := 0.45 + 0.55*ny
	return uint8(rr * shade), uint8(gg * shade), uint8(bb * shade)
}

// TerrainMeshSystem rebuilds the GPU mesh for any chunk marked MeshDirty.
// Two passes: Active (full 65×65) and Relevant (decimated 33×33). Each pass
// uses a separate filter so we hit a single archetype at a time.
//
// LOD-tier transitions hit this system because TerrainStreamingSystem stamps
// MeshDirty on every transition. On rebuild, the existing rl.Model is
// unloaded BEFORE the new one is assigned to keep GPU memory in step.
type TerrainMeshSystem struct {
	activeFilter   *ecs.Filter3[components.Heightmap, components.MeshDirty, components.LODActive]
	relevantFilter *ecs.Filter3[components.Heightmap, components.MeshDirty, components.LODRelevant]
	chunkMeshMap   *ecs.Map[components.ChunkMesh]
	meshDirtyMap   *ecs.Map[components.MeshDirty]
}

func (sys *TerrainMeshSystem) InitUI(w *ecs.World) {
	sys.activeFilter = ecs.NewFilter3[components.Heightmap, components.MeshDirty, components.LODActive](w)
	sys.relevantFilter = ecs.NewFilter3[components.Heightmap, components.MeshDirty, components.LODRelevant](w)
	sys.chunkMeshMap = ecs.NewMap[components.ChunkMesh](w)
	sys.meshDirtyMap = ecs.NewMap[components.MeshDirty](w)
}

func (TerrainMeshSystem) Name() string { return "terrain_mesh" }

func (TerrainMeshSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// builtMesh carries the upload-ready data for one chunk plus its destination
// entity. We can't UploadMesh / LoadModelFromMesh inside an active query
// (that touches archetypes via chunkMeshMap), so we collect first and apply
// after the iteration closes — same deferred-mutation pattern as elsewhere.
type builtMesh struct {
	id ecs.Entity
	// Held heap-allocated and pinned by raylib's UploadMesh so the GPU upload
	// can read them; we also need the slices to stay alive across the call.
	verts   []float32
	norms   []float32
	colors  []uint8
	indices []uint16
	tris    int32
}

func (sys TerrainMeshSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	var built []builtMesh

	// Pass 1: Active — full 65×65 resolution.
	{
		q := sys.activeFilter.Query()
		for q.Next() {
			hm, _, _ := q.Get()
			b := buildChunkMesh(hm.Heights, components.ChunkResolution)
			b.id = q.Entity()
			built = append(built, b)
		}
	}

	// Pass 2: Relevant — every-other-vertex decimation. Sampling stride 2,
	// so the resolution drops to (65+1)/2 = 33.
	{
		q := sys.relevantFilter.Query()
		for q.Next() {
			hm, _, _ := q.Get()
			res := (components.ChunkResolution + 1) / 2 // 33 when ChunkRes = 65
			b := buildDecimatedMesh(hm.Heights, components.ChunkResolution, res)
			b.id = q.Entity()
			built = append(built, b)
		}
	}

	// Apply: upload + assign + clear MeshDirty. Old model (if any) unloaded
	// before the new one is wired in.
	for i := range built {
		b := &built[i]
		mesh := rl.Mesh{
			VertexCount:   int32(len(b.verts) / 3),
			TriangleCount: b.tris,
		}
		if len(b.verts) > 0 {
			mesh.Vertices = &b.verts[0]
		}
		if len(b.norms) > 0 {
			mesh.Normals = &b.norms[0]
		}
		if len(b.colors) > 0 {
			mesh.Colors = &b.colors[0]
		}
		if len(b.indices) > 0 {
			mesh.Indices = &b.indices[0]
		}
		rl.UploadMesh(&mesh, false)

		if existing := sys.chunkMeshMap.Get(b.id); existing != nil {
			if existing.Uploaded {
				// rl.UnloadMesh has a goManagedMeshIDs guard; rl.UnloadModel
				// would not, and would C.free our Go-allocated pointers.
				rl.UnloadMesh(&existing.Mesh)
			}
			existing.Mesh = mesh
			existing.Uploaded = true
		} else {
			cm := components.ChunkMesh{Mesh: mesh, Uploaded: true}
			sys.chunkMeshMap.Add(b.id, &cm)
		}
		sys.meshDirtyMap.Remove(b.id)
	}
}

// buildChunkMesh builds a flat-grid mesh at full source resolution. heights
// is a (srcRes × srcRes) row-major grid (row-major along Z), spaced step =
// ChunkSize/(srcRes-1) metres apart. The resulting mesh uses vertex local
// space [0, ChunkSize] on X/Z so the chunk entity's WorldPos translates the
// whole thing into world space at draw time.
func buildChunkMesh(heights [components.ChunkResolution * components.ChunkResolution]float32, srcRes int) builtMesh {
	heightAt := func(i, j int) float32 {
		return heights[j*srcRes+i]
	}
	return assembleGrid(heightAt, srcRes, srcRes)
}

// buildDecimatedMesh samples every (srcRes-1)/(dstRes-1) source vertex along
// each axis. Critically, the *border* vertices are sampled exactly — index 0
// hits source 0, index dstRes-1 hits source srcRes-1 — so the edge geometry
// joins cleanly to a neighbour at the same tier. (Across-tier seams are
// hidden by the skirts.)
func buildDecimatedMesh(heights [components.ChunkResolution * components.ChunkResolution]float32, srcRes, dstRes int) builtMesh {
	srcStep := (srcRes - 1) / (dstRes - 1)
	heightAt := func(i, j int) float32 {
		si := i * srcStep
		sj := j * srcStep
		return heights[sj*srcRes+si]
	}
	return assembleGrid(heightAt, srcRes, dstRes)
}

// assembleGrid is the shared mesh builder.
//
//   - heightAt(i, j) returns the world-Y at logical column i, row j.
//   - srcRes is the source heightmap side (used to compute world-space step
//     for finite-difference normals via neighbour heights even at chunk edges).
//   - dstRes is the side count of the mesh being assembled (== srcRes for
//     Active, half-ish for Relevant).
//
// Top vertices are coloured by reliefColor(height, normal-Y) so the form
// reads at distance without a runtime light. Skirts are added at the end:
// 4 perimeter strips dropped skirtDrop metres below their corresponding top
// vertices, hiding seams from any side angle.
func assembleGrid(heightAt func(i, j int) float32, srcRes, dstRes int) builtMesh {
	step := components.ChunkSize / float32(dstRes-1) // world-space spacing along surface
	srcStep := components.ChunkSize / float32(srcRes-1)

	topVertCount := dstRes * dstRes
	topQuadSide := dstRes - 1
	topTriCount := topQuadSide * topQuadSide * 2
	skirtVertCount := 4 * dstRes
	skirtTriCount := 4 * (dstRes - 1) * 2

	totalVerts := topVertCount + skirtVertCount
	totalTris := topTriCount + skirtTriCount

	verts := make([]float32, 0, totalVerts*3)
	norms := make([]float32, 0, totalVerts*3)
	colors := make([]uint8, 0, totalVerts*4)
	indices := make([]uint16, 0, totalTris*3)

	// 1. Top surface vertices (row-major: row j along Z).
	for j := 0; j < dstRes; j++ {
		z := float32(j) * step
		for i := 0; i < dstRes; i++ {
			x := float32(i) * step
			y := heightAt(i, j)

			// Normal via central differences on local heights. We use srcStep
			// (the *true* world-space spacing of the underlying samples) so
			// the slope estimate doesn't get distorted by decimation. Edge
			// vertices fall back to one-sided differences.
			var hl, hr, hd, hu float32
			if i > 0 {
				hl = heightAt(i-1, j)
			} else {
				hl = y
			}
			if i < dstRes-1 {
				hr = heightAt(i+1, j)
			} else {
				hr = y
			}
			if j > 0 {
				hd = heightAt(i, j-1)
			} else {
				hd = y
			}
			if j < dstRes-1 {
				hu = heightAt(i, j+1)
			} else {
				hu = y
			}
			// Tangents: dx along +X = (2*srcStep, hr-hl, 0); dz along +Z =
			// (0, hu-hd, 2*srcStep). Normal is dz × dx (so it points +Y on
			// flat ground).
			nx := -(hr - hl) * (2 * srcStep)
			ny := (2 * srcStep) * (2 * srcStep)
			nz := -(hu - hd) * (2 * srcStep)
			invLen := 1.0 / float32(math.Sqrt(float64(nx*nx+ny*ny+nz*nz)))
			nx *= invLen
			ny *= invLen
			nz *= invLen

			verts = append(verts, x, y, z)
			norms = append(norms, nx, ny, nz)
			cr, cg, cb := reliefColor(y, ny)
			colors = append(colors, cr, cg, cb, 255)
		}
	}

	// 2. Top surface indices. Counter-clockwise winding when viewed from +Y
	// (above), so faces look up.
	idx := func(i, j int) uint16 {
		return uint16(j*dstRes + i)
	}
	for j := 0; j < dstRes-1; j++ {
		for i := 0; i < dstRes-1; i++ {
			a := idx(i, j)
			b := idx(i+1, j)
			c := idx(i, j+1)
			d := idx(i+1, j+1)
			// Two triangles per quad: (a, c, b) and (b, c, d).
			indices = append(indices, a, c, b, b, c, d)
		}
	}

	// 3. Skirts. For each of the 4 borders, emit dstRes "lower" vertices
	// directly under the top border vertex (Y - skirtDrop), then stitch
	// quads between top and lower vertices.
	//
	// We walk the border in *strip order* so consecutive lower verts share
	// an edge with consecutive top verts, making indexing trivial.
	skirtBase := uint16(topVertCount)

	addSkirt := func(borderIdx func(t int) uint16, normal [3]float32) {
		startLower := uint16(len(verts) / 3)
		// Emit the lower-row vertices.
		for t := 0; t < dstRes; t++ {
			topI := borderIdx(t)
			tx := verts[topI*3]
			ty := verts[topI*3+1]
			tz := verts[topI*3+2]
			verts = append(verts, tx, ty-skirtDrop, tz)
			norms = append(norms, normal[0], normal[1], normal[2])
			colors = append(colors, skirtColor[0], skirtColor[1], skirtColor[2], skirtColor[3])
		}
		// Stitch quads. Winding chosen so the visible face points outward
		// (matches the supplied normal). For each segment t -> t+1:
		//   top[t] - top[t+1]
		//      |       |
		//   low[t] - low[t+1]
		// outward-facing tris: (top[t], low[t], top[t+1]) and (top[t+1], low[t], low[t+1]).
		for t := 0; t < dstRes-1; t++ {
			tA := borderIdx(t)
			tB := borderIdx(t + 1)
			lA := startLower + uint16(t)
			lB := startLower + uint16(t+1)
			indices = append(indices, tA, lA, tB, tB, lA, lB)
		}
		_ = skirtBase
	}

	// Border walks. Pick t-direction such that the outward face triangulation
	// above results in correctly oriented (outward) normals. For each border,
	// "outward" is the negative or positive X/Z direction.

	// -Z border (j = 0): walk t along +X. Outward normal = (0, 0, -1).
	//   Triangulation (tA, lA, tB) above winds CCW when viewed from -Z, good.
	addSkirt(func(t int) uint16 { return idx(t, 0) }, [3]float32{0, 0, -1})
	// +X border (i = dstRes-1): walk t along +Z. Outward = (+1, 0, 0).
	addSkirt(func(t int) uint16 { return idx(dstRes-1, t) }, [3]float32{1, 0, 0})
	// +Z border (j = dstRes-1): walk t along -X. Outward = (0, 0, +1).
	addSkirt(func(t int) uint16 { return idx(dstRes-1-t, dstRes-1) }, [3]float32{0, 0, 1})
	// -X border (i = 0): walk t along -Z. Outward = (-1, 0, 0).
	addSkirt(func(t int) uint16 { return idx(0, dstRes-1-t) }, [3]float32{-1, 0, 0})

	return builtMesh{
		verts:   verts,
		norms:   norms,
		colors:  colors,
		indices: indices,
		tris:    int32(len(indices) / 3),
	}
}
