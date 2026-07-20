package ui

import (
	"fmt"
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// drawInspectorVehicle: class / HP / motion+gear / road status / faction /
// squad rows.
func drawInspectorVehicle(ctx InspectorCtx, ent ecs.Entity, x, y int32) int32 {
	if !ctx.World.Alive(ent) {
		drawText(ctx.Font, "(vehicle no longer alive)", x, y, inspectorFontSize, inspectorTextDim)
		return y + inspectorRowH
	}
	veh := ctx.VehicleMap.Get(ent)
	if veh == nil {
		return y
	}
	spec := components.SpecForVehicle(veh.Kind)
	drawText(ctx.Font, fmt.Sprintf("Vehicle #%X  %s", ent.ID()&0xFFFF, spec.Name),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH * 2

	if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
		drawText(ctx.Font, fmt.Sprintf("HP:        %.0f / %.0f", hp.Current, hp.Max),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		gear := "D"
		if mo.Speed < -0.01 {
			gear = "R"
		} else if mo.Speed < 0.01 {
			gear = "-"
		}
		yawDeg := mo.Yaw * 180.0 / math.Pi
		drawText(ctx.Font, fmt.Sprintf("Motion:    %.1f m/s [%s]  yaw %.0f°",
			float32(math.Abs(float64(mo.Speed))), gear, yawDeg),
			x, y, inspectorFontSize, inspectorText)
		y += inspectorRowH
	}
	drawText(ctx.Font, "Road:      "+roadStatusLabel(ctx, ent),
		x, y, inspectorFontSize, inspectorText)
	y += inspectorRowH
	if ctx.VehicleOverrideMap != nil {
		if ov := ctx.VehicleOverrideMap.Get(ent); ov != nil && ov.Kind != components.VehicleReflexNone {
			drawText(ctx.Font, "Reflex:    "+components.VehicleReflexLabel(ov.Kind),
				x, y, inspectorFontSize, inspectorHighlight)
			y += inspectorRowH
		}
	}
	if eq := ctx.EquipmentMap.Get(ent); eq != nil {
		for _, w := range [2]ecs.Entity{eq.Primary, eq.Secondary} {
			if w == (ecs.Entity{}) || !ctx.World.Alive(w) {
				continue
			}
			if wc := ctx.WeaponMap.Get(w); wc != nil {
				drawText(ctx.Font, fmt.Sprintf("Weapon:    %-8s ammo %d",
					components.SpecForWeapon(wc.Kind).Name, wc.Ammo),
					x, y, inspectorFontSize, inspectorText)
				y += inspectorRowH
			}
		}
	}
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(ent); f != nil {
			drawText(ctx.Font, "Faction:   "+factionLabel(f.ID),
				x, y, inspectorFontSize, inspectorText)
			y += inspectorRowH
		}
	}
	if sm := ctx.SquadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		drawText(ctx.Font, fmt.Sprintf("Squad:     #%X slot %d", sm.Squad.ID()&0xFFF, sm.SlotIndex),
			x, y, inspectorFontSize, inspectorText)
	} else {
		drawText(ctx.Font, "Squad:     - (soloist)",
			x, y, inspectorFontSize, inspectorTextDim)
	}
	return y + inspectorRowH
}

func roadStatusLabel(ctx InspectorCtx, ent ecs.Entity) string {
	if ctx.RoadFollowerMap == nil {
		return "off-road"
	}
	f := ctx.RoadFollowerMap.Get(ent)
	if f == nil || f.Edge < 0 {
		return "off-road"
	}
	kind := "road"
	if g := ctx.RoadGraph; g != nil && int(f.Edge) < len(g.Edges) {
		switch g.Edges[f.Edge].Kind {
		case components.RoadHighway:
			kind = "highway"
		case components.RoadLocal:
			kind = "local road"
		case components.RoadDirtTrack:
			kind = "dirt track"
		case components.RoadBridge:
			kind = "BRIDGE"
		}
	}
	return fmt.Sprintf("%s (edge %d, %.0f%%)", kind, f.Edge, f.T*100)
}
