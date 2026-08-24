package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// ai_air_aa_gun — Phase 20 M2 slice A, four claims, all measured:
//
//	acquire  the crew's RADAR sees the transit far past anything its optics
//	         could (optics 200 m with a rear-facing penalty; floor 300)
//	gate     FireOnAir off = not one round leaves the gun, however long the
//	         target sits in range and in awareness (criterion 6)
//	fire     the moment the rule flips, rounds leave and damage lands
//	danger   near misses reach the airframe's OWN Threat — the stimulus the
//	         M2 air reflexes will read arrives without any ground hash (P10)
//
// The gate flip is scripted before tick 1000 (save/load rule); the verdict
// only reads state and may run later.
const (
	aaVerdictAt float32 = 24.0
	aaAltDial   float32 = 60.0
	aaSpeed     float32 = 50.0
	aaEntryX    float32 = -750
	aaExitX     float32 = 500
	aaEnableAt  float32 = 15.5
	// Floors sit below the spec numbers they test so a regression has to be
	// real: radar 520 vs optics 200; ZU-23 range 380.
	aaAcquireFloorM float32 = 300
	aaHurtFloor     float32 = 60
	aaSuppFloor     float32 = 0.03
)

func aiAirAASpawn(world *ecs.World, squadService *systems.SquadService,
	unitFactory aiUnitSpawn, vehicleFactory *entities.VehicleFactory,
	aircraftFactory *entities.AircraftFactory, posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if aircraftFactory == nil || vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z, agl float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z) + agl
		return p
	}

	gun := vehicleFactory.Spawn(wp(0, 0, 0), components.VehicleAAGun,
		components.FactionEnemyRed, components.ControllerAI)
	guard := unitFactory(wp(6, 6, 0))
	if f := ecs.NewMap[components.Faction](world).Get(guard); f != nil {
		f.ID = components.FactionEnemyRed
	}
	if c := ecs.NewMap[components.Controller](world).Get(guard); c != nil {
		c.Owner = components.ControllerAI
	}
	ecs.NewMap[components.HP](world).Add(guard, &components.HP{Current: 100, Max: 100})

	squad := squadService.CreateFromUnits([]ecs.Entity{gun, guard}, components.FormationLoose)
	// The gate under test must sit ON the squad: CreateFromUnits stamps no
	// EngagementRules, and the squad-less fallback in shouldFire says yes to
	// air on purpose (an AA soloist must defend its sky).
	squadService.EngagementRulesMap().Add(squad, &components.EngagementRules{
		Mode: components.FreeFire, FireOnInf: true, FireOnArm: true, FireOnAir: false,
	})

	entry := wp(aaEntryX, 0, aaAltDial)
	aircraftFactory.Arrival(components.AirArrival{
		At:         2.0,
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionPlayer,
		Controller: components.ControllerLocal,
		Entry:      entry,
		Exit:       systems.AirExitFor(entry, 60),
		AltRef:     components.AltAGL,
		AltSet:     aaAltDial,
		SpeedSet:   aaSpeed,
	})

	return &aiTestState{
		sceneID:      aiSceneID(),
		aaActive:     true,
		aaGun:        gun,
		aaSquad:      squad,
		aaAcquireAt:  -1,
		aaDownAt:     -1,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		AircraftMap:  ecs.NewMap[components.Aircraft](world),
		AwarenessMap: ecs.NewMap[components.Awareness](world),
		HPMap:        ecs.NewMap[components.HP](world),
		EquipMap:     ecs.NewMap[components.Equipment](world),
		WeaponMap:    ecs.NewMap[components.Weapon](world),
		ThreatMap:    ecs.NewMap[components.Threat](world),
		logRes:       ecs.NewResource[components.EventLog](world),
		airFilter:    ecs.NewFilter1[components.Aircraft](world),
		verdictAt:    aaVerdictAt,
		nextSampleAt: 4,
	}
}

func (s *aiTestState) updateAirAA(elapsed float32) {
	if s.verdictDone {
		return
	}
	if s.aaScout == (ecs.Entity{}) {
		q := s.airFilter.Query()
		for q.Next() {
			s.aaScout = q.Entity()
		}
		if s.aaScout != (ecs.Entity{}) {
			fmt.Printf("[ai-test %s] t=%.1fs SCOUT RELEASED\n", s.sceneID, elapsed)
		}
		if elapsed >= s.verdictAt {
			s.aaVerdict(elapsed)
		}
		return
	}

	if s.aaGunWeapon == (ecs.Entity{}) {
		if eq := s.EquipMap.Get(s.aaGun); eq != nil && eq.Primary != (ecs.Entity{}) {
			s.aaGunWeapon = eq.Primary
			if w := s.WeaponMap.Get(s.aaGunWeapon); w != nil {
				s.aaAmmoStart = w.Ammo
				s.aaAmmoNow = w.Ammo
			}
		}
	}
	if w := s.WeaponMap.Get(s.aaGunWeapon); w != nil {
		if !s.aaEnabled && w.Ammo != s.aaAmmoStart {
			s.aaShotsBeforeGate = true
		}
		s.aaAmmoNow = w.Ammo
	}

	if s.World.Alive(s.aaScout) {
		pos := s.PosMap.Get(s.aaScout)
		if pos == nil {
			return
		}
		if !s.aaLegPushed {
			if aq := s.VehQueueMap.Get(s.aaScout); aq != nil {
				exit := components.WorldPos{}.Add(rl.Vector3{X: aaExitX, Z: 0})
				exit.Local.Y = systems.GroundHeight(aaExitX, 0) + aaAltDial
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: exit,
				})
				s.aaLegPushed = true
			}
		}
		if hp := s.HPMap.Get(s.aaScout); hp != nil {
			if s.aaHPStart == 0 {
				s.aaHPStart = hp.Current
			}
			s.aaHPNow = hp.Current
		}
		if t := s.ThreatMap.Get(s.aaScout); t != nil && t.Suppression > s.aaSuppSeen {
			s.aaSuppSeen = t.Suppression
		}
		if s.aaAcquireAt < 0 {
			if aware := s.AwarenessMap.Get(s.aaGun); aware != nil {
				for i := range aware.LastSeen {
					if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == s.aaScout {
						s.aaAcquireAt = elapsed
						s.aaAcquireD = s.rangeTo(*pos, s.aaGun)
						fmt.Printf("[ai-test %s] t=%.1fs CREW ACQUIRED the scout at %.0f m\n",
							s.sceneID, elapsed, s.aaAcquireD)
						break
					}
				}
			}
		}
	} else if s.aaDownAt < 0 {
		s.aaDownAt = elapsed
		fmt.Printf("[ai-test %s] t=%.1fs SCOUT DOWN\n", s.sceneID, elapsed)
	}

	if !s.aaEnabled && elapsed >= aaEnableAt {
		if r := s.SquadService.EngagementRulesMap().Get(s.aaSquad); r != nil {
			r.FireOnAir = true
			s.aaEnabled = true
			fmt.Printf("[ai-test %s] t=%.1fs FIRE ON AIR ENABLED (ammo=%d)\n",
				s.sceneID, elapsed, s.aaAmmoNow)
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		d := float32(0)
		if s.World.Alive(s.aaScout) {
			if pos := s.PosMap.Get(s.aaScout); pos != nil {
				d = s.rangeTo(*pos, s.aaGun)
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs d=%.0f ammo=%d hp=%.0f supp=%.2f\n",
			s.sceneID, elapsed, d, s.aaAmmoNow, s.aaHPNow, s.aaSuppSeen)
	}
	if elapsed >= s.verdictAt {
		s.aaVerdict(elapsed)
	}
}

func (s *aiTestState) aaVerdict(elapsed float32) {
	acquire := s.aaAcquireAt >= 0 && s.aaAcquireAt < aaEnableAt &&
		s.aaAcquireD >= aaAcquireFloorM
	gate := !s.aaShotsBeforeGate
	fired := s.aaAmmoNow < s.aaAmmoStart
	hpLost := s.aaHPStart - s.aaHPNow
	if s.aaDownAt >= 0 {
		hpLost = s.aaHPStart
	}
	hurt := hpLost >= aaHurtFloor
	supp := s.aaSuppSeen >= aaSuppFloor
	// If the airframe died, the log must say so as an air loss, not a KIA.
	downOK := s.aaDownAt < 0 || s.aaAirDownLogged()

	pass := acquire && gate && fired && hurt && supp && downOK
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (acquire=%.0fm@%.1fs gate=%v fired=%d hpLost=%.0f supp=%.2f down=%.1fs event=%v t=%.1fs)\n",
		s.sceneID, verdict, s.aaAcquireD, s.aaAcquireAt, gate,
		int(s.aaAmmoStart-s.aaAmmoNow), hpLost, s.aaSuppSeen, s.aaDownAt, downOK, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}

func (s *aiTestState) aaAirDownLogged() bool {
	log := s.logRes.Get()
	if log == nil {
		return false
	}
	for _, e := range log.Latest(log.Count) {
		if e.Kind == components.EventAirDown {
			return true
		}
	}
	return false
}
