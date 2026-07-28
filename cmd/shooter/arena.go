package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	coverTallH = 2.4 // blocks sight and bullets
	coverLowH  = 0.9 // shoot over it, still can't walk through
	fieldHalf  = groundSize / 2
)

var (
	colorCoverTall = rl.Color{R: 78, G: 86, B: 100, A: 255}
	colorCoverLow  = rl.Color{R: 104, G: 88, B: 62, A: 255}

	// One half of the arena; every piece off the origin gets a twin at (-x, -z),
	// so both teams face exactly the same walls. Nothing reaches past |x| = 15,
	// which keeps the two spawn lines in the open.
	coverLayout = []struct{ X, Z, W, D, H float32 }{
		{0, 0, 4, 4, coverTallH},
		{0, 10, 12, 1.2, coverLowH},
		{7.5, 4, 1.2, 8, coverTallH},
		{12, -8, 5, 1.2, coverLowH},
		{5, -12, 3, 3, coverTallH},
		{13.5, 2, 1.2, 6, coverLowH},
	}
)

func buildArena() Physics {
	p := Physics{HalfXZ: fieldHalf}
	for _, c := range coverLayout {
		p.Boxes = append(p.Boxes, BoxAt(rl.Vector3{X: c.X, Y: c.H / 2, Z: c.Z}, c.W, c.H, c.D))
		if c.X != 0 || c.Z != 0 {
			p.Boxes = append(p.Boxes, BoxAt(rl.Vector3{X: -c.X, Y: c.H / 2, Z: -c.Z}, c.W, c.H, c.D))
		}
	}
	return p
}

func drawCovers(p *Physics) {
	for _, b := range p.Boxes {
		centre, size := b.Centre(), b.Size()
		colour := colorCoverLow
		if size.Y > coverLowH {
			colour = colorCoverTall
		}
		rl.DrawCubeV(centre, size, colour)
		rl.DrawCubeWiresV(centre, size, colorOutline)
	}
}
