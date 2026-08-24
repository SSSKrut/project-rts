package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// CoverageRing is one detection radius of the selection-coverage overlay.
type CoverageRing struct {
	Kind    components.SensorKind
	RadiusM float32
}

// CoverageView is the per-frame snapshot of the selected group's sensory
// reach: detection rings per live channel, the emission (being-heard) radius,
// weapon ranges and the LOS fan from the profile's own position. Root fills
// it once per frame; the map and the 3D overlay read the same copy, so the
// two surfaces cannot disagree.
type CoverageView struct {
	Active     bool
	Origin     components.WorldPos
	AirProfile bool
	Runs       [][]components.VisRun
	VisR       float32
	Falloff    components.FalloffKind
	Rings      []CoverageRing
	EmitR      float32
	EmitOrigin components.WorldPos
	WeaponRs   []float32
}

// SensorRingColor keys the ring palette: Optical matches the hold-V sensor
// ring, ESM matches the bearing dashes it produces.
func SensorRingColor(k components.SensorKind) rl.Color {
	switch k {
	case components.SensorOptical:
		return rl.Color{R: 240, G: 220, B: 80, A: 255}
	case components.SensorThermal:
		return rl.Color{R: 230, G: 140, B: 200, A: 255}
	case components.SensorRadar:
		return rl.Color{R: 110, G: 200, B: 240, A: 255}
	case components.SensorAcoustic:
		return rl.Color{R: 170, G: 170, B: 170, A: 255}
	case components.SensorESM:
		return rl.Color{R: 240, G: 190, B: 90, A: 255}
	}
	return rl.Magenta
}

// CoverageEmitColor is danger, not capability: how far the selection is heard.
var CoverageEmitColor = rl.Color{R: 235, G: 80, B: 70, A: 255}

var coverageWeaponColor = rl.Color{R: 240, G: 120, B: 80, A: 170}

// drawMapCoverage renders the persistent selection overlay: LOS fan (ground
// profiles only), dashed detection circles, emission fill, weapon circles.
// Dashed = detection, solid = weapon / emission.
func drawMapCoverage(content rl.Rectangle, ctx MapRenderCtx) {
	cov := ctx.Coverage
	if cov == nil || !cov.Active {
		return
	}
	drawMapFanRuns(content, ctx.Cam, cov.Origin, cov.Runs, cov.VisR, cov.Falloff, 24, 70)
	if cov.EmitR > 0 {
		ec := MapWorldToPanel(cov.EmitOrigin, ctx.Cam, content)
		radPx := cov.EmitR * ctx.Cam.Zoom
		fill := CoverageEmitColor
		fill.A = 16
		rl.DrawCircleV(ec, radPx, fill)
		line := CoverageEmitColor
		line.A = 190
		rl.DrawCircleLines(int32(ec.X), int32(ec.Y), radPx, line)
	}
	center := MapWorldToPanel(cov.Origin, ctx.Cam, content)
	for _, ring := range cov.Rings {
		col := SensorRingColor(ring.Kind)
		col.A = 180
		drawMapDashedCircle(center, ring.RadiusM*ctx.Cam.Zoom, col)
	}
	for _, wr := range cov.WeaponRs {
		rl.DrawCircleLines(int32(center.X), int32(center.Y), wr*ctx.Cam.Zoom, coverageWeaponColor)
	}
}

func drawMapDashedCircle(c rl.Vector2, radPx float32, col rl.Color) {
	segs := int(radPx * 0.35)
	if segs < 24 {
		segs = 24
	}
	if segs > 180 {
		segs = 180
	}
	segs &^= 1
	pt := func(i int) rl.Vector2 {
		ang := float64(i) * (2 * math.Pi / float64(segs))
		return rl.Vector2{
			X: c.X + float32(math.Sin(ang))*radPx,
			Y: c.Y + float32(math.Cos(ang))*radPx,
		}
	}
	for i := 0; i < segs; i += 2 {
		rl.DrawLineV(pt(i), pt(i+1), col)
	}
}

// drawMapFanRuns is the shared sector renderer behind the hold-V mirror and
// the coverage overlay.
func drawMapFanRuns(content rl.Rectangle, cam MapCamera, origin components.WorldPos,
	runs [][]components.VisRun, rangeM float32, falloff components.FalloffKind,
	aBase, aSpan float32) {
	if len(runs) == 0 {
		return
	}
	ox := float32(origin.Chunk.X)*components.ChunkSize + origin.Local.X
	oz := float32(origin.Chunk.Z)*components.ChunkSize + origin.Local.Z
	at := func(dx, dz float32) rl.Vector2 {
		wp := components.WorldPos{Local: rl.Vector3{X: ox + dx, Z: oz + dz}}
		return MapWorldToPanel(wp, cam, content)
	}
	rays := len(runs)
	halfStep := math.Pi / float64(rays)
	fill := rl.Color{R: 70, G: 210, B: 130}
	for r, rr := range runs {
		angC := float64(r) * (2 * math.Pi / float64(rays))
		s0 := float32(math.Sin(angC - halfStep))
		c0 := float32(math.Cos(angC - halfStep))
		s1 := float32(math.Sin(angC + halfStep))
		c1 := float32(math.Cos(angC + halfStep))
		for _, run := range rr {
			if run.T1 <= run.T0 {
				continue
			}
			a := components.Falloff(falloff, (run.T0+run.T1)*0.5, rangeM)
			col := fill
			col.A = uint8(aBase + aSpan*a)
			v00 := at(s0*run.T0, c0*run.T0)
			v01 := at(s1*run.T0, c1*run.T0)
			v10 := at(s0*run.T1, c0*run.T1)
			v11 := at(s1*run.T1, c1*run.T1)
			rl.DrawTriangle(v00, v11, v01, col)
			rl.DrawTriangle(v00, v10, v11, col)
		}
	}
}
