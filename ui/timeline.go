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
	Order     ecs.Entity // zero for a historical block: the entity is gone
	Target    components.WorldPos
	KindCode  components.OrderKindCode
	StateCode components.OrderStateCode
	StartT    float32 // game-time seconds
	EndT      float32 // game-time seconds
	Progress  float32 // 0..1
	IsHead    bool

	// Historical marks a block recovered from OrderHistory. Left of the
	// now-line the extent is measured rather than estimated, and the block
	// carries how it ended.
	Historical bool
	Outcome    components.OrderOutcome
	// Started is false for an order cancelled while still queued — it has an
	// end but never ran, so it draws as a bare outcome tick.
	Started bool
}

// HistoryBlock converts a finished order into a block. A record that never ran
// has no measured extent, so it collapses onto its issue time and renders as a
// bare outcome tick rather than pretending to span anything.
func HistoryBlock(r components.OrderRecord) TimelineOrderBlock {
	startT := r.StartedT
	if !r.Started() {
		startT = r.IssuedT
	}
	return TimelineOrderBlock{
		Target:     r.Target,
		KindCode:   r.Kind,
		StartT:     startT,
		EndT:       r.EndedT,
		Progress:   r.Progress,
		Historical: true,
		Outcome:    r.Outcome,
		Started:    r.Started(),
	}
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

// Normalize repairs a view that arrived zero-valued (a fresh floater) or from
// an older persisted layout, so geometry never divides by a zero scale.
func (v *TimelineViewState) Normalize() {
	if v.PixelsPerSec < TimelineMinPxPerSec {
		v.PixelsPerSec = NewTimelineView().PixelsPerSec
	}
	if v.PixelsPerSec > TimelineMaxPxPerSec {
		v.PixelsPerSec = TimelineMaxPxPerSec
	}
	if v.LabelW <= 0 {
		v.LabelW = timelineLabelW
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

// timelineBars reports which sliders the current content needs. The order is
// fixed — H is decided first, over the full width — so the two answers can't
// chase each other's reserved strip.
func timelineBars(panel Panel, view TimelineViewState, data TimelineData) (h, v bool) {
	c := ContentRect(panel)
	if c.Width <= 0 || c.Height <= 0 {
		return false, false
	}
	if trackW := c.Width - TimelineLabelWidth(view); trackW > 0 && view.PixelsPerSec > 0 {
		t0, t1 := timelineTimeSpan(data)
		h = t1-t0 > trackW/view.PixelsPerSec
	}
	rowsH := c.Height - timelineHeaderH
	if h {
		rowsH -= timelineScrollThick
	}
	return h, timelineRowsHeight(data) > rowsH
}

// TimelineRowsRect is the band the squad rows live in: below the header, and
// clear of whichever sliders are actually on screen.
func TimelineRowsRect(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	c := ContentRect(panel)
	hBar, vBar := timelineBars(panel, view, data)
	h := c.Height - timelineHeaderH
	if hBar {
		h -= timelineScrollThick
	}
	w := c.Width
	if vBar {
		w -= timelineScrollThick
	}
	if h < 0 {
		h = 0
	}
	if w < 0 {
		w = 0
	}
	return rl.Rectangle{X: c.X, Y: c.Y + timelineHeaderH, Width: w, Height: h}
}

// TimelineVScrollRect is the row slider on the panel's right edge; zero when
// every row already fits.
func TimelineVScrollRect(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	if _, vBar := timelineBars(panel, view, data); !vBar {
		return rl.Rectangle{}
	}
	c := ContentRect(panel)
	rows := TimelineRowsRect(panel, view, data)
	return rl.Rectangle{
		X: c.X + c.Width - timelineScrollThick, Y: rows.Y,
		Width: timelineScrollThick, Height: rows.Height,
	}
}

// TimelineHScrollRect is the time slider under the tracks; zero when the whole
// span already fits.
func TimelineHScrollRect(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	hBar, vBar := timelineBars(panel, view, data)
	if !hBar {
		return rl.Rectangle{}
	}
	c := ContentRect(panel)
	x := c.X + TimelineLabelWidth(view)
	right := c.X + c.Width
	if vBar {
		right -= timelineScrollThick
	}
	w := right - x
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
	over := timelineRowsHeight(data) - TimelineRowsRect(panel, view, data).Height
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

// ClampTimelineOffset keeps a hand-panned view over the content. Follow writes
// OffsetT directly and is deliberately exempt — parking the now-line 30 % from
// the right edge means looking past the last block, which this would undo.
func ClampTimelineOffset(panel Panel, view *TimelineViewState, data TimelineData) {
	track := TimelineHScrollRect(panel, *view, data)
	if track.Width <= 0 || view.PixelsPerSec <= 0 {
		// No slider means the whole span fits; pin to the start.
		t0, _ := timelineTimeSpan(data)
		view.OffsetT = t0
		return
	}
	t0, t1 := timelineTimeSpan(data)
	max := t1 - track.Width/view.PixelsPerSec
	if max < t0 {
		max = t0
	}
	if view.OffsetT < t0 {
		view.OffsetT = t0
	}
	if view.OffsetT > max {
		view.OffsetT = max
	}
}

// TimelineVScrollThumb returns a zero rect when there is nothing to scroll.
func TimelineVScrollThumb(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	track := TimelineVScrollRect(panel, view, data)
	total := timelineRowsHeight(data)
	if track.Height <= 0 || total <= track.Height {
		return rl.Rectangle{}
	}
	h := track.Height * track.Height / total
	if h < timelineThumbMin {
		h = timelineThumbMin
	}
	frac := clampUnit(view.ScrollY / (total - track.Height))
	return rl.Rectangle{
		X: track.X, Y: track.Y + (track.Height-h)*frac,
		Width: track.Width, Height: h,
	}
}

// TimelineHScrollThumb mirrors the vertical one over the time axis.
func TimelineHScrollThumb(panel Panel, view TimelineViewState, data TimelineData) rl.Rectangle {
	track := TimelineHScrollRect(panel, view, data)
	if track.Width <= 0 || view.PixelsPerSec <= 0 {
		return rl.Rectangle{}
	}
	t0, t1 := timelineTimeSpan(data)
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
	track := TimelineHScrollRect(panel, *view, data)
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
	track := TimelineVScrollRect(panel, *view, data)
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
	timelineHeaderH float32 = 26
	// Wide enough for "idle N  <Kind> cancelled" — the status line is the
	// column's reason to exist, and a truncated one reads as a bug.
	timelineLabelW       float32 = 176
	timelineLabelMinW    float32 = 90
	timelineDividerGrab  float32 = 4
	timelineScrollThick  float32 = 8
	timelineThumbMin     float32 = 24
	timelineRowH         float32 = 32
	timelineRowPad       float32 = 4
	timelineOutcomeCapW  float32 = 3
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
	timelineBlockEdge   = rl.Color{R: 40, G: 46, B: 54, A: 220}
	timelineHeadEdge    = rl.Color{R: 230, G: 235, B: 240, A: 220}

	// Outcome caps on the trailing edge of a finished block. Kind stays in the
	// fill colour, so how it ended has to read from somewhere else.
	timelineOutcomeDone   = rl.Color{R: 110, G: 210, B: 120, A: 255}
	timelineOutcomeFailed = rl.Color{R: 230, G: 90, B: 70, A: 255}
	timelineOutcomeCancel = rl.Color{R: 130, G: 136, B: 146, A: 255}
)

func outcomeColor(o components.OrderOutcome) rl.Color {
	switch o {
	case components.OutcomeFailed:
		return timelineOutcomeFailed
	case components.OutcomeCancelled:
		return timelineOutcomeCancel
	}
	return timelineOutcomeDone
}

// TimelineStyle is the panel's entry into the shared widget layer; per-call
// copies override FontSize for the rows that need a smaller face.
func TimelineStyle(font rl.Font) Style {
	return Style{
		Font:     font,
		FontSize: 14,
		RowH:     timelineRowH,
		Gap:      4,
		Text:     timelineText,
		TextDim:  timelineTextDim,
		Header:   timelineTextDim,
		Fill:     timelineGutterBG,
		Border:   timelineBlockEdge,
	}
}

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
	view.Normalize()
	st := TimelineStyle(font)

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
	drawTimelineTicks(&st, header, content.Y+content.Height, *view)

	rows := TimelineRowsRect(panel, *view, data)
	ScrollTimelineRows(panel, view, data, 0) // re-clamp after a resize

	// Only the rows the viewport can actually show are drawn; the scissor
	// below clips, but clipping happens after the draw call is issued.
	first, last := timelineVisibleRows(rows, *view, len(data.Rows))

	// The squad list is drawn before the tracks scissor opens — inside it the
	// gutter sits outside the clip rect and vanished entirely.
	rl.BeginScissorMode(int32(gutter.X), int32(rows.Y), int32(gutter.Width), int32(rows.Height))
	for i := first; i <= last; i++ {
		drawTimelineRowLabel(&st, content, labelW, timelineRowY(rows, *view, i), data.Rows[i])
	}
	rl.EndScissorMode()
	drawTimelineGutterHeader(&st, gutter, len(data.Rows))

	rl.BeginScissorMode(int32(tracksX), int32(rows.Y),
		int32(rows.X+rows.Width-tracksX), int32(rows.Height))
	for i := first; i <= last; i++ {
		drawTimelineRow(&st, tracksX, rows.X+rows.Width-tracksX,
			timelineRowY(rows, *view, i), data.Rows[i], *view)
	}
	rl.EndScissorMode()
	drawTimelineScrollbars(panel, *view, data, cursor)

	div := TimelineDividerRect(panel, *view)
	divCol := timelineDivider
	if dividerActive || pointInRect(cursor, div) {
		divCol = timelineDividerHi
	}
	rl.DrawRectangle(int32(tracksX-1), int32(content.Y), 2, int32(content.Height), divCol)

	if len(data.Rows) == 0 {
		msg := "No squads to display"
		m := rl.MeasureTextEx(font, msg, st.FontSize, 1.0)
		rl.DrawTextEx(font, msg,
			rl.Vector2{X: tracksX + (tracksW-m.X)*0.5, Y: content.Y + content.Height*0.5 - m.Y*0.5},
			st.FontSize, 1.0, timelineEmptyText)
	}

	// Drawn last so it sits on top of every block.
	nowX := timelineTimeToX(data.NowT, tracksX, *view)
	if nowX >= tracksX && nowX <= tracksX+tracksW {
		rl.DrawLineEx(
			rl.Vector2{X: nowX, Y: content.Y + 2},
			rl.Vector2{X: nowX, Y: content.Y + content.Height - 2},
			2, timelineNowLine)
	}
}

// timelineVisibleRows is the inclusive index range intersecting the viewport;
// last < first when the list is empty.
func timelineVisibleRows(rows rl.Rectangle, view TimelineViewState, count int) (int, int) {
	if count == 0 || rows.Height <= 0 {
		return 0, -1
	}
	first := int(view.ScrollY / timelineRowH)
	last := int((view.ScrollY + rows.Height) / timelineRowH)
	if first < 0 {
		first = 0
	}
	if last >= count {
		last = count - 1
	}
	return first, last
}

// timelineRowY resolves a row's top edge, scroll included.
func timelineRowY(rows rl.Rectangle, view TimelineViewState, idx int) float32 {
	return rows.Y + float32(idx)*timelineRowH - view.ScrollY
}

func drawTimelineRow(st *Style, tracksX, tracksW, rowY float32,
	row TimelineSquadRow, view TimelineViewState) {
	rl.DrawLine(int32(tracksX), int32(rowY+timelineRowH),
		int32(tracksX+tracksW), int32(rowY+timelineRowH), timelineRowSep)

	for _, ord := range row.Orders {
		bx := timelineBlockRect(ord, rowY, tracksX, view)
		if bx.X+bx.Width < tracksX || bx.X > tracksX+tracksW {
			continue
		}
		drawTimelineBlock(st, ord, bx)
	}
}

// drawTimelineScrollbars paints the two sliders; each hides itself when its
// axis already fits.
func drawTimelineScrollbars(panel Panel, view TimelineViewState, data TimelineData,
	cursor rl.Vector2) {
	if vThumb := TimelineVScrollThumb(panel, view, data); vThumb.Height > 0 {
		rl.DrawRectangleRec(TimelineVScrollRect(panel, view, data), timelineScrollTrack)
		rl.DrawRectangleRec(vThumb, timelineThumbColor(cursor, vThumb))
	}
	if hThumb := TimelineHScrollThumb(panel, view, data); hThumb.Width > 0 {
		rl.DrawRectangleRec(TimelineHScrollRect(panel, view, data), timelineScrollTrack)
		rl.DrawRectangleRec(hThumb, timelineThumbColor(cursor, hThumb))
	}
}

func timelineThumbColor(cursor rl.Vector2, thumb rl.Rectangle) rl.Color {
	if pointInRect(cursor, thumb) {
		return timelineThumbHot
	}
	return timelineThumb
}

func drawTimelineGutterHeader(st *Style, gutter rl.Rectangle, rows int) {
	head := rl.Rectangle{X: gutter.X, Y: gutter.Y, Width: gutter.Width, Height: timelineHeaderH}
	rl.DrawRectangleRec(head, timelineHeaderBG)
	sub := *st
	sub.FontSize = 13
	TextClipped(&sub, rl.Rectangle{
		X: head.X + 8, Y: head.Y + (timelineHeaderH-sub.FontSize)*0.5,
		Width: head.Width - 12, Height: sub.FontSize,
	}, fmt.Sprintf("Squads (%d)", rows), st.TextDim)
}

// drawTimelineRowLabel is the squad-list entry: colour chip, name, and what
// the squad is doing right now — the whole point of the column.
func drawTimelineRowLabel(st *Style, content rl.Rectangle, labelW, rowY float32,
	row TimelineSquadRow) {
	chip := rl.Rectangle{X: content.X + 6, Y: rowY + 4, Width: 4, Height: timelineRowH - 8}
	rl.DrawRectangleRec(chip, row.Color)

	textX := chip.X + chip.Width + 6
	maxW := content.X + labelW - textX - 6
	name := row.Name
	if name == "" {
		name = fmt.Sprintf("Squad #%X", row.Squad.ID()&0xFFF)
	}
	TextClipped(st, rl.Rectangle{X: textX, Y: rowY + 3, Width: maxW, Height: st.FontSize},
		name, st.Text)
	sub := *st
	sub.FontSize = 12
	TextClipped(&sub, rl.Rectangle{
		X: textX, Y: rowY + 4 + st.FontSize, Width: maxW, Height: sub.FontSize,
	}, rowActivity(row), st.TextDim)

	rl.DrawLine(int32(content.X), int32(rowY+timelineRowH),
		int32(content.X+labelW), int32(rowY+timelineRowH), timelineRowSep)
}

// rowActivity summarises the head order. An idle squad reports what it last
// did instead — "idle" alone hides the difference between a squad that just
// arrived and one whose order failed.
func rowActivity(row TimelineSquadRow) string {
	for _, ord := range row.Orders {
		if ord.IsHead {
			return fmt.Sprintf("%s %d%%  %d men",
				orderKindLabel(ord.KindCode), int(ord.Progress*100), row.Members)
		}
	}
	// Terse on purpose: the gutter is ~24 characters wide, and "what happened
	// last" is worth more of them than the word "men".
	for i := len(row.Orders) - 1; i >= 0; i-- {
		if ord := row.Orders[i]; ord.Historical {
			return fmt.Sprintf("idle %d  %s %s", row.Members,
				orderKindLabel(ord.KindCode), components.OrderOutcomeLabel(ord.Outcome))
		}
	}
	return fmt.Sprintf("idle  %d men", row.Members)
}

// timelineBlockRect is the single source of a block's on-screen box — the
// hit test and the renderer must never compute it differently.
func timelineBlockRect(ord TimelineOrderBlock, rowY, tracksX float32,
	view TimelineViewState) rl.Rectangle {
	x0 := timelineTimeToX(ord.StartT, tracksX, view)
	x1 := timelineTimeToX(ord.EndT, tracksX, view)
	if x1-x0 < 4 {
		x1 = x0 + 4
	}
	return rl.Rectangle{
		X: x0, Y: rowY + timelineRowPad,
		Width: x1 - x0, Height: timelineRowH - 2*timelineRowPad,
	}
}

func drawTimelineBlock(st *Style, ord TimelineOrderBlock, bx rl.Rectangle) {
	if ord.Historical {
		drawTimelineHistoryBlock(st, ord, bx)
		return
	}
	fill := orderKindColor(ord.KindCode)
	switch ord.StateCode {
	case components.OrderStateBlocked, components.OrderStateFailed:
		fill = rl.Color{R: 230, G: 110, B: 80, A: 255}
	case components.OrderStateCancelled:
		fill = rl.Color{R: 100, G: 100, B: 110, A: 255}
	case components.OrderStateCompleted:
		fill = dimColor(fill, 0.55)
	}

	Bar(bx, ord.Progress, dimColor(fill, 0.35), fill, 0)
	if ord.IsHead {
		rl.DrawRectangleLinesEx(bx, 2, timelineHeadEdge)
	} else {
		rl.DrawRectangleLinesEx(bx, 1, timelineBlockEdge)
	}
	drawTimelineBlockLabel(st, ord, bx, fill)
}

// drawTimelineHistoryBlock paints the past: quieter than the plan, filled only
// as far as the order actually got, and capped with how it ended. A cancelled
// MoveTo at 60 % is the whole point — the bar itself answers "did they make it".
func drawTimelineHistoryBlock(st *Style, ord TimelineOrderBlock, bx rl.Rectangle) {
	cap := outcomeColor(ord.Outcome)
	if !ord.Started {
		// Queued and cancelled before its turn: no extent to fill, just a mark
		// saying the order existed and never ran.
		rl.DrawRectangleRec(rl.Rectangle{
			X: bx.X, Y: bx.Y, Width: timelineOutcomeCapW, Height: bx.Height,
		}, cap)
		return
	}
	fill := dimColor(orderKindColor(ord.KindCode), 0.5)
	Bar(bx, ord.Progress, dimColor(fill, 0.3), fill, 0)
	rl.DrawRectangleLinesEx(bx, 1, timelineBlockEdge)
	rl.DrawRectangleRec(rl.Rectangle{
		X: bx.X + bx.Width - timelineOutcomeCapW, Y: bx.Y,
		Width: timelineOutcomeCapW, Height: bx.Height,
	}, cap)
	drawTimelineBlockLabel(st, ord, bx, fill)
}

func drawTimelineBlockLabel(st *Style, ord TimelineOrderBlock, bx rl.Rectangle, fill rl.Color) {
	label := orderKindLabel(ord.KindCode)
	if rl.MeasureTextEx(st.Font, label, st.FontSize, 1.0).X+6 >= bx.Width {
		return
	}
	TextCentered(st, rl.Rectangle{X: bx.X + 4, Y: bx.Y, Width: bx.Width - 8, Height: bx.Height},
		label, contrastTextColor(fill))
}

func drawTimelineTicks(st *Style, header rl.Rectangle, panelBottomY float32, view TimelineViewState) {
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

	tick := *st
	tick.FontSize = 13
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
			Text(&tick, rl.Rectangle{X: x + 3, Y: header.Y + 2}, timeLabel(t), timelineText)
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
	return "T+" + shortClock(sec)
}

func timelineTimeToX(t, tracksX float32, view TimelineViewState) float32 {
	return tracksX + (t-view.OffsetT)*view.PixelsPerSec
}

// TimelineHit addresses a block by position, not by entity: a historical block
// has no live order to name, and a recycled entity ID would alias one.
// HitOrder=false means the cursor is over a row but not a block.
type TimelineHit struct {
	Squad    ecs.Entity
	Row      int
	Block    int
	HitOrder bool
}

// Block resolves a hit back to its block; ok=false when the hit addresses no
// block or the snapshot changed underneath it.
func (d TimelineData) Block(hit TimelineHit) (TimelineOrderBlock, bool) {
	if !hit.HitOrder || hit.Row < 0 || hit.Row >= len(d.Rows) {
		return TimelineOrderBlock{}, false
	}
	row := d.Rows[hit.Row]
	if hit.Block < 0 || hit.Block >= len(row.Orders) {
		return TimelineOrderBlock{}, false
	}
	return row.Orders[hit.Block], true
}

// TimelineHitTest returns ok=false when cursor is outside the tracks area.
func TimelineHitTest(panel Panel, data TimelineData, view TimelineViewState, cursor rl.Vector2) (TimelineHit, bool) {
	content := ContentRect(panel)
	if !pointInRect(cursor, content) {
		return TimelineHit{}, false
	}
	view.Normalize()
	rows := TimelineRowsRect(panel, view, data)
	if !pointInRect(cursor, rows) {
		return TimelineHit{}, true
	}
	tracksX := content.X + TimelineLabelWidth(view)
	rowIdx := int((cursor.Y - rows.Y + view.ScrollY) / timelineRowH)
	if rowIdx < 0 || rowIdx >= len(data.Rows) {
		return TimelineHit{}, true
	}
	row := data.Rows[rowIdx]
	hit := TimelineHit{Squad: row.Squad, Row: rowIdx, Block: -1}
	if cursor.X < tracksX {
		return hit, true
	}
	rowY := timelineRowY(rows, view, rowIdx)
	// Last match wins: blocks are laid down oldest first, so a later one drawn
	// over an overlap is also the one the player sees.
	for i, ord := range row.Orders {
		if pointInRect(cursor, timelineBlockRect(ord, rowY, tracksX, view)) {
			hit.Block, hit.HitOrder = i, true
		}
	}
	return hit, true
}

// DrawTimelineTooltip is invoked only when TimelineHit.HitOrder is true.
func DrawTimelineTooltip(font rl.Font, cursor rl.Vector2, ord TimelineOrderBlock) {
	st := TimelineStyle(font)
	st.FontSize = 13
	drawTimelineTooltipBox(&st, cursor, timelineTooltipLines(ord))
}

// timelineTooltipLines separates the two claims the panel makes: a plan block
// says "est", a historical one reports measured times and how it ended.
func timelineTooltipLines(ord TimelineOrderBlock) []string {
	kind := orderKindLabel(ord.KindCode)
	if !ord.Historical {
		return []string{
			kind + "  " + orderStateLabel(ord.StateCode),
			fmt.Sprintf("issued T+%s, est %.0fs", shortClock(ord.StartT), ord.EndT-ord.StartT),
			fmt.Sprintf("progress %d%%", int(ord.Progress*100)),
		}
	}
	head := kind + "  " + components.OrderOutcomeLabel(ord.Outcome)
	if !ord.Started {
		return []string{head, "queued T+" + shortClock(ord.StartT) + ", never started"}
	}
	return []string{
		head,
		fmt.Sprintf("started T+%s, ran %.0fs", shortClock(ord.StartT), ord.EndT-ord.StartT),
		fmt.Sprintf("ended at %d%%", int(ord.Progress*100)),
	}
}

func drawTimelineTooltipBox(st *Style, cursor rl.Vector2, lines []string) {
	maxW := float32(0)
	for _, l := range lines {
		if w := rl.MeasureTextEx(st.Font, l, st.FontSize, 1.0).X; w > maxW {
			maxW = w
		}
	}
	const pad float32 = 6
	lineH := st.FontSize + 2
	rect := rl.Rectangle{
		X: cursor.X + 14, Y: cursor.Y + 14,
		Width:  maxW + 2*pad,
		Height: lineH*float32(len(lines)) + 2*pad,
	}
	rl.DrawRectangleRec(rect, rl.Color{R: 18, G: 22, B: 28, A: 240})
	rl.DrawRectangleLinesEx(rect, 1, rl.Color{R: 80, G: 90, B: 105, A: 255})
	for i, l := range lines {
		Text(st, rl.Rectangle{X: rect.X + pad, Y: rect.Y + pad + float32(i)*lineH}, l, st.Text)
	}
}

func shortClock(t float32) string {
	if t < 0 {
		t = 0
	}
	return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60)
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
