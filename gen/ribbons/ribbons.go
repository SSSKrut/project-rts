// Package ribbons builds swept-strip geometry for the world's linear
// features — road carriageways, bridge decks and river surfaces. Pure data:
// terrain comes in through a Sampler, geometry goes out as upload-ready
// vertex arrays, so the same code serves the game and any sandbox.
package ribbons

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Sampler returns terrain height at a world XZ point.
type Sampler func(wx, wz float32) float32

// Mesh is upload-ready geometry. Vertices are relative to (AnchorX, AnchorZ)
// so a far-from-origin road keeps float32 precision; the caller translates
// by the anchor at draw time. Cull* is a world-space bounding circle.
type Mesh struct {
	Verts   []float32
	Norms   []float32
	Colors  []uint8
	Indices []uint16
	AnchorX float32
	AnchorZ float32
	CullX   float32
	CullZ   float32
	CullR   float32
}

// TriangleCount is what rl.Mesh wants alongside the vertex arrays.
func (m *Mesh) TriangleCount() int32 { return int32(len(m.Indices) / 3) }

// meshVertLimit keeps indices in uint16. Builders split a run into a new
// mesh before crossing it.
const meshVertLimit = 60000

func (m *Mesh) roomFor(verts int) bool {
	return len(m.Verts)/3+verts <= meshVertLimit
}

// quad appends one flat-shaded quad. a0→a1 and b0→b1 are the two rails of a
// strip step; cA colours the a0/b0 rail, cB the a1/b1 rail. up decides the
// winding: the face normal is flipped to agree with it, so callers pass the
// side the quad should be visible from instead of tracking vertex order.
func (m *Mesh) quad(a0, a1, b0, b1 rl.Vector3, cA, cB [4]uint8, up rl.Vector3) {
	n := faceNormal(a0, a1, b1)
	flip := n.X*up.X+n.Y*up.Y+n.Z*up.Z < 0
	if flip {
		n = rl.Vector3{X: -n.X, Y: -n.Y, Z: -n.Z}
	}
	shade := 0.45 + 0.55*absF(n.Y)
	base := uint16(len(m.Verts) / 3)
	for _, v := range [4]struct {
		p rl.Vector3
		c [4]uint8
	}{{a0, cA}, {a1, cB}, {b1, cB}, {b0, cA}} {
		m.Verts = append(m.Verts, v.p.X-m.AnchorX, v.p.Y, v.p.Z-m.AnchorZ)
		m.Norms = append(m.Norms, n.X, n.Y, n.Z)
		m.Colors = append(m.Colors,
			uint8(float32(v.c[0])*shade), uint8(float32(v.c[1])*shade),
			uint8(float32(v.c[2])*shade), v.c[3])
	}
	if flip {
		m.Indices = append(m.Indices, base, base+3, base+2, base, base+2, base+1)
	} else {
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
}

// box appends the four side faces of an upright box — bridge piers.
func (m *Mesh) box(cx, cz, y0, y1, half float32, col [4]uint8) {
	c := [4]rl.Vector3{
		{X: cx - half, Z: cz - half}, {X: cx + half, Z: cz - half},
		{X: cx + half, Z: cz + half}, {X: cx - half, Z: cz + half},
	}
	for i := 0; i < 4; i++ {
		a, b := c[i], c[(i+1)%4]
		out := rl.Vector3{X: (a.X+b.X)*0.5 - cx, Z: (a.Z+b.Z)*0.5 - cz}
		m.quad(
			rl.Vector3{X: a.X, Y: y1, Z: a.Z}, rl.Vector3{X: b.X, Y: y1, Z: b.Z},
			rl.Vector3{X: a.X, Y: y0, Z: a.Z}, rl.Vector3{X: b.X, Y: y0, Z: b.Z},
			col, col, out)
	}
}

// finishCull sets the bounding circle from the accumulated vertices.
func (m *Mesh) finishCull() {
	if len(m.Verts) == 0 {
		return
	}
	minX, maxX := float32(math.Inf(1)), float32(math.Inf(-1))
	minZ, maxZ := minX, maxX
	for i := 0; i < len(m.Verts); i += 3 {
		x := m.Verts[i] + m.AnchorX
		z := m.Verts[i+2] + m.AnchorZ
		minX, maxX = minF(minX, x), maxF(maxX, x)
		minZ, maxZ = minF(minZ, z), maxF(maxZ, z)
	}
	m.CullX = (minX + maxX) * 0.5
	m.CullZ = (minZ + maxZ) * 0.5
	m.CullR = 0.5 * float32(math.Sqrt(float64((maxX-minX)*(maxX-minX)+(maxZ-minZ)*(maxZ-minZ))))
}

func faceNormal(a, b, c rl.Vector3) rl.Vector3 {
	ux, uy, uz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	vx, vy, vz := c.X-a.X, c.Y-a.Y, c.Z-a.Z
	nx := uy*vz - uz*vy
	ny := uz*vx - ux*vz
	nz := ux*vy - uy*vx
	l := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
	if l == 0 {
		return rl.Vector3{Y: 1}
	}
	return rl.Vector3{X: nx / l, Y: ny / l, Z: nz / l}
}

var up = rl.Vector3{Y: 1}

const pi float32 = math.Pi

func acos(v float32) float32 { return float32(math.Acos(float64(v))) }

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
