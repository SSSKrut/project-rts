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

// SurfaceParams controls material classification and the relief colour ramp.
// Zero values derive from TerrainParams.AmplitudeM; maps may override them.
type SurfaceParams struct {
	SandBand    [2]float32 `json:"sandBand"`
	RockBand    [2]float32 `json:"rockBand"`
	SlopeBand   [2]float32 `json:"slopeBand"`
	ReliefScale float32    `json:"reliefScale"`
}

func DefaultSurfaceParams() SurfaceParams {
	return SurfaceParams{
		SandBand:    [2]float32{-6.5, -3.0},
		RockBand:    [2]float32{5.5, 9.0},
		SlopeBand:   [2]float32{0.14, 0.48},
		ReliefScale: 1,
	}
}

var surfaceCfg = DefaultSurfaceParams()

func SetSurfaceParams(p SurfaceParams) {
	d := DefaultSurfaceParams()
	scale := float32(terrainCfg.AmplitudeM / 8)
	if scale <= 0 {
		scale = 1
	}
	d.SandBand = scaleSurfaceBand(d.SandBand, scale)
	d.RockBand = scaleSurfaceBand(d.RockBand, scale)
	d.ReliefScale = scale
	if p.SandBand[1] > p.SandBand[0] {
		d.SandBand = p.SandBand
	}
	if p.RockBand[1] > p.RockBand[0] {
		d.RockBand = p.RockBand
	}
	if p.SlopeBand[1] > p.SlopeBand[0] {
		d.SlopeBand = p.SlopeBand
	}
	if p.ReliefScale > 0 {
		d.ReliefScale = p.ReliefScale
	}
	surfaceCfg = d
}

func CurrentSurfaceParams() SurfaceParams { return surfaceCfg }

func scaleSurfaceBand(b [2]float32, scale float32) [2]float32 {
	return [2]float32{b[0] * scale, b[1] * scale}
}

// Height/colour control points (ascending in H after ReliefScale normalisation).
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

// ReliefColor exposes the ramp to render-side consumers (far-terrain rings)
// so distant ground shares the chunk meshes' albedo exactly.
func ReliefColor(y float32) (uint8, uint8, uint8) { return reliefColor(y) }

// reliefColor maps height to the ground's albedo — a linear blend between
// reliefStops. Pure colour: the slope shade that used to be baked in here now
// comes from the world shader's sun.
func reliefColor(y float32) (uint8, uint8, uint8) {
	y /= surfaceCfg.ReliefScale
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
	return uint8(rr), uint8(gg), uint8(bb)
}

// TerrainMeshSystem rebuilds GPU mesh for any chunk marked MeshDirty.
// Two passes: Active (full 65×65) and Relevant (decimated 33×33). On
// rebuild, the existing mesh is unloaded BEFORE the new one is assigned.
type TerrainMeshSystem struct {
	activeFilter   *ecs.Filter3[components.Heightmap, components.MeshDirty, components.LODActive]
	relevantFilter *ecs.Filter3[components.Heightmap, components.MeshDirty, components.LODRelevant]
	chunkMeshMap   *ecs.Map[components.ChunkMesh]
	meshDirtyMap   *ecs.Map[components.MeshDirty]
	chunkCoordMap  *ecs.Map[components.ChunkCoord]
	heightmapMap   *ecs.Map[components.Heightmap]
	chunkIndexRes  ecs.Resource[TerrainChunkIndex]
	riversRes      ecs.Resource[components.Rivers]
}

func (sys *TerrainMeshSystem) InitUI(w *ecs.World) {
	sys.activeFilter = ecs.NewFilter3[components.Heightmap, components.MeshDirty, components.LODActive](w)
	sys.relevantFilter = ecs.NewFilter3[components.Heightmap, components.MeshDirty, components.LODRelevant](w)
	sys.chunkMeshMap = ecs.NewMap[components.ChunkMesh](w)
	sys.meshDirtyMap = ecs.NewMap[components.MeshDirty](w)
	sys.chunkCoordMap = ecs.NewMap[components.ChunkCoord](w)
	sys.heightmapMap = ecs.NewMap[components.Heightmap](w)
	sys.chunkIndexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
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
	uvs     []float32
	uvs2    []float32
	indices []uint16
	tris    int32
}

// meshBudgetPerTick caps the rebuild+upload burst: a streaming-ring shift
// dirties a whole chunk row at once and a 20-mesh burst is a visible hitch.
// Leftovers keep MeshDirty and drain over the next ticks; Active
// (near-camera) chunks go first.
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
			cc := sys.chunkCoordMap.Get(q.Entity())
			if cc == nil {
				continue
			}
			heightAt := sys.chunkHeightSampler(*cc, hm)
			biomeAt := sys.chunkBiomeSampler(*cc)
			b := buildChunkMeshFromSampler(heightAt, components.ChunkResolution, biomeAt)
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
			cc := sys.chunkCoordMap.Get(q.Entity())
			if cc == nil {
				continue
			}
			res := (components.ChunkResolution + 1) / 2
			heightAt := sys.chunkHeightSampler(*cc, hm)
			biomeAt := sys.chunkBiomeSampler(*cc)
			b := buildDecimatedMeshFromSampler(heightAt, components.ChunkResolution, res, biomeAt)
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
		if len(b.uvs) > 0 {
			mesh.Texcoords = &b.uvs[0]
		}
		if len(b.uvs2) > 0 {
			mesh.Texcoords2 = &b.uvs2[0]
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

// chunkHeightSampler resolves local source-grid coordinates, including the
// one-vertex halo needed by central-difference normals at chunk boundaries.
// Missing neighbours fall back to procgen until streaming makes them resident.
func (sys TerrainMeshSystem) chunkHeightSampler(cc components.ChunkCoord, own *components.Heightmap) func(i, j int) float32 {
	idx := sys.chunkIndexRes.Get()
	const stride = components.ChunkResolution - 1
	return func(i, j int) float32 {
		if i >= 0 && i < components.ChunkResolution && j >= 0 && j < components.ChunkResolution {
			return own.Heights[j*components.ChunkResolution+i]
		}
		di := floorGridChunk(i, stride)
		dj := floorGridChunk(j, stride)
		ni := i - int(di)*stride
		nj := j - int(dj)*stride
		ncc := components.ChunkCoord{X: cc.X + di, Z: cc.Z + dj}
		if idx != nil {
			if ent, ok := idx.Loaded[ncc]; ok {
				if hm := sys.heightmapMap.Get(ent); hm != nil {
					return hm.Heights[nj*components.ChunkResolution+ni]
				}
			}
		}
		step := components.ChunkSize / float32(stride)
		wx := (float32(ncc.X)*float32(stride) + float32(ni)) * step
		wz := (float32(ncc.Z)*float32(stride) + float32(nj)) * step
		return GroundHeight(wx, wz)
	}
}

func (sys TerrainMeshSystem) chunkBiomeSampler(cc components.ChunkCoord) func(i, j int) SurfaceBiome {
	rivers := sys.riversRes.Get()
	step := components.ChunkSize / float32(components.ChunkResolution-1)
	return func(i, j int) SurfaceBiome {
		wx := (float32(cc.X)*float32(components.ChunkResolution-1) + float32(i)) * step
		wz := (float32(cc.Z)*float32(components.ChunkResolution-1) + float32(j)) * step
		return SurfaceBiomeAt(wx, wz, rivers)
	}
}

func floorGridChunk(v, stride int) int32 {
	q := v / stride
	if v%stride < 0 {
		q--
	}
	return int32(q)
}

// Vertex local space is [0, ChunkSize] on X/Z so the chunk's WorldPos
// translates the whole mesh into world space at draw time.
func buildChunkMeshFromSampler(heightAt func(i, j int) float32, srcRes int, biomeAt func(i, j int) SurfaceBiome) builtMesh {
	return assembleGrid(heightAt, srcRes, biomeAt)
}

// buildDecimatedMeshFromSampler samples every (srcRes-1)/(dstRes-1) source
// vertex. Border vertices are sampled exactly so edges join cleanly to
// neighbours at the same tier; across-tier seams hide behind skirts.
func buildDecimatedMeshFromSampler(sample func(i, j int) float32, srcRes, dstRes int, biomeSample func(i, j int) SurfaceBiome) builtMesh {
	srcStep := (srcRes - 1) / (dstRes - 1)
	heightAt := func(i, j int) float32 {
		return sample(i*srcStep, j*srcStep)
	}
	var biomeAt func(i, j int) SurfaceBiome
	if biomeSample != nil {
		biomeAt = func(i, j int) SurfaceBiome {
			return biomeSample(i*srcStep, j*srcStep)
		}
	}
	return assembleGrid(heightAt, dstRes, biomeAt)
}

// assembleGrid is the shared mesh builder. heightAt must provide a one-vertex
// halo outside the destination grid so normals agree across chunk boundaries.
// Skirts = 4 perimeter strips dropped skirtDrop metres below their top vertices.
func assembleGrid(heightAt func(i, j int) float32, dstRes int, biomeAt func(i, j int) SurfaceBiome) builtMesh {
	step := components.ChunkSize / float32(dstRes-1)

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
	uvs := make([]float32, 0, totalVerts*2)
	uvs2 := make([]float32, 0, totalVerts*2)
	indices := make([]uint16, 0, totalTris*3)

	for j := 0; j < dstRes; j++ {
		z := float32(j) * step
		for i := 0; i < dstRes; i++ {
			x := float32(i) * step
			y := heightAt(i, j)

			// dx along +X = (2*step, hr-hl, 0); dz along +Z =
			// (0, hu-hd, 2*step). Normal = dz × dx (points +Y on flat).
			hl := heightAt(i-1, j)
			hr := heightAt(i+1, j)
			hd := heightAt(i, j-1)
			hu := heightAt(i, j+1)
			nx := -(hr - hl) * (2 * step)
			ny := (2 * step) * (2 * step)
			nz := -(hu - hd) * (2 * step)
			invLen := 1.0 / float32(math.Sqrt(float64(nx*nx+ny*ny+nz*nz)))
			nx *= invLen
			ny *= invLen
			nz *= invLen

			verts = append(verts, x, y, z)
			norms = append(norms, nx, ny, nz)
			cr, cg, cb := reliefColor(y)
			colors = append(colors, cr, cg, cb, 255)
			var biome SurfaceBiome
			if biomeAt != nil {
				biome = biomeAt(i, j)
			}
			uvs = append(uvs, biome.Forest, biome.Rocky)
			uvs2 = append(uvs2, biome.Bushland, biome.Moisture)
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
			uvs = append(uvs, uvs[topI*2], uvs[topI*2+1])
			uvs2 = append(uvs2, uvs2[topI*2], uvs2[topI*2+1])
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
		uvs:     uvs,
		uvs2:    uvs2,
		indices: indices,
		tris:    int32(len(indices) / 3),
	}
}
