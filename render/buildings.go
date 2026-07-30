// Package render holds pure draw helpers shared by the game and the
// generator sandbox. Nothing here touches ECS or game state — inputs are a
// render-space position plus the component being drawn, so cmd/ tools render
// exactly what the game renders.
package render

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// DrawFloor draws a horizontal grey plate at the floor's render position.
// Floor.Y is the slab top - drop a thin slab below it. `fogged` desaturates
// the slab when the level is un-discovered or stale.
func DrawFloor(pos rl.Vector3, f components.Floor, fogged bool) {
	const slabThickness float32 = 0.15
	col := rl.Color{R: 110, G: 110, B: 120, A: 255}
	if fogged {
		col = rl.Color{R: 55, G: 55, B: 60, A: 220}
	}
	c := rl.Vector3{X: pos.X, Y: pos.Y - slabThickness*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: f.SizeX, Y: slabThickness, Z: f.SizeZ}, col)
}

// DrawWall renders a wall segment with optional opening (door / window).
// WallSegment local axes after Rotatef(Yaw): +Z = along wall, +X = thickness,
// +Y = up. `outward` is the wall's outward XZ unit vector and `camPos` the
// render-space camera, used by WallRenderCameraFacing to fade walls facing
// the camera.
func DrawWall(pos rl.Vector3, w components.WallSegment, mode components.WallRenderMode, outward rl.Vector3, camPos rl.Vector3, fogged bool) {
	wallCol := rl.Color{R: 175, G: 170, B: 165, A: 255}
	doorCol := rl.Color{R: 90, G: 60, B: 35, A: 255}
	winCol := rl.Color{R: 160, G: 200, B: 230, A: 200}
	lintelCol := rl.Color{R: 150, G: 145, B: 140, A: 255}

	if fogged {
		wallCol = rl.Color{R: 75, G: 75, B: 80, A: 220}
		lintelCol = rl.Color{R: 65, G: 65, B: 70, A: 220}
		doorCol = rl.Color{R: 50, G: 50, B: 55, A: 220}
		winCol = rl.Color{R: 95, G: 100, B: 110, A: 200}
	}

	wireMode := mode == components.WallRenderWireframe

	if mode == components.WallRenderCameraFacing {
		// After Rotatef(yaw,+Y) local +Z maps to world (sin(yaw), 0, cos(yaw)).
		s := float32(math.Sin(float64(w.Yaw)))
		c := float32(math.Cos(float64(w.Yaw)))
		wcx := pos.X + (w.Length*0.5)*s
		wcz := pos.Z + (w.Length*0.5)*c
		dx := camPos.X - wcx
		dz := camPos.Z - wcz
		l := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if l > 0.0001 {
			dx /= l
			dz /= l
		}
		if outward.X*dx+outward.Z*dz > 0.2 {
			wallCol.A = 60
			lintelCol.A = 60
		}
	}

	drawCube := func(c rl.Vector3, size rl.Vector3, col rl.Color) {
		if wireMode {
			rl.DrawCubeWires(c, size.X, size.Y, size.Z, col)
		} else {
			rl.DrawCubeV(c, size, col)
		}
	}

	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(w.Yaw*(180.0/math.Pi), 0, 1, 0)

	if w.OpeningKind == components.OpeningNone || w.OpeningWidth <= 0 {
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: w.Length * 0.5}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: w.Length}, wallCol)
		rl.PopMatrix()
		return
	}

	openingCenter := w.OpeningCenterT * w.Length
	openStart := openingCenter - w.OpeningWidth*0.5
	openEnd := openingCenter + w.OpeningWidth*0.5
	if openStart < 0 {
		openStart = 0
	}
	if openEnd > w.Length {
		openEnd = w.Length
	}

	if openStart > 0 {
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: openStart * 0.5}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: openStart}, wallCol)
	}
	if openEnd < w.Length {
		rightLen := w.Length - openEnd
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: openEnd + rightLen*0.5}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: rightLen}, wallCol)
	}

	if w.OpeningBottom > 0 {
		c := rl.Vector3{X: 0, Y: w.OpeningBottom * 0.5, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: w.OpeningBottom, Z: openEnd - openStart}, lintelCol)
	}
	lintelBottom := w.OpeningBottom + w.OpeningHeight
	if lintelBottom < w.Height {
		lintelH := w.Height - lintelBottom
		c := rl.Vector3{X: 0, Y: lintelBottom + lintelH*0.5, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: lintelH, Z: openEnd - openStart}, lintelCol)
	}

	// Door / window panels stay solid even in CameraFacing alpha mode.
	panelY := w.OpeningBottom + w.OpeningHeight*0.5
	switch w.OpeningKind {
	case components.OpeningDoor:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness * 0.6, Y: w.OpeningHeight, Z: openEnd - openStart}, doorCol)
	case components.OpeningWindow:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness * 0.3, Y: w.OpeningHeight, Z: openEnd - openStart}, winCol)
	}

	rl.PopMatrix()
}

// DrawStairs draws a tilted slab approximating a stairwell. `pos` is the
// bottom-of-stairs anchor; the slab tilts up along Yaw (+Z by default).
func DrawStairs(pos rl.Vector3, s components.Stairs) {
	col := rl.Color{R: 130, G: 110, B: 95, A: 255}
	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(s.Yaw*(180.0/math.Pi), 0, 1, 0)
	pitchDeg := float32(math.Atan2(float64(s.Rise), float64(s.Length))) * 180.0 / math.Pi
	rl.Rotatef(-pitchDeg, 1, 0, 0)
	hyp := float32(math.Sqrt(float64(s.Length*s.Length + s.Rise*s.Rise)))
	c := rl.Vector3{X: 0, Y: 0.1, Z: hyp * 0.5}
	rl.DrawCubeV(c, rl.Vector3{X: s.Width, Y: 0.2, Z: hyp}, col)
	rl.PopMatrix()
}
