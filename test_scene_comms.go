package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// lite_comms_range — block A M0. The number and nothing but the number: no
// order gating, no leash, no direction finding yet.
//
// A LADDER, not a march. The claims are about a formula, and a formula is
// measured by standing at known distances, not by walking between them: a
// squad crosses 800 m of relay range in four minutes, which would put every
// scripted event long past the tick-1000 save/load line. Everything here
// happens at spawn plus one killed radioman at t=8 s.
//
//	ladder   four squads at fixed distances read Comms / Degraded / Cut /
//	         Silent — the bands land where the model says they land
//	halving  killing a squad's radioman halves its quality from the same spot;
//	         that is the whole reason a radioman is a target
//	drop     and from mid-Green that halving costs two bands
//	quiet    nothing flickers: no motion, and hysteresis holds the boundaries
const (
	commsVerdictAt float32 = 14.0
	commsKillAt    float32 = 8.0
	// The zero value of CommsBand is Green (a commander that never joins the
	// net must read as working), so every rung below Green settles once on its
	// first computed tick. Flicker counting starts after that settle.
	commsSettleAt float32 = 2.0
	// Distances chosen against RelayRangeSpawnM = 800: quality is 1 - d/R, so
	// 120 m => 0.85 (Green), 200 => 0.75 (Green, and 0.375 = Cut once halved),
	// 360 => 0.55 (Degraded), 580 => 0.275 (Cut), 760 => 0.05 (Silent).
	commsDistGreen float32 = 120
	commsDistKill  float32 = 200
	commsDistAmber float32 = 360
	commsDistRed   float32 = 580
	commsDistDark  float32 = 760
	// Tolerance on the measured quality against the closed form.
	commsQTol float32 = 0.02
)

// commsProbe is one rung of the ladder.
type commsProbe struct {
	name     string
	squad    ecs.Entity
	radioman ecs.Entity
	distM    float32
	want     components.CommsBand
	q0       float32 // quality sampled before the kill
	q1       float32 // and after
	band0    components.CommsBand
	band1    components.CommsBand
	flips    int
	last     components.CommsBand
	seen     bool
}

func aiCommsSpawn(world *ecs.World, squadService *systems.SquadService,
	roleService *systems.RoleService, unitFactory aiUnitSpawn,
	damageService *systems.DamageService, playerFaction uint8,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if squadService == nil || roleService == nil || damageService == nil {
		fmt.Printf("[ai-test %s] NO SERVICE — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}

	systems.SpawnRelay(world, wp(0, 0), playerFaction, components.RelayRangeSpawnM)

	// Two men per rung: a leader and a radioman. The radioman is the point, so
	// this is the smallest roster that has one.
	rung := func(name string, d float32, want components.CommsBand) commsProbe {
		leader := unitFactory(wp(d, 0))
		radioman := unitFactory(wp(d, 3))
		roleService.AssignRole(leader, components.RoleLeader)
		roleService.AssignRole(radioman, components.RoleRadioOperator)
		sq := squadService.CreateFromUnits([]ecs.Entity{leader, radioman},
			components.FormationLoose)
		return commsProbe{name: name, squad: sq, radioman: radioman, distM: d, want: want}
	}

	probes := []commsProbe{
		rung("green", commsDistGreen, components.CommsGreen),
		rung("kill", commsDistKill, components.CommsGreen),
		rung("amber", commsDistAmber, components.CommsAmber),
		rung("red", commsDistRed, components.CommsRed),
		rung("dark", commsDistDark, components.CommsDark),
	}

	return &aiTestState{
		sceneID:      aiSceneID(),
		commsActive:  true,
		commsProbes:  probes,
		World:        world,
		SquadService: squadService,
		Damage:       damageService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		HPMap:        ecs.NewMap[components.HP](world),
		CommsMap:     ecs.NewMap[components.CommsState](world),
		verdictAt:    commsVerdictAt,
		nextSampleAt: 3,
	}
}

func (s *aiTestState) updateComms(elapsed float32) {
	if s.verdictDone {
		return
	}
	for i := range s.commsProbes {
		p := &s.commsProbes[i]
		cs := s.CommsMap.Get(p.squad)
		if cs == nil {
			continue
		}
		if cs.Band != p.last || !p.seen {
			if p.seen && elapsed >= commsSettleAt {
				p.flips++
			}
			p.last = cs.Band
			p.seen = true
		}
		if !s.commsKilled {
			p.q0, p.band0 = cs.Quality, cs.Band
		} else {
			p.q1, p.band1 = cs.Quality, cs.Band
		}
	}

	if elapsed >= commsKillAt && !s.commsKilled {
		s.commsKilled = true
		for i := range s.commsProbes {
			p := &s.commsProbes[i]
			if p.name != "kill" || !s.World.Alive(p.radioman) {
				continue
			}
			s.Damage.ApplyDeath(p.radioman)
			fmt.Printf("[ai-test %s] t=%.1fs RADIOMAN DOWN at %.0f m (was %s %.2f)\n",
				s.sceneID, elapsed, p.distM, p.band0, p.q0)
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		for i := range s.commsProbes {
			p := &s.commsProbes[i]
			if cs := s.CommsMap.Get(p.squad); cs != nil {
				fmt.Printf("[ai-test %s] t=%.1fs %-6s d=%.0f q=%.3f band=%s\n",
					s.sceneID, elapsed, p.name, p.distM, cs.Quality, cs.Band)
			}
		}
	}
	if elapsed >= s.verdictAt {
		s.commsVerdict(elapsed)
	}
}

func (s *aiTestState) commsVerdict(elapsed float32) {
	ladder, quiet := true, true
	var killed *commsProbe
	for i := range s.commsProbes {
		p := &s.commsProbes[i]
		if p.name == "kill" {
			killed = p
			// The killed rung is allowed exactly one flip: its own drop.
			if p.flips > 1 {
				quiet = false
			}
			continue
		}
		want := 1 - p.distM/components.RelayRangeSpawnM
		if p.band1 != p.want || abs32(p.q1-want) > commsQTol {
			ladder = false
			fmt.Printf("[ai-test %s] LADDER MISS %s: q=%.3f (want %.3f) band=%s (want %s)\n",
				s.sceneID, p.name, p.q1, want, p.band1, p.want)
		}
		if p.flips > 0 {
			quiet = false
		}
	}

	halving, drop := false, false
	if killed != nil && killed.q0 > 0 {
		halving = abs32(killed.q1-killed.q0*components.CommsNoRadioFactor) <= commsQTol
		drop = int(killed.band1)-int(killed.band0) >= 2
	}

	pass := ladder && halving && drop && quiet
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	kq0, kq1 := float32(0), float32(0)
	kb0, kb1 := components.CommsGreen, components.CommsGreen
	if killed != nil {
		kq0, kq1, kb0, kb1 = killed.q0, killed.q1, killed.band0, killed.band1
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (ladder=%v radio %.3f%s -> %.3f%s halved=%v drop=%v quiet=%v t=%.1fs)\n",
		s.sceneID, verdict, ladder, kq0, kb0, kq1, kb1, halving, drop, quiet, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
