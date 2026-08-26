package ui

import (
	"fmt"
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// drawInspectorVehicle: class / HP / motion+gear / road status / faction /
// squad rows.
func drawInspectorVehicle(ctx InspectorCtx, ent ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	if !ctx.World.Alive(ent) {
		TextRow(&col, &ctx.st, "(vehicle no longer alive)", ctx.st.TextDim)
		return int32(col.Y)
	}
	veh := ctx.VehicleMap.Get(ent)
	if veh == nil {
		return int32(col.Y)
	}
	spec := components.SpecForVehicle(veh.Kind)
	TextRowClipped(&col, &ctx.st,
		fmt.Sprintf("Vehicle #%X  %s", ent.ID()&0xFFFF, spec.Name), ctx.st.Text)
	col.Skip(ctx.st.RowH)

	if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
		TextRowClipped(&col, &ctx.st,
			fmt.Sprintf("HP:        %.0f / %.0f", hp.Current, hp.Max), ctx.st.Text)
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		gear := "D"
		if mo.Speed < -0.01 {
			gear = "R"
		} else if mo.Speed < 0.01 {
			gear = "-"
		}
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Motion:    %.1f m/s [%s]  yaw %.0f deg",
			float32(math.Abs(float64(mo.Speed))), gear, mo.Yaw*180.0/math.Pi), ctx.st.Text)
	}
	TextRowClipped(&col, &ctx.st, "Road:      "+roadStatusLabel(ctx, ent), ctx.st.Text)
	if ctx.VehicleOverrideMap != nil {
		if ov := ctx.VehicleOverrideMap.Get(ent); ov != nil && ov.Kind != components.VehicleReflexNone {
			TextRowClipped(&col, &ctx.st,
				"Reflex:    "+components.VehicleReflexLabel(ov.Kind), inspectorHighlight)
		}
	}
	if eq := ctx.EquipmentMap.Get(ent); eq != nil {
		for _, w := range [2]ecs.Entity{eq.Primary, eq.Secondary} {
			if w == (ecs.Entity{}) || !ctx.World.Alive(w) {
				continue
			}
			if wc := ctx.WeaponMap.Get(w); wc != nil {
				TextRowClipped(&col, &ctx.st, fmt.Sprintf("Weapon:    %-8s ammo %d",
					components.SpecForWeapon(wc.Kind).Name, wc.Ammo), ctx.st.Text)
			}
		}
	}
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(ent); f != nil {
			TextRowClipped(&col, &ctx.st, "Faction:   "+factionLabel(f.ID), ctx.st.Text)
		}
	}
	if sm := ctx.SquadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		TextRowClipped(&col, &ctx.st, fmt.Sprintf("Squad:     #%X slot %d",
			sm.Squad.ID()&0xFFF, sm.SlotIndex), ctx.st.Text)
		// A member answers over its squad's net, not its own set.
		if cs := ctx.CommsMap.Get(sm.Squad); cs != nil {
			vehicleRadioRow(&col, &ctx, cs)
		}
	} else {
		TextRowClipped(&col, &ctx.st, "Squad:     - (soloist)", ctx.st.TextDim)
		if cs := ctx.CommsMap.Get(ent); cs != nil {
			vehicleRadioRow(&col, &ctx, cs)
		}
	}
	return int32(col.Y)
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

func vehicleRadioRow(col *Column, ctx *InspectorCtx, cs *components.CommsState) {
	TextRowClipped(col, &ctx.st, fmt.Sprintf("Radio:     %-9s %.0f%%",
		cs.Band.String(), cs.Quality*100), CommsBandColor(cs.Band, &ctx.st))
}
