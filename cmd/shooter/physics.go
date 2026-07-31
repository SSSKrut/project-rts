package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// The sandbox's whole physics engine: axis-aligned boxes as the static bodies,
// swept rays for bullets and line of sight, circle push-out for anything on
// foot. It knows nothing about teams, damage or drawing.

const (
	solveIterations = 3
	skin            = 1e-3
)

type AABB struct{ Min, Max rl.Vector3 }

func BoxAt(centre rl.Vector3, w, h, d float32) AABB {
	half := rl.Vector3{X: w / 2, Y: h / 2, Z: d / 2}
	return AABB{Min: rl.Vector3Subtract(centre, half), Max: rl.Vector3Add(centre, half)}
}

func (b AABB) Centre() rl.Vector3 { return rl.Vector3Scale(rl.Vector3Add(b.Min, b.Max), 0.5) }
func (b AABB) Size() rl.Vector3   { return rl.Vector3Subtract(b.Max, b.Min) }

// growXZ inflates the footprint by r — the Minkowski trick that turns a moving
// circle into a moving point.
func (b AABB) growXZ(r float32) AABB {
	b.Min.X, b.Min.Z = b.Min.X-r, b.Min.Z-r
	b.Max.X, b.Max.Z = b.Max.X+r, b.Max.Z+r
	return b
}

// RayT returns the distance along a unit dir at which the ray first enters the
// box, or false if it misses inside maxT. Plain slab test.
func (b AABB) RayT(origin, dir rl.Vector3, maxT float32) (float32, bool) {
	o := [3]float32{origin.X, origin.Y, origin.Z}
	d := [3]float32{dir.X, dir.Y, dir.Z}
	lo := [3]float32{b.Min.X, b.Min.Y, b.Min.Z}
	hi := [3]float32{b.Max.X, b.Max.Y, b.Max.Z}

	tMin, tMax := float32(0), maxT
	for a := range 3 {
		if abs32(d[a]) < 1e-6 {
			if o[a] < lo[a] || o[a] > hi[a] {
				return 0, false
			}
			continue
		}
		t1, t2 := (lo[a]-o[a])/d[a], (hi[a]-o[a])/d[a]
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if tMin, tMax = max(tMin, t1), min(tMax, t2); tMin > tMax {
			return 0, false
		}
	}
	return tMin, true
}

// PushOutXZ lifts a circle out of the box along the shallowest horizontal axis
// — the cheapest resolve that still slides a body along the wall it hit. Height
// is ignored: nobody in this sandbox vaults, so a knee-high wall stops a walker
// exactly like a tall one.
func (b AABB) PushOutXZ(p rl.Vector3, r float32) (rl.Vector3, bool) {
	g := b.growXZ(r)
	if p.X <= g.Min.X || p.X >= g.Max.X || p.Z <= g.Min.Z || p.Z >= g.Max.Z {
		return p, false
	}

	dx, dz := g.Min.X-p.X, g.Min.Z-p.Z
	if right := g.Max.X - p.X; right < -dx {
		dx = right
	}
	if front := g.Max.Z - p.Z; front < -dz {
		dz = front
	}

	if abs32(dx) < abs32(dz) {
		p.X += dx + copysign32(skin, dx)
	} else {
		p.Z += dz + copysign32(skin, dz)
	}
	return p, true
}

// RayCylinderT intersects a ray with an upright cylinder standing on base —
// the body shape every character wears.
func RayCylinderT(origin, dir rl.Vector3, maxT float32, base rl.Vector3, r, h float32) (float32, bool) {
	ox, oz := origin.X-base.X, origin.Z-base.Z
	a := dir.X*dir.X + dir.Z*dir.Z
	if a < 1e-9 {
		return 0, false
	}

	twoA := 2 * a
	b := 2 * (ox*dir.X + oz*dir.Z)
	disc := b*b - 2*twoA*(ox*ox+oz*oz-r*r)
	if disc < 0 {
		return 0, false
	}

	sq := sqrt32(disc)
	t := (-b - sq) / twoA
	if t < 0 {
		t = (-b + sq) / twoA // ray started inside the cylinder
	}
	if t < 0 || t > maxT {
		return 0, false
	}
	if y := origin.Y + dir.Y*t; y < base.Y || y > base.Y+h {
		return 0, false
	}
	return t, true
}

// SegDistXZ is the closest horizontal distance from p to the segment ab — how
// near a round actually passed, rather than where it ended up.
func SegDistXZ(a, b, p rl.Vector3) float32 {
	abx, abz := b.X-a.X, b.Z-a.Z
	apx, apz := p.X-a.X, p.Z-a.Z

	t := float32(0)
	if den := abx*abx + abz*abz; den > 1e-9 {
		t = rl.Clamp((apx*abx+apz*abz)/den, 0, 1)
	}
	dx, dz := apx-abx*t, apz-abz*t
	return sqrt32(dx*dx + dz*dz)
}

// SeparateXZ pushes two overlapping bodies apart, half the overlap each.
func SeparateXZ(a, b *rl.Vector3, minDist float32) {
	dx, dz := b.X-a.X, b.Z-a.Z
	d2 := dx*dx + dz*dz
	if d2 >= minDist*minDist {
		return
	}
	if d2 < 1e-6 {
		dx, dz, d2 = 1, 0, 1 // exactly stacked: shove along +X
	}

	d := sqrt32(d2)
	push := (minDist - d) / (2 * d)
	a.X, a.Z = a.X-dx*push, a.Z-dz*push
	b.X, b.Z = b.X+dx*push, b.Z+dz*push
}

// Physics is the static scene: solid boxes inside a square arena.
type Physics struct {
	Boxes  []AABB
	HalfXZ float32
}

// Trace returns the nearest box entry along the ray.
func (p *Physics) Trace(origin, dir rl.Vector3, maxT float32) (float32, bool) {
	best, found := maxT, false
	for _, b := range p.Boxes {
		if t, ok := b.RayT(origin, dir, best); ok {
			best, found = t, true
		}
	}
	return best, found
}

// Blocked reports whether a box stands between the two points — the sight query
// bullets and NPC trigger discipline both lean on.
func (p *Physics) Blocked(from, to rl.Vector3) bool {
	d := rl.Vector3Subtract(to, from)
	dist := rl.Vector3Length(d)
	if dist < 1e-4 {
		return false
	}
	_, hit := p.Trace(from, rl.Vector3Scale(d, 1/dist), dist)
	return hit
}

// MoveCircle walks a body by delta and resolves it out of every box it ends up
// inside, so a step into a wall slides along it instead of stopping dead.
func (p *Physics) MoveCircle(pos, delta rl.Vector3, r float32) rl.Vector3 {
	pos = rl.Vector3Add(pos, delta)

	for range solveIterations {
		moved := false
		for _, b := range p.Boxes {
			if next, ok := b.PushOutXZ(pos, r); ok {
				pos, moved = next, true
			}
		}
		if !moved {
			break
		}
	}

	lim := p.HalfXZ - r
	pos.X = rl.Clamp(pos.X, -lim, lim)
	pos.Z = rl.Clamp(pos.Z, -lim, lim)
	return pos
}

func abs32(v float32) float32  { return float32(math.Abs(float64(v))) }
func sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }
func copysign32(v, sign float32) float32 {
	return float32(math.Copysign(float64(v), float64(sign)))
}
