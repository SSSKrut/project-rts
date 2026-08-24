package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

const (
	unitDotR    float32 = 2.5
	unitSpokeW  float32 = 1.0
	soloistDotR float32 = 3
	unitDotEdge         = 0.55 // outline = dot colour scaled toward black

	// vehicleSymbolHalf runs a touch larger than a squad marker: a hull is
	// the heaviest single thing on the field and reads as such.
	vehicleSymbolHalf float32 = 8

	// ownDotsMinSpreadPx is deliberately far below the contact threshold:
	// for enemy tracks the point is to suppress a scatter, for our own
	// soldiers it is to reveal one as soon as the zoom can separate them.
	ownDotsMinSpreadPx float32 = 8
)

// squadMemberPoints projects every live member onto the panel. Fixed array:
// a roster is capped at SquadRosterSize, so this never allocates.
func squadMemberPoints(ctx MapRenderCtx, roster *components.CommandRoster,
	content rl.Rectangle) ([components.SquadRosterSize]rl.Vector2, int) {
	var pts [components.SquadRosterSize]rl.Vector2
	n := 0
	if roster == nil || ctx.PosMap == nil || ctx.World == nil {
		return pts, 0
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		// Vehicles carry their own marker; a hull must not shrink to a dot
		// just because it rides in a mixed squad.
		if ctx.VehicleMap != nil && ctx.VehicleMap.Has(mem) {
			continue
		}
		p := ctx.PosMap.Get(mem)
		if p == nil {
			continue
		}
		pts[n] = MapWorldToPanel(*p, ctx.Cam, content)
		n++
	}
	return pts, n
}

// anyBeyond reports whether any point sits further than limit px from centre.
func anyBeyond(centre rl.Vector2, pts []rl.Vector2, limit float32) bool {
	l2 := limit * limit
	for _, p := range pts {
		dx, dy := p.X-centre.X, p.Y-centre.Y
		if dx*dx+dy*dy > l2 {
			return true
		}
	}
	return false
}

// drawSquadMemberDots wires each soldier back to the squad symbol once the
// squad is spread wide enough on screen to read as a crowd. Same threshold
// as contact clusters, so both sides of the map behave alike.
func drawSquadMemberDots(content rl.Rectangle, ctx MapRenderCtx,
	roster *components.CommandRoster, centre rl.Vector2, col rl.Color) {
	pts, n := squadMemberPoints(ctx, roster, content)
	if n < 2 || !anyBeyond(centre, pts[:n], ownDotsMinSpreadPx) {
		return
	}
	edge := dimColor(col, unitDotEdge)
	spoke := withAlpha(edge, 0.7)
	for _, p := range pts[:n] {
		rl.DrawLineEx(centre, p, unitSpokeW, spoke)
		rl.DrawCircleV(p, unitDotR, col)
		rl.DrawCircleLines(int32(p.X), int32(p.Y), unitDotR, edge)
	}
}

// drawOwnVehicles gives every own vehicle its own symbol. A hull is a single
// heavy asset, so it never collapses into a squad marker or a soldier dot —
// one machine, one marker, at any zoom.
func drawOwnVehicles(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.VehicleFilter == nil {
		return
	}
	q := ctx.VehicleFilter.Query()
	for q.Next() {
		pos, veh := q.Get()
		ent := q.Entity()
		if unitAffiliation(ctx, ent) != components.AffilFriend {
			continue
		}
		p := MapWorldToPanel(*pos, ctx.Cam, content)
		spec := components.SymbolSpec{
			Affiliation: components.AffilFriend,
			Dimension:   components.DimVehicleClass,
			Icon:        vehicleIcon(veh.Kind),
		}
		DrawSymbol(spec, p, vehicleSymbolHalf, 1.0)
		bounds := SymbolBounds(spec.Affiliation, p, vehicleSymbolHalf)
		if isSelectedEntity(ctx.Selected, ent) {
			rl.DrawRectangleLinesEx(InflateRect(bounds, 2), 1.5, mapSelectionRing)
		}
		if ctx.Hovered == ent {
			rl.DrawRectangleLinesEx(InflateRect(bounds, 4), 1.5, mapHoverRing)
		}
	}
}

// vehicleIcon: a truck hauls, everything else in the current roster fights.
func vehicleIcon(k components.VehicleKind) components.IconKind {
	if k == components.VehicleTruck {
		return components.IconSupply
	}
	return components.IconArmor
}

// drawSoloistUnits draws own units that belong to no squad — nothing else on
// the map represents them. Hostiles are the contact layer's business, so this
// pass never leaks a unit the player has not detected.
func drawSoloistUnits(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.UnitFilter == nil || ctx.SquadMemberMap == nil {
		return
	}
	q := ctx.UnitFilter.Query()
	for q.Next() {
		pos, _, _ := q.Get()
		ent := q.Entity()
		if ctx.SquadMemberMap.Has(ent) {
			continue
		}
		if unitAffiliation(ctx, ent) != components.AffilFriend {
			continue
		}
		p := MapWorldToPanel(*pos, ctx.Cam, content)
		col := symbolPalette[components.AffilFriend].Fill
		rl.DrawCircleV(p, soloistDotR, col)
		rl.DrawCircleLines(int32(p.X), int32(p.Y), soloistDotR,
			symbolPalette[components.AffilFriend].Outline)
		if isSelectedEntity(ctx.Selected, ent) {
			rl.DrawCircleLines(int32(p.X), int32(p.Y), soloistDotR+3, mapSelectionRing)
		}
		if ctx.Hovered == ent {
			rl.DrawCircleLines(int32(p.X), int32(p.Y), soloistDotR+5, mapHoverRing)
		}
	}
}

// PickOwnEntityAt hit-tests the individual-unit layer: vehicle symbols first
// (they are the bigger, heavier target), then soloist soldier dots. Squad
// members are not offered — their marker is the squad's, which PickSquadAt
// owns. Zero entity when nothing is within pickRadiusPx.
func PickOwnEntityAt(screenPos rl.Vector2, ctx MapRenderCtx, panel Panel,
	pickRadiusPx float32) ecs.Entity {
	content := ContentRect(panel)
	if !pointInRect(screenPos, content) {
		return ecs.Entity{}
	}
	var best ecs.Entity
	bestD := pickRadiusPx * pickRadiusPx
	test := func(p rl.Vector2, ent ecs.Entity) {
		dx := p.X - screenPos.X
		dy := p.Y - screenPos.Y
		if d := dx*dx + dy*dy; d < bestD {
			bestD = d
			best = ent
		}
	}
	if ctx.VehicleFilter != nil {
		q := ctx.VehicleFilter.Query()
		for q.Next() {
			pos, _ := q.Get()
			ent := q.Entity()
			if unitAffiliation(ctx, ent) != components.AffilFriend {
				continue
			}
			test(MapWorldToPanel(*pos, ctx.Cam, content), ent)
		}
	}
	if ctx.AircraftFilter != nil {
		q := ctx.AircraftFilter.Query()
		for q.Next() {
			pos, _ := q.Get()
			ent := q.Entity()
			if unitAffiliation(ctx, ent) != components.AffilFriend {
				continue
			}
			test(MapWorldToPanel(*pos, ctx.Cam, content), ent)
		}
	}
	if ctx.UnitFilter != nil && ctx.SquadMemberMap != nil {
		q := ctx.UnitFilter.Query()
		for q.Next() {
			pos, _, _ := q.Get()
			ent := q.Entity()
			if ctx.SquadMemberMap.Has(ent) {
				continue
			}
			if unitAffiliation(ctx, ent) != components.AffilFriend {
				continue
			}
			test(MapWorldToPanel(*pos, ctx.Cam, content), ent)
		}
	}
	return best
}

// unitAffiliation reads the unit's own faction; legacy spawns without one
// fall through to the player faction, matching the selection path.
func unitAffiliation(ctx MapRenderCtx, ent ecs.Entity) components.Affiliation {
	if ctx.FactionMap == nil {
		return components.AffilFriend
	}
	if f := ctx.FactionMap.Get(ent); f != nil {
		return AffiliationForFaction(f.ID)
	}
	return components.AffilFriend
}

// drawOwnAircraft mirrors drawOwnVehicles for airframes. The map is the
// command surface for anything that operates over kilometres, so an aircraft
// that only exists in the 3D view is an aircraft the player cannot command
// once it leaves the camera.
func drawOwnAircraft(content rl.Rectangle, ctx MapRenderCtx) {
	if ctx.AircraftFilter == nil {
		return
	}
	q := ctx.AircraftFilter.Query()
	for q.Next() {
		pos, _ := q.Get()
		ent := q.Entity()
		if unitAffiliation(ctx, ent) != components.AffilFriend {
			continue
		}
		p := MapWorldToPanel(*pos, ctx.Cam, content)
		// Same builder the contact layer uses, so an airframe of one's own and
		// a detected one wear the same glyph. Hand-building the spec here left
		// Icon at zero and drew a featureless friendly rectangle — present on
		// the map and indistinguishable from everything else on it.
		spec := DefaultSpecForDimension(components.AffilFriend, components.DimAirClass)
		DrawSymbol(spec, p, vehicleSymbolHalf, 1.0)
		bounds := SymbolBounds(spec.Affiliation, p, vehicleSymbolHalf)
		if isSelectedEntity(ctx.Selected, ent) {
			rl.DrawRectangleLinesEx(InflateRect(bounds, 2), 1.5, mapSelectionRing)
		}
		if ctx.Hovered == ent {
			rl.DrawRectangleLinesEx(InflateRect(bounds, 4), 1.5, mapHoverRing)
		}
	}
}
