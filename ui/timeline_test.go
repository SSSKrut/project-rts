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
