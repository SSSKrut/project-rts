package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// drawUnitCube draws a placeholder soldier as an olive cube sitting on the
// terrain at WorldPos. Height collapses with Stance; a small role-tinted cap
// sits on top (taller for Leaders).
func drawUnitCube(pos rl.Vector3, st components.Stance, role components.UnitRoleKind) {
	drawUnitCubeAlpha(pos, st, role, 1.0)
}

// drawUnitCubeAlpha is the alpha-aware variant used by Phase 18.5.G tail
// rendering when an enemy contact is fading out of 3D after LOS loss.
func drawUnitCubeAlpha(pos rl.Vector3, st components.Stance, role components.UnitRoleKind, alpha float32) {
	scale := func(c rl.Color) rl.Color {
		c.A = uint8(float32(c.A) * alpha)
		return c
	}
	height := unitStanceHeight(st.Code)
	c := rl.Vector3{X: pos.X, Y: pos.Y + height*0.5, Z: pos.Z}
	rl.DrawCubeV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, scale(rl.Color{R: 80, G: 95, B: 55, A: 255}))
	rl.DrawCubeWiresV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, scale(rl.Color{R: 40, G: 50, B: 30, A: 255}))

	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	capPos := rl.Vector3{X: pos.X, Y: pos.Y + height + capHeight*0.5, Z: pos.Z}
	capColor := scale(components.RoleColor(role))
	rl.DrawCubeV(capPos, rl.Vector3{X: 0.55, Y: capHeight, Z: 0.55}, capColor)
	rl.DrawCubeWiresV(capPos, rl.Vector3{X: 0.55, Y: capHeight, Z: 0.55}, scale(rl.Color{R: 20, G: 20, B: 20, A: 220}))
}

// drawUnitRoleLabel projects the unit's head-above-cap point into the 3D
// panel's content rect and draws the role ShortLabel as a small floating
// pill. Called from the 2D pass (after the 3D RT has been composited) so the
// label sits on top of the scene without depth fighting.
func drawUnitRoleLabel(renderPos rl.Vector3, st components.Stance, role components.UnitRoleKind,
	font rl.Font, panel3DContent rl.Rectangle) {
	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
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
	if sp.X < 0 || sp.Y < 0 || sp.X > panel3DContent.Width || sp.Y > panel3DContent.Height {
		return
	}
	label := role.ShortLabel()
	const fontSize float32 = 14
	size := rl.MeasureTextEx(font, label, fontSize, 1)
	screenX := panel3DContent.X + sp.X - size.X*0.5
	screenY := panel3DContent.Y + sp.Y - size.Y*0.5

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

	// White / black text by background luminance.
	lum := 0.299*float32(bg.R) + 0.587*float32(bg.G) + 0.114*float32(bg.B)
	textColor := rl.Color{R: 0, G: 0, B: 0, A: 255}
	if lum < 140 {
		textColor = rl.Color{R: 255, G: 255, B: 255, A: 255}
	}
	rl.DrawTextEx(font, label, rl.Vector2{X: screenX, Y: screenY}, fontSize, 1, textColor)
}

// drawUnitStaminaBar draws a horizontal Stamina bar above the cap, only when
// ratio < 0.8. Green / yellow / red by ratio zone (>=0.5 / >=0.2 / below).
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

// drawUnitHPBar draws an HP pill above the Stamina bar. Hidden at full HP.
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
		return
	}

	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
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

// drawUnitExposureBar: thin strip above the HP pill while enemies build up
// detection on this unit; full red = spotted.
func drawUnitExposureBar(renderPos rl.Vector3, st components.Stance, role components.UnitRoleKind,
	exposure float32, panel3DContent rl.Rectangle) {
	if exposure < 0.05 {
		return
	}
	ratio := exposure
	if ratio > 1 {
		ratio = 1
	}
	height := unitStanceHeight(st.Code)
	capHeight := float32(0.15)
	if role == components.RoleLeader {
		capHeight = 0.30
	}
	topPos := rl.Vector3{
		X: renderPos.X,
		Y: renderPos.Y + height + capHeight + 0.65,
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
	const barW, barH float32 = 40, 2
	screenX := panel3DContent.X + sp.X - barW*0.5
	screenY := panel3DContent.Y + sp.Y - barH*0.5
	rl.DrawRectangleRec(rl.Rectangle{X: screenX, Y: screenY, Width: barW, Height: barH},
		rl.Color{R: 28, G: 30, B: 36, A: 200})
	fill := rl.Color{R: 240, G: 160, B: 40, A: 255}
	if ratio >= 1 {
		fill = rl.Color{R: 230, G: 60, B: 60, A: 255}
	}
	rl.DrawRectangleRec(rl.Rectangle{
		X: screenX, Y: screenY, Width: barW * ratio, Height: barH,
	}, fill)
}

// drawGhostUnit draws a translucent body cube (no role cap) to preview where
// a unit would stand after a Move order completes. Stance drives cube height.
func drawGhostUnit(pos rl.Vector3, st components.Stance, alpha uint8) {
	height := unitStanceHeight(st.Code)
	c := rl.Vector3{X: pos.X, Y: pos.Y + height*0.5, Z: pos.Z}
	body := rl.Color{R: 200, G: 200, B: 220, A: alpha}
	wires := rl.Color{R: 240, G: 240, B: 250, A: alpha + 40}
	if wires.A < alpha {
		wires.A = 255
	}
	rl.DrawCubeV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, body)
	rl.DrawCubeWiresV(c, rl.Vector3{X: 0.6, Y: height, Z: 0.6}, wires)
}

// drawGhostArc draws a wedge-shaped sector indicator at ground level (two
// outline rays + a triangle fan fill). `center` is render-space at ground
// level; `length` is wedge radius in metres.
//
// Yaw convention matches Motion.Yaw: rotation around +Y, 0 = +Z forward,
// increases clockwise (atan2(dx, dz)).
func drawGhostArc(center rl.Vector3, facingYaw, halfAngleRad, length float32, col rl.Color) {
	if length <= 0 || halfAngleRad <= 0 {
		return
	}
	const steps = 12
	startAng := float64(facingYaw - halfAngleRad)
	endAng := float64(facingYaw + halfAngleRad)
	lift := float32(0.05)

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
		rl.DrawTriangle3D(c, p1, p2, fill)
		prevX, prevZ = nx, nz
	}

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

func unitStanceHeight(code components.StanceCode) float32 {
	return components.SpecForStance(code).BodyHeight
}

// Faction-split palettes. Player = blue/green spectrum, enemy = red/orange.
// SplitMix hash picks per-squad shade within each faction.
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

var squadPaletteNeutral = [...]rl.Color{
	{R: 200, G: 200, B: 80, A: 230},  // mustard
	{R: 180, G: 180, B: 100, A: 230}, // olive
}

var squadPaletteWildlife = [...]rl.Color{
	{R: 140, G: 95, B: 60, A: 230},  // brown
	{R: 110, G: 80, B: 50, A: 230},  // dark brown
	{R: 160, G: 120, B: 80, A: 230}, // tan
}

// squadColorFor picks a palette slot by faction then hashes the entity ID
// for per-squad shade. Missing Faction falls back to Player palette.
func squadColorFor(ent ecs.Entity, faction uint8) rl.Color {
	var palette []rl.Color
	switch faction {
	case components.FactionEnemyRed:
		palette = squadPaletteEnemy[:]
	case components.FactionNeutral:
		palette = squadPaletteNeutral[:]
	case components.FactionWildlife:
		palette = squadPaletteWildlife[:]
	default:
		palette = squadPalettePlayer[:]
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
// from the center out to every live member.
func drawSquadConnections(center rl.Vector3, members []rl.Vector3, col rl.Color) {
	rl.DrawCircle3D(center, 0.6, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, col)
	for _, m := range members {
		rl.DrawLine3D(center, m, col)
	}
}
