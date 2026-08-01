package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Plan timeline: squad rows on Y, time on X. Order blocks span
// [StartT..EndT]; a "now" line marks current game-time.

type TimelineSquadRow struct {
	Squad   ecs.Entity
	Name    string
	Color   rl.Color
	Members int
	Orders  []TimelineOrderBlock
}

type TimelineOrderBlock struct {
	Order     ecs.Entity
	KindCode  components.OrderKindCode
	StateCode components.OrderStateCode
	StartT    float32 // game-time seconds (IssuedAt)
	EndT      float32 // game-time seconds (StartT + estimated duration)
	Progress  float32 // 0..1
	IsHead    bool
}

// TimelineData is the per-frame snapshot for DrawTimelinePanel.
type TimelineData struct {
	Rows []TimelineSquadRow
	NowT float32
}

type TimelineViewState struct {
	OffsetT      float32
	PixelsPerSec float32
	Follow       bool
	// LabelW is the squad-list column width; 0 reads as the default.
	LabelW float32
	// ScrollY scrolls the squad rows; both halves of a row share it, so the
	// list and its tracks can never drift apart.
	ScrollY float32
}

func NewTimelineView() TimelineViewState {
	return TimelineViewState{
		OffsetT:      0,
		PixelsPerSec: 8,
		Follow:       true,
		LabelW:       timelineLabelW,
	}
}

// TimelineLabelWidth resolves the gutter width, defaulting a zero value so
// a view restored from an older state still lays out.
func TimelineLabelWidth(view TimelineViewState) float32 {
	if view.LabelW <= 0 {
		return timelineLabelW
	}
	return view.LabelW
}

// TimelineRowsRect is the band the squad rows live in: below the header,
// above the horizontal scrollbar, left of the vertical one.
func TimelineRowsRect(panel Panel, view TimelineViewState) rl.Rectangle {
	c := ContentRect(panel)
	h := c.Height - timelineHeaderH - timelineScrollThick
	if h < 0 {
		h = 0
	}
	w := c.Width - timelineScrollThick
	if w < 0 {
		w = 0
	}
	return rl.Rectangle{X: c.X, Y: c.Y + timelineHeaderH, Width: w, Height: h}
}

// TimelineVScrollRect is the row slider, on the panel's right edge.
func TimelineVScrollRect(panel Panel, view TimelineViewState) rl.Rectangle {
	c := ContentRect(panel)
	rows := TimelineRowsRect(panel, view)
	return rl.Rectangle{
		X: c.X + c.Width - timelineScrollThick, Y: rows.Y,
		Width: timelineScrollThick, Height: rows.Height,
	}
}

// TimelineHScrollRect is the time slider, under the tracks.
func TimelineHScrollRect(panel Panel, view TimelineViewState) rl.Rectangle {
	c := ContentRect(panel)
	x := c.X + TimelineLabelWidth(view)
	w := c.X + c.Width - timelineScrollThick - x
	if w < 0 {
		w = 0
	}
	return rl.Rectangle{
		X: x, Y: c.Y + c.Height - timelineScrollThick,
		Width: w, Height: timelineScrollThick,
	}
}

// TimelineOverGutter routes the wheel: over the squad list it scrolls rows,
// over the tracks it pans time.
func TimelineOverGutter(panel Panel, view TimelineViewState, cursor rl.Vector2) bool {
	c := ContentRect(panel)
	return pointInRect(cursor, c) && cursor.X < c.X+TimelineLabelWidth(view)
}

// timelineTimeSpan is the scrollable time range: from mission start to the
// last planned block, with a margin so the tail is reachable.
func timelineTimeSpan(data TimelineData) (float32, float32) {
	maxT := data.NowT
	for _, r := range data.Rows {
		for _, o := range r.Orders {
			if o.EndT > maxT {
				maxT = o.EndT
			}
		}
	}
	return 0, maxT + 15
}

func timelineRowsHeight(data TimelineData) float32 {
	return float32(len(data.Rows)) * timelineRowH
}

// TimelineScrollYMax is 0 when every row already fits.
func TimelineScrollYMax(panel Panel, view TimelineViewState, data TimelineData) float32 {
	over := timelineRowsHeight(data) - TimelineRowsRect(panel, view).Height
	if over < 0 {
		return 0
	}
	return over
}

// ScrollTimelineRows moves the row viewport by dyPx and clamps it.
func ScrollTimelineRows(panel Panel, view *TimelineViewState, data TimelineData, dyPx float32) {
	view.ScrollY += dyPx
	max := TimelineScrollYMax(panel, *view, data)
	if view.ScrollY > max {
		view.ScrollY = max
	}
	if view.ScrollY < 0 {
		view.ScrollY = 0
	}
}

// TimelineVScrollThumb returns a zero rect when there is nothing to scroll.
func TimelineVScrollThumb(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	track := TimelineVScrollRect(panel, view)
	total := timelineRowsHeight(data)
	if total <= track.Height || track.Height <= 0 {
		return rl.Rectangle{}
	}
	h := track.Height * track.Height / total
	if h < timelineThumbMin {
		h = timelineThumbMin
	}
	frac := view.ScrollY / (total - track.Height)
	frac = clampUnit(frac)
	return rl.Rectangle{
		X: track.X, Y: track.Y + (track.Height-h)*frac,
		Width: track.Width, Height: h,
	}
}

// TimelineHScrollThumb mirrors the vertical one over the time axis.
func TimelineHScrollThumb(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	track := TimelineHScrollRect(panel, view)
	t0, t1 := timelineTimeSpan(data)
	if view.PixelsPerSec <= 0 || track.Width <= 0 {
		return rl.Rectangle{}
	}
	visible := track.Width / view.PixelsPerSec
	span := t1 - t0
	if span <= visible {
		return rl.Rectangle{}
	}
	w := track.Width * visible / span
	if w < timelineThumbMin {
		w = timelineThumbMin
	}
	frac := clampUnit((view.OffsetT - t0) / (span - visible))
	return rl.Rectangle{
		X: track.X + (track.Width-w)*frac, Y: track.Y,
		Width: w, Height: track.Height,
	}
}

// SetTimelineOffsetFromThumb maps a thumb-left position back to a time and
// drops Follow — grabbing the slider means the player took the wheel.
func SetTimelineOffsetFromThumb(panel Panel, view *TimelineViewState, data TimelineData, thumbX float32) {
	track := TimelineHScrollRect(panel, *view)
	thumb := TimelineHScrollThumb(panel, *view, data)
	if thumb.Width <= 0 || track.Width <= thumb.Width {
		return
	}
	t0, t1 := timelineTimeSpan(data)
	visible := track.Width / view.PixelsPerSec
	frac := clampUnit((thumbX - track.X) / (track.Width - thumb.Width))
	view.OffsetT = t0 + frac*((t1-t0)-visible)
	view.Follow = false
}

// SetTimelineScrollFromThumb is the vertical twin.
func SetTimelineScrollFromThumb(panel Panel, view *TimelineViewState, data TimelineData, thumbY float32) {
	track := TimelineVScrollRect(panel, *view)
	thumb := TimelineVScrollThumb(panel, *view, data)
	if thumb.Height <= 0 || track.Height <= thumb.Height {
		return
	}
	frac := clampUnit((thumbY - track.Y) / (track.Height - thumb.Height))
	view.ScrollY = frac * TimelineScrollYMax(panel, *view, data)
}

func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// TimelineDividerRect is the grab band between the squad list and the tracks.
func TimelineDividerRect(panel Panel, view TimelineViewState) rl.Rectangle {
	c := ContentRect(panel)
	x := c.X + TimelineLabelWidth(view)
	return rl.Rectangle{
		X: x - timelineDividerGrab, Y: c.Y,
		Width: timelineDividerGrab * 2, Height: c.Height,
	}
}

// ClampTimelineLabelW keeps both halves usable: the list never eats the
// tracks, and never shrinks below a readable name column.
func ClampTimelineLabelW(panel Panel, w float32) float32 {
	c := ContentRect(panel)
	max := c.Width * 0.6
	if max < timelineLabelMinW {
		max = timelineLabelMinW
	}
	if w < timelineLabelMinW {
		w = timelineLabelMinW
	}
	if w > max {
		w = max
	}
	return w
}

const (
	timelineHeaderH      float32 = 26
	timelineLabelW       float32 = 150
	timelineLabelMinW    float32 = 90
	timelineDividerGrab  float32 = 4
	timelineScrollThick  float32 = 8
	timelineThumbMin     float32 = 24
	timelineRowH         float32 = 32
	timelineRowPad       float32 = 4
	TimelineMinPxPerSec  float32 = 2
	TimelineMaxPxPerSec  float32 = 40
	timelineFollowAnchor float32 = 0.70 // now-line position while following
)

var (
	timelineBG          = rl.Color{R: 14, G: 16, B: 20, A: 255}
	timelineGutterBG    = rl.Color{R: 20, G: 24, B: 30, A: 255}
	timelineHeaderBG    = rl.Color{R: 22, G: 26, B: 33, A: 255}
	timelineTickMaj     = rl.Color{R: 120, G: 130, B: 145, A: 255}
	timelineTickMin     = rl.Color{R: 70, G: 78, B: 90, A: 255}
	timelineText        = rl.Color{R: 220, G: 224, B: 230, A: 255}
	timelineTextDim     = rl.Color{R: 130, G: 140, B: 150, A: 255}
	timelineNowLine     = rl.Color{R: 90, G: 220, B: 255, A: 220}
	timelineRowSep      = rl.Color{R: 28, G: 32, B: 38, A: 255}
	timelineEmptyText   = rl.Color{R: 110, G: 118, B: 128, A: 255}
	timelineDivider     = rl.Color{R: 46, G: 54, B: 66, A: 255}
	timelineDividerHi   = rl.Color{R: 90, G: 130, B: 180, A: 255}
	timelineScrollTrack = rl.Color{R: 24, G: 28, B: 34, A: 255}
	timelineThumb       = rl.Color{R: 70, G: 80, B: 95, A: 230}
	timelineThumbHot    = rl.Color{R: 110, G: 140, B: 180, A: 240}
)

// Estimated block durations for display only (NOT simulation).
const (
	TimelineMoveSpeedMps        float32 = 5.0
	TimelineDefendDurationSec   float32 = 30.0
	TimelineGarrisonDurationSec float32 = 30.0
	TimelineAttackDurationSec   float32 = 30.0
	TimelinePatrolDurationSec   float32 = 30.0
	TimelineSuppressDurationSec float32 = 30.0 // matches SuppressFire spec cap
	TimelineUnknownDurationSec  float32 = 15.0
	TimelineMinBlockDurationSec float32 = 3.0 // floor so click target stays usable
)

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

// DrawTimelinePanel mutates view when Follow is enabled so the now-line
// stays parked near the right edge.
func DrawTimelinePanel(panel Panel, font rl.Font, data TimelineData, view *TimelineViewState,
	cursor rl.Vector2, dividerActive bool) {
	content := ContentRect(panel)
	rl.DrawRectangleRec(content, timelineBG)
	if content.Width <= 0 || content.Height <= 0 {
		return
	}

	labelW := TimelineLabelWidth(*view)
	if view.Follow {
		visibleSec := (content.Width - labelW) / view.PixelsPerSec
		view.OffsetT = data.NowT - visibleSec*timelineFollowAnchor
	}

	gutter := rl.Rectangle{X: content.X, Y: content.Y, Width: labelW, Height: content.Height}
	rl.DrawRectangleRec(gutter, timelineGutterBG)

	tracksX := content.X + labelW
	tracksW := content.Width - labelW

	header := rl.Rectangle{X: tracksX, Y: content.Y, Width: tracksW, Height: timelineHeaderH}
	rl.DrawRectangleRec(header, timelineHeaderBG)
	drawTimelineTicks(font, header, content.Y+content.Height, view)

	rows := TimelineRowsRect(panel, *view)
	ScrollTimelineRows(panel, view, data, 0) // re-clamp after a resize

	// The squad list is drawn before the tracks scissor opens — inside it the
	// gutter sits outside the clip rect and vanished entirely.
	rl.BeginScissorMode(int32(gutter.X), int32(rows.Y), int32(gutter.Width), int32(rows.Height))
	for i, row := range data.Rows {
		drawTimelineRowLabel(font, content, labelW, timelineRowY(rows, view, i), row)
	}
	rl.EndScissorMode()
	drawTimelineGutterHeader(font, gutter, len(data.Rows))

	rl.BeginScissorMode(int32(tracksX), int32(rows.Y),
		int32(rows.X+rows.Width-tracksX), int32(rows.Height))
	for i, row := range data.Rows {
		drawTimelineRow(font, tracksX, rows.X+rows.Width-tracksX,
			timelineRowY(rows, view, i), row, view, data.NowT)
	}
	rl.EndScissorMode()
	drawTimelineScrollbars(panel, view, data, cursor)

	div := TimelineDividerRect(panel, *view)
	divCol := timelineDivider
	if dividerActive || pointInRect(cursor, div) {
		divCol = timelineDividerHi
	}
	rl.DrawRectangle(int32(tracksX-1), int32(content.Y), 2, int32(content.Height), divCol)

	if len(data.Rows) == 0 {
		const sz int32 = 15
		msg := "No squads to display"
		m := rl.MeasureTextEx(font, msg, float32(sz), 1.0)
		rl.DrawTextEx(font, msg,
			rl.Vector2{X: tracksX + (tracksW-m.X)*0.5, Y: content.Y + content.Height*0.5 - m.Y*0.5},
			float32(sz), 1.0, timelineEmptyText)
	}

	// Drawn last so it sits on top of every block.
	nowX := timelineTimeToX(data.NowT, tracksX, view)
	if nowX >= tracksX && nowX <= tracksX+tracksW {
		rl.DrawLineEx(
			rl.Vector2{X: nowX, Y: content.Y + 2},
			rl.Vector2{X: nowX, Y: content.Y + content.Height - 2},
			2, timelineNowLine)
	}
}

// timelineRowY resolves a row's top edge, scroll included.
func timelineRowY(rows rl.Rectangle, view *TimelineViewState, idx int) float32 {
	return rows.Y + float32(idx)*timelineRowH - view.ScrollY
}

func drawTimelineRow(font rl.Font, tracksX, tracksW, rowY float32,
	row TimelineSquadRow, view *TimelineViewState, nowT float32) {
	rl.DrawLine(int32(tracksX), int32(rowY+timelineRowH),
		int32(tracksX+tracksW), int32(rowY+timelineRowH), timelineRowSep)

	for _, ord := range row.Orders {
		drawTimelineBlock(font, ord, rowY, tracksX, view, nowT)
	}
}

// drawTimelineScrollbars paints the two sliders; each hides itself when its
// axis already fits.
func drawTimelineScrollbars(panel Panel, view *TimelineViewState, data TimelineData,
	cursor rl.Vector2) {
	vTrack := TimelineVScrollRect(panel, *view)
	if vThumb := TimelineVScrollThumb(panel, *view, data); vThumb.Height > 0 {
		rl.DrawRectangleRec(vTrack, timelineScrollTrack)
		rl.DrawRectangleRec(vThumb, timelineThumbColor(cursor, vThumb))
	}
	hTrack := TimelineHScrollRect(panel, *view)
	if hThumb := TimelineHScrollThumb(panel, *view, data); hThumb.Width > 0 {
		rl.DrawRectangleRec(hTrack, timelineScrollTrack)
		rl.DrawRectangleRec(hThumb, timelineThumbColor(cursor, hThumb))
	}
}

func timelineThumbColor(cursor rl.Vector2, thumb rl.Rectangle) rl.Color {
	if pointInRect(cursor, thumb) {
		return timelineThumbHot
	}
	return timelineThumb
}

func drawTimelineGutterHeader(font rl.Font, gutter rl.Rectangle, rows int) {
	head := rl.Rectangle{X: gutter.X, Y: gutter.Y, Width: gutter.Width, Height: timelineHeaderH}
	rl.DrawRectangleRec(head, timelineHeaderBG)
	const sz int32 = 13
	label := fmt.Sprintf("Squads (%d)", rows)
	rl.DrawTextEx(font, label,
		rl.Vector2{X: head.X + 8, Y: head.Y + (timelineHeaderH-float32(sz))*0.5},
		float32(sz), 1.0, timelineTextDim)
}

// drawTimelineRowLabel is the squad-list entry: colour chip, name, and what
// the squad is doing right now — the whole point of the column.
func drawTimelineRowLabel(font rl.Font, content rl.Rectangle, labelW, rowY float32,
	row TimelineSquadRow) {
	chip := rl.Rectangle{X: content.X + 6, Y: rowY + 4, Width: 4, Height: timelineRowH - 8}
	rl.DrawRectangleRec(chip, row.Color)

	textX := chip.X + chip.Width + 6
	maxW := content.X + labelW - textX - 6
	const nameSz int32 = 14
	const subSz int32 = 12
	name := row.Name
	if name == "" {
		name = fmt.Sprintf("Squad #%X", row.Squad.ID()&0xFFF)
	}
	rl.DrawTextEx(font, clipText(font, name, float32(nameSz), maxW),
		rl.Vector2{X: textX, Y: rowY + 3}, float32(nameSz), 1.0, timelineText)
	rl.DrawTextEx(font, clipText(font, rowActivity(row), float32(subSz), maxW),
		rl.Vector2{X: textX, Y: rowY + 3 + float32(nameSz) + 1}, float32(subSz), 1.0, timelineTextDim)

	rl.DrawLine(int32(content.X), int32(rowY+timelineRowH),
		int32(content.X+labelW), int32(rowY+timelineRowH), timelineRowSep)
}

// rowActivity summarises the head order; an empty queue reads as strength
// alone so a parked squad still shows how many bodies it has.
func rowActivity(row TimelineSquadRow) string {
	for _, ord := range row.Orders {
		if !ord.IsHead {
			continue
		}
		return fmt.Sprintf("%s %d%%  %d men",
			orderKindLabel(ord.KindCode), int(ord.Progress*100), row.Members)
	}
	return fmt.Sprintf("idle  %d men", row.Members)
}

// clipText trims with an ellipsis so a narrowed column never bleeds text
// across the divider.
func clipText(font rl.Font, s string, size, maxW float32) string {
	if maxW <= 0 {
		return ""
	}
	if rl.MeasureTextEx(font, s, size, 1.0).X <= maxW {
		return s
	}
	for len(s) > 1 {
		s = s[:len(s)-1]
		if rl.MeasureTextEx(font, s+"..", size, 1.0).X <= maxW {
			return s + ".."
		}
	}
	return ""
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

	// Integer indices over multiples of minorEverySec avoid float drift
	// (a previous version had every tick render as major).
	firstMinor := int(startT / minorEverySec)
	if float32(firstMinor)*minorEverySec < startT {
		firstMinor++
	}
	lastMinor := int(endT / minorEverySec)

	const sz int32 = 13
	for i := firstMinor; i <= lastMinor; i++ {
		t := float32(i) * minorEverySec
		// Nothing happened before the mission clock started; drawing those
		// ticks stamped a row of identical "T+00:00" labels at boot.
		if t < 0 {
			continue
		}
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
			rl.DrawLine(int32(x), int32(header.Y+header.Height),
				int32(x), int32(panelBottomY), rl.Color{R: 28, G: 32, B: 38, A: 180})
		}
	}
}

// tickIntervalSec targets ~80 px between major ticks.
func tickIntervalSec(pxPerSec float32) float32 {
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

// TimelineHit: empty Order means cursor is over a row but not a block.
type TimelineHit struct {
	Squad    ecs.Entity
	Order    ecs.Entity
	HitOrder bool
}

// TimelineHitTest returns ok=false when cursor is outside the tracks area.
func TimelineHitTest(panel Panel, data TimelineData, view TimelineViewState, cursor rl.Vector2) (TimelineHit, bool) {
	content := ContentRect(panel)
	if !pointInRect(cursor, content) {
		return TimelineHit{}, false
	}
	rows := TimelineRowsRect(panel, view)
	if !pointInRect(cursor, rows) {
		return TimelineHit{}, true
	}
	tracksX := content.X + TimelineLabelWidth(view)
	rowIdx := int((cursor.Y - rows.Y + view.ScrollY) / timelineRowH)
	if rowIdx < 0 || rowIdx >= len(data.Rows) {
		return TimelineHit{}, true
	}
	row := data.Rows[rowIdx]
	hit := TimelineHit{Squad: row.Squad}
	if cursor.X < tracksX {
		return hit, true
	}
	rowY := timelineRowY(rows, &view, rowIdx)
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

// DrawTimelineTooltip is invoked only when TimelineHit.HitOrder is true.
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

// dimColor scales RGB by f (0..1) while preserving alpha.
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
