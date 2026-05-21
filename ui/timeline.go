package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Phase 18 plan timeline. Squad rows on Y, time on X (left = past, right =
// future). Order blocks span [StartT..EndT]; a "now" line marks current
// game-time. Click on a block fires TimelineHitTest -> caller jumps camera +
// re-selects the squad.

// TimelineSquadRow is one squad's strip.
type TimelineSquadRow struct {
	Squad  ecs.Entity
	Name   string
	Color  rl.Color
	Orders []TimelineOrderBlock
}

// TimelineOrderBlock is one order rendered as a coloured rectangle.
type TimelineOrderBlock struct {
	Order     ecs.Entity
	KindCode  components.OrderKindCode
	StateCode components.OrderStateCode
	StartT    float32 // game-time seconds (IssuedAt)
	EndT      float32 // game-time seconds (StartT + estimated duration)
	Progress  float32 // 0..1
	IsHead    bool    // true for the current head of the queue
}

// TimelineData is the per-frame snapshot main.go builds and hands to
// DrawTimelinePanel. Pure data, no ECS handles inside the draw call.
type TimelineData struct {
	Rows []TimelineSquadRow
	NowT float32 // current game-time seconds (T+ on the toolbar)
}

// TimelineViewState owns the pan/zoom of the timeline panel. Mutated by the
// draw call (auto-follow) and by mouse-wheel input from main.go.
type TimelineViewState struct {
	OffsetT      float32 // seconds, left edge of timeline content
	PixelsPerSec float32 // zoom level
	Follow       bool    // when true, OffsetT auto-advances with NowT
}

// NewTimelineView returns sensible defaults: 8 px/s (= 1 min per 480 px),
// follow enabled (now-line parked near the right edge).
func NewTimelineView() TimelineViewState {
	return TimelineViewState{
		OffsetT:      0,
		PixelsPerSec: 8,
		Follow:       true,
	}
}

const (
	timelineHeaderH      float32 = 26
	timelineLabelW       float32 = 150 // left gutter with squad name
	timelineRowH         float32 = 32
	timelineRowPad       float32 = 4
	TimelineMinPxPerSec  float32 = 2
	TimelineMaxPxPerSec  float32 = 40
	timelineFollowAnchor float32 = 0.70 // now-line at 70 % of width while following
)

var (
	timelineBG        = rl.Color{R: 14, G: 16, B: 20, A: 255}
	timelineGutterBG  = rl.Color{R: 20, G: 24, B: 30, A: 255}
	timelineHeaderBG  = rl.Color{R: 22, G: 26, B: 33, A: 255}
	timelineTickMaj   = rl.Color{R: 120, G: 130, B: 145, A: 255}
	timelineTickMin   = rl.Color{R: 70, G: 78, B: 90, A: 255}
	timelineText      = rl.Color{R: 220, G: 224, B: 230, A: 255}
	timelineTextDim   = rl.Color{R: 130, G: 140, B: 150, A: 255}
	timelineNowLine   = rl.Color{R: 90, G: 220, B: 255, A: 220}
	timelineRowSep    = rl.Color{R: 28, G: 32, B: 38, A: 255}
	timelineEmptyText = rl.Color{R: 110, G: 118, B: 128, A: 255}
)

// Phase 18 estimated durations for blocks (display only, NOT simulation).
// Used by main.go when building TimelineData. Kept here so the renderer
// and snapshot agree on placeholder shape.
const (
	TimelineMoveSpeedMps          float32 = 5.0
	TimelineDefendDurationSec     float32 = 30.0
	TimelineGarrisonDurationSec   float32 = 30.0
	TimelineAttackDurationSec     float32 = 30.0
	TimelinePatrolDurationSec     float32 = 30.0
	TimelineSuppressDurationSec   float32 = 30.0 // matches SuppressFire spec cap
	TimelineUnknownDurationSec    float32 = 15.0
	TimelineMinBlockDurationSec   float32 = 3.0 // floor so click target stays usable
)

// orderKindColor returns the canonical fill colour for an order block.
// Palette echoes pie-menu / map iconography.
func orderKindColor(k components.OrderKindCode) rl.Color {
	switch k {
	case components.OrderKindMoveTo:
		return rl.Color{R: 80, G: 140, B: 220, A: 255}
	case components.OrderKindGarrison:
		return rl.Color{R: 160, G: 110, B: 200, A: 255}
	case components.OrderKindOccupyTrench:
		return rl.Color{R: 160, G: 120, B: 80, A: 255}
	case components.OrderKindDefendPosition:
		return rl.Color{R: 90, G: 180, B: 180, A: 255}
	case components.OrderKindPatrol:
		return rl.Color{R: 110, G: 180, B: 110, A: 255}
	case components.OrderKindAttackTarget:
		return rl.Color{R: 220, G: 90, B: 90, A: 255}
	case components.OrderKindSuppressFire:
		return rl.Color{R: 230, G: 150, B: 70, A: 255}
	}
	return rl.Color{R: 100, G: 100, B: 110, A: 255}
}

// DrawTimelinePanel renders the bottom timeline panel. Mutates view when
// Follow is enabled so the now-line stays parked near the right edge.
func DrawTimelinePanel(panel Panel, font rl.Font, data TimelineData, view *TimelineViewState) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, timelineBG)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}

	// Auto-follow keeps the now-line at ~70% of the visible width.
	if view.Follow {
		visibleSec := (content.Width - timelineLabelW) / view.PixelsPerSec
		view.OffsetT = data.NowT - visibleSec*timelineFollowAnchor
	}

	gutter := rl.Rectangle{X: content.X, Y: content.Y, Width: timelineLabelW, Height: content.Height}
	rl.DrawRectangleRec(gutter, timelineGutterBG)

	tracksX := content.X + timelineLabelW
	tracksW := content.Width - timelineLabelW

	header := rl.Rectangle{X: tracksX, Y: content.Y, Width: tracksW, Height: timelineHeaderH}
	rl.DrawRectangleRec(header, timelineHeaderBG)
	drawTimelineTicks(font, header, content.Y+content.Height, view)

	rl.BeginScissorMode(int32(tracksX), int32(content.Y+timelineHeaderH),
		int32(tracksW), int32(content.Height-timelineHeaderH))
	for i, row := range data.Rows {
		drawTimelineRow(font, content, tracksX, tracksW, i, row, view, data.NowT)
	}
	rl.EndScissorMode()

	if len(data.Rows) == 0 {
		const sz int32 = 15
		msg := "No squads to display"
		m := rl.MeasureTextEx(font, msg, float32(sz), 1.0)
		rl.DrawTextEx(font, msg,
			rl.Vector2{X: tracksX + (tracksW-m.X)*0.5, Y: content.Y + content.Height*0.5 - m.Y*0.5},
			float32(sz), 1.0, timelineEmptyText)
	}

	// Now-line drawn last so it sits on top of every block.
	nowX := timelineTimeToX(data.NowT, tracksX, view)
	if nowX >= tracksX && nowX <= tracksX+tracksW {
		rl.DrawLineEx(
			rl.Vector2{X: nowX, Y: content.Y + 2},
			rl.Vector2{X: nowX, Y: content.Y + content.Height - 2},
			2, timelineNowLine)
	}
}

func drawTimelineRow(font rl.Font, content rl.Rectangle, tracksX, tracksW float32,
	rowIdx int, row TimelineSquadRow, view *TimelineViewState, nowT float32) {
	rowY := content.Y + timelineHeaderH + float32(rowIdx)*timelineRowH
	if rowY > content.Y+content.Height {
		return
	}
	// Label gutter (drawn outside scissor too so it stays readable).
	chip := rl.Rectangle{X: content.X + 6, Y: rowY + 4, Width: 12, Height: timelineRowH - 8}
	rl.DrawRectangleRec(chip, row.Color)
	const sz int32 = 15
	name := row.Name
	if name == "" {
		name = fmt.Sprintf("Squad #%X", row.Squad.ID()&0xFFF)
	}
	rl.DrawTextEx(font, name,
		rl.Vector2{X: chip.X + chip.Width + 6, Y: rowY + (timelineRowH-float32(sz))*0.5},
		float32(sz), 1.0, timelineText)

	// Row separator below.
	rl.DrawLine(int32(tracksX), int32(rowY+timelineRowH),
		int32(tracksX+tracksW), int32(rowY+timelineRowH), timelineRowSep)

	for _, ord := range row.Orders {
		drawTimelineBlock(font, ord, rowY, tracksX, view, nowT)
	}
}

func drawTimelineBlock(font rl.Font, ord TimelineOrderBlock, rowY, tracksX float32,
	view *TimelineViewState, nowT float32) {
	x0 := timelineTimeToX(ord.StartT, tracksX, view)
	x1 := timelineTimeToX(ord.EndT, tracksX, view)
	if x1-x0 < 4 {
		x1 = x0 + 4
	}
	bx := rl.Rectangle{X: x0, Y: rowY + timelineRowPad, Width: x1 - x0, Height: timelineRowH - 2*timelineRowPad}

	fill := orderKindColor(ord.KindCode)
	// State overrides palette.
	switch ord.StateCode {
	case components.OrderStateBlocked, components.OrderStateFailed:
		fill = rl.Color{R: 230, G: 110, B: 80, A: 255}
	case components.OrderStateCancelled:
		fill = rl.Color{R: 100, G: 100, B: 110, A: 255}
	case components.OrderStateCompleted:
		fill = dimColor(fill, 0.55)
	}
	track := dimColor(fill, 0.35)

	rl.DrawRectangleRec(bx, track)
	prog := ord.Progress
	if prog < 0 {
		prog = 0
	}
	if prog > 1 {
		prog = 1
	}
	if prog > 0 {
		fillRect := bx
		fillRect.Width = bx.Width * prog
		rl.DrawRectangleRec(fillRect, fill)
	}
	if ord.IsHead {
		rl.DrawRectangleLinesEx(bx, 2, rl.Color{R: 230, G: 235, B: 240, A: 220})
	} else {
		rl.DrawRectangleLinesEx(bx, 1, rl.Color{R: 40, G: 46, B: 54, A: 220})
	}

	// Label inside the block if width allows.
	const labelSz int32 = 14
	label := orderKindLabel(ord.KindCode)
	m := rl.MeasureTextEx(font, label, float32(labelSz), 1.0)
	if m.X+6 < bx.Width {
		rl.DrawTextEx(font, label,
			rl.Vector2{X: bx.X + 4, Y: bx.Y + (bx.Height-m.Y)*0.5},
			float32(labelSz), 1.0, contrastTextColor(fill))
	}
	_ = nowT
}

func drawTimelineTicks(font rl.Font, header rl.Rectangle, panelBottomY float32, view *TimelineViewState) {
	if view.PixelsPerSec <= 0 {
		return
	}
	majorEverySec := tickIntervalSec(view.PixelsPerSec)
	minorPerMajor := 5
	minorEverySec := majorEverySec / float32(minorPerMajor)
	if minorEverySec <= 0 {
		minorEverySec = 1
		minorPerMajor = int(majorEverySec / minorEverySec)
	}

	startT := view.OffsetT
	endT := startT + header.Width/view.PixelsPerSec

	// Integer indices over multiples of minorEverySec - avoids float drift
	// that made every tick look major in the previous version.
	firstMinor := int(startT / minorEverySec)
	if float32(firstMinor)*minorEverySec < startT {
		firstMinor++
	}
	lastMinor := int(endT / minorEverySec)

	const sz int32 = 13
	for i := firstMinor; i <= lastMinor; i++ {
		t := float32(i) * minorEverySec
		x := timelineTimeToX(t, header.X, view)
		if x < header.X || x > header.X+header.Width {
			continue
		}
		isMajor := i%minorPerMajor == 0
		c := timelineTickMin
		yTop := header.Y + header.Height - 4
		if isMajor {
			c = timelineTickMaj
			yTop = header.Y + header.Height - 8
		}
		rl.DrawLine(int32(x), int32(yTop), int32(x), int32(header.Y+header.Height), c)
		if isMajor {
			label := timeLabel(t)
			rl.DrawTextEx(font, label,
				rl.Vector2{X: x + 3, Y: header.Y + 2},
				float32(sz), 1.0, timelineText)
			// Faint vertical guide down through the row area.
			rl.DrawLine(int32(x), int32(header.Y+header.Height),
				int32(x), int32(panelBottomY), rl.Color{R: 28, G: 32, B: 38, A: 180})
		}
	}
}

func tickIntervalSec(pxPerSec float32) float32 {
	// Aim for ~80 px between major ticks.
	target := 80.0 / pxPerSec
	steps := []float32{5, 10, 15, 30, 60, 120, 300, 600}
	for _, s := range steps {
		if s >= target {
			return s
		}
	}
	return steps[len(steps)-1]
}

func timeLabel(sec float32) string {
	if sec < 0 {
		sec = 0
	}
	mins := int(sec) / 60
	secs := int(sec) % 60
	return fmt.Sprintf("T+%02d:%02d", mins, secs)
}

func timelineTimeToX(t, tracksX float32, view *TimelineViewState) float32 {
	return tracksX + (t-view.OffsetT)*view.PixelsPerSec
}

func timelineXToTime(x, tracksX float32, view *TimelineViewState) float32 {
	if view.PixelsPerSec <= 0 {
		return view.OffsetT
	}
	return view.OffsetT + (x-tracksX)/view.PixelsPerSec
}

// TimelineHit is returned by TimelineHitTest. Empty Order means the cursor
// is over a row (or empty area) but not a block.
type TimelineHit struct {
	Squad ecs.Entity
	Order ecs.Entity
	// HitOrder is true when the cursor sits inside a block (not just the row).
	HitOrder bool
}

// TimelineHitTest finds which squad row and order block (if any) sits under
// the cursor. Returns ok=false when the cursor is outside the tracks area.
func TimelineHitTest(panel Panel, data TimelineData, view TimelineViewState, cursor rl.Vector2) (TimelineHit, bool) {
	content := ContentRect(panel)
	if !pointInRect(cursor, content) {
		return TimelineHit{}, false
	}
	tracksX := content.X + timelineLabelW
	// Cursor in the label gutter: row hit only.
	rowIdx := int((cursor.Y - content.Y - timelineHeaderH) / timelineRowH)
	if rowIdx < 0 || rowIdx >= len(data.Rows) {
		return TimelineHit{}, true
	}
	row := data.Rows[rowIdx]
	hit := TimelineHit{Squad: row.Squad}
	if cursor.X < tracksX {
		return hit, true
	}
	// Cursor in tracks: find the order whose rect contains it.
	rowY := content.Y + timelineHeaderH + float32(rowIdx)*timelineRowH
	for _, ord := range row.Orders {
		x0 := timelineTimeToX(ord.StartT, tracksX, &view)
		x1 := timelineTimeToX(ord.EndT, tracksX, &view)
		if x1-x0 < 4 {
			x1 = x0 + 4
		}
		bx := rl.Rectangle{X: x0, Y: rowY + timelineRowPad, Width: x1 - x0, Height: timelineRowH - 2*timelineRowPad}
		if pointInRect(cursor, bx) {
			hit.Order = ord.Order
			hit.HitOrder = true
			return hit, true
		}
	}
	return hit, true
}

// DrawTimelineTooltip paints a small rect at `cursor` showing the order's
// kind / state / progress. Caller invokes only when TimelineHit.HitOrder.
func DrawTimelineTooltip(font rl.Font, cursor rl.Vector2, ord TimelineOrderBlock) {
	const sz int32 = 13
	lines := [3]string{
		orderKindLabel(ord.KindCode) + "  " + orderStateLabel(ord.StateCode),
		fmt.Sprintf("issued T+%s, est %.0fs",
			shortClock(ord.StartT), ord.EndT-ord.StartT),
		fmt.Sprintf("progress %d%%", int(ord.Progress*100)),
	}
	maxW := float32(0)
	for _, l := range lines {
		w := rl.MeasureTextEx(font, l, float32(sz), 1.0).X
		if w > maxW {
			maxW = w
		}
	}
	pad := float32(6)
	rect := rl.Rectangle{
		X: cursor.X + 14, Y: cursor.Y + 14,
		Width:  maxW + 2*pad,
		Height: float32(sz+2)*float32(len(lines)) + 2*pad,
	}
	rl.DrawRectangleRec(rect, rl.Color{R: 18, G: 22, B: 28, A: 240})
	rl.DrawRectangleLinesEx(rect, 1, rl.Color{R: 80, G: 90, B: 105, A: 255})
	for i, l := range lines {
		rl.DrawTextEx(font, l,
			rl.Vector2{X: rect.X + pad, Y: rect.Y + pad + float32(i)*float32(sz+2)},
			float32(sz), 1.0, timelineText)
	}
}

func shortClock(t float32) string {
	if t < 0 {
		t = 0
	}
	mins := int(t) / 60
	secs := int(t) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

// dimColor scales RGB by `f` (0..1) while preserving alpha. Used for
// progress-bar tracks (the unfilled portion of an order block).
func dimColor(c rl.Color, f float32) rl.Color {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return rl.Color{
		R: uint8(float32(c.R) * f),
		G: uint8(float32(c.G) * f),
		B: uint8(float32(c.B) * f),
		A: c.A,
	}
}
