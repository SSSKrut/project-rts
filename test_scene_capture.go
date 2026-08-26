package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// lite_capture — block B. The point is the object the game is played over, and
// this measures every claim the model makes about it at once.
//
//	solo      one man takes a neutral point in about ten seconds
//	three     three take it in about a third of that
//	frozen    both sides standing on it stops the clock dead
//	netDrop   losing a point kills its relay, and a squad that was living off
//	          it falls out of contact in the SAME tick — which is the whole
//	          reason points and radios are one mechanic and not two
const (
	capVerdictAt float32 = 34.0
	capStartAt   float32 = 1.0
	// Contest: an enemy walks onto the third point at this time.
	capContestAt float32 = 6.0
	// Lanes far enough apart that no point counts another lane's bodies.
	capLaneZ float32 = 300
	// Expected times, with generous slack: the claim is the RATIO between one
	// man and three, not a stopwatch reading.
	capSoloMin, capSoloMax   float32 = 7, 14
	capThreeMin, capThreeMax float32 = 2, 6
	// The net point's relay: small enough that the squad beside it is Green
	// only while the point is held.
	capRelayM float32 = 120
)

type capProbe struct {
	name    string
	point   ecs.Entity
	takenAt float32
}

func aiCaptureSpawn(world *ecs.World, squadService *systems.SquadService,
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
	man := func(x, z float32, faction uint8) ecs.Entity {
		e := unitFactory(wp(x, z))
		roleService.AssignRole(e, components.RoleRifleman)
		if f := ecs.NewMap[components.Faction](world).Get(e); f != nil {
			f.ID = faction
		}
		if c := ecs.NewMap[components.Controller](world).Get(e); c != nil {
			c.Owner = components.ControllerAI
		}
		// Nobody shoots: the scene is about standing on ground, and a firefight
		// would decide it by casualties instead.
		ecs.NewMap[components.EngagementRules](world).Add(e,
			&components.EngagementRules{Mode: components.HoldFire})
		return e
	}

	// Lane 1 — one man on a neutral point.
	solo := systems.SpawnControlPoint(world, wp(0, 0), components.FactionNone,
		components.ControlPointRadiusM, 0)
	man(0, 0, playerFaction)

	// Lane 2 — three men on an identical point.
	three := systems.SpawnControlPoint(world, wp(0, capLaneZ), components.FactionNone,
		components.ControlPointRadiusM, 0)
	for i := 0; i < 3; i++ {
		man(float32(i)*2, capLaneZ, playerFaction)
	}

	// Lane 3 — a point the player already holds, carrying the net a squad
	// beside it lives on. An enemy walks in and freezes it, then takes it.
	netPoint := systems.SpawnControlPoint(world, wp(0, capLaneZ*2), playerFaction,
		components.ControlPointRadiusM, capRelayM)
	man(0, capLaneZ*2, playerFaction)
	// 25 m of a 120 m node: q = 0.79, comfortably Green while the point is
	// held and zero the moment it is not.
	squad := squadService.CreateFromTemplate(systems.TmplMotorRifle,
		wp(25, capLaneZ*2), components.FormationLine,
		components.Faction{ID: playerFaction},
		components.Controller{Owner: components.ControllerLocal}, roleService, unitFactory)

	return &aiTestState{
		sceneID: aiSceneID(),
		capProbes: []capProbe{
			{name: "solo", point: solo},
			{name: "three", point: three},
		},
		capNetPoint:  netPoint,
		capNetSquad:  squad,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		CommsMap:     ecs.NewMap[components.CommsState](world),
		PointMap:     ecs.NewMap[components.ControlPoint](world),
		capSpawnMan:  man,
		verdictAt:    capVerdictAt,
		orderAt:      capStartAt,
		nextSampleAt: capStartAt + 3,
	}
}

func (s *aiTestState) updateCapture(elapsed float32) {
	if s.verdictDone {
		return
	}
	for i := range s.capProbes {
		p := &s.capProbes[i]
		if p.takenAt > 0 {
			continue
		}
		if cp := s.PointMap.Get(p.point); cp != nil && cp.Owner == components.FactionPlayer {
			p.takenAt = elapsed
			fmt.Printf("[ai-test %s] t=%.1fs %s TAKEN\n", s.sceneID, elapsed, p.name)
		}
	}

	// The contest: one enemy steps onto the held point.
	if elapsed >= capContestAt && !s.capContested {
		s.capContested = true
		s.capSpawnMan(0, capLaneZ*2+2, components.FactionEnemyRed)
		if cp := s.PointMap.Get(s.capNetPoint); cp != nil {
			s.capProgressAtContest = cp.Progress
		}
		if cs := s.CommsMap.Get(s.capNetSquad); cs != nil {
			s.capBandBefore = cs.Band
		}
		fmt.Printf("[ai-test %s] t=%.1fs ENEMY ON THE POINT (band was %s)\n",
			s.sceneID, elapsed, s.capBandBefore)
	}
	if cp := s.PointMap.Get(s.capNetPoint); cp != nil {
		if cp.Contested {
			s.capFrozenSeen = true
			if cp.Progress != s.capProgressAtContest {
				s.capProgressMoved = true
			}
		}
		if cp.Owner != components.FactionPlayer && s.capLostAt == 0 {
			s.capLostAt = elapsed
		}
	}
	if s.capContested {
		if cs := s.CommsMap.Get(s.capNetSquad); cs != nil {
			if cs.Band > s.capBandAfter {
				s.capBandAfter = cs.Band
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		var prog float32
		contested := false
		owner := components.FactionNone
		if cp := s.PointMap.Get(s.capNetPoint); cp != nil {
			prog, contested, owner = cp.Progress, cp.Contested, cp.Owner
		}
		band := components.CommsGreen
		if cs := s.CommsMap.Get(s.capNetSquad); cs != nil {
			band = cs.Band
		}
		fmt.Printf("[ai-test %s] t=%.1fs solo=%.1f three=%.1f | net owner=%d prog=%.2f contested=%v band=%s\n",
			s.sceneID, elapsed, s.capProbes[0].takenAt, s.capProbes[1].takenAt,
			owner, prog, contested, band)
	}
	if elapsed >= s.verdictAt {
		s.captureVerdict(elapsed)
	}
}

func (s *aiTestState) captureVerdict(elapsed float32) {
	s.verdictDone = true
	solo, three := s.capProbes[0].takenAt, s.capProbes[1].takenAt
	soloOK := solo >= capSoloMin && solo <= capSoloMax
	threeOK := three >= capThreeMin && three <= capThreeMax
	// Frozen is TWO facts: the point reported contested, and the clock did not
	// move while it was. Either alone would pass on a point nobody touched.
	frozen := s.capFrozenSeen && !s.capProgressMoved
	netDrop := s.capBandBefore == components.CommsGreen &&
		s.capBandAfter > s.capBandBefore
	pass := soloOK && threeOK && frozen && netDrop
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (solo=%.1fs three=%.1fs band %s->%s | solo=%v three=%v frozen=%v netDrop=%v t=%.1fs)\n",
		s.sceneID, verdict, solo, three, s.capBandBefore, s.capBandAfter,
		soloOK, threeOK, frozen, netDrop, elapsed)
	fmt.Println("============================================================")
}
