package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
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

// drawVehicleBox draws a placeholder hull (+ turret block for turreted
// classes) sized from VehicleSpec, yawed with the hull. turretYaw is
// hull-relative; the top block + gun rotate with it.
func drawVehicleBox(pos rl.Vector3, yaw, turretYaw float32, kind components.VehicleKind, col rl.Color) {
	spec := components.SpecForVehicle(kind)
	hull := rl.Vector3{X: spec.BoxWid, Y: spec.BoxHgt * 0.62, Z: spec.BoxLen}
	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y+hull.Y*0.5, pos.Z)
	rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
	// Wire alpha follows body alpha so ghost previews stay translucent.
	wire := rl.Color{R: 30, G: 34, B: 26, A: col.A}
	rl.DrawCubeV(rl.Vector3{}, hull, col)
	rl.DrawCubeWiresV(rl.Vector3{}, hull, wire)
	rl.Rotatef(turretYaw*(180.0/math.Pi), 0, 1, 0)
	top := rl.Vector3{X: spec.BoxWid * 0.6, Y: spec.BoxHgt * 0.38, Z: spec.BoxLen * 0.45}
	topCol := rl.Color{R: uint8(float32(col.R) * 0.75), G: uint8(float32(col.G) * 0.75),
		B: uint8(float32(col.B) * 0.75), A: col.A}
	rl.DrawCubeV(rl.Vector3{Y: hull.Y*0.5 + top.Y*0.5, Z: spec.BoxLen * 0.05}, top, topCol)
	rl.DrawCubeWiresV(rl.Vector3{Y: hull.Y*0.5 + top.Y*0.5, Z: spec.BoxLen * 0.05}, top, wire)
	if spec.TurretSlewDps > 0 {
		gun := rl.Vector3{X: 0.22, Y: 0.22, Z: spec.BoxLen * 0.55}
		rl.DrawCubeV(rl.Vector3{Y: hull.Y*0.5 + top.Y*0.5, Z: spec.BoxLen*0.05 + top.Z*0.5 + gun.Z*0.5},
			gun, topCol)
	}
	rl.PopMatrix()
}

// drawAircraftBox is the placeholder for an unbaked airframe: a fuselage with
// a disc where the rotor is. The disc matters — without it a box in the sky
// reads as a floating crate and there is no telling which way is up.
func drawAircraftBox(pos rl.Vector3, yaw float32, kind components.AircraftKind, col rl.Color) {
	spec := components.SpecForAircraft(kind)
	body := rl.Vector3{X: spec.BoxWid * 0.55, Y: spec.BoxHgt * 0.45, Z: spec.BoxLen * 0.7}
	rl.PushMatrix()
	rl.Translatef(pos.X, pos.Y+body.Y*0.5, pos.Z)
	rl.Rotatef(yaw*(180.0/math.Pi), 0, 1, 0)
	wire := rl.Color{R: 30, G: 34, B: 26, A: col.A}
	rl.DrawCubeV(rl.Vector3{}, body, col)
	rl.DrawCubeWiresV(rl.Vector3{}, body, wire)
	rl.DrawCircle3D(rl.Vector3{Y: body.Y*0.5 + 0.6}, spec.BoxLen*0.42,
		rl.Vector3{X: 1}, 90, wire)
	rl.PopMatrix()
}

// drawAircraftShadow puts a disc on the ground under the airframe. It is the
// only altitude cue a top-down-ish camera has: two aircraft at 30 m and 300 m
// draw at nearly the same screen position, and the gap to the shadow is what
// tells them apart.
func (g *Game) drawAircraftShadow(pos components.WorldPos, renderPos rl.Vector3,
	kind components.AircraftKind) {
	agl := pos.Local.Y - g.groundAt(pos)
	if agl < 0.5 {
		return
	}
	spec := components.SpecForAircraft(kind)
	// Fade and spread with height, the way a real penumbra does.
	t := clampF(agl/400, 0, 1)
	alpha := uint8(110 * (1 - t))
	if alpha < 12 {
		return
	}
	r := spec.BoxLen*0.4 + agl*0.05
	c := rl.Vector3{X: renderPos.X, Y: renderPos.Y - agl + 0.15, Z: renderPos.Z}
	rl.DrawCircle3D(c, r, rl.Vector3{X: 1}, 90, rl.Color{R: 20, G: 24, B: 20, A: alpha})
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

// drawAircraftSelection marks a selected airframe. A ring at the hull alone is
// not enough: at any useful camera distance six metres of radius is a few
// pixels of nothing, and the player has no idea WHERE over the ground the
// thing is. The tether to the ground point is the actual information — height
// and map position in one mark.
func (g *Game) drawAircraftSelection(pos components.WorldPos, renderPos rl.Vector3,
	kind components.AircraftKind, c rl.Color) {
	spec := components.SpecForAircraft(kind)
	rl.DrawCircle3D(renderPos, spec.ColliderR, rl.Vector3{X: 1, Y: 0, Z: 0}, 90, c)
	agl := pos.Local.Y - g.groundAt(pos)
	if agl < 1 {
		return
	}
	foot := rl.Vector3{X: renderPos.X, Y: renderPos.Y - agl + 0.2, Z: renderPos.Z}
	faint := c
	faint.A = 120
	rl.DrawLine3D(renderPos, foot, faint)
	rl.DrawCircle3D(foot, spec.ColliderR*0.7, rl.Vector3{X: 1}, 90, c)
}

const (
	// fieldSymbolHalf keeps the over-head symbol small: it is an identifier,
	// not a panel. Half the map's size reads at a glance without covering
	// the men.
	fieldSymbolHalf float32 = 9
	// A hull is the heaviest single thing on the field and reads as such —
	// the same step up the map takes from a squad marker to a vehicle one.
	fieldVehicleHalf float32 = 10
	fieldSquadLiftM  float32 = 3.2
	fieldHullLiftM   float32 = 1.6
	fieldTintH       float32 = 2
)

// fieldProjector turns a world point into a panel point, or reports it off
// screen.
type fieldProjector struct {
	panel rl.Rectangle
	w, h  int32
}

func (p fieldProjector) at(pos components.WorldPos, lift float32) (rl.Vector2, bool) {
	rp := pos.ToRenderSpace(systems.CurrentOriginChunk)
	rp.Y += lift
	sp := rl.GetWorldToScreenEx(rp, systems.CurrentCamera, p.w, p.h)
	if sp.X < 0 || sp.Y < 0 || sp.X > p.panel.Width || sp.Y > p.panel.Height {
		return rl.Vector2{}, false
	}
	return rl.Vector2{X: p.panel.X + sp.X, Y: p.panel.Y + sp.Y}, true
}

// drawFieldSymbols puts map symbols over the field: one per squad, one per
// hull, one per airframe. The letters were the complaint — eight R/MG tags over
// eight bodies is noise, and a squad is the thing orders are given to. A
// machine keeps its own mark even inside a squad: one machine, one marker, the
// same rule the map plays by, because a hull is never interchangeable with the
// men around it.
//
// A marker rides a BODY and never decides visibility for itself. That is what
// stops a squad the sensors have not found from leaking a symbol over empty
// grass, which is exactly what it used to do.
func (g *Game) drawFieldSymbols() {
	panel := g.Frame.Panel3DContent
	proj := fieldProjector{panel: panel, w: int32(panel.Width), h: int32(panel.Height)}
	if proj.w < 1 || proj.h < 1 {
		return
	}
	symCtx := ui.MapRenderCtx{
		World:            g.App.World,
		RoleMap:          g.Maps.Role,
		VehicleMap:       g.Maps.Vehicle,
		FactionMap:       g.Maps.Faction,
		SquadOverrideMap: g.Maps.SquadOverride,
	}
	now := g.Svc.Squad.Clock()
	g.fieldSquadSymbols(proj, symCtx, now)
	g.fieldVehicleSymbols(proj)
	g.fieldAircraftSymbols(proj)
}

func (g *Game) fieldSquadSymbols(proj fieldProjector, symCtx ui.MapRenderCtx, now float32) {
	q := g.Filt.Squad.Query()
	for q.Next() {
		_, roster := q.Get()
		squad := q.Entity()
		centre, alpha, ok := g.visibleRosterCentre(roster, now)
		if !ok {
			continue
		}
		screen, on := proj.at(centre, fieldSquadLiftM)
		if !on {
			continue
		}
		spec := ui.SquadSymbolSpec(symCtx, squad, roster)
		ui.DrawSymbol(spec, screen, fieldSymbolHalf, alpha)
		g.drawFieldTint(spec, screen, fieldSymbolHalf, squad, alpha)
	}
}

func (g *Game) fieldVehicleSymbols(proj fieldProjector) {
	q := g.Filt.VehicleRender.Query()
	for q.Next() {
		pos, veh := q.Get()
		ent := q.Entity()
		vspec := components.SpecForVehicle(veh.Kind)
		screen, on := proj.at(*pos, vspec.BoxHgt+fieldHullLiftM)
		if !on {
			continue
		}
		spec := components.SymbolSpec{
			Affiliation: ui.AffiliationForFaction(g.factionOf(ent)),
			Dimension:   components.DimVehicleClass,
			Icon:        ui.VehicleIcon(veh.Kind),
		}
		ui.DrawSymbol(spec, screen, fieldVehicleHalf, 1)
		g.drawFieldTint(spec, screen, fieldVehicleHalf, g.commanderOf(ent), 1)
	}
}

func (g *Game) fieldAircraftSymbols(proj fieldProjector) {
	q := g.Filt.AircraftRender.Query()
	for q.Next() {
		pos, ac := q.Get()
		ent := q.Entity()
		screen, on := proj.at(*pos, components.SpecForAircraft(ac.Kind).BoxHgt+fieldHullLiftM)
		if !on {
			continue
		}
		spec := ui.DefaultSpecForDimension(ui.AffiliationForFaction(g.factionOf(ent)),
			components.DimAirClass)
		ui.DrawSymbol(spec, screen, fieldVehicleHalf, 1)
		g.drawFieldTint(spec, screen, fieldVehicleHalf, g.commanderOf(ent), 1)
	}
}

// drawFieldTint rides the squad's palette colour under the frame. It is
// own-force organisation — which group this thing answers to — so a hostile
// mark gets none.
func (g *Game) drawFieldTint(spec components.SymbolSpec, screen rl.Vector2,
	half float32, cmd ecs.Entity, alpha float32) {
	if spec.Affiliation != components.AffilFriend {
		return
	}
	col := g.squadColor(cmd)
	if col.A == 0 {
		return
	}
	col.A = uint8(float32(col.A) * alpha)
	b := ui.SymbolBounds(spec.Affiliation, screen, half)
	rl.DrawRectangleRec(rl.Rectangle{
		X: b.X, Y: b.Y + b.Height + 1, Width: b.Width, Height: fieldTintH,
	}, col)
}

// visibleRosterCentre averages the members the 3D pass actually drew, and
// reports the freshest of their alphas. Averaging the whole roster instead
// would put the mark where the undetected half of a squad is standing.
func (g *Game) visibleRosterCentre(roster *components.CommandRoster,
	now float32) (components.WorldPos, float32, bool) {
	var ref components.WorldPos
	var sum rl.Vector3
	var n, best float32
	found := false
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !g.App.World.Alive(mem) {
			continue
		}
		alpha, visible := g.bodyAlpha(mem, now)
		if !visible {
			continue
		}
		pos := g.Maps.Pos.Get(mem)
		if pos == nil {
			continue
		}
		if !found {
			ref, found = *pos, true
		}
		d := pos.Sub(ref)
		sum.X += d.X
		sum.Y += d.Y
		sum.Z += d.Z
		n++
		if alpha > best {
			best = alpha
		}
	}
	if !found {
		return components.WorldPos{}, 0, false
	}
	return ref.Add(rl.Vector3{X: sum.X / n, Y: sum.Y / n, Z: sum.Z / n}), best, true
}

// fowTailSec is how long a body lingers, fading, after the sensors lose it.
const fowTailSec float32 = 2.0

// bodyAlpha answers what the 3D pass draws this entity at: own force solid,
// anything else riding its contact and fading over the tail. Shared by the
// bodies and by the marker layer above them so the two cannot disagree.
func (g *Game) bodyAlpha(ent ecs.Entity, now float32) (float32, bool) {
	// Only men are fogged today — the hull and airframe passes draw every
	// machine regardless of contact, so a mark over one has to as well.
	if g.Maps.Vehicle.Has(ent) || g.Maps.Aircraft.Has(ent) {
		return 1, true
	}
	if f := g.Maps.Faction.Get(ent); f == nil || f.ID == components.FactionPlayer {
		return 1, true
	}
	contact, ok := g.Res.ContactRegistry.Tracked[ent]
	if !ok || !g.App.World.Alive(contact) {
		return 0, false
	}
	c := g.Maps.Contact.Get(contact)
	// A bearing is not a position. An ESM intercept holds the RECEIVER's
	// coordinates, so honouring it here drew the emitter's body at its true
	// place — a radio switched on revealed the man carrying it. Every other
	// consumer already filters these out; this pass reached past them into
	// the registry.
	if c == nil || c.BearingOnly {
		return 0, false
	}
	age := now - c.LastSeenTime
	if age >= fowTailSec {
		return 0, false
	}
	if age <= 0 {
		return 1, true
	}
	return 1 - age/fowTailSec, true
}

func (g *Game) factionOf(ent ecs.Entity) uint8 {
	if f := g.Maps.Faction.Get(ent); f != nil {
		return f.ID
	}
	return components.FactionPlayer
}

// commanderOf: a machine in a squad wears that squad's colour, a soloist its
// own — the same colour its route line is drawn in either way.
func (g *Game) commanderOf(ent ecs.Entity) ecs.Entity {
	if sm := g.Maps.SquadMember.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		return sm.Squad
	}
	return ent
}
