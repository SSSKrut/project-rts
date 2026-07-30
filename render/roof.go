package render

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// DrawRoof renders a roof cap: four trapezoid slopes rising from the eave
// rectangle to a flat top deck, plus the deck itself. `pos` is the eave-
// rectangle centre at eave height (Roof.SizeX/SizeZ already include overhang).
//
// Winding is counter-clockwise seen from OUTSIDE each face, so raylib's
// default backface culling keeps the shell solid and lets the camera see
// through it from below when a cutaway removes the top storey.
func DrawRoof(pos rl.Vector3, r components.Roof, fogged bool) {
	if r.Kind == components.RoofNone || r.SizeX <= 0 || r.SizeZ <= 0 {
		return
	}

	slopeCol := rl.Color{R: 120, G: 78, B: 62, A: 255}
	deckCol := rl.Color{R: 138, G: 96, B: 78, A: 255}
	if fogged {
		slopeCol = rl.Color{R: 62, G: 52, B: 50, A: 220}
		deckCol = rl.Color{R: 70, G: 60, B: 56, A: 220}
	}

	hx := r.SizeX * 0.5
	hz := r.SizeZ * 0.5
	top := pos.Y + r.Height
	inset := r.Inset
	if inset > hx {
		inset = hx
	}
	if inset > hz {
		inset = hz
	}
	dx := hx - inset
	dz := hz - inset

	// Eave ring (y = pos.Y) and deck ring (y = top), both listed
	// counter-clockwise viewed from above starting at (-x, -z).
	e := [4]rl.Vector3{
		{X: pos.X - hx, Y: pos.Y, Z: pos.Z - hz},
		{X: pos.X + hx, Y: pos.Y, Z: pos.Z - hz},
		{X: pos.X + hx, Y: pos.Y, Z: pos.Z + hz},
		{X: pos.X - hx, Y: pos.Y, Z: pos.Z + hz},
	}
	d := [4]rl.Vector3{
		{X: pos.X - dx, Y: top, Z: pos.Z - dz},
		{X: pos.X + dx, Y: top, Z: pos.Z - dz},
		{X: pos.X + dx, Y: top, Z: pos.Z + dz},
		{X: pos.X - dx, Y: top, Z: pos.Z + dz},
	}

	// Slopes. For side i the outward face is (e[i], e[i+1], d[i+1], d[i]);
	// wound as two triangles that read counter-clockwise from outside.
	for i := 0; i < 4; i++ {
		j := (i + 1) & 3
		rl.DrawTriangle3D(e[i], d[j], e[j], slopeCol)
		rl.DrawTriangle3D(e[i], d[i], d[j], slopeCol)
	}

	// Deck, facing up.
	rl.DrawTriangle3D(d[0], d[2], d[1], deckCol)
	rl.DrawTriangle3D(d[0], d[3], d[2], deckCol)

	// Eave fascia: a thin skirt hanging below the overhang so the roof reads
	// as a slab edge instead of a paper-thin cut against the wall.
	if components.RoofThickness > 0 {
		lowY := pos.Y - components.RoofThickness
		for i := 0; i < 4; i++ {
			j := (i + 1) & 3
			a := e[i]
			b := e[j]
			al := rl.Vector3{X: a.X, Y: lowY, Z: a.Z}
			bl := rl.Vector3{X: b.X, Y: lowY, Z: b.Z}
			rl.DrawTriangle3D(a, bl, b, slopeCol)
			rl.DrawTriangle3D(a, al, bl, slopeCol)
		}
	}
}
