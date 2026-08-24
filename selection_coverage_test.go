package main

import (
	"strings"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// Closure criterion 3 of Phase 20.7: for a selected unit both circles are
// visible at once — how far it detects and from how far it is detected — and
// toggling the radar changes both. An airframe profile gets rings, never a
// fan.
func TestCoverageReactsToEmitterToggle(t *testing.T) {
	world := ecs.NewWorld(64)
	ecs.AddResource(world, &systems.TerrainChunkIndex{})
	cs := newCoverageState(world)

	ent := world.NewEntity()
	pos := components.WorldPos{}.Add(rl.Vector3{X: 10, Y: 120, Z: -30})
	cs.posMap.Add(ent, &pos)
	sn := components.Sensors{Count: 3}
	sn.Channels[0] = components.SensorChannel{Kind: components.SensorOptical, BaseRangeM: 140}
	sn.Channels[1] = components.SensorChannel{Kind: components.SensorRadar, BaseRangeM: 420, EmitRangeM: 1100}
	sn.Channels[2] = components.SensorChannel{Kind: components.SensorESM, BaseRangeM: 1400}
	sn.SetChannel(1, false)
	cs.sensorsMap.Add(ent, &sn)
	cs.aircraftMap.Add(ent, &components.Aircraft{Kind: components.AircraftHeliAttack})

	sel := []ecs.Entity{ent}
	cs.update(world, sel)
	v := &cs.View
	if !v.Active || !v.AirProfile {
		t.Fatalf("profile not picked: active=%v air=%v", v.Active, v.AirProfile)
	}
	if len(v.Runs) != 0 {
		t.Fatalf("airframe must not get a fan")
	}
	if got := ringKinds(v.Rings); got != "Optical+ESM" {
		t.Fatalf("radar OFF must hide its ring: got %s", got)
	}
	if v.EmitR != 0 {
		t.Fatalf("radar OFF must silence the emission circle: got %.0f", v.EmitR)
	}

	cs.sensorsMap.Get(ent).SetChannel(1, true)
	cs.update(world, sel)
	if got := ringKinds(v.Rings); got != "Optical+Radar+ESM" {
		t.Fatalf("radar ON must add its ring: got %s", got)
	}
	if v.EmitR != 1100 {
		t.Fatalf("emission circle must be the radar's emit range: got %.0f", v.EmitR)
	}
}

func ringKinds(rings []ui.CoverageRing) string {
	parts := make([]string, 0, len(rings))
	for _, r := range rings {
		parts = append(parts, r.Kind.String())
	}
	return strings.Join(parts, "+")
}
