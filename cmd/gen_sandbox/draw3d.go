package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/render"
)

var (
	colChunkLine = rl.Color{R: 90, G: 110, B: 150, A: 200}
	colFpClean   = rl.Color{R: 70, G: 150, B: 90, A: 160}
	colFpWarn    = rl.Color{R: 210, G: 180, B: 60, A: 200}
	colFpError   = rl.Color{R: 220, G: 80, B: 70, A: 230}
	colFpSplit   = rl.Color{R: 240, G: 120, B: 240, A: 240}
)

func (sb *sandbox) drawScene() {
	vp := sb.viewport()
	sb.ensureRT(vp)
	cam := sb.camera()

	rl.BeginTextureMode(sb.rt)
	rl.ClearBackground(rl.Color{R: 28, G: 32, B: 38, A: 255})
	rl.BeginMode3D(cam)

	if sb.showGrid {
		rl.DrawGrid(80, 2)
		sb.drawChunkLines()
	}

	mode := components.WallRenderMode(sb.wallMode)
	for i := range sb.cells {
		c := &sb.cells[i]
		for _, p := range c.plans {
			sb.drawPlan(p, mode, cam.Position)
		}
		if sb.showFootprint {
			sb.drawFootprint(c)
		}
	}

	rl.EndMode3D()
	rl.EndTextureMode()

	// Negative source height flips the OpenGL bottom-up RT, same as ui.Scene3DRT.
	src := rl.Rectangle{Width: float32(sb.rtW), Height: -float32(sb.rtH)}
	rl.DrawTexturePro(sb.rt.Texture, src, vp, rl.Vector2{}, 0, rl.White)
	sb.drawCellLabels(cam, vp)
}

// drawChunkLines marks the 64 m chunk lattice. A building whose footprint
// crosses one of these violates the single-chunk constraint, so the lattice is
// the most load-bearing gridline in the viewer.
func (sb *sandbox) drawChunkLines() {
	const reach = 4
	const y = 0.03
	ext := float32(reach) * components.ChunkSize
	for i := -reach; i <= reach; i++ {
		v := float32(i) * components.ChunkSize
		rl.DrawLine3D(rl.Vector3{X: v, Y: y, Z: -ext}, rl.Vector3{X: v, Y: y, Z: ext}, colChunkLine)
		rl.DrawLine3D(rl.Vector3{X: -ext, Y: y, Z: v}, rl.Vector3{X: ext, Y: y, Z: v}, colChunkLine)
	}
}

func (sb *sandbox) drawPlan(p *components.BuildingPlan, mode components.WallRenderMode, camPos rl.Vector3) {
	for i := range p.Floors {
		fs := &p.Floors[i]
		if !sb.levelVisible(int(fs.LevelRef)) {
			continue
		}
		render.DrawFloor(planWorld(p, fs.Local), fs.Floor, false)
	}
	for i := range p.Walls {
		ws := &p.Walls[i]
		if !sb.wallVisible(ws.LevelRefs) {
			continue
		}
		render.DrawWall(planWorld(p, ws.Local), ws.Segment, mode, ws.OutwardNormal, camPos, false)
	}
	for i := range p.Stairs {
		ss := &p.Stairs[i]
		if !sb.stairVisible(ss) {
			continue
		}
		render.DrawStairs(planWorld(p, ss.Local), ss.Stairs)
	}
	if sb.showRoof {
		for i := range p.Roofs {
			rs := &p.Roofs[i]
			// A level filter means "show me that storey" — the roof would sit
			// on top of it and hide exactly what was asked for.
			if sb.levelFilter >= 0 {
				continue
			}
			render.DrawRoof(planWorld(p, rs.Local), rs.Roof, false)
		}
	}
}

func (sb *sandbox) levelVisible(ref int) bool {
	return sb.levelFilter < 0 || ref == int(components.NoLevelRef) || ref == sb.levelFilter
}

func (sb *sandbox) wallVisible(refs []uint8) bool {
	if sb.levelFilter < 0 || len(refs) == 0 {
		return true
	}
	for _, r := range refs {
		if int(r) == sb.levelFilter {
			return true
		}
	}
	return false
}

// stairVisible keeps a stair while EITHER of the levels it joins is shown —
// a stair belongs to both ends, unlike a floor.
func (sb *sandbox) stairVisible(s *components.StairSpec) bool {
	if sb.levelFilter < 0 || len(s.Anchors) == 0 {
		return true
	}
	for _, a := range s.Anchors {
		if int(a.LevelRef) == sb.levelFilter {
			return true
		}
	}
	return false
}

// drawFootprint outlines the cell and colour-codes its validation verdict, so
// a bad sample is findable in a 12-cell matrix without reading the list.
func (sb *sandbox) drawFootprint(c *cell) {
	col := colFpClean
	switch {
	case c.oversize:
		col = colFpSplit
	case c.errors > 0:
		col = colFpError
	case c.warns > 0:
		col = colFpWarn
	}
	h := maxF(c.topY, 1)
	centre := rl.Vector3{
		X: 0.5 * (c.bounds.MinX + c.bounds.MaxX),
		Y: h * 0.5,
		Z: 0.5 * (c.bounds.MinZ + c.bounds.MaxZ),
	}
	size := rl.Vector3{
		X: c.bounds.MaxX - c.bounds.MinX,
		Y: h,
		Z: c.bounds.MaxZ - c.bounds.MinZ,
	}
	rl.DrawCubeWiresV(centre, size, col)
}

// worldToScreen projects a world point into the viewport rect, for 3D labels.
func (sb *sandbox) worldToScreen(p rl.Vector3, cam rl.Camera3D, vp rl.Rectangle) (rl.Vector2, bool) {
	// Behind-camera points project to a mirrored on-screen position; reject
	// them by the sign of the view-direction dot.
	fx := cam.Target.X - cam.Position.X
	fy := cam.Target.Y - cam.Position.Y
	fz := cam.Target.Z - cam.Position.Z
	dx := p.X - cam.Position.X
	dy := p.Y - cam.Position.Y
	dz := p.Z - cam.Position.Z
	if fx*dx+fy*dy+fz*dz <= 0 {
		return rl.Vector2{}, false
	}
	s := rl.GetWorldToScreenEx(p, cam, int32(vp.Width), int32(vp.Height))
	if math.IsNaN(float64(s.X)) || math.IsNaN(float64(s.Y)) {
		return rl.Vector2{}, false
	}
	return rl.Vector2{X: s.X + vp.X, Y: s.Y + vp.Y}, true
}
