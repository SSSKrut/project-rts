package ui

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// The airframe panel is DIALS, not buttons (Phase 20 P1). Every control here
// sets a standing value the airframe then holds by itself; none of them is an
// action, and none of them has to be repeated. That is the whole reason a
// helicopter does not need a click per second the way a steered vehicle would.
//
// It carries no singleton state for the same reason the Behavior panel does
// not: every value it edits is a component, so the workspace leaf and a floater
// showing the same airframe cannot disagree.
const (
	altStepM    float32 = 10
	altStepBigM float32 = 50
	speedStepMs float32 = 5
	dialRowGap  float32 = 3
	// The steppers are narrow and the mode chip is not: "-" and "+" need a
	// target, the third chip needs a word. Splitting them buys back the width
	// the readout was losing to a uniform row.
	dialStepWide float32 = 26
	dialModeWide float32 = 54
)

func drawInspectorAircraft(ctx InspectorCtx, ent ecs.Entity, x, y, width int32) int32 {
	col := Column{X: float32(x), Y: float32(y), W: float32(width)}
	if !ctx.World.Alive(ent) {
		TextRow(&col, &ctx.st, "(airframe no longer alive)", ctx.st.TextDim)
		return int32(col.Y)
	}
	ac := ctx.AircraftMap.Get(ent)
	if ac == nil {
		return int32(col.Y)
	}
	spec := components.SpecForAircraft(ac.Kind)
	TextRowClipped(&col, &ctx.st,
		fmt.Sprintf("Aircraft #%X  %s", ent.ID()&0xFFFF, spec.Name), ctx.st.Header)
	col.Skip(ctx.st.RowH * 0.4)

	drawAircraftState(ctx, &col, ent, ac, spec)
	col.Skip(ctx.st.RowH * 0.5)
	drawSpeedDial(ctx, &col, ac, spec)
	drawAltitudeDial(ctx, &col, ac, spec)
	col.Skip(ctx.st.RowH * 0.5)
	drawEmissionDials(ctx, &col, ent)

	if f := ctx.FactionMap.Get(ent); f != nil {
		TextRowClipped(&col, &ctx.st, "Faction:   "+factionLabel(f.ID), ctx.st.Text)
	}
	return int32(col.Y)
}

// drawAircraftState is the read-only half: what the airframe is actually
// doing, which is not the same as what it was told to do. The gap between
// "Alt set" and the flown altitude IS the feedback that the dial is working.
func drawAircraftState(ctx InspectorCtx, col *Column, ent ecs.Entity,
	ac *components.Aircraft, spec *components.AircraftSpec) {
	if hp := ctx.HPMap.Get(ent); hp != nil && hp.Max > 0 {
		TextRowClipped(col, &ctx.st,
			fmt.Sprintf("HP:        %.0f / %.0f", hp.Current, hp.Max), ctx.st.Text)
	}
	if mo := ctx.MotionMap.Get(ent); mo != nil {
		TextRowClipped(col, &ctx.st, fmt.Sprintf("Speed:     %.0f m/s   yaw %.0f deg",
			float32(math.Abs(float64(mo.Speed))), mo.Yaw*180.0/math.Pi), ctx.st.Text)
	}
	if p := ctx.PosMap.Get(ent); p != nil {
		agl := p.Local.Y
		if ctx.GroundAt != nil {
			agl -= ctx.GroundAt(*p)
		}
		TextRowClipped(col, &ctx.st, fmt.Sprintf("Altitude:  %.0f m AGL   %s",
			agl, components.AltBandOf(agl)), ctx.st.Text)
	}
	fuelCol := ctx.st.Text
	if ac.Fuel < spec.FuelSec*0.15 {
		fuelCol = inspectorHighlight
	}
	state := "on task"
	if ac.Egressing {
		state = "EGRESS"
		fuelCol = inspectorHighlight
	}
	TextRowClipped(col, &ctx.st,
		fmt.Sprintf("Fuel:      %.0f s   (%s)", ac.Fuel, state), fuelCol)
}

func drawSpeedDial(ctx InspectorCtx, col *Column, ac *components.Aircraft,
	spec *components.AircraftSpec) {
	row := col.Band(ctx.st.RowH)
	TextClipped(&ctx.st, dialReadout(row),
		fmt.Sprintf("Speed:     %.0f m/s", ac.SpeedSet), ctx.st.Text)
	if Chip(ctx.in, &ctx.st, dialChip(row, 2), "-", false) {
		ac.SpeedSet = clampDial(ac.SpeedSet-speedStepMs, 0, spec.MaxSpeed)
	}
	if Chip(ctx.in, &ctx.st, dialChip(row, 1), "+", false) {
		ac.SpeedSet = clampDial(ac.SpeedSet+speedStepMs, 0, spec.MaxSpeed)
	}
	if Chip(ctx.in, &ctx.st, dialChip(row, 0), "Cruise", ac.SpeedSet == spec.CruiseSpeed) {
		ac.SpeedSet = spec.CruiseSpeed
	}
	col.Skip(dialRowGap)
}

func drawAltitudeDial(ctx InspectorCtx, col *Column, ac *components.Aircraft,
	spec *components.AircraftSpec) {
	row := col.Band(ctx.st.RowH)
	ref := "AGL"
	if ac.AltRef == components.AltAMSL {
		ref = "AMSL"
	}
	TextClipped(&ctx.st, dialReadout(row),
		fmt.Sprintf("Alt:       %.0f m", ac.AltSet), ctx.st.Text)
	if Chip(ctx.in, &ctx.st, dialChip(row, 2), "-", false) {
		ac.AltSet = clampDial(ac.AltSet-altStep(ctx), 0, spec.CeilingM)
	}
	if Chip(ctx.in, &ctx.st, dialChip(row, 1), "+", false) {
		ac.AltSet = clampDial(ac.AltSet+altStep(ctx), 0, spec.CeilingM)
	}
	// The reference is part of the dial, not a mode elsewhere: "30 m" means
	// two different flights depending on which of these is lit.
	if Chip(ctx.in, &ctx.st, dialChip(row, 0), ref, ac.AltRef == components.AltAGL) {
		if ac.AltRef == components.AltAGL {
			ac.AltRef = components.AltAMSL
		} else {
			ac.AltRef = components.AltAGL
		}
	}
	col.Skip(dialRowGap)
}

// drawEmissionDials is the core control of the phase. Each active channel
// shows BOTH halves of its bargain on one line — what it buys and what it
// costs — because a player who cannot see the cost is not making a choice.
func drawEmissionDials(ctx InspectorCtx, col *Column, ent ecs.Entity) {
	sensors := ctx.SensorsMap.Get(ent)
	if sensors == nil {
		return
	}
	Header(col, &ctx.st, "Emissions")
	for i := uint8(0); i < sensors.Count; i++ {
		c := &sensors.Channels[i]
		on := sensors.ChannelOn(i)
		switch {
		case c.EmitRangeM > 0:
			row := col.Band(ctx.st.RowH)
			label := fmt.Sprintf("%s  sees %.0f / seen %.0f",
				c.Kind, c.BaseRangeM, c.EmitRangeM)
			if Toggle(ctx.in, &ctx.st,
				rl.Rectangle{X: row.X, Y: row.Y, Width: row.Width, Height: row.Height},
				label, on) {
				sensors.SetChannel(i, !on)
			}
			col.Skip(dialRowGap)
		case c.Kind == components.SensorESM:
			// Passive: nothing to trade, so it is stated rather than offered.
			TextRowClipped(col, &ctx.st,
				fmt.Sprintf("%-9s passive, hears %.0f m", c.Kind.String()+":", c.BaseRangeM),
				ctx.st.TextDim)
		default:
			TextRowClipped(col, &ctx.st,
				fmt.Sprintf("%-9s %.0f m", c.Kind.String()+":", c.BaseRangeM), ctx.st.TextDim)
		}
	}
}

// altStep gives the fine step normally and a coarse one while Shift is held —
// a transit climb is 1500 m and nobody should click it in tens.
func altStep(ctx InspectorCtx) float32 {
	if ctx.Shift {
		return altStepBigM
	}
	return altStepM
}

// dialChip lays chips out from the RIGHT edge, so the readout on the left can
// be any length without the buttons moving under the cursor. Index 0 is the
// mode chip, 1 and 2 the steppers.
func dialChip(row rl.Rectangle, fromRight int) rl.Rectangle {
	w := dialStepWide
	x := row.X + row.Width - dialModeWide
	switch fromRight {
	case 0:
		w = dialModeWide
	case 1:
		x -= dialStepWide + dialRowGap
	default:
		x -= 2 * (dialStepWide + dialRowGap)
	}
	return rl.Rectangle{X: x, Y: row.Y, Width: w, Height: row.Height}
}

// dialReadout is what is left of the row once the chips have taken their side.
func dialReadout(row rl.Rectangle) rl.Rectangle {
	w := row.Width - dialModeWide - 2*(dialStepWide+dialRowGap) - dialRowGap
	if w < 0 {
		w = 0
	}
	return rl.Rectangle{X: row.X, Y: row.Y, Width: w, Height: row.Height}
}

func clampDial(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
