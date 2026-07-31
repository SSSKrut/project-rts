package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

const ghostBodyAlpha uint8 = 80

const ghostMaxSquadsRendered = 1

type ghostContext struct {
	world            *ecs.World
	posMap           *ecs.Map[components.WorldPos]
	rosterMap        *ecs.Map[components.CommandRoster]
	formationDataMap *ecs.Map[components.FormationData]
	stanceMap        *ecs.Map[components.Stance]
	movementMap      *ecs.Map[components.MovementProfile]
	squadMemberMap   *ecs.Map[components.SquadMember]

	hitTester      *HitTester
	buildingIndex  *systems.BuildingChildIndex
	wallMap        *ecs.Map[components.WallSegment]
	windowMap      *ecs.Map[components.Window]
	floorMap       *ecs.Map[components.Floor]
	levelMemberMap *ecs.Map[components.LevelMember]
	levelMap       *ecs.Map[components.Level]
	trenches       *components.TrenchNetwork
	trenchRootMap  *ecs.Map[components.TrenchRoot]
	vehicleMap     *ecs.Map[components.Vehicle]
	squadColor     func(ent ecs.Entity) rl.Color
}

// primarySquadForGhost returns the first Squad entity touched by `selected`,
// or zero if none. Multi-squad selection only previews the first squad.
func primarySquadForGhost(selected []ecs.Entity, smMap *ecs.Map[components.SquadMember]) ecs.Entity {
	if len(selected) == 0 || smMap == nil {
		return ecs.Entity{}
	}
	for _, e := range selected {
		sm := smMap.Get(e)
		if sm == nil {
			continue
		}
		if sm.Squad != (ecs.Entity{}) {
			return sm.Squad
		}
	}
	return ecs.Entity{}
}

// ghostStanceFor picks the stance the ghost cubes should adopt. Priority:
// squad MovementProfile.Stance -> commander's current Stance -> standing.
func (g *ghostContext) ghostStanceFor(squad ecs.Entity, roster *components.CommandRoster) components.Stance {
	if g.movementMap != nil {
		if mp := g.movementMap.Get(squad); mp != nil {
			return components.Stance{Code: mp.Stance}
		}
	}
	if roster != nil && roster.Count > 0 {
		commander := roster.Members[0]
		if commander != (ecs.Entity{}) && g.world.Alive(commander) {
			if s := g.stanceMap.Get(commander); s != nil {
				return *s
			}
		}
	}
	return components.Stance{Code: components.StanceStand}
}

// drawSelectionGhost renders the ghost-preview formation at the cursor's
// world target. Must run between BeginMode3D and EndMode3D (after the real
// unit pass so ghosts layer above terrain without z-fighting).
//
// `cursorOver3D` gates the entire pass. `dragFacing != nil` overrides the
// hover-derived yaw with the player's drag-derived facing.
func drawSelectionGhost(
	g *ghostContext,
	selected []ecs.Entity,
	cursorOver3D bool,
	cursorTarget components.WorldPos,
	targetOK bool,
	dragFacing *float32,
	popupKind *components.OrderKindCode,
	popupLevel ecs.Entity,
	levelMap *ecs.Map[components.Level],
) {
	if !cursorOver3D || !targetOK || g == nil {
		return
	}
	drawGhostVehicles(g, selected, cursorTarget, dragFacing)
	squad := primarySquadForGhost(selected, g.squadMemberMap)
	if squad == (ecs.Entity{}) || !g.world.Alive(squad) {
		return
	}
	roster := g.rosterMap.Get(squad)
	fd := g.formationDataMap.Get(squad)
	if roster == nil || fd == nil || roster.Count == 0 {
		return
	}
	center, ok := systems.SquadCenter(g.world, roster, g.posMap)
	if !ok {
		return
	}

	// Forward selection: drag-derived yaw wins over hover-derived.
	var forward rl.Vector3
	if dragFacing != nil {
		s, c := math.Sincos(float64(*dragFacing))
		forward = rl.Vector3{X: float32(s), Y: 0, Z: float32(c)}
	} else {
		diff := cursorTarget.Sub(center)
		dx, dz := diff.X, diff.Z
		magSq := dx*dx + dz*dz
		forward = rl.Vector3{X: 0, Y: 0, Z: 1}
		if magSq > 1e-4 {
			mag := float32(math.Sqrt(float64(magSq)))
			forward = rl.Vector3{X: dx / mag, Y: 0, Z: dz / mag}
		}
	}

	stance := g.ghostStanceFor(squad, roster)

	// Popup-hover override: preview the popup's kind instead of the hit-test default.
	if popupKind != nil && *popupKind == components.OrderKindDefendPosition {
		drawGhostFormation(cursorTarget, fd.Type, fd.Spacing, roster.Count, forward, stance)
		drawDefendPositionArc(cursorTarget, forward, squad, g.squadColor)
		return
	}

	// Floor picker — ghost dots on the specific Level's centre.
	if popupLevel != (ecs.Entity{}) && levelMap != nil {
		if lvl := levelMap.Get(popupLevel); lvl != nil {
			cWP := components.WorldPos{}
			cWP = cWP.Add(rl.Vector3{X: lvl.AABB.CenterX(), Y: lvl.AABB.MinY, Z: lvl.AABB.CenterZ()})
			drawGhostFormation(cWP, components.FormationLoose, 1.6, roster.Count, forward, stance)
			return
		}
	}

	// "Attacking position at windows" — window-attached ghost (Garrison).
	if popupKind != nil && *popupKind == components.OrderKindGarrison {
		hit := HitTestResult{Kind: HitTerrain}
		if g.hitTester != nil {
			hit = g.hitTester.HitTest(cursorTarget)
		}
		if hit.Kind == HitBuilding {
			if drawGhostAtWindows(g, hit.Entity, roster.Count, stance) {
				return
			}
		}
	}

	// HitTester classifies the cursor target; dispatch to a placement strategy.
	// Falls back to standard formation if hitTester or kind data is missing.
	hit := HitTestResult{Kind: HitTerrain}
	if g.hitTester != nil {
		hit = g.hitTester.HitTest(cursorTarget)
	}
	switch hit.Kind {
	case HitBuilding:
		if drawGhostInBuilding(g, hit.Entity, roster.Count, stance) {
			return
		}
	case HitTrench:
		if drawGhostAlongTrench(g, hit.Entity, roster.Count, stance) {
			return
		}
	}
	drawGhostFormation(cursorTarget, fd.Type, fd.Spacing, roster.Count, forward, stance)
}

// drawGhostAtWindows places ghost cubes at the first N windows of `building`
// (greedy first-N, no threat-aware ranking). Returns true on placement.
func drawGhostAtWindows(g *ghostContext, building ecs.Entity, count uint8, stance components.Stance) bool {
	if g.buildingIndex == nil || g.windowMap == nil || g.wallMap == nil {
		return false
	}
	children, ok := g.buildingIndex.Loaded[building]
	if !ok || len(children) == 0 {
		return false
	}
	placed := uint8(0)
	for _, ch := range children {
		if placed >= count {
			break
		}
		if !g.world.Alive(ch) {
			continue
		}
		if g.windowMap.Get(ch) == nil {
			continue
		}
		wall := g.wallMap.Get(ch)
		wPos := g.posMap.Get(ch)
		if wall == nil || wPos == nil {
			continue
		}
		offsetAlong := wall.OpeningCenterT * wall.Length
		dx := offsetAlong * float32(math.Sin(float64(wall.Yaw)))
		dz := offsetAlong * float32(math.Cos(float64(wall.Yaw)))
		ghostWP := wPos.Add(rl.Vector3{X: dx, Y: 0, Z: dz})
		ghostRender := ghostWP.ToRenderSpace(systems.CurrentOriginChunk)
		drawGhostUnit(ghostRender, stance, ghostBodyAlpha)
		placed++
	}
	return placed > 0
}

// drawDefendPositionArc draws a 90° wedge at the cursor target oriented
// along `forward`. Sector colour reuses the per-squad palette.
func drawDefendPositionArc(target components.WorldPos, forward rl.Vector3, squad ecs.Entity,
	colorFn func(ecs.Entity) rl.Color) {
	const halfAngle = math.Pi / 4
	const length float32 = 8
	yaw := float32(math.Atan2(float64(forward.X), float64(forward.Z)))
	center := target.ToRenderSpace(systems.CurrentOriginChunk)
	col := rl.Color{R: 200, G: 200, B: 220, A: 230}
	if colorFn != nil {
		col = colorFn(squad)
	}
	fill := rl.Color{R: col.R, G: col.G, B: col.B, A: 60}
	drawGhostArc(center, yaw, halfAngle, length, fill)
}

// drawGhostInBuilding places ghost cubes at floor centres of `building`,
// splitting the roster across storeys (ceil(N/M) per floor; last takes the
// remainder). Returns false on missing index / no floors so caller can fall
// back to standard formation.
func drawGhostInBuilding(g *ghostContext, building ecs.Entity, count uint8, stance components.Stance) bool {
	if g.buildingIndex == nil || g.floorMap == nil || count == 0 {
		return false
	}
	children, ok := g.buildingIndex.Loaded[building]
	if !ok || len(children) == 0 {
		return false
	}
	// Sort floors by level ascending so distribution is deterministic.
	type floorCentre struct {
		pos    components.WorldPos
		level  uint8
		rooms  [components.MaxRoomsPerLevel]components.AABB2D
		roomsN uint8
	}
	var floors []floorCentre
	for _, ch := range children {
		if !g.world.Alive(ch) {
			continue
		}
		f := g.floorMap.Get(ch)
		if f == nil {
			continue
		}
		fp := g.posMap.Get(ch)
		if fp == nil {
			continue
		}
		// Floor.WorldPos IS the plate centre, NOT a corner — gen/buildings
		// passes the building centre. render_buildings.go DrawCubeV and
		// GroundStickSystem both treat fp as centre; do the same here.
		fc := floorCentre{pos: *fp, level: f.Level}
		if lm := g.levelMemberMap.Get(ch); lm != nil &&
			lm.Level != (ecs.Entity{}) && g.world.Alive(lm.Level) {
			if lv := g.levelMap.Get(lm.Level); lv != nil {
				fc.rooms = lv.Rooms
				fc.roomsN = lv.RoomCount
			}
		}
		floors = append(floors, fc)
	}
	if len(floors) == 0 {
		return false
	}
	for i := 1; i < len(floors); i++ {
		for j := i; j > 0 && floors[j-1].level > floors[j].level; j-- {
			floors[j-1], floors[j] = floors[j], floors[j-1]
		}
	}
	m := uint8(len(floors))
	perFloor := (count + m - 1) / m
	if perFloor == 0 {
		perFloor = 1
	}
	remaining := count
	const ghostFloorSpacing float32 = 1.6
	forward := rl.Vector3{X: 0, Y: 0, Z: 1}
	for _, fl := range floors {
		if remaining == 0 {
			break
		}
		take := perFloor
		if take > remaining {
			take = remaining
		}
		if rc := uint8(fl.roomsN); rc > 1 {
			// Same round-robin the FormationSystem interior branch executes:
			// member k → room k%rc, Loose slot k/rc around the room centre.
			for k := uint8(0); k < take; k++ {
				room := fl.rooms[k%rc]
				offX, offZ := systems.FormationOffset(components.FormationLoose,
					k/rc, ghostFloorSpacing, forward)
				wp := components.WorldPos{}.Add(rl.Vector3{
					X: room.CenterX() + offX,
					Y: fl.pos.Local.Y,
					Z: room.CenterZ() + offZ,
				})
				drawGhostUnit(wp.ToRenderSpace(systems.CurrentOriginChunk), stance, ghostBodyAlpha)
			}
		} else {
			drawGhostFormation(fl.pos, components.FormationLoose,
				ghostFloorSpacing, take, forward, stance)
		}
		remaining -= take
	}
	return remaining < count
}

// drawGhostAlongTrench places ghost cubes equal-spaced along the polyline of
// `trenchRoot`. Endpoints get an inset rather than sitting on the tips.
func drawGhostAlongTrench(g *ghostContext, trenchRoot ecs.Entity, count uint8, stance components.Stance) bool {
	if g.trenchRootMap == nil || g.trenches == nil {
		return false
	}
	root := g.trenchRootMap.Get(trenchRoot)
	if root == nil || root.Index < 0 || root.Index >= len(g.trenches.Lines) {
		return false
	}
	points := g.trenches.Lines[root.Index].Points
	if len(points) < 2 || count == 0 {
		return false
	}
	totalLen := polylineTotalLen(points)
	if totalLen <= 0 {
		return false
	}
	denom := float32(count + 1)
	for i := uint8(0); i < count; i++ {
		t := float32(i+1) / denom
		wp := pointAlongPolyline(points, t*totalLen)
		ghostRender := wp.ToRenderSpace(systems.CurrentOriginChunk)
		drawGhostUnit(ghostRender, stance, ghostBodyAlpha)
	}
	return true
}

// drawGhostVehicles previews soloist vehicles as translucent hull boxes in
// line abreast at the cursor. Facing = drag-derived yaw, else first-hull →
// cursor bearing. Squadded vehicles are covered by the formation ghost.
func drawGhostVehicles(g *ghostContext, selected []ecs.Entity,
	target components.WorldPos, dragFacing *float32) {
	if g.vehicleMap == nil {
		return
	}
	type vehGhost struct {
		ent  ecs.Entity
		kind components.VehicleKind
	}
	var vgs []vehGhost
	var widest float32
	for _, e := range selected {
		if e == (ecs.Entity{}) || !g.world.Alive(e) {
			continue
		}
		v := g.vehicleMap.Get(e)
		if v == nil {
			continue
		}
		if sm := g.squadMemberMap.Get(e); sm != nil && sm.Squad != (ecs.Entity{}) {
			continue
		}
		vgs = append(vgs, vehGhost{ent: e, kind: v.Kind})
		if w := components.SpecForVehicle(v.Kind).BoxWid; w > widest {
			widest = w
		}
	}
	if len(vgs) == 0 {
		return
	}
	yaw := float32(0)
	if dragFacing != nil {
		yaw = *dragFacing
	} else if p := g.posMap.Get(vgs[0].ent); p != nil {
		diff := target.Sub(*p)
		if diff.X*diff.X+diff.Z*diff.Z > 1e-4 {
			yaw = float32(math.Atan2(float64(diff.X), float64(diff.Z)))
		}
	}
	rightX := float32(math.Cos(float64(yaw)))
	rightZ := -float32(math.Sin(float64(yaw)))
	spacing := widest + 2
	half := float32(len(vgs)-1) * 0.5
	for i, v := range vgs {
		off := (float32(i) - half) * spacing
		wp := target.Add(rl.Vector3{X: rightX * off, Z: rightZ * off})
		col := rl.Color{R: 200, G: 220, B: 235, A: ghostBodyAlpha}
		if g.squadColor != nil {
			c := g.squadColor(v.ent)
			col = rl.Color{R: c.R, G: c.G, B: c.B, A: ghostBodyAlpha}
		}
		drawVehicleBox(wp.ToRenderSpace(systems.CurrentOriginChunk), yaw, 0, v.kind, col)
	}
}

// facingFromSquadToTarget returns the yaw the squad would face if it walked
// from its current center to `target`. Returns (0, false) when no squad /
// empty roster / center unknown.
func facingFromSquadToTarget(g *ghostContext, selected []ecs.Entity, target components.WorldPos) (float32, bool) {
	if g == nil {
		return 0, false
	}
	squad := primarySquadForGhost(selected, g.squadMemberMap)
	if squad == (ecs.Entity{}) || !g.world.Alive(squad) {
		return 0, false
	}
	roster := g.rosterMap.Get(squad)
	if roster == nil {
		return 0, false
	}
	center, ok := systems.SquadCenter(g.world, roster, g.posMap)
	if !ok {
		return 0, false
	}
	diff := target.Sub(center)
	if diff.X*diff.X+diff.Z*diff.Z < 1e-4 {
		return 0, false
	}
	return float32(math.Atan2(float64(diff.X), float64(diff.Z))), true
}

func polylineTotalLen(points []components.WorldPos) float32 {
	var sum float32
	for i := 1; i < len(points); i++ {
		d := points[i].Sub(points[i-1])
		sum += float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
	}
	return sum
}

// pointAlongPolyline returns the WorldPos at arc-length `dist` from points[0].
// Clamps to endpoints; callers don't need to bound `dist`.
func pointAlongPolyline(points []components.WorldPos, dist float32) components.WorldPos {
	if len(points) == 0 {
		return components.WorldPos{}
	}
	if dist <= 0 || len(points) == 1 {
		return points[0]
	}
	remaining := dist
	for i := 1; i < len(points); i++ {
		d := points[i].Sub(points[i-1])
		segLen := float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
		if segLen <= 0 {
			continue
		}
		if remaining <= segLen {
			t := remaining / segLen
			return points[i-1].Add(rl.Vector3{X: d.X * t, Y: d.Y * t, Z: d.Z * t})
		}
		remaining -= segLen
	}
	return points[len(points)-1]
}

// drawGhostFormation places `count` ghost cubes around `center` using the
// same FormationOffset layout that FormationSystem writes.
func drawGhostFormation(
	center components.WorldPos,
	kind components.FormationKind,
	spacing float32,
	count uint8,
	forward rl.Vector3,
	stance components.Stance,
) {
	for i := uint8(0); i < count; i++ {
		offX, offZ := systems.FormationOffset(kind, i, spacing, forward)
		ghostWP := center.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
		ghostRender := ghostWP.ToRenderSpace(systems.CurrentOriginChunk)
		// Y sampled from cursor — ghosts may float/sink slightly on rolling terrain.
		drawGhostUnit(ghostRender, stance, ghostBodyAlpha)
	}
}
