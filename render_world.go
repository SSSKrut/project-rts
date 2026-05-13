package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

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
//	Cost = 0           → black (impassable)
//	Cost = navCostRoad → light blue (OnRoad — M6.4)
//	Cost = navCostOpen → green (open field)
//	Cost = navCostRough → yellow-orange (rough)
//	Cost = navCostTrench → orange (trench — M6.4)
//	other              → magenta (unknown)
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

// drawCoverMapOverlay — same shape as drawNavGridOverlay but with a single
// translucent blue plate per cell whose alpha tracks BaseCover. 0 cover ⇒
// fully transparent (cell skipped); 8 covered directions ⇒ 90% blue.
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

// drawFloorNavOverlay renders one FloorNavGrid as a layer of coloured plates
// 5 cm above the floor surface. Black = Cost=0 (wall / blocked); green = open.
func drawFloorNavOverlay(floorPos components.WorldPos, grid *components.FloorNavGrid) {
	chunkBase := (components.WorldPos{Chunk: floorPos.Chunk}).ToRenderSpace(systems.CurrentOriginChunk)
	const plate float32 = 0.85
	const lift float32 = 0.05
	for cj := uint8(0); cj < grid.SizeZ; cj++ {
		for ci := uint8(0); ci < grid.SizeX; ci++ {
			cell := grid.Cells[int(cj)*components.MaxFloorSide+int(ci)]
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

// drawRoadGraphDebug overlays the road graph in 3D — one coloured line per
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
// Floor.Y is the slab top — drop a thin slab below it.
func drawBuildingFloor(pos rl.Vector3, f components.Floor) {
	const slabThickness float32 = 0.15
	// Centre cube vertically below the floor surface.
	c := rl.Vector3{X: pos.X, Y: pos.Y - slabThickness*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: f.SizeX, Y: slabThickness, Z: f.SizeZ},
		rl.Color{R: 110, G: 110, B: 120, A: 255})
}

// drawBuildingWall renders a wall segment with optional opening (door / window).
// WallSegment local axes after Rotatef(Yaw): +Z = along wall, +X = thickness,
// +Y = up. Walls without an opening are one cube; walls with one are split
// into left + right solids, lintel above and (for windows) sill below, with
// a coloured panel filling the opening.
func drawBuildingWall(pos rl.Vector3, w components.WallSegment) {
	wallCol := rl.Color{R: 175, G: 170, B: 165, A: 255}
	doorCol := rl.Color{R: 90, G: 60, B: 35, A: 255}
	winCol := rl.Color{R: 160, G: 200, B: 230, A: 200}
	lintelCol := rl.Color{R: 150, G: 145, B: 140, A: 255}

	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y, pos.Z)
	rl.Rotatef(w.Yaw*(180.0/math.Pi), 0, 1, 0)

	if w.OpeningKind == components.OpeningNone || w.OpeningWidth <= 0 {
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: w.Length * 0.5}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: w.Length}, wallCol)
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
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: openStart}, wallCol)
	}
	if openEnd < w.Length {
		rightLen := w.Length - openEnd
		c := rl.Vector3{X: 0, Y: w.Height * 0.5, Z: openEnd + rightLen*0.5}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.Height, Z: rightLen}, wallCol)
	}

	// Sill (only when bottom > 0, i.e. windows).
	if w.OpeningBottom > 0 {
		c := rl.Vector3{X: 0, Y: w.OpeningBottom * 0.5, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: w.OpeningBottom, Z: openEnd - openStart}, lintelCol)
	}
	// Lintel above opening.
	lintelBottom := w.OpeningBottom + w.OpeningHeight
	if lintelBottom < w.Height {
		lintelH := w.Height - lintelBottom
		c := rl.Vector3{X: 0, Y: lintelBottom + lintelH*0.5, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness, Y: lintelH, Z: openEnd - openStart}, lintelCol)
	}

	// Panel inside the opening.
	panelY := w.OpeningBottom + w.OpeningHeight*0.5
	switch w.OpeningKind {
	case components.OpeningDoor:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness * 0.6, Y: w.OpeningHeight, Z: openEnd - openStart}, doorCol)
	case components.OpeningWindow:
		c := rl.Vector3{X: 0, Y: panelY, Z: openingCenter}
		rl.DrawCubeV(c, rl.Vector3{X: w.Thickness * 0.3, Y: w.OpeningHeight, Z: openEnd - openStart}, winCol)
	}

	rl.PopMatrix()
}

// drawUnitCube draws a placeholder soldier as an olive cube sitting on the
// terrain at WorldPos. Height collapses with Stance — standing = 1.8 m,
// crouching = 1.1 m, prone = 0.4 m. Phase 16 polish replaces this with proper
// animated meshes.
func drawUnitCube(pos rl.Vector3, st components.Stance) {
	height := unitStanceHeight(st.Code)
	c := rl.Vector3{X: pos.X, Y: pos.Y + height*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6},
		rl.Color{R: 80, G: 95, B: 55, A: 255})
	rl.DrawCubeWiresV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6},
		rl.Color{R: 40, G: 50, B: 30, A: 255})
}

func unitStanceHeight(code components.StanceCode) float32 {
	switch code {
	case components.StanceCrouch:
		return 1.1
	case components.StanceProne:
		return 0.4
	default:
		return 1.8
	}
}

// squadPalette is a small fixed palette of distinguishable colours. Each
// Squad picks a slot deterministically from its entity ID, so the same squad
// keeps the same colour across frames. Eight entries is enough until the
// scene grows beyond ~8 squads; collisions are visually obvious in worst case
// (Phase 14 may move to per-squad-stored colour with player override).
var squadPalette = [...]rl.Color{
	{R: 220, G: 80, B: 80, A: 230},
	{R: 80, G: 200, B: 90, A: 230},
	{R: 80, G: 130, B: 230, A: 230},
	{R: 230, G: 190, B: 60, A: 230},
	{R: 180, G: 100, B: 220, A: 230},
	{R: 230, G: 130, B: 200, A: 230},
	{R: 60, G: 200, B: 200, A: 230},
	{R: 230, G: 150, B: 80, A: 230},
}

// squadColor maps an entity ID to a palette slot via a SplitMix-style mix.
func squadColor(id uint32) rl.Color {
	x := id ^ 0x9e3779b9
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return squadPalette[int(x%uint32(len(squadPalette)))]
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
