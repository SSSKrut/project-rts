package main

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// ai_air_manpads — Phase 20 M2 slice B. A MANPADS gunner against a transiting
// gunship; every claim is about the missile being an ENTITY:
//
//	launch   a round leaves the tube and exists in the world
//	warn     the airframe reacts while the round is still FLYING — the flare
//	         override arms in the warn window, which is the entire reason the
//	         missile is not an instant roll (P6)
//	resolve  the duel ends one of the two honest ways: the round takes the
//	         bait (decoy roll) or damage lands; either way the near pass is
//	         measured in metres
//	abort    an airframe cut below the HP floor breaks off and egresses
//
// All world writes happen at spawn; everything after is sim-driven, so the
// tick-1000 save/load rule is satisfied by construction.
const (
	manVerdictAt float32 = 30.0
	manAltDial   float32 = 60.0
	manSpeed     float32 = 50.0
	manEntryX    float32 = -800
	manExitX     float32 = 600
	manSightM    float32 = 950 // the gunner's cued sight; stock optics are 40
	manHurtFloor float32 = 150 // Igla damage 200; a hit must read as a hit
)

func aiAirManpadsSpawn(world *ecs.World, unitFactory aiUnitSpawn,
	aircraftFactory *entities.AircraftFactory, posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if aircraftFactory == nil {
		fmt.Printf("[ai-test %s] NO FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z, agl float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z) + agl
		return p
	}

	gunner := unitFactory(wp(0, 0, 0))
	if f := ecs.NewMap[components.Faction](world).Get(gunner); f != nil {
		f.ID = components.FactionEnemyRed
	}
	if c := ecs.NewMap[components.Controller](world).Get(gunner); c != nil {
		c.Owner = components.ControllerAI
	}
	ecs.NewMap[components.HP](world).Add(gunner, &components.HP{Current: 100, Max: 100})
	sensorsMap := ecs.NewMap[components.Sensors](world)
	if sn := sensorsMap.Get(gunner); sn != nil {
		sn.Channels[0].BaseRangeM = manSightM
	}

	// The tube, by hand — RoleService knows roles, not this bench.
	spec := components.SpecForWeapon(components.WeaponIgla)
	tube := world.NewEntity()
	weaponMap := ecs.NewMap[components.Weapon](world)
	weaponMap.Add(tube, &components.Weapon{
		Kind:       components.WeaponIgla,
		Ammo:       spec.Ammo,
		RangeM:     spec.RangeM,
		RoF:        spec.RoF,
		Damage:     spec.Damage,
		Dispersion: spec.Dispersion,
	})
	ecs.NewMap[components.OwnedBy](world).Add(tube, &components.OwnedBy{Owner: gunner})
	tubePos := wp(0, 0, 0)
	posMap.Add(tube, &tubePos)
	equipMap := ecs.NewMap[components.Equipment](world)
	if equipMap.Has(gunner) {
		eq := equipMap.Get(gunner)
		eq.Primary = tube
		eq.Active = tube
	} else {
		equipMap.Add(gunner, &components.Equipment{Primary: tube, Active: tube})
	}

	entry := wp(manEntryX, 0, manAltDial)
	aircraftFactory.Arrival(components.AirArrival{
		At:         2.6,
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionPlayer,
		Controller: components.ControllerLocal,
		Entry:      entry,
		Exit:       systems.AirExitFor(entry, 60),
		AltRef:     components.AltAGL,
		AltSet:     manAltDial,
		SpeedSet:   manSpeed,
	})

	return &aiTestState{
		sceneID:      aiSceneID(),
		manActive:    true,
		manGunner:    gunner,
		manTube:      tube,
		manAmmoStart: spec.Ammo,
		manAmmoNow:   spec.Ammo,
		manMinDist:   1e9,
		manDownAt:    -1,
		World:        world,
		PosMap:       posMap,
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		AircraftMap:  ecs.NewMap[components.Aircraft](world),
		SensorsMap:   sensorsMap,
		HPMap:        ecs.NewMap[components.HP](world),
		WeaponMap:    weaponMap,
		AirOvMap:     ecs.NewMap[components.AircraftOverride](world),
		MissileF:     ecs.NewFilter2[components.Missile, components.WorldPos](world),
		airFilter:    ecs.NewFilter1[components.Aircraft](world),
		verdictAt:    manVerdictAt,
		nextSampleAt: 4,
	}
}

func (s *aiTestState) updateAirManpads(elapsed float32) {
	if s.verdictDone {
		return
	}
	if s.manScout == (ecs.Entity{}) {
		q := s.airFilter.Query()
		for q.Next() {
			s.manScout = q.Entity()
		}
		if s.manScout != (ecs.Entity{}) {
			fmt.Printf("[ai-test %s] t=%.1fs SCOUT RELEASED\n", s.sceneID, elapsed)
		}
		if elapsed >= s.verdictAt {
			s.manVerdict(elapsed)
		}
		return
	}

	if w := s.WeaponMap.Get(s.manTube); w != nil && w.Ammo < s.manAmmoNow {
		fmt.Printf("[ai-test %s] t=%.1fs LAUNCH (ammo %d -> %d)\n",
			s.sceneID, elapsed, s.manAmmoNow, w.Ammo)
		s.manAmmoNow = w.Ammo
	}

	scoutAlive := s.World.Alive(s.manScout)
	var scoutX, scoutY, scoutZ float32
	if scoutAlive {
		if pos := s.PosMap.Get(s.manScout); pos != nil {
			scoutX = float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
			scoutZ = float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
			scoutY = pos.Local.Y
		}
		if !s.manLegPushed {
			if aq := s.VehQueueMap.Get(s.manScout); aq != nil {
				exit := components.WorldPos{}.Add(rl.Vector3{X: manExitX, Z: 0})
				exit.Local.Y = systems.GroundHeight(manExitX, 0) + manAltDial
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: exit,
				})
				s.manLegPushed = true
			}
		}
		if hp := s.HPMap.Get(s.manScout); hp != nil {
			if s.manHPStart == 0 {
				s.manHPStart = hp.Current
			}
			s.manHPNow = hp.Current
		}
		if ov := s.AirOvMap.Get(s.manScout); ov != nil {
			if ov.Kind == components.AirReflexFlare {
				if !s.manFlareSeen {
					fmt.Printf("[ai-test %s] t=%.1fs FLARES OUT\n", s.sceneID, elapsed)
				}
				s.manFlareSeen = true
			}
			if ov.Kind == components.AirReflexAbort {
				s.manAbortSeen = true
			}
		}
		if ac := s.AircraftMap.Get(s.manScout); ac != nil && ac.Egressing {
			if !s.manEgressSeen {
				fmt.Printf("[ai-test %s] t=%.1fs SCOUT BREAKS OFF\n", s.sceneID, elapsed)
			}
			s.manEgressSeen = true
		}
	} else if s.manDownAt < 0 && !s.manLeft {
		// Gone from the world: an egressing airframe LEFT (traffic despawn at
		// the exit ring — the successful outcome of Abort); anything else is
		// a kill.
		if s.manEgressSeen {
			s.manLeft = true
			fmt.Printf("[ai-test %s] t=%.1fs SCOUT LEFT THE MAP\n", s.sceneID, elapsed)
		} else {
			s.manDownAt = elapsed
			fmt.Printf("[ai-test %s] t=%.1fs SCOUT DOWN\n", s.sceneID, elapsed)
		}
	}

	qm := s.MissileF.Query()
	for qm.Next() {
		m, mpos := qm.Get()
		s.manMissileSeen = true
		if m.Decoyed && !s.manDecoySeen {
			s.manDecoySeen = true
			fmt.Printf("[ai-test %s] t=%.1fs MISSILE DECOYED\n", s.sceneID, elapsed)
		}
		if scoutAlive {
			mx := float32(mpos.Chunk.X)*components.ChunkSize + mpos.Local.X
			mz := float32(mpos.Chunk.Z)*components.ChunkSize + mpos.Local.Z
			dx, dy, dz := scoutX-mx, scoutY-mpos.Local.Y, scoutZ-mz
			if d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz))); d < s.manMinDist {
				s.manMinDist = d
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs ammo=%d hp=%.0f flare=%v decoy=%v minD=%.0f\n",
			s.sceneID, elapsed, s.manAmmoNow, s.manHPNow,
			s.manFlareSeen, s.manDecoySeen, s.manMinDist)
	}
	if elapsed >= s.verdictAt {
		s.manVerdict(elapsed)
	}
}

func (s *aiTestState) manVerdict(elapsed float32) {
	launched := s.manMissileSeen && s.manAmmoNow < s.manAmmoStart
	hpLost := s.manHPStart - s.manHPNow
	if s.manDownAt >= 0 {
		hpLost = s.manHPStart
	}
	// One honest ending per round: the bait taken, or the warhead felt.
	resolved := s.manDecoySeen || hpLost >= manHurtFloor
	// The HP floor is 40%: an airframe cut below it must have broken off
	// (or never dropped that far at all).
	belowFloor := s.manHPStart > 0 && s.manHPNow < s.manHPStart*0.4
	abortOK := !belowFloor || s.manEgressSeen || s.manDownAt >= 0

	pass := launched && s.manFlareSeen && resolved && abortOK
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (launches=%d flare=%v decoy=%v hpLost=%.0f minD=%.0fm egress=%v left=%v down=%.1fs t=%.1fs)\n",
		s.sceneID, verdict, int(s.manAmmoStart-s.manAmmoNow), s.manFlareSeen,
		s.manDecoySeen, hpLost, s.manMinDist, s.manEgressSeen, s.manLeft, s.manDownAt, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}
