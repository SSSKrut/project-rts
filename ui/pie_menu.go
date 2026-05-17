package ui

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// PieMenu is the radial RMB-hold UI from PHASE-11.md P12. The player holds
// RMB; after `HoldThreshold` the menu activates around the press position.
// Cursor angle from the centre selects a segment; release commits it. Tap
// (release before threshold) bypasses the menu — caller falls back to the
// hit-test resolver.
//
// Phase 11 has 5 segments (one per OrderKindCode). The kind enum order is
// the segment order (clockwise from "up"); changing the enum changes the
// menu layout.
type PieMenu struct {
	Active   bool
	Origin   rl.Vector2 // cursor at press time, == centre of the menu
	StartedAt time.Time
	// Target is the WorldPos that was under the cursor at press time. Saved
	// so the player can sweep cursor outward to pick a segment without
	// losing the target lock.
	Target components.WorldPos
	// SourcePanel records which panel started the press, so map releases
	// can route through map coordinate flow and 3D releases through 3D.
	SourcePanel PanelID

	// Phase 13.6 M13.6.4: facing-drag.
	//
	// HasSelection is set by Begin to the current selection count > 0; drives
	// the drag-disambig branch in Tick — without a squad to face, drag still
	// reclassifies as camera-orbit (legacy behaviour).
	//
	// InFacingDrag is true once the cursor has moved past DragCancelThreshold
	// AND HasSelection — RMB-drag is committed to facing input rather than
	// camera orbit. SourcePanel stays non-None so OrbitSystem keeps its hands
	// off (gated by main.go through OrbitInputEnabled).
	HasSelection bool
	InFacingDrag bool

	// Phase 13.6 M13.6.5: while Active, HoveringKind tracks which segment is
	// currently under the cursor (matches the value Draw highlights). Caller
	// reads this to swap the ghost preview to a kind-specific visualisation —
	// e.g. DefendPosition draws a sector arc indicator. Zero / not-Active
	// means no hover info available.
	HoveringKind  components.OrderKindCode
	HoveringValid bool
}

// HoldThreshold is how long RMB must stay down before the menu opens. Below
// the threshold, RMB-release falls back to the tap path (hit-test resolver).
const HoldThreshold = 200 * time.Millisecond

// DragCancelThreshold (in screen pixels) is the cursor displacement that
// reclassifies a held RMB as a camera-orbit drag instead of a pie-menu hold.
// Above this, the pie state is abandoned and OrbitSystem regains the input.
const DragCancelThreshold float32 = 8

// PieInnerRadius / PieOuterRadius are the menu's geometry in screen px. The
// inner zone is the "cancel" region — release with the cursor inside the
// inner ring means no order. Outer ring is the visual cap.
const (
	PieInnerRadius float32 = 28
	PieOuterRadius float32 = 110
)

// pieSegments is the canonical kind ordering for the menu. Index = segment
// slot, starting at the top and going clockwise.
var pieSegments = []components.OrderKindCode{
	components.OrderKindMoveTo,
	components.OrderKindGarrison,
	components.OrderKindOccupyTrench,
	components.OrderKindDefendPosition,
	components.OrderKindPatrol,
	// Phase 14 M14.4: SuppressFire — drench a sector with fire. Commit
	// resolves with terrain Pos (entity zero); AmmoCap / Radius come from
	// the per-order OrderParamSuppress, which IssueOrder default-fills
	// when the param is missing.
	components.OrderKindSuppressFire,
}

// Begin records the press position and target. Caller invokes this on
// IsMouseButtonPressed(MouseButtonRight). Doesn't open the menu yet —
// Tick(cursor, isDown) does that once the hold threshold passes.
//
// Phase 13.6 M13.6.4: hasSelection flips the drag-disambig branch — when a
// squad is selected, a drag past DragCancelThreshold commits to facing-input
// (InFacingDrag) instead of falling through to camera-orbit. Caller must keep
// passing the current selection state; capturing it at press time matches
// the modifier-snapshot semantics used elsewhere.
func (m *PieMenu) Begin(origin rl.Vector2, target components.WorldPos, source PanelID, hasSelection bool) {
	m.Origin = origin
	m.StartedAt = time.Now()
	m.Target = target
	m.SourcePanel = source
	m.Active = false
	m.HasSelection = hasSelection
	m.InFacingDrag = false
}

// PieTickResult bundles the per-frame outcome. ReleasedAsTap is true when
// RMB was released without the menu activating AND with little cursor drift
// — caller runs the hit-test path. ReleasedAsDrag means cursor moved enough
// to reclassify as a camera-orbit drag (caller does nothing — orbit already
// happened). ReleasedAsCommit means the player chose `Kind`. Cancelled means
// they released over the centre cancel zone.
//
// Phase 13.6 M13.6.4: ReleasedAsFacingDrag means HasSelection was true and the
// cursor moved past the drag threshold — drag committed to facing input.
// FacingYaw carries the screen-derived yaw at release (atan2(dx, -dy), so
// 0 = up, increases clockwise — matches Motion.Yaw / FormationOffset
// rotation around +Y).
type PieTickResult struct {
	ReleasedAsTap        bool
	ReleasedAsDrag       bool
	ReleasedAsCommit     bool
	ReleasedAsFacingDrag bool
	Cancelled            bool
	Kind                 components.OrderKindCode
	FacingYaw            float32
}

// Tick advances the state machine. PHASE-11.md P12 + drag reclassification:
//
//   - Cursor moved beyond DragCancelThreshold while held → reclassify as a
//     camera orbit drag; reset state and let OrbitSystem take RMB back over.
//   - Held >= HoldThreshold without dragging → activate menu.
//   - Release with menu active → commit chosen segment (or Cancelled if in
//     the inner zone).
//   - Release without menu and no drag → tap; caller runs hit-test resolver.
//   - Release after drag reclass → drop; caller does nothing.
//
// Called every frame while SourcePanel != PanelNone.
func (m *PieMenu) Tick(cursor rl.Vector2, rmbDown, rmbReleased bool) PieTickResult {
	// Drag reclass: once the cursor moves far enough, route the input.
	// Phase 13.6 M13.6.4: when HasSelection is true, drag commits to
	// facing-input (InFacingDrag) instead of releasing the pie to camera
	// orbit. The pie remains "owned" (SourcePanel stays set) so OrbitSystem
	// stays gated off until release.
	if rmbDown && !m.Active && !m.InFacingDrag {
		dx := cursor.X - m.Origin.X
		dy := cursor.Y - m.Origin.Y
		if dx*dx+dy*dy > DragCancelThreshold*DragCancelThreshold {
			if m.HasSelection {
				m.InFacingDrag = true
				// Fall through to release handling below — facing-drag is the
				// new active mode.
			} else {
				m.SourcePanel = PanelNone
				m.Active = false
				if rmbReleased {
					return PieTickResult{ReleasedAsDrag: true}
				}
				return PieTickResult{ReleasedAsDrag: false} // drag in progress
			}
		}
	}

	if !m.Active && !m.InFacingDrag && rmbDown && time.Since(m.StartedAt) >= HoldThreshold {
		m.Active = true
	}

	// Phase 13.6 M13.6.5: track which segment the cursor is over so the ghost
	// preview can adapt while the pie is open. Drop the hover when the pie
	// isn't active (no menu = no segments to hover).
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
		// Compute yaw from cursor delta. Screen Y grows downward, so we
		// flip dy: atan2(dx, -dy) yields 0 = up (away from camera = +Z),
		// increasing clockwise. Matches Motion.Yaw / FormationOffset
		// convention. P-note in PHASE-13.6.md "Facing-drag yaw conversion":
		// this screen-space approximation works because the camera looks
		// down at the ground; if camera angles ever produce wrong-feel
		// facing, we'll project both cursor positions to world space.
		dx := cursor.X - m.Origin.X
		dy := cursor.Y - m.Origin.Y
		yaw := float32(math.Atan2(float64(dx), float64(-dy)))
		m.Reset()
		return PieTickResult{ReleasedAsFacingDrag: true, FacingYaw: yaw}
	}
	if !m.Active {
		// Released without menu and without drag = tap.
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

// Active state? helper.
func (m *PieMenu) IsActive() bool { return m.Active }

// Reset clears state. Idempotent; safe to call after Tick.
func (m *PieMenu) Reset() {
	m.Active = false
	m.SourcePanel = PanelNone
	m.InFacingDrag = false
	m.HasSelection = false
	m.HoveringValid = false
}

// segmentAt returns the kind under the cursor (or (0, false) if inside the
// inner cancel zone).
func (m *PieMenu) segmentAt(cursor rl.Vector2) (components.OrderKindCode, bool) {
	dx := cursor.X - m.Origin.X
	dy := cursor.Y - m.Origin.Y
	rSq := dx*dx + dy*dy
	if rSq < PieInnerRadius*PieInnerRadius {
		return 0, false
	}
	// atan2(dx, -dy) gives angle from "up" (negative Y) going clockwise. Map
	// the [-π, π) range to [0, 2π).
	ang := math.Atan2(float64(dx), float64(-dy))
	if ang < 0 {
		ang += 2 * math.Pi
	}
	segCount := float64(len(pieSegments))
	segArc := 2 * math.Pi / segCount
	// Rotate so segment 0 is centred at angle 0 (up).
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

// Draw paints the radial menu when active. Caller invokes this after panel
// content + chrome so the menu sits on top of everything.
func (m *PieMenu) Draw(font rl.Font, cursor rl.Vector2) {
	if !m.Active {
		return
	}
	// Background disk.
	rl.DrawCircleV(m.Origin, PieOuterRadius, pieRingBG)
	rl.DrawCircleLines(int32(m.Origin.X), int32(m.Origin.Y), PieOuterRadius, pieRingBorder)
	rl.DrawCircleV(m.Origin, PieInnerRadius, rl.Color{R: 8, G: 10, B: 14, A: 240})
	rl.DrawCircleLines(int32(m.Origin.X), int32(m.Origin.Y), PieInnerRadius, pieRingBorder)

	// Highlight the segment under cursor.
	hovered, hasHover := m.segmentAt(cursor)
	segCount := float64(len(pieSegments))
	segArc := 2 * math.Pi / segCount
	for i, k := range pieSegments {
		// Centre angle for this segment (clockwise from up).
		centerAng := float64(i) * segArc
		labelDist := float32((PieInnerRadius + PieOuterRadius) * 0.5)
		lx := m.Origin.X + labelDist*float32(math.Sin(centerAng))
		ly := m.Origin.Y - labelDist*float32(math.Cos(centerAng))

		col := pieSegDim
		if hasHover && hovered == k {
			// Highlight wedge — approximate with a translucent ring slice
			// drawn via DrawCircleSector (raylib has DrawCircleSector? if
			// not, fall back to a translucent disk under the label).
			drawPieWedge(m.Origin, PieInnerRadius+2, PieOuterRadius-2,
				centerAng-segArc*0.5, centerAng+segArc*0.5, pieHover)
			col = pieSegLabel
		}

		label := pieKindLabel(k)
		// Centre the text at (lx, ly).
		const fs int32 = 13
		w := rl.MeasureTextEx(font, label, float32(fs), 1).X
		rl.DrawTextEx(font, label,
			rl.Vector2{X: lx - w*0.5, Y: ly - float32(fs)*0.5},
			float32(fs), 1, col)
	}

	// Inner "cancel" affordance — small × if cursor in cancel zone.
	if !hasHover {
		const fs int32 = 12
		txt := "cancel"
		w := rl.MeasureTextEx(font, txt, float32(fs), 1).X
		rl.DrawTextEx(font, txt,
			rl.Vector2{X: m.Origin.X - w*0.5, Y: m.Origin.Y - float32(fs)*0.5},
			float32(fs), 1, pieCancelText)
	}
}

// drawPieWedge approximates a ring sector. raylib's DrawCircleSector handles
// the case but uses the (centre, radius) signature for a full disk slice;
// we want an annulus slice, so render as a fan of triangles between the two
// radii.
func drawPieWedge(centre rl.Vector2, rIn, rOut float32, angStart, angEnd float64, col rl.Color) {
	const steps = 16
	for i := 0; i < steps; i++ {
		t0 := float64(i) / steps
		t1 := float64(i+1) / steps
		a0 := angStart + (angEnd-angStart)*t0
		a1 := angStart + (angEnd-angStart)*t1
		// Vertices of the trapezoid for this sliver.
		sa0, ca0 := math.Sin(a0), math.Cos(a0)
		sa1, ca1 := math.Sin(a1), math.Cos(a1)
		p0In := rl.Vector2{X: centre.X + rIn*float32(sa0), Y: centre.Y - rIn*float32(ca0)}
		p1In := rl.Vector2{X: centre.X + rIn*float32(sa1), Y: centre.Y - rIn*float32(ca1)}
		p0Out := rl.Vector2{X: centre.X + rOut*float32(sa0), Y: centre.Y - rOut*float32(ca0)}
		p1Out := rl.Vector2{X: centre.X + rOut*float32(sa1), Y: centre.Y - rOut*float32(ca1)}
		// Two triangles. raylib expects CCW winding to fill; if your draw
		// shows nothing flip the order.
		rl.DrawTriangle(p0In, p1Out, p0Out, col)
		rl.DrawTriangle(p0In, p1In, p1Out, col)
	}
}

func pieKindLabel(k components.OrderKindCode) string {
	switch k {
	case components.OrderKindMoveTo:
		return "Move"
	case components.OrderKindGarrison:
		return "Garrison"
	case components.OrderKindOccupyTrench:
		return "Trench"
	case components.OrderKindDefendPosition:
		return "Defend"
	case components.OrderKindPatrol:
		return "Patrol"
	case components.OrderKindSuppressFire:
		return "Suppress"
	case components.OrderKindAttackTarget:
		return "Attack"
	}
	return "?"
}
