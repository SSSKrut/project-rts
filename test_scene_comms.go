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

// lite_comms_orders — block A M1. The delivery gate, one rung per band, plus
// the one case that makes the mechanic a decision instead of a punishment:
// a plan made while cut off arrives on its own the moment the net comes back,
// without the player clicking a second time.
//
//	green   order runs at once
//	amber   order runs, but only after the degraded net has paid its seconds
//	cut     order is BUILT and never runs — and the squad it was aimed at has
//	        not moved either
//	return  same as cut, until a relay comes up nearby; then it runs itself
const (
	commsOrdVerdictAt float32 = 14.0
	commsOrdOrderAt   float32 = 1.0
	commsOrdRescueAt  float32 = 6.0
	// Far enough that no rung finishes its march inside the scene: a completed
	// order is despawned, and the verdict needs the entity to still be there.
	commsOrdMoveM float32 = 400
	// Green must be indistinguishable from no gate at all.
	commsOrdInstant float32 = 0.5
	// Rescue: one resolver pass plus the sampling grain.
	commsOrdRescueWin float32 = 2.0
	commsOrdRescueD   float32 = 700
)

type commsOrderProbe struct {
	name      string
	squad     ecs.Entity
	order     ecs.Entity
	distM     float32
	issuedAt  float32
	startedAt float32 // 0 = never ran
	movedM    float32
	origin    components.WorldPos
}

func aiCommsOrdersSpawn(world *ecs.World, squadService *systems.SquadService,
	roleService *systems.RoleService, unitFactory aiUnitSpawn,
	playerFaction uint8, posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if squadService == nil || roleService == nil {
		fmt.Printf("[ai-test %s] NO SERVICE — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	systems.SpawnRelay(world, wp(0, 0), playerFaction, components.RelayRangeSpawnM)

	rung := func(name string, x, z float32) commsOrderProbe {
		leader := unitFactory(wp(x, z))
		radioman := unitFactory(wp(x, z+3))
		roleService.AssignRole(leader, components.RoleLeader)
		roleService.AssignRole(radioman, components.RoleRadioOperator)
		sq := squadService.CreateFromUnits([]ecs.Entity{leader, radioman},
			components.FormationLoose)
		return commsOrderProbe{name: name, squad: sq, distM: x, origin: wp(x, z)}
	}
	probes := []commsOrderProbe{
		rung("green", commsDistGreen, 0),
		rung("amber", commsDistAmber, 0),
		rung("cut", commsDistDark, 0),
		rung("return", 0, commsDistDark),
	}
	return &aiTestState{
		sceneID:      aiSceneID(),
		commsOrders:  probes,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		CommsMap:     ecs.NewMap[components.CommsState](world),
		StateMap:     ecs.NewMap[components.OrderState](world),
		playerFac:    playerFaction,
		verdictAt:    commsOrdVerdictAt,
		orderAt:      commsOrdOrderAt,
		nextSampleAt: commsOrdOrderAt + 2,
	}
}

func (s *aiTestState) updateCommsOrders(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		s.orderFired = true
		for i := range s.commsOrders {
			p := &s.commsOrders[i]
			// Every rung is told to march the same distance outward, so "did it
			// move" is one number and not four.
			goal := p.origin
			if p.name == "return" {
				goal = goal.Add(rl.Vector3{Z: commsOrdMoveM})
			} else {
				goal = goal.Add(rl.Vector3{X: -commsOrdMoveM})
			}
			p.order = s.SquadService.IssueOrder(p.squad, components.OrderKindMoveTo,
				goal, ecs.Entity{}, false, systems.OrderParams{})
			p.issuedAt = elapsed
			band := components.CommsGreen
			if cs := s.CommsMap.Get(p.squad); cs != nil {
				band = cs.Band
			}
			fmt.Printf("[ai-test %s] t=%.1fs ORDER to %-6s band=%s\n",
				s.sceneID, elapsed, p.name, band)
		}
		return
	}

	if elapsed >= commsOrdRescueAt && !s.commsRescued {
		s.commsRescued = true
		for i := range s.commsOrders {
			if s.commsOrders[i].name != "return" {
				continue
			}
			at := components.WorldPos{}.Add(rl.Vector3{Z: commsOrdRescueD})
			at.Local.Y = systems.GroundHeight(0, commsOrdRescueD)
			systems.SpawnRelay(s.World, at, s.playerFac, components.RelayRangePointM)
			fmt.Printf("[ai-test %s] t=%.1fs RELAY UP at %.0f m — nobody clicked anything\n",
				s.sceneID, elapsed, commsOrdRescueD)
		}
	}

	for i := range s.commsOrders {
		p := &s.commsOrders[i]
		if p.startedAt == 0 && p.order != (ecs.Entity{}) {
			started := !s.World.Alive(p.order)
			if st := s.StateMap.Get(p.order); st != nil {
				started = started || st.Code == components.OrderStateInProgress ||
					st.Code == components.OrderStateCompleted
			}
			if started {
				p.startedAt = elapsed
				fmt.Printf("[ai-test %s] t=%.1fs %-6s ORDER RUNNING (+%.1fs)\n",
					s.sceneID, elapsed, p.name, elapsed-p.issuedAt)
			}
		}
		if roster := s.RosterMap.Get(p.squad); roster != nil {
			if now, ok := systems.SquadAnchorPos(s.World, roster, s.PosMap); ok {
				if d := balXZ(now, p.origin); d > p.movedM {
					p.movedM = d
				}
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		for i := range s.commsOrders {
			p := &s.commsOrders[i]
			band := components.CommsGreen
			if cs := s.CommsMap.Get(p.squad); cs != nil {
				band = cs.Band
			}
			fmt.Printf("[ai-test %s] t=%.1fs %-6s band=%-8s started=%.1f moved=%.0fm\n",
				s.sceneID, elapsed, p.name, band, p.startedAt, p.movedM)
		}
	}
	if elapsed >= s.verdictAt {
		s.commsOrdersVerdict(elapsed)
	}
}

func (s *aiTestState) commsOrdersVerdict(elapsed float32) {
	get := func(name string) *commsOrderProbe {
		for i := range s.commsOrders {
			if s.commsOrders[i].name == name {
				return &s.commsOrders[i]
			}
		}
		return nil
	}
	green, amber, cut, ret := get("green"), get("amber"), get("cut"), get("return")
	if green == nil || amber == nil || cut == nil || ret == nil {
		fmt.Printf("[ai-test %s] MISSING PROBE — aborting\n", s.sceneID)
		s.verdictDone = true
		return
	}
	lag := func(p *commsOrderProbe) float32 {
		if p.startedAt == 0 {
			return -1
		}
		return p.startedAt - p.issuedAt
	}
	gl, al, rl2 := lag(green), lag(amber), lag(ret)

	instant := gl >= 0 && gl <= commsOrdInstant
	delayed := al >= components.CommsDeliverFastS && al <= components.CommsDeliverSlowS+1
	// Cut is TWO claims in one: the order never ran, and the squad it was aimed
	// at is still where it was. A gate that only checked state would pass on an
	// order that was silently executed anyway.
	blocked := cut.startedAt == 0 && cut.movedM < 5
	rescued := rl2 >= 0 && ret.startedAt >= commsOrdRescueAt &&
		ret.startedAt-commsOrdRescueAt <= commsOrdRescueWin

	pass := instant && delayed && blocked && rescued
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (green=+%.1fs amber=+%.1fs cut=%.0fm ran=%v return=+%.1fs | instant=%v delayed=%v blocked=%v rescued=%v t=%.1fs)\n",
		s.sceneID, verdict, gl, al, cut.movedM, cut.startedAt > 0, rl2,
		instant, delayed, blocked, rescued, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}
