package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// drawNavPath renders the path from the anchor through every remaining
// waypoint. Empty/nil paths render nothing. Lifted slightly so the line is
// visible against the terrain.
func drawNavPath(path []components.WorldPos, anchorPos components.WorldPos) {
	if len(path) == 0 {
		return
	}
	prev := anchorPos.ToRenderSpace(systems.CurrentOriginChunk)
	prev.Y += 0.4
	for i := range path {
		p := path[i].ToRenderSpace(systems.CurrentOriginChunk)
		p.Y += 0.4
		rl.DrawLine3D(prev, p, rl.Magenta)
		prev = p
	}
	target := path[0].ToRenderSpace(systems.CurrentOriginChunk)
	target.Y += 0.4
	rl.DrawCircle3D(target, 0.6, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, rl.Magenta)
}

// drawNavGridOverlay draws a flat coloured plate per NavCell at the cell's
// terrain height + 5 cm. Cell size is 1 m; plates are sized to 0.9 m so
// neighbouring cells visibly separate. Colour LUT:
//
//	Cost = 0           -> black (impassable)
//	Cost = navCostRoad -> light blue (OnRoad - M6.4)
//	Cost = navCostOpen -> green (open field)
//	Cost = navCostRough -> yellow-orange (rough)
//	Cost = navCostTrench -> orange (trench - M6.4)
//	other              -> magenta (unknown)
func drawNavGridOverlay(chunkPos components.WorldPos, cc components.ChunkCoord,
	grid *components.NavGrid, hm *components.Heightmap) {
	chunkRender := chunkPos.ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.9
	const lift float32 = 0.05
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			cell := grid.Cells[cj*components.NavGridSide+ci]
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			centerY := (h00 + h10 + h01 + h11) * 0.25

			col := navCellColor(cell)
			c := rl.Vector3{
				X: chunkRender.X + float32(ci) + 0.5,
				Y: centerY + lift,
				Z: chunkRender.Z + float32(cj) + 0.5,
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate}, col)
		}
	}
	_ = cc
}

// drawCoverMapOverlay - same shape as drawNavGridOverlay but with a single
// translucent blue plate per cell whose alpha tracks BaseCover. 0 cover =>
// fully transparent (cell skipped); 8 covered directions => 90% blue.
func drawCoverMapOverlay(chunkPos components.WorldPos, cc components.ChunkCoord,
	cov *components.CoverMap, hm *components.Heightmap) {
	chunkRender := chunkPos.ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.9
	const lift float32 = 0.06
	for cj := 0; cj < components.NavGridSide; cj++ {
		row0 := cj * components.ChunkResolution
		row1 := row0 + components.ChunkResolution
		for ci := 0; ci < components.NavGridSide; ci++ {
			cell := cov.Cells[cj*components.NavGridSide+ci]
			if cell.BaseCover == 0 {
				continue
			}
			h00 := hm.Heights[row0+ci]
			h10 := hm.Heights[row0+ci+1]
			h01 := hm.Heights[row1+ci]
			h11 := hm.Heights[row1+ci+1]
			centerY := (h00 + h10 + h01 + h11) * 0.25

			alpha := uint8(int(cell.BaseCover))
			c := rl.Vector3{
				X: chunkRender.X + float32(ci) + 0.5,
				Y: centerY + lift,
				Z: chunkRender.Z + float32(cj) + 0.5,
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate},
				rl.Color{R: 60, G: 110, B: 230, A: alpha})
		}
	}
	_ = cc
}

// drawFloorNavOverlay renders one LevelNavGrid as a layer of coloured plates
// 5 cm above the floor surface. Black = Cost=0 (wall / blocked); green = open.
func drawFloorNavOverlay(floorPos components.WorldPos, grid *components.LevelNavGrid) {
	chunkBase := (components.WorldPos{Chunk: floorPos.Chunk}).ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.85
	const lift float32 = 0.05
	for cj := uint8(0); cj < grid.SizeZ; cj++ {
		for ci := uint8(0); ci < grid.SizeX; ci++ {
			cell := grid.Cells[int(cj)*components.MaxLevelSide+int(ci)]
			c := rl.Vector3{
				X: chunkBase.X + grid.Origin.X + float32(ci) + 0.5,
				Y: grid.Origin.Y + lift,
				Z: chunkBase.Z + grid.Origin.Z + float32(cj) + 0.5,
			}
			col := rl.Color{R: 80, G: 200, B: 80, A: 150}
			if cell.Cost == 0 {
				col = rl.Color{R: 20, G: 20, B: 20, A: 220}
			}
			rl.DrawCubeV(c, rl.Vector3{X: plate, Y: 0.02, Z: plate}, col)
		}
	}
}

func navCellColor(cell components.NavCell) rl.Color {
	if cell.Cost == 0 {
		return rl.Color{R: 0, G: 0, B: 0, A: 200}
	}
	if cell.Flags&components.NavInTrench != 0 {
		return rl.Color{R: 230, G: 130, B: 30, A: 170}
	}
	if cell.Flags&components.NavOnRoad != 0 {
		return rl.Color{R: 120, G: 180, B: 230, A: 170}
	}
	switch cell.Cost {
	case 4:
		return rl.Color{R: 80, G: 200, B: 80, A: 150}
	case 8:
		return rl.Color{R: 230, G: 200, B: 60, A: 170}
	default:
		return rl.Color{R: 220, G: 60, B: 220, A: 200}
	}
}

// drawRoadGraphDebug overlays the road graph in 3D - one coloured line per
// edge, plus a small marker cube at each node. Colour by RoadKind: white
// Highway, blue Local, brown DirtTrack, yellow Bridge.
func drawRoadGraphDebug(g *components.RoadGraph) {
	for i := range g.Edges {
		e := &g.Edges[i]
		from := g.Nodes[e.From].Pos
		to := g.Nodes[e.To].Pos
		fr := from.ToRenderSpace(systems.CurrentOriginChunk)
		tr := to.ToRenderSpace(systems.CurrentOriginChunk)
		// Lift slightly so the line isn't buried in the road surface.
		fr.Y += 0.5
		tr.Y += 0.5
		var col rl.Color
		switch e.Kind {
		case components.RoadHighway:
			col = rl.White
		case components.RoadLocal:
			col = rl.Blue
		case components.RoadDirtTrack:
			col = rl.Brown
		case components.RoadBridge:
			col = rl.Yellow
		default:
			col = rl.Magenta
		}
		rl.DrawLine3D(fr, tr, col)
	}
	for i := range g.Nodes {
		p := g.Nodes[i].Pos.ToRenderSpace(systems.CurrentOriginChunk)
		p.Y += 0.5
		rl.DrawCube(p, 0.6, 0.6, 0.6, rl.Black)
	}
}

// drawProp renders one placeholder prop primitive. Yaw radians around +Y;
// scale uniform. Position is the prop's *foot* (ground contact), so primitives
// lift themselves to sit on top.
func drawProp(meta components.PropMeta, pos rl.Vector3, yaw, scale float32) {
	switch meta.Primitive {
	case components.PrimitiveCube:
		sx := meta.Size.X * scale
		sy := meta.Size.Y * scale
		sz := meta.Size.Z * scale
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y+sy*0.5, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawCubeV(rl.Vector3{}, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
			rl.PopMatrix()
		} else {
			c := rl.Vector3{X: pos.X, Y: pos.Y + sy*0.5, Z: pos.Z}
			rl.DrawCubeV(c, rl.Vector3{X: sx, Y: sy, Z: sz}, meta.Color)
		}

	case components.PrimitiveSphere:
		r := meta.Size.X * scale
		c := rl.Vector3{X: pos.X, Y: pos.Y + r*0.5, Z: pos.Z}
		rl.DrawSphere(c, r, meta.Color)

	case components.PrimitiveCylinder:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, r, 8, meta.Color)

	case components.PrimitiveCone:
		r := meta.Size.X * scale
		h := meta.Size.Y * scale
		bottom := pos
		top := rl.Vector3{X: pos.X, Y: pos.Y + h, Z: pos.Z}
		rl.DrawCylinderEx(bottom, top, r, 0, 8, meta.Color)

	case components.PrimitivePlane:
		size := rl.Vector2{X: meta.Size.X * scale, Y: meta.Size.Z * scale}
		if yaw != 0 {
			rl.PushMatrix()
			rl.Translatef(pos.X, pos.Y, pos.Z)
			rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
			rl.DrawPlane(rl.Vector3{}, size, meta.Color)
			rl.PopMatrix()
		} else {
			rl.DrawPlane(pos, size, meta.Color)
		}

	case components.PrimitiveTree:
		// Trunk + canopy composite. 6-sided is enough at silhouette resolution.
		trunkR := meta.Size.X * scale
		trunkH := meta.Size.Y * scale
		canopyR := meta.Size.Z * scale
		canopyH := trunkH * 1.5
		trunkBase := pos
		trunkTop := rl.Vector3{X: pos.X, Y: pos.Y + trunkH, Z: pos.Z}
		canopyTip := rl.Vector3{X: pos.X, Y: pos.Y + trunkH + canopyH, Z: pos.Z}
		rl.DrawCylinderEx(trunkBase, trunkTop, trunkR, trunkR, 6, meta.TrunkColor)
		rl.DrawCylinderEx(trunkTop, canopyTip, canopyR, 0, 6, meta.Color)
	}
}

// drawBuildingFloor draws a horizontal grey plate at the floor's WorldPos.
// Floor.Y is the slab top - drop a thin slab below it. Phase 16.C.2: when
// `fogged` is true (level un-discovered or stale), the slab desaturates
// toward dark grey - visual cue that the room hasn't been seen recently.
func drawBuildingFloor(pos rl.Vector3, f components.Floor, fogged bool) {
	const slabThickness float32 = 0.15
	col := rl.Color{R: 110, G: 110, B: 120, A: 255}
	if fogged {
		col = rl.Color{R: 55, G: 55, B: 60, A: 220}
	}
	c := rl.Vector3{X: pos.X, Y: pos.Y - slabThickness*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: f.SizeX, Y: slabThickness, Z: f.SizeZ}, col)
}

// drawBuildingWall renders a wall segment with optional opening (door / window).
// WallSegment local axes after Rotatef(Yaw): +Z = along wall, +X = thickness,
// +Y = up. Walls without an opening are one cube; walls with one are split
// into left + right solids, lintel above and (for windows) sill below, with
// a coloured panel filling the opening.
//
// Phase 16.C.4: WallRenderMode polishes the cutaway look.
//   - WallRenderAll          : solid (default).
//   - WallRenderCameraFacing : if the wall's outward normal points toward
//     the camera, opaque wall mass becomes semi-transparent so the player
//     can see in. Door / window panels stay solid.
//   - WallRenderWireframe    : every cube draws as an outline.
//
// `outward` is the wall's outward XZ unit vector (CoverDirection.Dir on the
// wall entity). For the All variant it's ignored.
func drawBuildingWall(pos rl.Vector3, w components.WallSegment, mode components.WallRenderMode, outward rl.Vector3, fogged bool) {
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
		// Wall centre in world coords. After Rotatef(yaw,+Y) local +Z maps
		// to world (sin(yaw), 0, cos(yaw)); the centre sits at Length/2
		// along that direction from `pos` and Height/2 up.
		s := float32(math.Sin(float64(w.Yaw)))
		c := float32(math.Cos(float64(w.Yaw)))
		wcx := pos.X + (w.Length*0.5)*s
		wcz := pos.Z + (w.Length*0.5)*c
		camPos := systems.CurrentCamera.Position
		dx := camPos.X - wcx
		dz := camPos.Z - wcz
		l := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if l > 0.0001 {
			dx /= l
			dz /= l
		}
		// If outward points toward the camera the player sees the wall's
		// exterior face -> ramp solid mass down to semi-transparent so the
		// interior is visible behind it.
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

	// Sill (only when bottom > 0, i.e. windows).
	if w.OpeningBottom > 0 {
		c := rl.Vector3{X: 0, Y: w.OpeningBottom * 0.5, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: w.OpeningBottom, Z: openEnd - openStart}, lintelCol)
	}
	// Lintel above opening.
	lintelBottom := w.OpeningBottom + w.OpeningHeight
	if lintelBottom < w.Height {
		lintelH := w.Height - lintelBottom
		c := rl.Vector3{X: 0, Y: lintelBottom + lintelH*0.5, Z: openingCenter}
		drawCube(c, rl.Vector3{X: w.Thickness, Y: lintelH, Z: openEnd - openStart}, lintelCol)
	}

	// Panel inside the opening. Door / window panels stay solid even in
	// CameraFacing alpha mode so the player can still identify them.
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

// drawUnitCube draws a placeholder soldier as an olive cube sitting on the
// terrain at WorldPos. Height collapses with Stance - standing = 1.8 m,
// crouching = 1.1 m, prone = 0.4 m. Phase 12 adds a role cap on top of the
// body - a small flat sub-cube tinted to the role colour. Leaders get a
// slightly taller cap so they read as senior at a glance. Phase 25 polish
// replaces the cubes with proper animated meshes.
func drawUnitCube(pos rl.Vector3, st components.Stance, role components.UnitRoleKind) {
	height := unitStanceHeight(st.Code)
	c := rl.Vector3{X: pos.X, Y: pos.Y + height*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6},
		rl.Color{R: 80, G: 95, B: 55, A: 255})
	rl.DrawCubeWiresV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6},
		rl.Color{R: 40, G: 50, B: 30, A: 255})

	// Role cap. Riflemen keep the olive body colour (the cap blends into the
	// soldier) so vanilla infantry doesn't visually shout; specialists get a
	// bright cap that reads at squad-glance distance. Leaders draw a taller
	// cap so they stand out even before the floating label catches the eye.
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	capPos := rl.Vector3{X: pos.X, Y: pos.Y + height + capHeight*0.5, Z: pos.Z}
	capColor := components.RoleColor(role)
	rl.DrawCubeV(capPos, rl.Vector3{X: 0.55, Y: capHeight, Z: 0.55}, capColor)
	rl.DrawCubeWiresV(capPos, rl.Vector3{X: 0.55, Y: capHeight, Z: 0.55},
		rl.Color{R: 20, G: 20, B: 20, A: 220})
}

// drawUnitRoleLabel projects the unit's head-above-cap point into the 3D
// panel's content rect and draws the role ShortLabel as a small floating
// pill. Called from the 2D pass (after the 3D RT has been composited) so the
// label sits on top of the scene without depth fighting.
//
// `renderPos` is the unit's render-space WorldPos (output of WorldPos.ToRenderSpace).
// `panel3DContent` is the rectangle the 3D scene composites into - used to
// offset the screen-space coordinates and to cull labels for off-screen
// units.
func drawUnitRoleLabel(renderPos rl.Vector3, st components.Stance, role components.UnitRoleKind,
	font rl.Font, panel3DContent rl.Rectangle) {
	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	// Label hangs 0.6 m above the cap so it doesn't visually collide with
	// the selection wireframe drawn around the body.
	headPos := rl.Vector3{
		X: renderPos.X,
		Y: renderPos.Y + height + capHeight + 0.6,
		Z: renderPos.Z,
	}
	w := int32(panel3DContent.Width)
	h := int32(panel3DContent.Height)
	if w < 1 || h < 1 {
		return
	}
	sp := rl.GetWorldToScreenEx(headPos, systems.CurrentCamera, w, h)
	// Cull labels for points behind the camera / off-panel.
	if sp.X < 0 || sp.Y < 0 || sp.X > panel3DContent.Width || sp.Y > panel3DContent.Height {
		return
	}
	label := role.ShortLabel()
	const fontSize float32 = 12
	size := rl.MeasureTextEx(font, label, fontSize, 1)
	// Position so the label centre lines up with the projected head.
	screenX := panel3DContent.X + sp.X - size.X*0.5
	screenY := panel3DContent.Y + sp.Y - size.Y*0.5

	// Pill background - role colour with low alpha so multiple labels can
	// overlap without becoming an opaque smear. Border keeps the pill
	// legible against terrain.
	bg := components.RoleColor(role)
	bg.A = 200
	const padX float32 = 3
	const padY float32 = 1
	rect := rl.Rectangle{
		X: screenX - padX, Y: screenY - padY,
		Width: size.X + 2*padX, Height: size.Y + 2*padY,
	}
	rl.DrawRectangleRec(rect, bg)
	rl.DrawRectangleLinesEx(rect, 1, rl.Color{R: 20, G: 20, B: 20, A: 220})

	// Text colour: choose white or black per role-colour luminance so the
	// label always reads.
	lum := 0.299*float32(bg.R) + 0.587*float32(bg.G) + 0.114*float32(bg.B)
	textColor := rl.Color{R: 0, G: 0, B: 0, A: 255}
	if lum < 140 {
		textColor = rl.Color{R: 255, G: 255, B: 255, A: 255}
	}
	rl.DrawTextEx(font, label, rl.Vector2{X: screenX, Y: screenY}, fontSize, 1, textColor)
}

// drawUnitStaminaBar projects the unit's cap-top point and draws a thin
// horizontal Stamina bar at PHASE-13.md P12: shown only when Current/MaxLevel
// < 0.8. Width 40 px, height 3 px; colour green / yellow / red by ratio
// zone (>=0.5 / >=0.2 / below). 2D screen-space pass, same render order as
// the role label (drawn after the 3D RT composites).
func drawUnitStaminaBar(renderPos rl.Vector3, st components.Stance, role components.UnitRoleKind,
	current, maxLevel float32, panel3DContent rl.Rectangle) {
	if maxLevel <= 0 {
		return
	}
	ratio := current / maxLevel
	if ratio < 0 {
		ratio = 0
	}
	if ratio >= 0.8 {
		return
	}

	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	// Bar sits just above the cap, below where the role label lives. 0.25 m
	// keeps it readable without colliding with the role pill.
	topPos := rl.Vector3{
		X: renderPos.X,
		Y: renderPos.Y + height + capHeight + 0.25,
		Z: renderPos.Z,
	}
	w := int32(panel3DContent.Width)
	h := int32(panel3DContent.Height)
	if w < 1 || h < 1 {
		return
	}
	sp := rl.GetWorldToScreenEx(topPos, systems.CurrentCamera, w, h)
	if sp.X < 0 || sp.Y < 0 || sp.X > panel3DContent.Width || sp.Y > panel3DContent.Height {
		return
	}
	const barW, barH float32 = 40, 3
	screenX := panel3DContent.X + sp.X - barW*0.5
	screenY := panel3DContent.Y + sp.Y - barH*0.5
	rl.DrawRectangleRec(rl.Rectangle{X: screenX, Y: screenY, Width: barW, Height: barH},
		rl.Color{R: 28, G: 30, B: 36, A: 220})
	fill := rl.Color{R: 80, G: 200, B: 80, A: 255}
	switch {
	case ratio < 0.2:
		fill = rl.Color{R: 220, G: 60, B: 60, A: 255}
	case ratio < 0.5:
		fill = rl.Color{R: 220, G: 200, B: 50, A: 255}
	}
	rl.DrawRectangleRec(rl.Rectangle{
		X: screenX, Y: screenY, Width: barW * ratio, Height: barH,
	}, fill)
	rl.DrawRectangleLinesEx(rl.Rectangle{X: screenX, Y: screenY, Width: barW, Height: barH},
		1, rl.Color{R: 10, G: 12, B: 16, A: 220})
}

// drawUnitHPBar - Phase 14 M14.6: thin red/yellow/green pill above the
// Stamina bar. Hidden when Current >= Max (no damage). Mirrors
// drawUnitStaminaBar geometry but sits +0.25 m higher so they stack
// readably.
func drawUnitHPBar(renderPos rl.Vector3, st components.Stance, role components.UnitRoleKind,
	current, max float32, panel3DContent rl.Rectangle) {
	if max <= 0 {
		return
	}
	ratio := current / max
	if ratio < 0 {
		ratio = 0
	}
	if ratio >= 1 {
		return // full HP - keep the screen quiet.
	}

	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	// HP sits above Stamina (0.25 m above cap) at +0.50 m - clear separation
	// so the two bars don't fuse visually when both are present.
	topPos := rl.Vector3{
		X: renderPos.X,
		Y: renderPos.Y + height + capHeight + 0.50,
		Z: renderPos.Z,
	}
	w := int32(panel3DContent.Width)
	h := int32(panel3DContent.Height)
	if w < 1 || h < 1 {
		return
	}
	sp := rl.GetWorldToScreenEx(topPos, systems.CurrentCamera, w, h)
	if sp.X < 0 || sp.Y < 0 || sp.X > panel3DContent.Width || sp.Y > panel3DContent.Height {
		return
	}
	const barW, barH float32 = 50, 3
	screenX := panel3DContent.X + sp.X - barW*0.5
	screenY := panel3DContent.Y + sp.Y - barH*0.5
	rl.DrawRectangleRec(rl.Rectangle{X: screenX, Y: screenY, Width: barW, Height: barH},
		rl.Color{R: 28, G: 30, B: 36, A: 220})
	fill := rl.Color{R: 80, G: 200, B: 80, A: 255}
	switch {
	case ratio < 0.3:
		fill = rl.Color{R: 220, G: 60, B: 60, A: 255}
	case ratio < 0.6:
		fill = rl.Color{R: 220, G: 200, B: 50, A: 255}
	}
	rl.DrawRectangleRec(rl.Rectangle{
		X: screenX, Y: screenY, Width: barW * ratio, Height: barH,
	}, fill)
	rl.DrawRectangleLinesEx(rl.Rectangle{X: screenX, Y: screenY, Width: barW, Height: barH},
		1, rl.Color{R: 10, G: 12, B: 16, A: 220})
}

// ParticleRenderCtx bundles ECS handles needed to walk every live particle.
// main.go builds one and passes to drawParticles each frame. Phase 14.5 M14.5.4.
type ParticleRenderCtx struct {
	Filter    *ecs.Filter3[components.Particle, components.WorldPos, components.ParticleVisual]
	EndMap    *ecs.Map[components.ParticleEnd]
}

// drawParticles walks every live Particle entity and dispatches per-kind
// draw calls. Replaces drawTracers + drawImpacts; the new kinds (smoke,
// dust, debris, muzzle flash) share the same pipeline.
//
// Phase 14.5 M14.5.4: raylib's DrawLine3D / DrawSphere / DrawCube are not
// instanced - each particle is its own draw call. Acceptable at the cap of
// 2000 live entries; Phase 16 may revisit if firefight density grows.
func drawParticles(ctx ParticleRenderCtx, now float32) {
	if ctx.Filter == nil {
		return
	}
	originChunk := systems.CurrentOriginChunk
	originBaseX := float32(originChunk.X) * components.ChunkSize
	originBaseZ := float32(originChunk.Z) * components.ChunkSize
	q := ctx.Filter.Query()
	for q.Next() {
		_, pos, vis := q.Get()
		age := now - vis.SpawnTime
		if age < 0 || age > vis.TTL {
			continue
		}
		fade := 1 - age/vis.TTL
		col := rl.Color{R: vis.Color.R, G: vis.Color.G, B: vis.Color.B,
			A: uint8(float32(vis.Color.A) * fade)}
		// Particle WorldPos.Local is the absolute world-space position
		// (writers set Chunk=0 to keep this simple); subtract the render
		// origin to get camera-relative coords. This mirrors the old
		// VisualEvents code path exactly.
		from := rl.Vector3{
			X: pos.Local.X - originBaseX,
			Y: pos.Local.Y,
			Z: pos.Local.Z - originBaseZ,
		}
		switch vis.Kind {
		case components.ParticleTracer:
			if end := ctx.EndMap.Get(q.Entity()); end != nil {
				to := rl.Vector3{
					X: end.To.X - originBaseX,
					Y: end.To.Y,
					Z: end.To.Z - originBaseZ,
				}
				rl.DrawLine3D(from, to, col)
			}
		case components.ParticleImpact, components.ParticleMuzzleFlash,
			components.ParticleSmoke, components.ParticleDust:
			r := vis.Size
			if r <= 0 {
				r = 0.1
			}
			rl.DrawSphere(from, r, col)
		case components.ParticleDebris:
			s := vis.Size
			if s <= 0 {
				s = 0.15
			}
			rl.DrawCube(from, s, s, s, col)
		}
	}
}

// drawGhostUnit draws a translucent body cube (no role cap) at the given
// position to preview where a unit would stand after a Move order completes.
// Phase 13.6 M13.6.1: visual is intentionally subdued - neutral grey-white,
// low alpha, no role tint - so real units stay dominant on screen. Stance
// drives the cube height so ghosts crouch / prone-prone with the squad's
// effective MovementProfile.
func drawGhostUnit(pos rl.Vector3, st components.Stance, alpha uint8) {
	height := unitStanceHeight(st.Code)
	c := rl.Vector3{X: pos.X, Y: pos.Y + height*0.5, Z: pos.Z}
	body := rl.Color{R: 200, G: 200, B: 220, A: alpha}
	// Outline alpha is biased a bit higher than the fill so the cube reads
	// even when many ghosts overlap (Loose formation can stack visually).
	wires := rl.Color{R: 240, G: 240, B: 250, A: alpha + 40}
	if wires.A < alpha {
		wires.A = 255
	}
	rl.DrawCubeV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, body)
	rl.DrawCubeWiresV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, wires)
}

// drawGhostArc draws a wedge-shaped sector indicator at ground level - two
// outline rays from `center` along `facingYaw +/- halfAngleRad`, plus a fan of
// translucent triangles filling the wedge. Used by DefendPosition ghost to
// show the held overwatch sector. Phase 13.6 M13.6.1: visual stub; Phase 14
// EngagementRules.SectorYaw/SectorHalfDot will be the runtime enforcement.
//
// `center` is render-space at ground level; `length` is the wedge radius in
// metres. The colour's RGB drives both fill and outline; outline uses A->255
// for legibility, fill uses A as-given (typically 60).
func drawGhostArc(center rl.Vector3, facingYaw, halfAngleRad, length float32, col rl.Color) {
	if length <= 0 || halfAngleRad <= 0 {
		return
	}
	// Yaw convention matches Motion.Yaw: rotation around +Y in radians,
	// 0 = +Z forward, increases clockwise (atan2(dx, dz)).
	const steps = 12
	startAng := float64(facingYaw - halfAngleRad)
	endAng := float64(facingYaw + halfAngleRad)
	lift := float32(0.05)

	// Triangle fan fill - pivot on the center, fan to `steps` points on the
	// arc. Alpha low so terrain reads through.
	fill := rl.Color{R: col.R, G: col.G, B: col.B, A: col.A}
	c := rl.Vector3{X: center.X, Y: center.Y + lift, Z: center.Z}
	prevX := c.X + length*float32(math.Sin(startAng))
	prevZ := c.Z + length*float32(math.Cos(startAng))
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		ang := startAng + (endAng-startAng)*t
		nx := c.X + length*float32(math.Sin(ang))
		nz := c.Z + length*float32(math.Cos(ang))
		p1 := rl.Vector3{X: prevX, Y: c.Y, Z: prevZ}
		p2 := rl.Vector3{X: nx, Y: c.Y, Z: nz}
		// raylib DrawTriangle3D winds CCW for front faces; the camera looks
		// down so either winding renders, but we keep the obvious one.
		rl.DrawTriangle3D(c, p1, p2, fill)
		prevX, prevZ = nx, nz
	}

	// Outline rays from center to wedge edges. Boost alpha for the lines so
	// the wedge boundary is clearly visible.
	outline := rl.Color{R: col.R, G: col.G, B: col.B, A: 220}
	leftEnd := rl.Vector3{
		X: c.X + length*float32(math.Sin(startAng)),
		Y: c.Y, Z: c.Z + length*float32(math.Cos(startAng)),
	}
	rightEnd := rl.Vector3{
		X: c.X + length*float32(math.Sin(endAng)),
		Y: c.Y, Z: c.Z + length*float32(math.Cos(endAng)),
	}
	rl.DrawLine3D(c, leftEnd, outline)
	rl.DrawLine3D(c, rightEnd, outline)
	// Connect arc tip-to-tip along the curve.
	prev := leftEnd
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		ang := startAng + (endAng-startAng)*t
		next := rl.Vector3{
			X: c.X + length*float32(math.Sin(ang)),
			Y: c.Y, Z: c.Z + length*float32(math.Cos(ang)),
		}
		rl.DrawLine3D(prev, next, outline)
		prev = next
	}
}

// unitStanceHeight reads body height from components.StanceSpecs. Phase 14.5
// M14.5.1 - old hard-coded switch replaced.
func unitStanceHeight(code components.StanceCode) float32 {
	return components.SpecForStance(code).BodyHeight
}

// Phase 14 M14.6 - faction-split palettes. Player squads pull from a
// blue/green spectrum; enemy squads from a red/orange spectrum. Same SplitMix
// hash inside each faction so two squads of the same side stay visually
// distinct. Slots-per-faction = 4 to keep close-hue variety without the
// player blue / enemy red feel becoming muddy.
var squadPalettePlayer = [...]rl.Color{
	{R: 80, G: 200, B: 90, A: 230},   // grass green
	{R: 80, G: 130, B: 230, A: 230},  // cobalt blue
	{R: 60, G: 200, B: 200, A: 230},  // teal
	{R: 130, G: 220, B: 130, A: 230}, // light leaf
}

var squadPaletteEnemy = [...]rl.Color{
	{R: 220, G: 80, B: 80, A: 230},   // crimson
	{R: 230, G: 150, B: 80, A: 230},  // burnt orange
	{R: 200, G: 60, B: 100, A: 230},  // magenta-red
	{R: 220, G: 110, B: 50, A: 230},  // brick
}

// squadColor maps an entity to a palette slot, split first by Faction so the
// player vs enemy distinction is immediately visible. Inside each faction a
// SplitMix hash on the entity ID picks a per-squad shade.
//
// Reads the global factionMap closure - squadColor itself is wired in main.go
// once factionMap exists. Missing Faction component -> Player palette
// (backwards-compat for any spawn path that didn't stamp one).
func squadColorFor(ent ecs.Entity, faction uint8) rl.Color {
	palette := squadPalettePlayer[:]
	if faction != components.FactionPlayer {
		palette = squadPaletteEnemy[:]
	}
	id := ent.ID()
	x := id ^ 0x9e3779b9
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return palette[int(x%uint32(len(palette)))]
}

// drawSquadConnections draws a small circle at the squad center plus lines
// from the center out to every live member. Used both by the per-selection
// overlay (always-on when selection is one squad) and the hold-K all-squads
// overlay.
func drawSquadConnections(center rl.Vector3, members []rl.Vector3, col rl.Color) {
	rl.DrawCircle3D(center, 0.6, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, col)
	for _, m := range members {
		rl.DrawLine3D(center, m, col)
	}
}

// drawBuildingStairs draws a tilted slab approximating a stairwell. WorldPos
// is the bottom-of-stairs anchor; slab tilts up along Yaw (+Z by default).
func drawBuildingStairs(pos rl.Vector3, s components.Stairs) {
	col := rl.Color{R: 130, G: 110, B: 95, A: 255}
	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(s.Yaw*(180.0/math.Pi), 0, 1, 0)
	// Pitch by angle = atan(Rise/Length) around local X axis. raylib's Rotatef
	// expects degrees; do everything in degrees from here.
	pitchDeg := float32(math.Atan2(float64(s.Rise), float64(s.Length))) * 180.0 / math.Pi
	rl.Rotatef(-pitchDeg, 1, 0, 0)
	// Slab centred along forward (Z) so its midpoint lies above the diagonal.
	hyp := float32(math.Sqrt(float64(s.Length*s.Length + s.Rise*s.Rise)))
	c := rl.Vector3{X: 0, Y: 0.1, Z: hyp * 0.5}
	rl.DrawCubeV(c, rl.Vector3{X: s.Width, Y: 0.2, Z: hyp}, col)
	rl.PopMatrix()
}
