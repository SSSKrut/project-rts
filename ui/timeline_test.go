package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func timelinePanel(w, h float32) Panel {
	return ContentToPanel(rl.Rectangle{X: 0, Y: 0, Width: w, Height: h}, "Timeline", PanelTimeline)
}

func TestTimelineLabelWidthDefaults(t *testing.T) {
	if got := TimelineLabelWidth(TimelineViewState{}); got != timelineLabelW {
		t.Errorf("zero width should fall back to default, got %v", got)
	}
	if got := TimelineLabelWidth(TimelineViewState{LabelW: 220}); got != 220 {
		t.Errorf("explicit width ignored, got %v", got)
	}
}

func TestClampTimelineLabelW(t *testing.T) {
	p := timelinePanel(800, 200)
	if got := ClampTimelineLabelW(p, 10); got != timelineLabelMinW {
		t.Errorf("narrow drag should clamp to min, got %v", got)
	}
	// 60 % of an 800-wide content rect, minus the chrome border.
	if got := ClampTimelineLabelW(p, 5000); got > ContentRect(p).Width*0.6+0.01 {
		t.Errorf("wide drag must leave tracks visible, got %v", got)
	}
	if got := ClampTimelineLabelW(p, 200); got != 200 {
		t.Errorf("in-range drag altered, got %v", got)
	}
}

// A collapsed panel must not produce an inverted clamp range.
func TestClampTimelineLabelWTinyPanel(t *testing.T) {
	p := timelinePanel(60, 100)
	if got := ClampTimelineLabelW(p, 300); got != timelineLabelMinW {
		t.Errorf("tiny panel should pin to min, got %v", got)
	}
}

func TestTimelineDividerFollowsWidth(t *testing.T) {
	p := timelinePanel(800, 200)
	view := TimelineViewState{LabelW: 240}
	div := TimelineDividerRect(p, view)
	want := ContentRect(p).X + 240
	if div.X+div.Width*0.5 != want {
		t.Errorf("divider centre %v, want %v", div.X+div.Width*0.5, want)
	}
}

// The hit test reads the same column width the panel drew with, or clicks
// land on the wrong row after a resize.
func TestTimelineHitTestFollowsWidth(t *testing.T) {
	p := timelinePanel(800, 200)
	squad := ecs.Entity{}
	data := TimelineData{Rows: []TimelineSquadRow{{
		Squad: squad,
		Orders: []TimelineOrderBlock{{
			KindCode: components.OrderKindMoveTo,
			StartT:   0, EndT: 20, IsHead: true,
		}},
	}}}
	view := TimelineViewState{PixelsPerSec: 8, LabelW: 300}
	content := ContentRect(p)
	// A point inside the widened gutter must not resolve to an order block.
	cursor := rl.Vector2{X: content.X + 250, Y: content.Y + timelineHeaderH + 5}
	hit, ok := TimelineHitTest(p, data, view, cursor)
	if !ok {
		t.Fatal("cursor inside the panel should hit-test")
	}
	if hit.HitOrder {
		t.Error("gutter click resolved to an order block")
	}
}

func TestRowActivity(t *testing.T) {
	idle := TimelineSquadRow{Members: 6}
	if got := rowActivity(idle); got != "idle  6 men" {
		t.Errorf("idle row: %q", got)
	}
	busy := TimelineSquadRow{Members: 8, Orders: []TimelineOrderBlock{
		{KindCode: components.OrderKindMoveTo, Progress: 0.5, IsHead: false},
		{KindCode: components.OrderKindDefendPosition, Progress: 0.25, IsHead: true},
	}}
	if got := rowActivity(busy); got != "Defend 25%  8 men" {
		t.Errorf("busy row should report the head order, got %q", got)
	}
}

// "idle" alone hides whether the squad arrived or its order fell over.
func TestRowActivityIdleReportsLastOrder(t *testing.T) {
	row := TimelineSquadRow{Members: 4, Orders: []TimelineOrderBlock{
		{KindCode: components.OrderKindMoveTo, Historical: true,
			Outcome: components.OutcomeCompleted, Started: true},
		{KindCode: components.OrderKindGarrison, Historical: true,
			Outcome: components.OutcomeFailed, Started: true},
	}}
	if got := rowActivity(row); got != "idle 4  Garrison failed" {
		t.Errorf("idle row with history: %q", got)
	}
	// A live head still wins over the past.
	row.Orders = append(row.Orders, TimelineOrderBlock{
		KindCode: components.OrderKindPatrol, Progress: 0.1, IsHead: true,
	})
	if got := rowActivity(row); got != "Patrol 10%  4 men" {
		t.Errorf("head order should win, got %q", got)
	}
}

func TestHistoryBlock(t *testing.T) {
	ran := HistoryBlock(components.OrderRecord{
		Kind: components.OrderKindMoveTo, IssuedT: 10, StartedT: 12, EndedT: 40,
		Progress: 0.6, Outcome: components.OutcomeCancelled,
	})
	if !ran.Historical || !ran.Started {
		t.Errorf("started record: %+v", ran)
	}
	// The block spans what happened, not what was planned: start is when it
	// ran, not when the player clicked.
	if ran.StartT != 12 || ran.EndT != 40 {
		t.Errorf("extent = [%v..%v], want [12..40]", ran.StartT, ran.EndT)
	}
	if ran.Progress != 0.6 || ran.Outcome != components.OutcomeCancelled {
		t.Errorf("record detail lost: %+v", ran)
	}

	never := HistoryBlock(components.OrderRecord{
		Kind: components.OrderKindPatrol, IssuedT: 10,
		StartedT: components.OrderNeverStarted, EndedT: 40,
		Outcome: components.OutcomeCancelled,
	})
	if never.Started {
		t.Error("never-started record reported as started")
	}
	// It collapses onto its issue time rather than claiming a 30 s run.
	if never.StartT != 10 {
		t.Errorf("StartT = %v, want the issue time 10", never.StartT)
	}
}

func TestTimelineTooltipLines(t *testing.T) {
	plan := TimelineOrderBlock{
		KindCode: components.OrderKindMoveTo, StartT: 60, EndT: 90, Progress: 0.5,
	}
	if got := timelineTooltipLines(plan); len(got) != 3 ||
		got[1] != "issued T+01:00, est 30s" {
		t.Errorf("plan tooltip: %q", got)
	}
	done := TimelineOrderBlock{
		KindCode: components.OrderKindMoveTo, StartT: 60, EndT: 95, Progress: 1,
		Historical: true, Started: true, Outcome: components.OutcomeCompleted,
	}
	got := timelineTooltipLines(done)
	if len(got) != 3 {
		t.Fatalf("history tooltip lines = %d, want 3", len(got))
	}
	if got[0] != "Move  done" {
		t.Errorf("history head line: %q", got[0])
	}
	if got[1] != "started T+01:00, ran 35s" {
		t.Errorf("history should report measured time, got %q", got[1])
	}
	if got[2] != "ended at 100%" {
		t.Errorf("history end state: %q", got[2])
	}
	never := TimelineOrderBlock{
		KindCode: components.OrderKindPatrol, StartT: 30, EndT: 30,
		Historical: true, Outcome: components.OutcomeCancelled,
	}
	if got := timelineTooltipLines(never); len(got) != 2 ||
		got[1] != "queued T+00:30, never started" {
		t.Errorf("never-started tooltip: %q", got)
	}
}

// The hit must address a block by index: a historical block has no entity, so
// several in one row would otherwise be indistinguishable.
func TestTimelineHitTestResolvesHistoryBlocks(t *testing.T) {
	p := timelinePanel(800, 200)
	view := TimelineViewState{PixelsPerSec: 8, LabelW: 150}
	squad := ecs.Entity{}
	data := TimelineData{NowT: 60, Rows: []TimelineSquadRow{{Squad: squad, Orders: []TimelineOrderBlock{
		{KindCode: components.OrderKindMoveTo, StartT: 0, EndT: 10,
			Historical: true, Started: true, Outcome: components.OutcomeCompleted},
		{KindCode: components.OrderKindGarrison, StartT: 20, EndT: 30,
			Historical: true, Started: true, Outcome: components.OutcomeFailed},
		{KindCode: components.OrderKindPatrol, StartT: 40, EndT: 80, IsHead: true},
	}}}}
	content := ContentRect(p)
	tracksX := content.X + TimelineLabelWidth(view)
	rowY := content.Y + timelineHeaderH

	// Centre of each block in turn: 5 s, 25 s, 60 s.
	for i, at := range []float32{5, 25, 60} {
		cursor := rl.Vector2{
			X: timelineTimeToX(at, tracksX, view),
			Y: rowY + timelineRowH*0.5,
		}
		hit, ok := TimelineHitTest(p, data, view, cursor)
		if !ok || !hit.HitOrder {
			t.Fatalf("t=%v should hit a block, got %+v ok=%v", at, hit, ok)
		}
		if hit.Block != i {
			t.Errorf("t=%v resolved to block %d, want %d", at, hit.Block, i)
		}
		blk, found := data.Block(hit)
		if !found || blk.KindCode != data.Rows[0].Orders[i].KindCode {
			t.Errorf("t=%v: Block() returned %+v found=%v", at, blk, found)
		}
	}

	// A gap between two history blocks is a row hit, not a block hit.
	gap := rl.Vector2{X: timelineTimeToX(15, tracksX, view), Y: rowY + timelineRowH*0.5}
	hit, ok := TimelineHitTest(p, data, view, gap)
	if !ok {
		t.Fatal("cursor inside the panel should hit-test")
	}
	if hit.HitOrder {
		t.Errorf("gap between blocks resolved to block %d", hit.Block)
	}
	if _, found := data.Block(hit); found {
		t.Error("Block() must reject a non-block hit")
	}
}

// Block() is the only path from a hit to its data, so it has to survive the
// snapshot shrinking under a stale hit.
func TestTimelineDataBlockBounds(t *testing.T) {
	data := TimelineData{Rows: []TimelineSquadRow{{
		Orders: []TimelineOrderBlock{{KindCode: components.OrderKindMoveTo}},
	}}}
	if _, ok := data.Block(TimelineHit{HitOrder: true, Row: 9, Block: 0}); ok {
		t.Error("out-of-range row accepted")
	}
	if _, ok := data.Block(TimelineHit{HitOrder: true, Row: 0, Block: 9}); ok {
		t.Error("out-of-range block accepted")
	}
	if _, ok := data.Block(TimelineHit{HitOrder: true, Row: 0, Block: 0}); !ok {
		t.Error("valid hit rejected")
	}
}

func rowsData(n int) TimelineData {
	d := TimelineData{NowT: 10}
	for i := 0; i < n; i++ {
		d.Rows = append(d.Rows, TimelineSquadRow{Members: 4})
	}
	return d
}

func TestTimelineRowScrollClamps(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()

	if got := TimelineScrollYMax(p, view, rowsData(3)); got != 0 {
		t.Errorf("rows that fit must not scroll, got %v", got)
	}
	many := rowsData(12)
	max := TimelineScrollYMax(p, view, many)
	if max <= 0 {
		t.Fatalf("overflowing rows should scroll, got %v", max)
	}

	ScrollTimelineRows(p, &view, many, 10_000)
	if view.ScrollY != max {
		t.Errorf("scroll past the end: got %v want %v", view.ScrollY, max)
	}
	ScrollTimelineRows(p, &view, many, -10_000)
	if view.ScrollY != 0 {
		t.Errorf("scroll past the start: got %v want 0", view.ScrollY)
	}
}

func TestTimelineThumbsHideWhenEverythingFits(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	view.PixelsPerSec = 2 // 650 px of track covers far more than the span
	data := rowsData(2)
	if got := TimelineVScrollThumb(p, view, data); got.Height != 0 {
		t.Errorf("vertical thumb drawn with nothing to scroll: %+v", got)
	}
	if got := TimelineHScrollThumb(p, view, data); got.Width != 0 {
		t.Errorf("horizontal thumb drawn with nothing to scroll: %+v", got)
	}
}

func TestTimelineVThumbRoundTrip(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	data := rowsData(12)
	track := TimelineVScrollRect(p, view, data)

	SetTimelineScrollFromThumb(p, &view, data, track.Y+track.Height) // dragged to the bottom
	if want := TimelineScrollYMax(p, view, data); view.ScrollY != want {
		t.Errorf("bottom drag: got %v want %v", view.ScrollY, want)
	}
	SetTimelineScrollFromThumb(p, &view, data, track.Y-50) // above the track
	if view.ScrollY != 0 {
		t.Errorf("top drag: got %v want 0", view.ScrollY)
	}
}

func TestTimelineHThumbDropsFollow(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	view.PixelsPerSec = 40
	data := TimelineData{NowT: 600, Rows: []TimelineSquadRow{{
		Orders: []TimelineOrderBlock{{EndT: 900, IsHead: true}},
	}}}
	track := TimelineHScrollRect(p, view, data)
	SetTimelineOffsetFromThumb(p, &view, data, track.X+track.Width*0.5)
	if view.Follow {
		t.Error("grabbing the slider must stop following the now-line")
	}
	if view.OffsetT <= 0 {
		t.Errorf("mid-track drag should land past the start, got %v", view.OffsetT)
	}
}

func TestTimelineOverGutterFollowsWidth(t *testing.T) {
	p := timelinePanel(800, 200)
	c := ContentRect(p)
	view := TimelineViewState{LabelW: 300}
	if !TimelineOverGutter(p, view, rl.Vector2{X: c.X + 280, Y: c.Y + 40}) {
		t.Error("point inside the widened list read as tracks")
	}
	if TimelineOverGutter(p, view, rl.Vector2{X: c.X + 320, Y: c.Y + 40}) {
		t.Error("point past the divider read as the list")
	}
}

// A click on the slider strip must not resolve to the row behind it.
func TestTimelineHitTestSkipsScrollbars(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	data := rowsData(12) // overflows vertically -> the right-hand strip exists
	c := ContentRect(p)
	if TimelineVScrollRect(p, view, data).Width <= 0 {
		t.Fatal("test needs a visible vertical slider")
	}
	cursor := rl.Vector2{X: c.X + c.Width - 4, Y: c.Y + timelineHeaderH + 10}
	hit, ok := TimelineHitTest(p, data, view, cursor)
	if !ok {
		t.Fatal("cursor inside the panel should hit-test")
	}
	if hit.HitOrder || hit.Squad != (ecs.Entity{}) {
		t.Errorf("scrollbar strip resolved to a row: %+v", hit)
	}
}

// Reserving a strip for a slider that isn't drawn leaves a dead band the
// player can't click through.
func TestTimelineBarsOnlyWhenNeeded(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	c := ContentRect(p)

	fits := rowsData(2)
	if got := TimelineVScrollRect(p, view, fits); got.Width != 0 {
		t.Errorf("vertical strip reserved with nothing to scroll: %+v", got)
	}
	if got := TimelineHScrollRect(p, view, fits); got.Width != 0 {
		t.Errorf("horizontal strip reserved with nothing to pan: %+v", got)
	}
	if got := TimelineRowsRect(p, view, fits); got.Height != c.Height-timelineHeaderH {
		t.Errorf("rows band %v, want the full height below the header %v",
			got.Height, c.Height-timelineHeaderH)
	}

	many := rowsData(12)
	if got := TimelineVScrollRect(p, view, many); got.Width != timelineScrollThick {
		t.Errorf("overflowing rows need a slider, got %+v", got)
	}
	if got := TimelineRowsRect(p, view, many); got.Width != c.Width-timelineScrollThick {
		t.Errorf("rows band must clear the slider, got width %v", got.Width)
	}
}

// A zero view arrives from a fresh floater and from a pre-v4 layout file.
func TestTimelineViewNormalize(t *testing.T) {
	var v TimelineViewState
	v.Normalize()
	if v.PixelsPerSec != NewTimelineView().PixelsPerSec {
		t.Errorf("zero scale not repaired, got %v", v.PixelsPerSec)
	}
	if v.LabelW != timelineLabelW {
		t.Errorf("zero gutter not repaired, got %v", v.LabelW)
	}
	v.PixelsPerSec = TimelineMaxPxPerSec * 10
	v.Normalize()
	if v.PixelsPerSec != TimelineMaxPxPerSec {
		t.Errorf("out-of-range scale not clamped, got %v", v.PixelsPerSec)
	}
}

// Panning must not be able to walk the view off the end of the mission.
func TestClampTimelineOffset(t *testing.T) {
	p := timelinePanel(800, 200)
	view := NewTimelineView()
	view.PixelsPerSec = 40
	data := TimelineData{NowT: 600, Rows: []TimelineSquadRow{{
		Orders: []TimelineOrderBlock{{EndT: 900, IsHead: true}},
	}}}
	_, t1 := timelineTimeSpan(data)
	track := TimelineHScrollRect(p, view, data)
	wantMax := t1 - track.Width/view.PixelsPerSec

	view.OffsetT = -500
	ClampTimelineOffset(p, &view, data)
	if view.OffsetT != 0 {
		t.Errorf("panned before mission start: got %v want 0", view.OffsetT)
	}
	view.OffsetT = 10_000
	ClampTimelineOffset(p, &view, data)
	if view.OffsetT != wantMax {
		t.Errorf("panned past the last block: got %v want %v", view.OffsetT, wantMax)
	}

	// Nothing to pan: the view pins to the start instead of drifting.
	fits := rowsData(2)
	view.PixelsPerSec = TimelineMinPxPerSec // whole span fits, no slider
	view.OffsetT = 500
	ClampTimelineOffset(p, &view, fits)
	if view.OffsetT != 0 {
		t.Errorf("span fits, offset should pin to 0, got %v", view.OffsetT)
	}
}

func TestTimelineVisibleRows(t *testing.T) {
	rows := rl.Rectangle{Y: 0, Height: 4 * timelineRowH}
	first, last := timelineVisibleRows(rows, TimelineViewState{}, 20)
	if first != 0 || last != 4 {
		t.Errorf("top of a long list: got [%d..%d] want [0..4]", first, last)
	}
	first, last = timelineVisibleRows(rows, TimelineViewState{ScrollY: 10 * timelineRowH}, 20)
	if first != 10 || last != 14 {
		t.Errorf("scrolled window: got [%d..%d] want [10..14]", first, last)
	}
	// The tail must never index past the slice.
	first, last = timelineVisibleRows(rows, TimelineViewState{ScrollY: 18 * timelineRowH}, 20)
	if first != 18 || last != 19 {
		t.Errorf("bottom of the list: got [%d..%d] want [18..19]", first, last)
	}
	if _, last = timelineVisibleRows(rows, TimelineViewState{}, 0); last != -1 {
		t.Errorf("empty list should yield an empty range, got last=%d", last)
	}
}
