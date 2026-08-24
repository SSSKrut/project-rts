package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// The map is the command surface, so anything the player owns has to be
// clickable on it. An airframe was drawn there and not picked, which is worse
// than being absent: the click fell through to "clicked empty ground" and
// cleared the selection the player had just made.
func TestPickOwnEntityFindsAircraft(t *testing.T) {
	world := ecs.NewWorld(64)
	posMap := ecs.NewMap[components.WorldPos](world)
	acMap := ecs.NewMap[components.Aircraft](world)
	vehMap := ecs.NewMap[components.Vehicle](world)
	facMap := ecs.NewMap[components.Faction](world)

	// Altitude on purpose: the map projects XZ and must ignore Y entirely.
	air := world.NewEntity()
	airPos := components.WorldPos{}.Add(rl.Vector3{X: 40, Y: 120, Z: -25})
	posMap.Add(air, &airPos)
	acMap.Add(air, &components.Aircraft{Kind: components.AircraftHeliAttack})
	facMap.Add(air, &components.Faction{ID: components.FactionPlayer})

	hull := world.NewEntity()
	hullPos := components.WorldPos{}.Add(rl.Vector3{X: -60, Z: 30})
	posMap.Add(hull, &hullPos)
	vehMap.Add(hull, &components.Vehicle{Kind: components.VehicleBTR})
	facMap.Add(hull, &components.Faction{ID: components.FactionPlayer})

	enemyAir := world.NewEntity()
	enemyPos := components.WorldPos{}.Add(rl.Vector3{X: 150, Z: 90})
	posMap.Add(enemyAir, &enemyPos)
	acMap.Add(enemyAir, &components.Aircraft{Kind: components.AircraftHeliAttack})
	facMap.Add(enemyAir, &components.Faction{ID: components.FactionEnemyRed})

	panel := Panel{Bounds: rl.Rectangle{X: 0, Y: 0, Width: 400, Height: 400}}
	ctx := MapRenderCtx{
		Cam:            NewMapCamera(),
		FactionMap:     facMap,
		VehicleFilter:  ecs.NewFilter2[components.WorldPos, components.Vehicle](world),
		AircraftFilter: ecs.NewFilter2[components.WorldPos, components.Aircraft](world),
	}
	content := ContentRect(panel)

	if got := PickOwnEntityAt(MapWorldToPanel(airPos, ctx.Cam, content), ctx, panel, 12); got != air {
		t.Fatalf("aircraft not picked: got %v want %v", got, air)
	}
	if got := PickOwnEntityAt(MapWorldToPanel(hullPos, ctx.Cam, content), ctx, panel, 12); got != hull {
		t.Fatalf("vehicle pick regressed: got %v want %v", got, hull)
	}
	// An enemy airframe is drawn as a contact, never as an own marker — picking
	// it here would hand the player something they cannot command.
	if got := PickOwnEntityAt(MapWorldToPanel(enemyPos, ctx.Cam, content), ctx, panel, 12); got != (ecs.Entity{}) {
		t.Fatalf("enemy aircraft picked as own: got %v", got)
	}
}
