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
	track := TimelineVScrollRect(p, view)

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
	track := TimelineHScrollRect(p, view)
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
	data := rowsData(3)
	c := ContentRect(p)
	cursor := rl.Vector2{X: c.X + 400, Y: c.Y + c.Height - 4}
	hit, ok := TimelineHitTest(p, data, view, cursor)
	if !ok {
		t.Fatal("cursor inside the panel should hit-test")
	}
	if hit.HitOrder || hit.Squad != (ecs.Entity{}) {
		t.Errorf("scrollbar strip resolved to a row: %+v", hit)
	}
}
