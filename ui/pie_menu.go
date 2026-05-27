package ui

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// PieMenu is the legacy radial RMB-hold UI; ContextMenu in context_menu.go
// has replaced it. Primitives may revive for future radial cases.
type PieMenu struct {
	Active      bool
	Origin      rl.Vector2
	StartedAt   time.Time
	Target      components.WorldPos
	SourcePanel PanelID

	HasSelection bool
	InFacingDrag bool

	HoveringKind  components.OrderKindCode
	HoveringValid bool
}

const HoldThreshold = 200 * time.Millisecond

// DragCancelThreshold reclassifies held RMB as a camera-orbit drag
// instead of a pie-menu hold.
const DragCancelThreshold float32 = 8

// Inner zone is the cancel region; outer is the visual cap.
const (
	PieInnerRadius float32 = 28
	PieOuterRadius float32 = 110
)

// pieSegments is derived from OrderKindSpecs entries with InPieMenu = true,
// sorted by PieSegmentOrder.
var pieSegments = buildPieSegments()

func buildPieSegments() []components.OrderKindCode {
	type entry struct {
		code  components.OrderKindCode
		order uint8
	}
	var entries []entry
	for i := range components.OrderKindSpecs {
		spec := &components.OrderKindSpecs[i]
		if spec.InPieMenu {
			entries = append(entries, entry{code: spec.Code, order: spec.PieSegmentOrder})
		}
	}
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].order > entries[j].order; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
	out := make([]components.OrderKindCode, len(entries))
	for i, e := range entries {
		out[i] = e.code
	}
	return out
}

// Begin records the press; Tick opens the menu once HoldThreshold passes.
// hasSelection flips drag-disambig: with a squad selected, drag past
// DragCancelThreshold commits to facing-input instead of camera-orbit.
func (m *PieMenu) Begin(origin rl.Vector2, target components.WorldPos, source PanelID, hasSelection bool) {
	m.Origin = origin
	m.StartedAt = time.Now()
	m.Target = target
	m.SourcePanel = source
	m.Active = false
	m.HasSelection = hasSelection
	m.InFacingDrag = false
}

type PieTickResult struct {
	ReleasedAsTap        bool
	ReleasedAsDrag       bool
	ReleasedAsCommit     bool
	ReleasedAsFacingDrag bool
	Cancelled            bool
	Kind                 components.OrderKindCode
	FacingYaw            float32
}

// Tick states:
//   - Cursor moved past DragCancelThreshold while held → camera-orbit
//     drag (or facing-input when HasSelection is true).
//   - Held >= HoldThreshold without dragging → activate menu.
//   - Release with menu active → commit segment (or Cancelled).
//   - Release without menu and no drag → tap; caller resolves hit-test.
func (m *PieMenu) Tick(cursor rl.Vector2, rmbDown, rmbReleased bool) PieTickResult {
	if rmbDown && !m.Active && !m.InFacingDrag {
		dx := cursor.X - m.Origin.X
		dy := cursor.Y - m.Origin.Y
		if dx*dx+dy*dy > DragCancelThreshold*DragCancelThreshold {
			if m.HasSelection {
				m.InFacingDrag = true
				// Fall through to release handling below.
			} else {
				m.SourcePanel = PanelNone
				m.Active = false
				if rmbReleased {
					return PieTickResult{ReleasedAsDrag: true}
				}
				return PieTickResult{ReleasedAsDrag: false}
			}
		}
	}

	if !m.Active && !m.InFacingDrag && rmbDown && time.Since(m.StartedAt) >= HoldThreshold {
		m.Active = true
	}

	if m.Active {
		if seg, ok := m.segmentAt(cursor); ok {
			m.HoveringKind = seg
			m.HoveringValid = true
		} else {
			m.HoveringValid = false
		}
	} else {
		m.HoveringValid = false
	}

	if !rmbReleased {
		return PieTickResult{}
	}

	if m.InFacingDrag {
		// Screen Y grows downward, so flip dy: atan2(dx, -dy) yields
		// 0 = up (= +Z away from camera), increasing clockwise. Matches
		// Motion.Yaw / FormationOffset convention.
		dx := cursor.X - m.Origin.X
		dy := cursor.Y - m.Origin.Y
		yaw := float32(math.Atan2(float64(dx), float64(-dy)))
		m.Reset()
		return PieTickResult{ReleasedAsFacingDrag: true, FacingYaw: yaw}
	}
	if !m.Active {
		m.Reset()
		return PieTickResult{ReleasedAsTap: true}
	}
	seg, ok := m.segmentAt(cursor)
	m.Reset()
	if !ok {
		return PieTickResult{Cancelled: true}
	}
	return PieTickResult{ReleasedAsCommit: true, Kind: seg}
}

func (m *PieMenu) IsActive() bool { return m.Active }

func (m *PieMenu) Reset() {
	m.Active = false
	m.SourcePanel = PanelNone
	m.InFacingDrag = false
	m.HasSelection = false
	m.HoveringValid = false
}

// segmentAt: (0, false) when the cursor sits in the inner cancel zone.
// atan2(dx, -dy) yields angle clockwise from "up" (matches FormationOffset);
// segment 0 is centred at angle 0.
func (m *PieMenu) segmentAt(cursor rl.Vector2) (components.OrderKindCode, bool) {
	dx := cursor.X - m.Origin.X
	dy := cursor.Y - m.Origin.Y
	rSq := dx*dx + dy*dy
	if rSq < PieInnerRadius*PieInnerRadius {
		return 0, false
	}
	ang := math.Atan2(float64(dx), float64(-dy))
	if ang < 0 {
		ang += 2 * math.Pi
	}
	segCount := float64(len(pieSegments))
	segArc := 2 * math.Pi / segCount
	idx := int(math.Floor((ang+segArc*0.5)/segArc)) % len(pieSegments)
	return pieSegments[idx], true
}

var (
	pieRingBG     = rl.Color{R: 14, G: 16, B: 22, A: 220}
	pieRingBorder = rl.Color{R: 80, G: 90, B: 110, A: 240}
	pieHover      = rl.Color{R: 50, G: 80, B: 110, A: 220}
	pieSegLabel   = rl.Color{R: 230, G: 235, B: 240, A: 255}
	pieSegDim     = rl.Color{R: 150, G: 155, B: 160, A: 220}
	pieCancelText = rl.Color{R: 220, G: 100, B: 80, A: 255}
)

// Draw must run after panel content + chrome so the menu sits on top.
func (m *PieMenu) Draw(font rl.Font, cursor rl.Vector2) {
	if !m.Active {
		return
	}
	rl.DrawCircleV(m.Origin, PieOuterRadius, pieRingBG)
	rl.DrawCircleLines(int32(m.Origin.X), int32(m.Origin.Y), PieOuterRadius, pieRingBorder)
	rl.DrawCircleV(m.Origin, PieInnerRadius, rl.Color{R: 8, G: 10, B: 14, A: 240})
	rl.DrawCircleLines(int32(m.Origin.X), int32(m.Origin.Y), PieInnerRadius, pieRingBorder)

	hovered, hasHover := m.segmentAt(cursor)
	segCount := float64(len(pieSegments))
	segArc := 2 * math.Pi / segCount
	for i, k := range pieSegments {
		centerAng := float64(i) * segArc
		labelDist := float32((PieInnerRadius + PieOuterRadius) * 0.5)
		lx := m.Origin.X + labelDist*float32(math.Sin(centerAng))
		ly := m.Origin.Y - labelDist*float32(math.Cos(centerAng))

		col := pieSegDim
		if hasHover && hovered == k {
			drawPieWedge(m.Origin, PieInnerRadius+2, PieOuterRadius-2,
				centerAng-segArc*0.5, centerAng+segArc*0.5, pieHover)
			col = pieSegLabel
		}

		label := pieKindLabel(k)
		const fs int32 = 15
		w := rl.MeasureTextEx(font, label, float32(fs), 1).X
		rl.DrawTextEx(font, label,
			rl.Vector2{X: lx - w*0.5, Y: ly - float32(fs)*0.5},
			float32(fs), 1, col)
	}

	if !hasHover {
		const fs int32 = 13
		txt := "cancel"
		w := rl.MeasureTextEx(font, txt, float32(fs), 1).X
		rl.DrawTextEx(font, txt,
			rl.Vector2{X: m.Origin.X - w*0.5, Y: m.Origin.Y - float32(fs)*0.5},
			float32(fs), 1, pieCancelText)
	}
}

// drawPieWedge fans triangles for an annulus slice (DrawCircleSector
// only handles full-disk slices). raylib needs CCW winding to fill.
func drawPieWedge(centre rl.Vector2, rIn, rOut float32, angStart, angEnd float64, col rl.Color) {
	const steps = 16
	for i := 0; i < steps; i++ {
		t0 := float64(i) / steps
		t1 := float64(i+1) / steps
		a0 := angStart + (angEnd-angStart)*t0
		a1 := angStart + (angEnd-angStart)*t1
		sa0, ca0 := math.Sin(a0), math.Cos(a0)
		sa1, ca1 := math.Sin(a1), math.Cos(a1)
		p0In := rl.Vector2{X: centre.X + rIn*float32(sa0), Y: centre.Y - rIn*float32(ca0)}
		p1In := rl.Vector2{X: centre.X + rIn*float32(sa1), Y: centre.Y - rIn*float32(ca1)}
		p0Out := rl.Vector2{X: centre.X + rOut*float32(sa0), Y: centre.Y - rOut*float32(ca0)}
		p1Out := rl.Vector2{X: centre.X + rOut*float32(sa1), Y: centre.Y - rOut*float32(ca1)}
		rl.DrawTriangle(p0In, p1Out, p0Out, col)
		rl.DrawTriangle(p0In, p1In, p1Out, col)
	}
}

func pieKindLabel(k components.OrderKindCode) string {
	if spec := components.SpecForOrderKind(k); spec.Name != "" {
		return spec.Name
	}
	return "?"
}
