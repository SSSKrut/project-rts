package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func barArea(width float32) rl.Rectangle {
	return rl.Rectangle{X: 0, Y: 100, Width: width, Height: BarHeight}
}

func TestBarLayoutGroupsAndOrder(t *testing.T) {
	world := ecs.NewWorld(16)
	a, b := world.NewEntity(), world.NewEntity()
	groups := []BarGroup{
		{Squad: a, Members: []BarMember{{Slot: 0}, {Slot: 1}}},
		{Squad: b, Members: []BarMember{{Slot: 0}}},
	}
	l := ComputeSquadBarLayout(groups, barArea(600))
	if len(l.Cards) != 3 {
		t.Fatalf("want 3 cards, got %d", len(l.Cards))
	}
	if len(l.Groups) != 2 {
		t.Fatalf("want 2 group boxes, got %d", len(l.Groups))
	}
	for i := 1; i < len(l.Cards); i++ {
		if l.Cards[i].Rect.X <= l.Cards[i-1].Rect.X {
			t.Errorf("cards must march left to right: %v", l.Cards)
		}
	}
	// The second group starts past the first one's box, with the gap between.
	if l.Groups[1].Rect.X < l.Groups[0].Rect.X+l.Groups[0].Rect.Width {
		t.Error("groups overlap")
	}
	if l.Used.Width <= 0 || l.Used.Width > 600 {
		t.Errorf("Used must cover the placed cards only, got %v", l.Used.Width)
	}
}

// Overflow must be reported, not silently dropped: a bar that shows four of
// six men while looking complete is worse than one that admits the cut.
func TestBarLayoutReportsOverflow(t *testing.T) {
	members := make([]BarMember, 8)
	for i := range members {
		members[i] = BarMember{Slot: uint8(i)}
	}
	l := ComputeSquadBarLayout([]BarGroup{{Members: members}}, barArea(200))
	if l.Hidden == 0 {
		t.Fatal("narrow area must report hidden cards")
	}
	if len(l.Cards)+l.Hidden != 8 {
		t.Errorf("placed %d + hidden %d != 8", len(l.Cards), l.Hidden)
	}
	for _, c := range l.Cards {
		if c.Rect.X+c.Rect.Width > 200 {
			t.Errorf("card overruns the area: %v", c.Rect)
		}
	}
}

// A hull needs a wider card, so the layout is not a fixed grid: neighbours
// must shift by the actual width, not by the infantry one.
func TestBarLayoutMixedWidths(t *testing.T) {
	groups := []BarGroup{{Members: []BarMember{
		{Slot: 0}, {Slot: 1, Wide: true}, {Slot: 2},
	}}}
	l := ComputeSquadBarLayout(groups, barArea(600))
	if len(l.Cards) != 3 {
		t.Fatalf("want 3 cards, got %d", len(l.Cards))
	}
	if l.Cards[1].Rect.Width <= l.Cards[0].Rect.Width {
		t.Error("hull card must be wider than an infantry card")
	}
	for i := 1; i < len(l.Cards); i++ {
		prev := l.Cards[i-1].Rect
		if l.Cards[i].Rect.X < prev.X+prev.Width {
			t.Errorf("card %d overlaps its predecessor: %v vs %v", i, l.Cards[i].Rect, prev)
		}
	}
}

// A dead man keeps his place for the tombstone window: cards that reshuffle
// the moment someone falls break the muscle memory the bar is built on.
func TestBarStateTombstoneKeepsPosition(t *testing.T) {
	world := ecs.NewWorld(16)
	rosterMap := ecs.NewMap[components.CommandRoster](world)
	memberMap := ecs.NewMap[components.SquadMember](world)

	squad := world.NewEntity()
	units := make([]ecs.Entity, 3)
	var roster components.CommandRoster
	for i := range units {
		units[i] = world.NewEntity()
		memberMap.Add(units[i], &components.SquadMember{Squad: squad, SlotIndex: uint8(i)})
		roster.Members[i] = units[i]
		roster.Count = uint8(i + 1)
	}
	rosterMap.Add(squad, &roster)

	state := NewSquadBarState()
	ctx := SquadBarCtx{
		InspectorMaps: InspectorMaps{RosterMap: rosterMap, SquadMemberMap: memberMap},
		World:         world,
		Selected:      []ecs.Entity{units[0]},
		Now:           10,
	}
	groups := state.Sync(ctx)
	if len(groups) != 1 || len(groups[0].Members) != 3 {
		t.Fatalf("want one group of 3, got %+v", groups)
	}

	// Middle man dies: SquadService.Leave compacts him out of the roster in
	// the same tick, so the bar's own memory is the only thing holding him.
	dead := units[1]
	roster.Members[1] = units[2]
	roster.Members[2] = ecs.Entity{}
	roster.Count = 2
	*rosterMap.Get(squad) = roster
	world.RemoveEntity(dead)

	ctx.Now = 11
	groups = state.Sync(ctx)
	if len(groups[0].Members) != 3 {
		t.Fatalf("tombstone must keep the card: %+v", groups[0].Members)
	}
	if groups[0].Members[1].Unit != dead || groups[0].Members[1].DeadAt != 11 {
		t.Errorf("dead man must stay in slot 1 with a stamp: %+v", groups[0].Members[1])
	}
	if groups[0].Members[2].Unit != units[2] {
		t.Error("survivors must not shift into the gap")
	}

	// Past the window the card is reclaimed.
	ctx.Now = 11 + barTombstone + 0.1
	groups = state.Sync(ctx)
	if len(groups[0].Members) != 2 {
		t.Errorf("expired tombstone must be dropped: %+v", groups[0].Members)
	}
}

// Selecting one man shows his whole squad — the bar is squad-scoped, so a
// drill-down must not collapse the surface being used to drill.
func TestBarStateScopeIsWholeSquad(t *testing.T) {
	world := ecs.NewWorld(16)
	rosterMap := ecs.NewMap[components.CommandRoster](world)
	memberMap := ecs.NewMap[components.SquadMember](world)

	squad := world.NewEntity()
	var roster components.CommandRoster
	for i := 0; i < 4; i++ {
		u := world.NewEntity()
		memberMap.Add(u, &components.SquadMember{Squad: squad, SlotIndex: uint8(i)})
		roster.Members[i] = u
		roster.Count = uint8(i + 1)
	}
	rosterMap.Add(squad, &roster)
	solo := world.NewEntity()

	state := NewSquadBarState()
	groups := state.Sync(SquadBarCtx{
		InspectorMaps: InspectorMaps{RosterMap: rosterMap, SquadMemberMap: memberMap},
		World:         world,
		Selected:      []ecs.Entity{roster.Members[2], solo},
	})
	var squadded, soloists int
	for _, g := range groups {
		if g.Squad == (ecs.Entity{}) {
			soloists += len(g.Members)
			continue
		}
		squadded += len(g.Members)
	}
	if squadded != 4 {
		t.Errorf("one selected member must expand to the full roster, got %d", squadded)
	}
	if soloists != 1 {
		t.Errorf("selected soloist needs its own bucket, got %d", soloists)
	}
}
