package ribbons

import "math"

// pathPt is one control point of a feature's centre line, tagged with the
// graph edge that owns the segment leaving it (-1 for rivers).
type pathPt struct {
	X, Z float32
	Edge int32
}

// fillet rounds every interior corner with a quadratic Bezier. radius and
// maxDev are per control point, so a chain of mixed road classes rounds each
// corner on its own terms. The tangent offset is clamped twice: to 0.4 of
// either neighbour's length, and so the curve's mid-point strays no further
// than maxDev from the corner. The second clamp is what keeps the drawn road
// inside the straight-edge corridor NavGrid and the router still use.
func fillet(pts []pathPt, radius, maxDev []float32) []pathPt {
	if len(pts) < 3 {
		return pts
	}
	out := make([]pathPt, 0, len(pts)*6)
	out = append(out, pts[0])
	for i := 1; i < len(pts)-1; i++ {
		p := pts[i]
		ux, uz := p.X-pts[i-1].X, p.Z-pts[i-1].Z
		vx, vz := pts[i+1].X-p.X, pts[i+1].Z-p.Z
		lu, lv := hypot(ux, uz), hypot(vx, vz)
		if lu <= 0 || lv <= 0 {
			continue
		}
		ux, uz = ux/lu, uz/lu
		vx, vz = vx/lv, vz/lv

		// |v-u| = 2·sin(turn/2); the Bezier mid-point sits d·sin(turn/2)/2
		// off the corner.
		s := hypot(vx-ux, vz-uz) * 0.5
		if s < 0.03 {
			out = append(out, p)
			continue
		}
		d := minF(radius[i], minF(0.4*lu, 0.4*lv))
		d = minF(d, 2*maxDev[i]/s)
		if d < 0.25 {
			out = append(out, p)
			continue
		}

		a := pathPt{X: p.X - ux*d, Z: p.Z - uz*d, Edge: pts[i-1].Edge}
		b := pathPt{X: p.X + vx*d, Z: p.Z + vz*d, Edge: p.Edge}
		out = append(out, a)
		steps := int(d) + 3
		for k := 1; k < steps; k++ {
			t := float32(k) / float32(steps)
			mt := 1 - t
			e := a.Edge
			if t >= 0.5 {
				e = b.Edge
			}
			out = append(out, pathPt{
				X:    mt*mt*a.X + 2*mt*t*p.X + t*t*b.X,
				Z:    mt*mt*a.Z + 2*mt*t*p.Z + t*t*b.Z,
				Edge: e,
			})
		}
		out = append(out, b)
	}
	out = append(out, pts[len(pts)-1])
	return out
}

// resample walks the polyline at a uniform arc-length step. The final point
// is always the exact input endpoint so neighbouring chains meet at the node.
func resample(pts []pathPt, step float32) []pathPt {
	if len(pts) < 2 || step <= 0 {
		return pts
	}
	out := make([]pathPt, 0, len(pts)*2)
	out = append(out, pts[0])
	pos := step
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		segLen := hypot(b.X-a.X, b.Z-a.Z)
		if segLen <= 0 {
			continue
		}
		for pos <= segLen {
			t := pos / segLen
			out = append(out, pathPt{
				X:    a.X + t*(b.X-a.X),
				Z:    a.Z + t*(b.Z-a.Z),
				Edge: a.Edge,
			})
			pos += step
		}
		pos -= segLen
	}
	last := pts[len(pts)-1]
	tail := out[len(out)-1]
	if hypot(last.X-tail.X, last.Z-tail.Z) > step*0.25 {
		out = append(out, last)
	} else {
		out[len(out)-1] = pathPt{X: last.X, Z: last.Z, Edge: tail.Edge}
	}
	return out
}

// smooth runs Laplacian passes over an open profile with both ends pinned.
// Entries flagged in fixed (may be nil) are held too — that is how a bridge
// deck keeps its height while the passes shape its approach ramps.
func smooth(y []float32, fixed []bool, iters int, lambda float32) {
	if len(y) < 3 {
		return
	}
	for it := 0; it < iters; it++ {
		prev := y[0]
		for i := 1; i < len(y)-1; i++ {
			cur := y[i]
			if fixed == nil || !fixed[i] {
				y[i] = cur + lambda*(prev+y[i+1]-2*cur)
			}
			prev = cur
		}
	}
}

func hypot(dx, dz float32) float32 {
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}
