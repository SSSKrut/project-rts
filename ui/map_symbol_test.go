package ui

import (
	"testing"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// symbolWorld builds a squad from the given roles (a nil Vehicle slot means
// infantry) and returns a ctx wired to it plus the squad entity.
type memberSpec struct {
	role    components.UnitRoleKind
	vehicle bool
}

func symbolWorld(t *testing.T, faction uint8, members []memberSpec) (MapRenderCtx, ecs.Entity, *components.CommandRoster) {
	t.Helper()
	world := ecs.NewWorld(64)
	roleMap := ecs.NewMap[components.UnitRole](world)
	vehMap := ecs.NewMap[components.Vehicle](world)
	facMap := ecs.NewMap[components.Faction](world)
	rosterMap := ecs.NewMap[components.CommandRoster](world)
	ovMap := ecs.NewMap[components.SquadSymbolOverride](world)

	var roster components.CommandRoster
	for _, m := range members {
		e := world.NewEntity()
		if m.vehicle {
			vehMap.Add(e, &components.Vehicle{})
		} else {
			roleMap.Add(e, &components.UnitRole{Kind: m.role})
		}
		roster.Members[roster.Count] = e
		roster.Count++
	}
	squad := world.NewEntity()
	rosterMap.Add(squad, &roster)
	facMap.Add(squad, &components.Faction{ID: faction})

	return MapRenderCtx{
		World:            world,
		RoleMap:          roleMap,
		VehicleMap:       vehMap,
		FactionMap:       facMap,
		SquadOverrideMap: ovMap,
	}, squad, rosterMap.Get(squad)
}

func rifles(n int) []memberSpec {
	out := make([]memberSpec, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, memberSpec{role: components.RoleRifleman})
	}
	return out
}

func TestSquadSymbolComposition(t *testing.T) {
	cases := []struct {
		name    string
		faction uint8
		members []memberSpec
		affil   components.Affiliation
		dim     components.Dimension
		icon    components.IconKind
	}{
		{
			name: "rifle squad reads infantry", faction: components.FactionPlayer,
			members: append(rifles(5), memberSpec{role: components.RoleLeader},
				memberSpec{role: components.RoleMachineGunner}),
			affil: components.AffilFriend, dim: components.DimInfantryClass,
			icon: components.IconInfantry,
		},
		{
			name: "one medic does not make a supply squad", faction: components.FactionPlayer,
			members: append(rifles(5), memberSpec{role: components.RoleMedic}),
			affil:   components.AffilFriend, dim: components.DimInfantryClass,
			icon: components.IconInfantry,
		},
		{
			name: "sniper pair reads recon", faction: components.FactionEnemyRed,
			members: []memberSpec{{role: components.RoleSniper}, {role: components.RoleSniper}},
			affil:   components.AffilHostile, dim: components.DimInfantryClass,
			icon: components.IconRecon,
		},
		{
			name: "engineer half reads supply", faction: components.FactionPlayer,
			members: []memberSpec{{role: components.RoleEngineer}, {role: components.RoleDemoMan},
				{role: components.RoleRifleman}, {role: components.RoleRifleman}},
			affil: components.AffilFriend, dim: components.DimInfantryClass,
			icon: components.IconSupply,
		},
		{
			name: "vehicle majority reads armor", faction: components.FactionPlayer,
			members: []memberSpec{{vehicle: true}, {vehicle: true}, {role: components.RoleRifleman}},
			affil:   components.AffilFriend, dim: components.DimVehicleClass,
			icon: components.IconArmor,
		},
		{
			name: "mounted infantry stays infantry", faction: components.FactionPlayer,
			members: append(rifles(4), memberSpec{vehicle: true}),
			affil:   components.AffilFriend, dim: components.DimInfantryClass,
			icon: components.IconInfantry,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, squad, roster := symbolWorld(t, tc.faction, tc.members)
			got := SquadSymbolSpec(ctx, squad, roster)
			if got.Affiliation != tc.affil {
				t.Errorf("affiliation: got %v want %v", got.Affiliation, tc.affil)
			}
			if got.Dimension != tc.dim {
				t.Errorf("dimension: got %v want %v", got.Dimension, tc.dim)
			}
			if got.Icon != tc.icon {
				t.Errorf("icon: got %v want %v", got.Icon, tc.icon)
			}
		})
	}
}

// A player assignment must win over whatever the roster looks like.
func TestSquadSymbolOverrideWins(t *testing.T) {
	ctx, squad, roster := symbolWorld(t, components.FactionPlayer, rifles(6))
	want := components.SymbolSpec{
		Affiliation: components.AffilNeutral,
		Dimension:   components.DimVehicleClass,
		Icon:        components.IconArtillery,
	}
	ecs.NewMap[components.SquadSymbolOverride](ctx.World).
		Add(squad, &components.SquadSymbolOverride{Spec: want})

	if got := SquadSymbolSpec(ctx, squad, roster); got != want {
		t.Fatalf("override ignored: got %+v want %+v", got, want)
	}
}

// Dead members must not count toward the composition majority.
func TestSquadSymbolSkipsDeadMembers(t *testing.T) {
	ctx, squad, roster := symbolWorld(t, components.FactionPlayer,
		[]memberSpec{{role: components.RoleSniper}, {role: components.RoleSniper},
			{role: components.RoleRifleman}, {role: components.RoleRifleman}})
	if got := SquadSymbolSpec(ctx, squad, roster); got.Icon != components.IconRecon {
		t.Fatalf("half snipers should read recon, got %v", got.Icon)
	}
	ctx.World.RemoveEntity(roster.Members[0])
	if got := SquadSymbolSpec(ctx, squad, roster); got.Icon != components.IconInfantry {
		t.Fatalf("one sniper of three should read infantry, got %v", got.Icon)
	}
}
