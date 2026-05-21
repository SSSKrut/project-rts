package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

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
// offset the screen-space coordinates and to cull labels for off-screen units.
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
// < 0.8. Width 40 px, height 3 px; colour green / yellow / red by ratio zone
// (>=0.5 / >=0.2 / below). 2D screen-space pass, same render order as the
// role label (drawn after the 3D RT composites).
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
// drawUnitStaminaBar geometry but sits +0.25 m higher so they stack readably.
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
// blue/green spectrum; enemy squads from a red/orange spectrum. Same
// SplitMix hash inside each faction so two squads of the same side stay
// visually distinct. Slots-per-faction = 4 to keep close-hue variety without
// the player blue / enemy red feel becoming muddy.
var squadPalettePlayer = [...]rl.Color{
	{R: 80, G: 200, B: 90, A: 230},   // grass green
	{R: 80, G: 130, B: 230, A: 230},  // cobalt blue
	{R: 60, G: 200, B: 200, A: 230},  // teal
	{R: 130, G: 220, B: 130, A: 230}, // light leaf
}

var squadPaletteEnemy = [...]rl.Color{
	{R: 220, G: 80, B: 80, A: 230},  // crimson
	{R: 230, G: 150, B: 80, A: 230}, // burnt orange
	{R: 200, G: 60, B: 100, A: 230}, // magenta-red
	{R: 220, G: 110, B: 50, A: 230}, // brick
}

// squadColorFor maps an entity to a palette slot, split first by Faction so
// the player vs enemy distinction is immediately visible. Inside each
// faction a SplitMix hash on the entity ID picks a per-squad shade.
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
