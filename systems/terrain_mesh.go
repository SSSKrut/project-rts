package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Muted earth tone for chunk skirts.
var skirtColor = [4]uint8{55, 45, 35, 255}

// Hides the height-mismatch seam between Active and Relevant chunks (their
// vertex resolutions interpolate the shared edge differently).
const skirtDrop float32 = 2.0

// Height/colour control points (ascending in H). Tuned for
// terrainAmplitude = 8 — adjust together if amplitude changes meaningfully.
var reliefStops = [...]struct {
	H       float32
	R, G, B float32
}{
	{-8, 55, 80, 50},    // wet lowland
	{-3, 90, 130, 70},   // dark grass
	{0, 125, 165, 90},   // grass
	{3, 160, 160, 100},  // dry grass / tan
	{6, 165, 140, 105},  // earth
	{9, 185, 175, 165},  // exposed rock
	{14, 220, 215, 205}, // peak
}

// reliefColor maps (height, normal-Y) to RGB. Linear blend between
// reliefStops, multiplied by a slope-based shade so the form reads on
// flat-shaded geometry without a runtime light.
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
	// 0.45 darkest (vertical wall) → 1.0 brightest (flat).
	shade := 0.45 + 0.55*ny
	return uint8(rr * shade), uint8(gg * shade), uint8(bb * shade)
}

// TerrainMeshSystem rebuilds GPU mesh for any chunk marked MeshDirty.
// Two passes: Active (full 65×65) and Relevant (decimated 33×33). On
// rebuild, the existing mesh is unloaded BEFORE the new one is assigned.
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

// builtMesh carries upload-ready data plus destination entity. Collected
// first, applied after iteration closes — chunkMeshMap touches archetypes.
type builtMesh struct {
	id ecs.Entity
	// Heap-allocated; UploadMesh reads them so the slices must stay alive
	// across the call.
	verts   []float32
	norms   []float32
	colors  []uint8
	indices []uint16
	tris    int32
}

// meshBudgetPerTick caps the rebuild+upload burst: a streaming-ring shift
// dirties a whole chunk row at once and a 20-mesh burst is a visible hitch
// (ISSUES #15). Leftovers keep MeshDirty and drain over the next ticks;
// Active (near-camera) chunks go first.
const meshBudgetPerTick = 3

func (sys TerrainMeshSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}

	var built []builtMesh

	{
		q := sys.activeFilter.Query()
		for q.Next() {
			hm, _, _ := q.Get()
			b := buildChunkMesh(hm.Heights, components.ChunkResolution)
			b.id = q.Entity()
			built = append(built, b)
			if len(built) >= meshBudgetPerTick {
				q.Close()
				break
			}
		}
	}

	// Relevant: every-other-vertex decimation. (65+1)/2 = 33.
	if len(built) < meshBudgetPerTick {
		q := sys.relevantFilter.Query()
		for q.Next() {
			hm, _, _ := q.Get()
			res := (components.ChunkResolution + 1) / 2
			b := buildDecimatedMesh(hm.Heights, components.ChunkResolution, res)
			b.id = q.Entity()
			built = append(built, b)
			if len(built) >= meshBudgetPerTick {
				q.Close()
				break
			}
		}
	}

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
				// UnloadMesh (not UnloadModel) — UnloadModel would C.free our
				// Go-allocated pointers.
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

// buildChunkMesh: full source resolution. Vertex local space is
// [0, ChunkSize] on X/Z so the chunk's WorldPos translates the whole thing
// into world space at draw time.
func buildChunkMesh(heights [components.ChunkResolution * components.ChunkResolution]float32, srcRes int) builtMesh {
	heightAt := func(i, j int) float32 {
		return heights[j*srcRes+i]
	}
	return assembleGrid(heightAt, srcRes, srcRes)
}

// buildDecimatedMesh samples every (srcRes-1)/(dstRes-1) source vertex.
// Border vertices are sampled exactly so edges join cleanly to neighbours
// at the same tier; across-tier seams hide behind skirts.
func buildDecimatedMesh(heights [components.ChunkResolution * components.ChunkResolution]float32, srcRes, dstRes int) builtMesh {
	srcStep := (srcRes - 1) / (dstRes - 1)
	heightAt := func(i, j int) float32 {
		si := i * srcStep
		sj := j * srcStep
		return heights[sj*srcRes+si]
	}
	return assembleGrid(heightAt, srcRes, dstRes)
}

// assembleGrid is the shared mesh builder. srcRes drives finite-difference
// normals via true world-spacing even at chunk edges. Skirts = 4 perimeter
// strips dropped skirtDrop metres below their top vertices.
func assembleGrid(heightAt func(i, j int) float32, srcRes, dstRes int) builtMesh {
	step := components.ChunkSize / float32(dstRes-1)
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

	for j := 0; j < dstRes; j++ {
		z := float32(j) * step
		for i := 0; i < dstRes; i++ {
			x := float32(i) * step
			y := heightAt(i, j)

			// Central differences with srcStep so the slope isn't distorted
			// by decimation. Edges fall back to one-sided differences.
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
			// dx along +X = (2*srcStep, hr-hl, 0); dz along +Z =
			// (0, hu-hd, 2*srcStep). Normal = dz × dx (points +Y on flat).
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

	// CCW winding from +Y so faces look up.
	idx := func(i, j int) uint16 {
		return uint16(j*dstRes + i)
	}
	for j := 0; j < dstRes-1; j++ {
		for i := 0; i < dstRes-1; i++ {
			a := idx(i, j)
			b := idx(i+1, j)
			c := idx(i, j+1)
			d := idx(i+1, j+1)
			indices = append(indices, a, c, b, b, c, d)
		}
	}

	// 4 border skirts: emit dstRes lower vertices (Y - skirtDrop) and stitch
	// quads to the top border. Walk in strip order for trivial indexing.
	skirtBase := uint16(topVertCount)

	addSkirt := func(borderIdx func(t int) uint16, normal [3]float32) {
		startLower := uint16(len(verts) / 3)
		for t := 0; t < dstRes; t++ {
			topI := borderIdx(t)
			tx := verts[topI*3]
			ty := verts[topI*3+1]
			tz := verts[topI*3+2]
			verts = append(verts, tx, ty-skirtDrop, tz)
			norms = append(norms, normal[0], normal[1], normal[2])
			colors = append(colors, skirtColor[0], skirtColor[1], skirtColor[2], skirtColor[3])
		}
		// Winding picks outward-facing tris: (top[t], low[t], top[t+1]) and
		// (top[t+1], low[t], low[t+1]).
		for t := 0; t < dstRes-1; t++ {
			tA := borderIdx(t)
			tB := borderIdx(t + 1)
			lA := startLower + uint16(t)
			lB := startLower + uint16(t+1)
			indices = append(indices, tA, lA, tB, tB, lA, lB)
		}
		_ = skirtBase
	}

	// Border walk directions chosen so triangulation yields outward normals.
	addSkirt(func(t int) uint16 { return idx(t, 0) }, [3]float32{0, 0, -1})
	addSkirt(func(t int) uint16 { return idx(dstRes-1, t) }, [3]float32{1, 0, 0})
	addSkirt(func(t int) uint16 { return idx(dstRes-1-t, dstRes-1) }, [3]float32{0, 0, 1})
	addSkirt(func(t int) uint16 { return idx(0, dstRes-1-t) }, [3]float32{-1, 0, 0})

	return builtMesh{
		verts:   verts,
		norms:   norms,
		colors:  colors,
		indices: indices,
		tris:    int32(len(indices) / 3),
	}
}
