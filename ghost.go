package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// Phase 13.6 - ghost preview render pass.
//
// Called from main.go inside BeginMode3D, after units / props / buildings have
// drawn (so ghost cubes layer on top of terrain without z-fighting). The
// function is a render-only no-op when no selection contains a Squad or when
// the cursor is not focused on Panel3D.

// ghostBodyAlpha is the alpha (0..255) used for ghost body cubes. Tunable
// in M13.6.6 playtest - 80 is ~31% opacity, low enough that real units stay
// dominant but visible enough at a glance.
const ghostBodyAlpha uint8 = 80

// ghostMaxSquadsRendered caps the number of ghosts we draw to keep the
// preview readable when the player has a multi-squad selection. P9 says
// Phase 13.6 only previews the first squad anyway, but the cap is here as
// defensive ceiling if the rule ever loosens.
const ghostMaxSquadsRendered = 1

// ghostContext bundles the per-frame handles the ghost preview needs. Built
// once outside the render loop in main.go and passed by pointer so we don't
// duplicate the long argument list across milestones.
type ghostContext struct {
	world            *ecs.World
	posMap           *ecs.Map[components.WorldPos]
	rosterMap        *ecs.Map[components.CommandRoster]
	formationDataMap *ecs.Map[components.FormationData]
	stanceMap        *ecs.Map[components.Stance]
	movementMap      *ecs.Map[components.MovementProfile]
	squadMemberMap   *ecs.Map[components.SquadMember]

	// Phase 13.6 M13.6.3 / 17.6 M17.6.2: per-kind ghost placement.
	hitTester     *HitTester
	buildingIndex *systems.BuildingChildIndex
	wallMap       *ecs.Map[components.WallSegment]
	windowMap     *ecs.Map[components.Window]
	// Phase 17.6: floor-based ghost for OccupyBuilding (new default on
	// HitBuilding) — distribute roster across storeys instead of windows.
	floorMap      *ecs.Map[components.Floor]
	trenches      *components.TrenchNetwork
	trenchRootMap *ecs.Map[components.TrenchRoot]
	// Phase 14 M14.6: faction-aware colour picker for the DefendPosition
	// arc (and any future ghost overlays that need to match the squad's
	// inspector / map tint).
	squadColor func(ent ecs.Entity) rl.Color
}

// primarySquadForGhost returns the first Squad entity touched by `selected`,
// or zero if none. Phase 13.6 P9: multi-squad selection falls back to the
// first squad - Phase 19 will spread per-squad ghost groups.
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
// squad MovementProfile.Stance (the resting posture squad returns to) ->
// commander's current Stance -> standing default. Lets the ghost preview
// reflect "we'll arrive in Crouch / Prone" when the player has chosen a
// stealth preset.
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
// world target. Phase 13.6 M13.6.2: terrain hover only - Garrison / Trench /
// DefendPosition placements land in M13.6.3 / M13.6.5. Caller must invoke
// this between BeginMode3D and EndMode3D (after the real unit pass so ghosts
// layer above terrain without z-fighting).
//
// `cursorOver3D` gates the entire pass - when the cursor leaves Panel3D the
// ghost disappears, so the player can navigate Inspector chips without the
// preview distracting them.
//
// `targetOK` and `cursorTarget` come from the per-frame mouseTargetWorldPos
// raycast; when the raycast misses (cursor sky-bound, parallel ray) we skip
// the pass rather than draw at the world origin.
//
// Phase 13.6 M13.6.4: when `dragFacing` != nil, override the hover-derived
// yaw with the player's drag-derived facing - ghost rotates with the cursor
// so the player sees the formation orientation before committing.
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

	// Forward selection: drag-derived yaw wins over hover-derived. M13.6.4
	// keeps the two paths symmetric - both end up as a unit vector consumed
	// by FormationOffset; only the source of the angle differs.
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

	// Phase 13.6 M13.6.5: pie menu hover overrides hit-test placement so the
	// player can preview a non-default order kind (DefendPosition arc) before
	// committing.
	if popupKind != nil && *popupKind == components.OrderKindDefendPosition {
		drawGhostFormation(cursorTarget, fd.Type, fd.Spacing, roster.Count, forward, stance)
		drawDefendPositionArc(cursorTarget, forward, squad, g.squadColor)
		return
	}

	// Phase 17.6 M17.6.9: popup-hover floor picker — ghost dots on the
	// specific Level's centre, concentrated (no per-floor split).
	if popupLevel != (ecs.Entity{}) && levelMap != nil {
		if lvl := levelMap.Get(popupLevel); lvl != nil {
			cWP := components.WorldPos{}
			cWP = cWP.Add(rl.Vector3{X: lvl.AABB.CenterX(), Y: lvl.AABB.MinY, Z: lvl.AABB.CenterZ()})
			drawGhostFormation(cWP, components.FormationLoose, 1.6, roster.Count, forward, stance)
			return
		}
	}

	// Phase 17.6 M17.6.9: popup-hover "Attacking position at windows" →
	// window-attached ghost (legacy Garrison preview). Other building-target
	// kinds (OccupyBuilding default / ClearBuilding / Hidden) keep the
	// floor-spread preview.
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

	// Phase 13.6 M13.6.3: per-kind ghost placement. HitTester classifies the
	// cursor target; we dispatch to a placement-strategy function. Falls back
	// to standard formation if hitTester or the kind-specific data is missing.
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
	// Terrain default - standard formation around cursor.
	drawGhostFormation(cursorTarget, fd.Type, fd.Spacing, roster.Count, forward, stance)
}

// drawGhostAtWindows places ghost cubes at the first N windows of `building`
// (greedy-first-N, no threat-aware ranking). Phase 17.6 M17.6.9: restored
// from the pre-M17.6.2 drawGhostInBuilding so the building popup's
// "Attacking position at windows" item previews the actual Garrison
// distribution. Returns true if at least one ghost was placed.
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

// drawDefendPositionArc draws a wedge-shaped sector indicator at the cursor
// target, oriented along `forward`. Phase 13.6 M13.6.5: visual stub for the
// future EngagementRules.SectorYaw / SectorHalfDot enforcement (Phase 14).
// Width fixed at 90 deg (45 deg each side of facing), length 8 m - matches the
// "sector vision" feel referenced in P8.
//
// Sector colour reuses the per-squad palette so multi-squad scenes can tell
// arcs apart visually; alpha is low for fill / high for outline so the wedge
// is unmistakable without obscuring the terrain underneath.
func drawDefendPositionArc(target components.WorldPos, forward rl.Vector3, squad ecs.Entity,
	colorFn func(ecs.Entity) rl.Color) {
	const halfAngle = math.Pi / 4 // 45 deg each side -> 90 deg total wedge
	const length float32 = 8
	// atan2(fx, fz) recovers the yaw used by FormationOffset.
	yaw := float32(math.Atan2(float64(forward.X), float64(forward.Z)))
	center := target.ToRenderSpace(systems.CurrentOriginChunk)
	col := rl.Color{R: 200, G: 200, B: 220, A: 230}
	if colorFn != nil {
		col = colorFn(squad)
	}
	// Lower alpha for fill; outline pumped via drawGhostArc internals.
	fill := rl.Color{R: col.R, G: col.G, B: col.B, A: 60}
	drawGhostArc(center, yaw, halfAngle, length, fill)
}

// drawGhostInBuilding places ghost cubes at floor centres of `building`,
// splitting the roster across storeys (Phase 17.6: matches the new default
// OccupyBuilding behaviour — "go in and spread by floors", not Garrison's
// "post at windows"). Returns true if at least one ghost was placed; false
// on missing index / no floors, so caller falls back to standard formation.
//
// Distribution: roster of N units across M floors gets
// ceil(N/M) units per floor (last floor takes the remainder). Inside one
// floor, ghosts arrange around the floor centre with a Loose-formation
// spacing — same FormationOffset the runtime uses, so the preview matches
// the eventual rest position.
//
// Garrison-style window-attached preview is retained as `drawGhostAtWindows`
// for the popup "Attacking position" item (M17.6.9 popup-hover swap).
func drawGhostInBuilding(g *ghostContext, building ecs.Entity, count uint8, stance components.Stance) bool {
	if g.buildingIndex == nil || g.floorMap == nil || count == 0 {
		return false
	}
	children, ok := g.buildingIndex.Loaded[building]
	if !ok || len(children) == 0 {
		return false
	}
	// Collect Floor children (one entity per storey). Sort by level ascending
	// so distribution is deterministic across frames and matches the
	// generator's level numbering.
	type floorCentre struct {
		pos   components.WorldPos
		level uint8
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
		// Phase 17.9 — Floor.WorldPos IS the plate centre (gen/buildings/house.go
		// AddFloor(rl.Vector3{X: cx, Y: baseY, Z: cz}, ...) passes building's
		// centre, not a corner). The previous "+SizeX/2 to get centre" was the
		// inverse of the actual semantics — render_buildings.go DrawCubeV
		// treats fp as centre, GroundStickSystem checks [centre±SizeX/2], so
		// every other reader was consistent except this code and the old
		// memberOnFloor.
		floors = append(floors, floorCentre{pos: *fp, level: f.Level})
	}
	if len(floors) == 0 {
		return false
	}
	// Insertion sort by level — tiny N (1..5).
	for i := 1; i < len(floors); i++ {
		for j := i; j > 0 && floors[j-1].level > floors[j].level; j-- {
			floors[j-1], floors[j] = floors[j], floors[j-1]
		}
	}
	// Split count across floors: ceil(count/M) per floor, last floor takes
	// the remainder. ceil(N/M) = (N + M - 1) / M.
	m := uint8(len(floors))
	perFloor := (count + m - 1) / m
	if perFloor == 0 {
		perFloor = 1
	}
	remaining := count
	const ghostFloorSpacing float32 = 1.6 // Loose-formation typical
	// Forward vector for FormationOffset — Loose layout is mostly radial, but
	// FormationOffset still needs a direction. Use +Z (north) as the canonical
	// fallback; floors are tight enough that orientation barely shows.
	forward := rl.Vector3{X: 0, Y: 0, Z: 1}
	for _, fl := range floors {
		if remaining == 0 {
			break
		}
		take := perFloor
		if take > remaining {
			take = remaining
		}
		drawGhostFormation(fl.pos, components.FormationLoose,
			ghostFloorSpacing, take, forward, stance)
		remaining -= take
	}
	return remaining < count
}

// drawGhostAlongTrench places ghost cubes equal-spaced along the polyline of
// `trenchRoot`. Distribution is `total_length x (i+1) / (count+1)` - endpoints
// get an inset rather than sitting on the polyline tips (visually cleaner).
//
// Returns true if at least one ghost was placed. False on bad index / empty
// polyline -> caller falls back to standard formation around cursor.
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

// facingFromSquadToTarget returns the yaw the squad would face if it walked
// from its current center to `target`. Phase 13.6 M13.6.5: used by the
// DefendPosition pie commit to derive the arrived sector orientation from a
// simple tap (no facing-drag) - keeps the pie-only flow useful without
// forcing the player to drag.
//
// Returns (0, false) when the selection has no squad, the roster is empty, or
// the squad center can't be computed. Caller skips OrderParamFacing then.
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

// polylineTotalLen sums segment lengths in metres (XZ plane only).
func polylineTotalLen(points []components.WorldPos) float32 {
	var sum float32
	for i := 1; i < len(points); i++ {
		d := points[i].Sub(points[i-1])
		sum += float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
	}
	return sum
}

// pointAlongPolyline returns the WorldPos at arc-length `dist` from points[0].
// Walks segments accumulating distance; lerps inside the segment that contains
// the target distance. Clamps to the polyline endpoints - callers don't need
// to bound `dist` themselves.
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
// same FormationOffset layout that FormationSystem writes. Extracted so the
// per-kind placements in M13.6.3 (Garrison / OccupyTrench / DefendPosition)
// can share the body-cube render loop while differing only in slot positions.
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
		// Sample ground plane at the cursor's Y - terrain height under each
		// ghost slot would require a heightmap lookup per slot. Phase 13.6
		// accepts the approximation; ghosts may float / sink slightly on
		// rolling terrain. Phase 21 polish can switch to per-slot height.
		drawGhostUnit(ghostRender, stance, ghostBodyAlpha)
	}
}
