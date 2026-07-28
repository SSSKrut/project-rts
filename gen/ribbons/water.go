package ribbons

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

const (
	waterStep    float32 = 2.0
	waterSink    float32 = 0.35
	waterMinSink float32 = 0.10
	waterMargin  float32 = 0.15
	waterIters           = 8
)

var waterColor = [4]uint8{58, 104, 160, 215}

// BuildWater turns river polylines into flat surface ribbons. The surface
// never climbs downstream, and its half-width is solved from the depth the
// water sits at in RiverCut's cosine trough — so the ribbon meets the banks
// instead of using a fixed plate size.
func BuildWater(rivers *components.Rivers, h Sampler) []Mesh {
	if rivers == nil || h == nil {
		return nil
	}
	var out []Mesh
	for pi := range rivers.Polylines {
		pl := &rivers.Polylines[pi]
		if len(pl.Points) < 2 || pl.Width <= 0 || pl.Depth <= 0 {
			continue
		}
		ctrl := make([]pathPt, len(pl.Points))
		for i, p := range pl.Points {
			ctrl[i] = pathPt{
				X:    float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
				Z:    float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z,
				Edge: -1,
			}
		}
		radius := make([]float32, len(ctrl))
		maxDev := make([]float32, len(ctrl))
		for i := range ctrl {
			radius[i], maxDev[i] = pl.Width, pl.Width*0.5
		}
		pts := resample(fillet(ctrl, radius, maxDev), waterStep)
		n := len(pts)
		if n < 2 {
			continue
		}

		bank := make([]float32, n)
		y := make([]float32, n)
		for i, p := range pts {
			bank[i] = h(p.X, p.Z)
			y[i] = bank[i] - waterSink
		}
		// Downstream the surface prefers to keep falling — that is what makes
		// a run read as flowing rather than as a chain of puddles — but it may
		// never sink below its own bed, or a stretch where the relief climbs
		// again would simply disappear under the terrain.
		smooth(y, nil, waterIters, profileLambda)
		maxSink := pl.Depth * 0.9
		step := 1
		start := 1
		if !flowsForward(bank) {
			step, start = -1, n-2
		}
		for i := start; i >= 0 && i < n; i += step {
			y[i] = minF(y[i], y[i-step])
			y[i] = clampF(y[i], bank[i]-maxSink, bank[i]-waterMinSink)
		}
		y[start-step] = clampF(y[start-step],
			bank[start-step]-maxSink, bank[start-step]-waterMinSink)

		net := Network{}
		mesh := net.newMesh(pts[0].X, pts[0].Z)
		half := func(i int) (rl.Vector3, rl.Vector3) {
			latX, latZ := lateral(pts, i)
			w := troughHalfWidth(bank[i]-y[i], pl.Depth, pl.Width) - waterMargin
			w = maxF(w, 0.2)
			return rl.Vector3{X: pts[i].X - latX*w, Y: y[i], Z: pts[i].Z - latZ*w},
				rl.Vector3{X: pts[i].X + latX*w, Y: y[i], Z: pts[i].Z + latZ*w}
		}
		for i := 0; i+1 < n; i++ {
			if !net.Meshes[mesh].roomFor(8) {
				mesh = net.newMesh(pts[i].X, pts[i].Z)
			}
			aL, aR := half(i)
			bL, bR := half(i + 1)
			net.Meshes[mesh].quad(aL, aR, bL, bR, waterColor, waterColor, up)
		}
		for i := range net.Meshes {
			net.Meshes[i].finishCull()
		}
		out = append(out, net.Meshes...)
	}
	return out
}

// troughHalfWidth solves RiverCut's cosine profile for the distance where
// the bed rises to the water surface.
func troughHalfWidth(sink, depth, width float32) float32 {
	if depth <= 0 {
		return width * 0.5
	}
	c := clampF(2*sink/depth-1, -1, 1)
	return clampF(width*acos(c)/pi, width*0.2, width)
}

// flowsForward reports whether the polyline runs downhill from its first
// point, which is the direction the surface may not climb.
func flowsForward(bank []float32) bool {
	q := len(bank) / 4
	if q < 1 {
		q = 1
	}
	var head, tail float32
	for i := 0; i < q; i++ {
		head += bank[i]
		tail += bank[len(bank)-1-i]
	}
	return head >= tail
}
