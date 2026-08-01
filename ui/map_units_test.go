package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func TestAnyBeyond(t *testing.T) {
	centre := rl.Vector2{X: 100, Y: 100}
	tight := []rl.Vector2{{X: 104, Y: 100}, {X: 100, Y: 105}}
	if anyBeyond(centre, tight, ownDotsMinSpreadPx) {
		t.Error("tight formation must stay collapsed")
	}
	spread := append(tight, rl.Vector2{X: 100, Y: 100 + ownDotsMinSpreadPx + 1})
	if !anyBeyond(centre, spread, ownDotsMinSpreadPx) {
		t.Error("a straggler past the threshold must expand the marker")
	}
}

// Only the player's own soldiers get an individual dot; a hostile unit is the
// contact layer's business and must never leak through this pass.
func TestUnitAffiliationGate(t *testing.T) {
	world := ecs.NewWorld(16)
	facMap := ecs.NewMap[components.Faction](world)

	own := world.NewEntity()
	facMap.Add(own, &components.Faction{ID: components.FactionPlayer})
	foe := world.NewEntity()
	facMap.Add(foe, &components.Faction{ID: components.FactionEnemyRed})
	legacy := world.NewEntity() // no Faction at all

	ctx := MapRenderCtx{World: world, FactionMap: facMap}
	if got := unitAffiliation(ctx, own); got != components.AffilFriend {
		t.Errorf("own unit: got %v", got)
	}
	if got := unitAffiliation(ctx, foe); got != components.AffilHostile {
		t.Errorf("hostile unit: got %v", got)
	}
	if got := unitAffiliation(ctx, legacy); got != components.AffilFriend {
		t.Errorf("faction-less unit should read friendly, got %v", got)
	}
}

// Members that died since the last tick must not project onto the map.
func TestSquadMemberPointsSkipsDead(t *testing.T) {
	world := ecs.NewWorld(16)
	posMap := ecs.NewMap[components.WorldPos](world)
	var roster components.CommandRoster
	for i := 0; i < 3; i++ {
		e := world.NewEntity()
		p := components.WorldPos{}.Add(rl.Vector3{X: float32(i) * 3})
		posMap.Add(e, &p)
		roster.Members[roster.Count] = e
		roster.Count++
	}
	ctx := MapRenderCtx{World: world, PosMap: posMap, Cam: NewMapCamera()}
	content := rl.Rectangle{X: 0, Y: 0, Width: 200, Height: 200}

	if _, n := squadMemberPoints(ctx, &roster, content); n != 3 {
		t.Fatalf("expected 3 points, got %d", n)
	}
	world.RemoveEntity(roster.Members[1])
	if _, n := squadMemberPoints(ctx, &roster, content); n != 2 {
		t.Fatalf("dead member still projected, got %d points", n)
	}
}
