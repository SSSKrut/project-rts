package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// ai_air_cas — Phase 20 M3. A gunship works one hull, and the four claims are
// the whole milestone:
//
//	tight    the airframe arrives weapons-tight (zero EngagementRules) and
//	         fires anyway, because AttackTarget overrides HoldFire — the way a
//	         player commands a gunship is by NAMING the target
//	standoff the missile leaves from past anything the hull could answer with:
//	         a hitscan weapon dies at weaponMaxRange, this one does not
//	unmask   an NOE dial plus terrain means the airframe cannot see what it was
//	         told to kill; it borrows altitude until it can, and gives it back
//	         once the target is gone (the two halves of the pop-up)
//	kill     damage lands through the class multiplier and the sector armour
//	         the round itself picked out of its own approach
//
// Every world write lands before tick 1000, so the save/load rule holds.
const (
	casVerdictAt float32 = 30.0
	casAltDial   float32 = 15.0 // NOE: the dial that makes terrain matter
	casSpeed     float32 = 50.0
	casEntryX    float32 = -900
	casHullX     float32 = 0
	// Floors sit under the numbers they test: optics are 140 m, so acquisition
	// past 250 can only be the radar; the cannon clamps at 192, so a launch
	// past 250 can only be the missile.
	casAcquireFloorM float32 = 250
	casStandoffM     float32 = 250
	casKeepOutM      float32 = 150 // it must never close to gun range
	casUnmaskFloorM  float32 = 4   // borrowed altitude that proves the pop-up
	casSinkTolM      float32 = 5   // back onto the dial after the fight
	// The ridge, stamped rather than hoped for: a scene whose central claim
	// depends on where a procedural hill happened to land measures luck.
	casRidgeX  float32 = -110
	casRidgeH  float32 = 10
	casRidgeR  float32 = 70
	casRidgeAt float32 = 0.3 // after terrain_gen has filled the chunks
)

func aiAirCASSpawn(world *ecs.World, squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory, aircraftFactory *entities.AircraftFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if aircraftFactory == nil || vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z, agl float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z) + agl
		return p
	}

	hull := vehicleFactory.Spawn(wp(casHullX, 0, 0), components.VehicleBTR,
		components.FactionEnemyRed, components.ControllerAI)

	entry := wp(casEntryX, 0, casAltDial)
	aircraftFactory.Arrival(components.AirArrival{
		At:         2.0,
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionPlayer,
		Controller: components.ControllerLocal,
		Entry:      entry,
		Exit:       systems.AirExitFor(entry, 60),
		AltRef:     components.AltAGL,
		AltSet:     casAltDial,
		SpeedSet:   casSpeed,
		// Loadout 0 = ATGM + cannon. Rules left at zero on purpose: an
		// airframe arrives weapons-tight, and the order is what opens fire.
	})

	return &aiTestState{
		sceneID:      aiSceneID(),
		casActive:    true,
		casHull:      hull,
		casAcquireD:  -1,
		casLaunchD:   -1,
		casMinD:      1e9,
		casKillAt:    -1,
		casSankAt:    -1,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		AircraftMap:  ecs.NewMap[components.Aircraft](world),
		AwarenessMap: ecs.NewMap[components.Awareness](world),
		SensorsMap:   ecs.NewMap[components.Sensors](world),
		HPMap:        ecs.NewMap[components.HP](world),
		EquipMap:     ecs.NewMap[components.Equipment](world),
		WeaponMap:    ecs.NewMap[components.Weapon](world),
		airFilter:    ecs.NewFilter1[components.Aircraft](world),
		airSampler:   systems.NewHeightSampler(world),
		casStamper:   systems.NewStamper(world),
		verdictAt:    casVerdictAt,
		nextSampleAt: 4,
	}
}

func (s *aiTestState) updateAirCAS(elapsed float32) {
	if s.verdictDone {
		return
	}
	// Raise the ridge once the procedural fill is in — stamping at spawn time
	// would be overwritten by terrain_gen on the first tick.
	if !s.casRidgeUp && elapsed >= casRidgeAt {
		s.casRidgeUp = true
		centre := components.WorldPos{}.Add(rl.Vector3{X: casRidgeX, Z: 0})
		s.casStamper.StampHeightmap(centre, systems.Crater(-casRidgeH, casRidgeR), casRidgeR)
		fmt.Printf("[ai-test %s] t=%.1fs RIDGE raised at x=%.0f (crest %.0f m, was %.0f)\n",
			s.sceneID, elapsed, casRidgeX,
			s.airSampler.Sample(casRidgeX, 0), systems.GroundHeight(casRidgeX, 0))
	}
	if s.casScout == (ecs.Entity{}) {
		q := s.airFilter.Query()
		for q.Next() {
			s.casScout = q.Entity()
		}
		if s.casScout == (ecs.Entity{}) {
			if elapsed >= s.verdictAt {
				s.casVerdict(elapsed)
			}
			return
		}
		// The radar is the standoff: optics stop at 140 m and the missile
		// reaches 1200, so what buys the range is the emitter — and the
		// emitter is what an ESM-equipped enemy hears (M1's bargain).
		if sn := s.SensorsMap.Get(s.casScout); sn != nil {
			if i := sn.FindChannel(components.SensorRadar); i >= 0 {
				sn.SetChannel(uint8(i), true)
			}
		}
		if eq := s.EquipMap.Get(s.casScout); eq != nil {
			s.casTube = eq.Primary
			if w := s.WeaponMap.Get(s.casTube); w != nil {
				s.casAmmoStart = w.Ammo
				s.casAmmoNow = w.Ammo
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs GUNSHIP RELEASED (radar on, weapons tight)\n",
			s.sceneID, elapsed)
	}

	// The order is the whole control model: tight rules plus a named target.
	if !s.casOrdered && s.World.Alive(s.casHull) {
		if hp := s.PosMap.Get(s.casHull); hp != nil {
			s.SquadService.IssueOrder(s.casScout, components.OrderKindAttackTarget,
				*hp, s.casHull, false, systems.OrderParams{})
			s.casOrdered = true
			fmt.Printf("[ai-test %s] t=%.1fs ORDERED to attack the hull\n", s.sceneID, elapsed)
		}
	}

	if !s.World.Alive(s.casScout) {
		if elapsed >= s.verdictAt {
			s.casVerdict(elapsed)
		}
		return
	}
	pos := s.PosMap.Get(s.casScout)
	if pos == nil {
		return
	}
	agl := systems.AircraftAGL(s.airSampler, *pos)
	if over := agl - casAltDial; over > s.casMaxUnmask {
		s.casMaxUnmask = over
	}
	if w := s.WeaponMap.Get(s.casTube); w != nil {
		if w.Ammo < s.casAmmoNow && s.casLaunchD < 0 {
			s.casLaunchD = s.rangeTo(*pos, s.casHull)
			fmt.Printf("[ai-test %s] t=%.1fs MISSILE AWAY at %.0f m (agl %.0f, dial %.0f)\n",
				s.sceneID, elapsed, s.casLaunchD, agl, casAltDial)
		}
		s.casAmmoNow = w.Ammo
	}

	if s.World.Alive(s.casHull) {
		if d := s.rangeTo(*pos, s.casHull); d < s.casMinD {
			s.casMinD = d
		}
		if hp := s.HPMap.Get(s.casHull); hp != nil {
			if s.casHullHP0 == 0 {
				s.casHullHP0 = hp.Current
			}
			s.casHullHP = hp.Current
		}
		if s.casAcquireD < 0 {
			if aware := s.AwarenessMap.Get(s.casScout); aware != nil {
				for i := range aware.LastSeen {
					if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == s.casHull {
						s.casAcquireD = s.rangeTo(*pos, s.casHull)
						fmt.Printf("[ai-test %s] t=%.1fs HULL ACQUIRED at %.0f m\n",
							s.sceneID, elapsed, s.casAcquireD)
						break
					}
				}
			}
		}
	} else if s.casKillAt < 0 {
		s.casKillAt = elapsed
		s.casHullHP = 0
		fmt.Printf("[ai-test %s] t=%.1fs HULL DESTROYED (min approach %.0f m)\n",
			s.sceneID, elapsed, s.casMinD)
	}
	// The descent half of the pop-up: height given back once the fight is over.
	if s.casKillAt >= 0 && s.casSankAt < 0 && agl-casAltDial <= casSinkTolM {
		s.casSankAt = elapsed
		fmt.Printf("[ai-test %s] t=%.1fs SANK BACK to the dial (agl %.0f)\n",
			s.sceneID, elapsed, agl)
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		d := float32(-1)
		if s.World.Alive(s.casHull) {
			d = s.rangeTo(*pos, s.casHull)
		}
		fmt.Printf("[ai-test %s] t=%.1fs d=%.0f agl=%.0f over=%.0f ammo=%d hullHP=%.0f\n",
			s.sceneID, elapsed, d, agl, agl-casAltDial, s.casAmmoNow, s.casHullHP)
	}
	if elapsed >= s.verdictAt {
		s.casVerdict(elapsed)
	}
}

func (s *aiTestState) casVerdict(elapsed float32) {
	acquire := s.casAcquireD >= casAcquireFloorM
	standoff := s.casLaunchD >= casStandoffM
	keepOut := s.casMinD >= casKeepOutM
	unmask := s.casMaxUnmask >= casUnmaskFloorM
	sank := s.casSankAt >= 0
	killed := s.casKillAt >= 0

	pass := acquire && standoff && keepOut && unmask && sank && killed
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (acquire=%.0fm launch=%.0fm minD=%.0fm unmask=%.0fm sank=%v kill=%.1fs t=%.1fs)\n",
		s.sceneID, verdict, s.casAcquireD, s.casLaunchD, s.casMinD,
		s.casMaxUnmask, sank, s.casKillAt, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}
