package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
)

// Far terrain: two render-only ring meshes sampled from the HeightPyramid,
// carrying ground past the streamed chunks out to the horizon. No ECS, no
// streaming — one DrawMesh each with the terrain material, so the surface
// blend, sun and cloud shadows apply unchanged. Rings sit a few metres BELOW
// the true surface: real chunks always win the depth test over them, and the
// chunk skirts hide the seam at the inner hole.
//
// Ring A's hole tracks the streamed chunk square exactly (Chebyshev, chunk
// grid aligned); Ring B's hole hides under Ring A with a one-cell overlap.
// Everything is rebuilt when the anchor crosses a chunk boundary — ~2 ms and
// worth zero bookkeeping.
type farRingSpec struct {
	level     int
	cell      float32
	halfCells int
	drop      float32
	maxRadius float32 // radial cull of cell centres; 0 = none
}

var farRingSpecs = [2]farRingSpec{
	{level: 0, cell: components.ChunkSize, halfCells: 64, drop: 2.0},
	{level: 1, cell: 512, halfCells: 30, drop: 6.0, maxRadius: 15000},
}

// Ring A hole half-extent in chunks. Matches terrainRelevantRadius: chunks
// within it are guaranteed drawn, so the hole never shows void.
const farHoleChunks = 6

// Ring B hole half-extent in metres; must stay under Ring A's coverage.
const farRingBHole float32 = 3584

type farRing struct {
	verts   []float32
	norms   []float32
	colors  []uint8
	indices []uint16
	mesh    rl.Mesh
	built   bool
}

type farTerrain struct {
	pyr    *systems.HeightPyramid
	rings  [2]farRing
	center components.ChunkCoord
	valid  bool
}

func newFarTerrain(pyr *systems.HeightPyramid) *farTerrain {
	return &farTerrain{pyr: pyr}
}

// ensure rebuilds both rings when the anchor's chunk changed.
func (ft *farTerrain) ensure(anchor components.ChunkCoord) {
	if ft == nil || ft.pyr == nil {
		return
	}
	if ft.valid && anchor == ft.center {
		return
	}
	ft.unload()
	cxw := float32(anchor.X) * components.ChunkSize
	czw := float32(anchor.Z) * components.ChunkSize

	specA := farRingSpecs[0]
	baseAX := cxw - float32(specA.halfCells)*specA.cell
	baseAZ := czw - float32(specA.halfCells)*specA.cell
	ft.rings[0] = buildFarRing(ft.pyr, specA, baseAX, baseAZ, cxw, czw, func(i, j int) bool {
		ci := int32(anchor.X) - int32(specA.halfCells) + int32(i)
		cj := int32(anchor.Z) - int32(specA.halfCells) + int32(j)
		return chebI32(ci-anchor.X) <= farHoleChunks && chebI32(cj-anchor.Z) <= farHoleChunks
	})

	specB := farRingSpecs[1]
	baseBX := snapDown(cxw-float32(specB.halfCells)*specB.cell, specB.cell)
	baseBZ := snapDown(czw-float32(specB.halfCells)*specB.cell, specB.cell)
	ft.rings[1] = buildFarRing(ft.pyr, specB, baseBX, baseBZ, cxw, czw, func(i, j int) bool {
		ccx := baseBX + (float32(i)+0.5)*specB.cell
		ccz := baseBZ + (float32(j)+0.5)*specB.cell
		return maxAbs(ccx-cxw, ccz-czw) < farRingBHole
	})

	ft.center = anchor
	ft.valid = true
}

func buildFarRing(pyr *systems.HeightPyramid, spec farRingSpec,
	baseX, baseZ, cxw, czw float32, inHole func(i, j int) bool) farRing {

	n := spec.halfCells * 2
	nv := n + 1
	r := farRing{
		verts:  make([]float32, 0, nv*nv*3),
		norms:  make([]float32, 0, nv*nv*3),
		colors: make([]uint8, 0, nv*nv*4),
	}
	for j := 0; j < nv; j++ {
		z := baseZ + float32(j)*spec.cell
		for i := 0; i < nv; i++ {
			x := baseX + float32(i)*spec.cell
			y := pyr.Sample(spec.level, x, z)

			hl := pyr.Sample(spec.level, x-spec.cell, z)
			hr := pyr.Sample(spec.level, x+spec.cell, z)
			hd := pyr.Sample(spec.level, x, z-spec.cell)
			hu := pyr.Sample(spec.level, x, z+spec.cell)
			nx := -(hr - hl) * (2 * spec.cell)
			ny := (2 * spec.cell) * (2 * spec.cell)
			nz := -(hu - hd) * (2 * spec.cell)
			invLen := 1.0 / float32(math.Sqrt(float64(nx*nx+ny*ny+nz*nz)))

			r.verts = append(r.verts, x, y-spec.drop, z)
			r.norms = append(r.norms, nx*invLen, ny*invLen, nz*invLen)
			cr, cg, cb := systems.ReliefColor(y)
			r.colors = append(r.colors, cr, cg, cb, 255)
		}
	}
	r.indices = make([]uint16, 0, n*n*6)
	idx := func(i, j int) uint16 { return uint16(j*nv + i) }
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			if inHole(i, j) {
				continue
			}
			if spec.maxRadius > 0 {
				ccx := baseX + (float32(i)+0.5)*spec.cell
				ccz := baseZ + (float32(j)+0.5)*spec.cell
				dx := ccx - cxw
				dz := ccz - czw
				if dx*dx+dz*dz > spec.maxRadius*spec.maxRadius {
					continue
				}
			}
			a := idx(i, j)
			b := idx(i+1, j)
			c := idx(i, j+1)
			d := idx(i+1, j+1)
			r.indices = append(r.indices, a, c, b, b, c, d)
		}
	}
	if len(r.indices) == 0 {
		return r
	}
	r.mesh = rl.Mesh{
		VertexCount:   int32(len(r.verts) / 3),
		TriangleCount: int32(len(r.indices) / 3),
		Vertices:      &r.verts[0],
		Normals:       &r.norms[0],
		Colors:        &r.colors[0],
		Indices:       &r.indices[0],
	}
	rl.UploadMesh(&r.mesh, false)
	r.built = true
	return r
}

// draw rebases world-coordinate meshes onto the current render origin, same
// translation the ribbons use.
func (ft *farTerrain) draw(material rl.Material) {
	if ft == nil || !ft.valid {
		return
	}
	originX := float32(systems.CurrentOriginChunk.X) * components.ChunkSize
	originZ := float32(systems.CurrentOriginChunk.Z) * components.ChunkSize
	xform := rl.MatrixTranslate(-originX, 0, -originZ)
	for i := range ft.rings {
		if ft.rings[i].built {
			rl.DrawMesh(ft.rings[i].mesh, material, xform)
		}
	}
}

func (ft *farTerrain) unload() {
	if ft == nil {
		return
	}
	for i := range ft.rings {
		if ft.rings[i].built {
			rl.UnloadMesh(&ft.rings[i].mesh)
			ft.rings[i] = farRing{}
		}
	}
	ft.valid = false
}

func chebI32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func snapDown(v, cell float32) float32 {
	return float32(math.Floor(float64(v/cell))) * cell
}

func maxAbs(a, b float32) float32 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if a > b {
		return a
	}
	return b
}
