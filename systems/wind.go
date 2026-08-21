package systems

import "rts-go/components"

// Layered wind: mean vector from Atmosphere × slow gust modulation + a
// divergence-free swirl field. The swirl is the rotated gradient (curl) of a
// smooth noise scalar — such a field has no sources or sinks, so smoke can
// never pile up in a phantom "drain". Frozen-turbulence hypothesis: the swirl
// pattern itself rides the mean wind, which is both physical and free.
//
// Deterministic in (position, time): sim consumers pass ctx.SimNow and stay
// replay-safe; render consumers may pass the wall clock for cosmetics.

// WindGust returns the slow strength multiplier (~0.2..1.8 at Gustiness 1).
func WindGust(atm *components.Atmosphere, t float64) float32 {
	if atm == nil || atm.Gustiness <= 0 {
		return 1
	}
	g := 1 + atm.Gustiness*float32(fbm2(t*0.11, 91.7))*1.6
	if g < 0.2 {
		g = 0.2
	}
	return g
}

// WindAt returns the horizontal wind vector (m/s) at a world point.
func WindAt(atm *components.Atmosphere, wx, wz float32, t float64) (float32, float32) {
	if atm == nil {
		return 0, 0
	}
	g := WindGust(atm, t)
	bx := atm.WindX * g
	bz := atm.WindZ * g
	if atm.Turbulence <= 0.01 || atm.TurbScaleM <= 1 {
		return bx, bz
	}
	s := 1.0 / float64(atm.TurbScaleM)
	px := (float64(wx) - float64(atm.WindX)*t) * s
	pz := (float64(wz) - float64(atm.WindZ)*t) * s
	const e = 0.35
	dpx := (fbm2(px+e, pz) - fbm2(px-e, pz)) / (2 * e)
	dpz := (fbm2(px, pz+e) - fbm2(px, pz-e)) / (2 * e)
	// Rotated gradient: (∂ψ/∂z, -∂ψ/∂x). fbm2 slopes run ~±0.5, so ×2 lands
	// the swirl amplitude near Turbulence m/s.
	return bx + float32(dpz)*atm.Turbulence*2.0,
		bz + float32(-dpx)*atm.Turbulence*2.0
}
