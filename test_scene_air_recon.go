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

// ai_air_recon — Phase 20 M1. One ingress, four claims, all of them measured
// as RANGES rather than as "it happened", because every one of them is a
// statement about a number:
//
//	esm      a passive receiver hears a live radar far beyond anything that
//	         images it, and the track it makes is a BEARING with no range
//	silent   switch the radar off and the intercept stops — the emitter, not
//	         the airframe, is what was ever detectable
//	radar    switching one's OWN radar on buys acquisition well past optical
//	audio    a helicopter is HEARD long before it is seen, which is the whole
//	         character of the machine and was silently broken until M1: the
//	         group cull clamped every candidate to 64 m and threw 400 m of
//	         rotor noise away
//
// The two halves of P4's bargain are the same code with the factions swapped.
// This scene watches the player's ESM against an enemy emitter because only
// the player's side keeps Contact entities; the enemy hearing the player's
// radar runs through the identical pass, and the asymmetry is in the
// bookkeeping, not in the detection.
const (
	reconVerdictAt float32 = 45.0
	reconAltDial   float32 = 120.0 // Low band: clean LOS for the radar claim
	reconSpeed     float32 = 45.0
	// Ingress starts west of everything and flies east down the x axis.
	reconEntryX  float32 = -1200
	reconEmitX   float32 = 0    // enemy gunship, radar running
	reconGroundX float32 = -500 // enemy hull, silent, on the way in
	reconGroundZ float32 = 40
	// The airframe stops short of the emitter so the run ends inside optical
	// range — that last stretch is what upgrades the bearing into a track.
	reconEndX float32 = -90
	// Claim floors, set below the spec numbers they are testing so a
	// regression has to be real rather than marginal: ESM reach 1100,
	// radar reach 420, rotor noise at the Low band 240.
	reconESMFloorM   float32 = 900
	reconRadarFloorM float32 = 200
	reconAudioFloorM float32 = 150
	reconBearingTolR float32 = 0.09 // ~5 deg
	// When the enemy shuts its radar down, and how long the intercept must
	// then stay stale to count as "stopped". Both scripted acts land well
	// inside the first thousand ticks on purpose: the save/load gate loads at
	// tick 1000 into a process with NO harness, so a stimulus applied after
	// that point happens in the continuous run and never in the loaded one.
	// That is a rule for every scripted scene, not a quirk of this one.
	reconSilenceAfter float32 = 4.0
	reconSilenceHold  float32 = 5.0
)

func aiAirReconSpawn(world *ecs.World, aircraftFactory *entities.AircraftFactory,
	vehicleFactory *entities.VehicleFactory, posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if aircraftFactory == nil || vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z, agl float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z) + agl
		return p
	}
	entry := wp(reconEntryX, 0, reconAltDial)

	// The player's scout. Radar arrives OFF, as the factory leaves it: the
	// first half of the scene is what a silent airframe can learn.
	aircraftFactory.Arrival(components.AirArrival{
		At:         2.0,
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionPlayer,
		Controller: components.ControllerLocal,
		Entry:      entry,
		Exit:       systems.AirExitFor(entry, 60),
		AltRef:     components.AltAGL,
		AltSet:     reconAltDial,
		SpeedSet:   reconSpeed,
	})

	emitPos := wp(reconEmitX, 0, 60)
	emitter := aircraftFactory.Spawn(components.AirArrival{
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionEnemyRed,
		Controller: components.ControllerAI,
		Entry:      emitPos,
		Exit:       emitPos,
		AltRef:     components.AltAGL,
		AltSet:     60,
	})
	hull := vehicleFactory.Spawn(wp(reconGroundX, reconGroundZ, 0), components.VehicleBTR,
		components.FactionEnemyRed, components.ControllerAI)

	sensorsMap := ecs.NewMap[components.Sensors](world)
	if sn := sensorsMap.Get(emitter); sn != nil {
		if i := sn.FindChannel(components.SensorRadar); i >= 0 {
			sn.SetChannel(uint8(i), true)
		}
	}

	return &aiTestState{
		sceneID:       aiSceneID(),
		reconActive:   true,
		reconEmitter:  emitter,
		reconHull:     hull,
		reconWaypoint: wp(reconEndX, 0, reconAltDial),
		reconESMAt:    -1,
		reconRadarAt:  -1,
		reconAudioAt:  -1,
		World:         world,
		PosMap:        posMap,
		MotionMap:     ecs.NewMap[components.Motion](world),
		VehQueueMap:   ecs.NewMap[components.ActionQueue](world),
		AircraftMap:   ecs.NewMap[components.Aircraft](world),
		SensorsMap:    sensorsMap,
		AwarenessMap:  ecs.NewMap[components.Awareness](world),
		ContactMap:    ecs.NewMap[components.Contact](world),
		regRes:        ecs.NewResource[components.ContactRegistry](world),
		airFilter:     ecs.NewFilter1[components.Aircraft](world),
		verdictAt:     reconVerdictAt,
		nextSampleAt:  6,
	}
}

func (s *aiTestState) updateAirRecon(elapsed float32) {
	if s.verdictDone {
		return
	}
	if s.reconScout == (ecs.Entity{}) {
		s.findReconScout(elapsed)
		if elapsed >= s.verdictAt {
			s.reconVerdict(elapsed)
		}
		return
	}
	if !s.World.Alive(s.reconScout) {
		s.reconVerdict(elapsed)
		return
	}

	pos := s.PosMap.Get(s.reconScout)
	aq := s.VehQueueMap.Get(s.reconScout)
	if pos == nil || aq == nil {
		return
	}
	if !s.reconLegPushed {
		systems.PushAction(aq, components.Action{
			Kind: components.ActionMoveTo, Target: s.reconWaypoint,
		})
		s.reconLegPushed = true
	}

	s.watchESM(elapsed, *pos)
	s.watchOwnRadar(elapsed, *pos)
	s.watchAudio(elapsed, *pos)
	s.driveEmissionsScript(elapsed)

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs x=%.0f dEmit=%.0f dHull=%.0f esm=%.0f radar=%.0f audio=%.0f\n",
			s.sceneID, elapsed, worldX(*pos),
			s.rangeTo(*pos, s.reconEmitter), s.rangeTo(*pos, s.reconHull),
			s.reconESMRange, s.reconRadarRange, s.reconAudioRange)
	}
	if elapsed >= s.verdictAt {
		s.reconVerdict(elapsed)
	}
}

// findReconScout claims the released airframe — the one the traffic system
// spawned, not the enemy this harness placed by hand.
func (s *aiTestState) findReconScout(elapsed float32) {
	q := s.airFilter.Query()
	for q.Next() {
		if e := q.Entity(); e != s.reconEmitter {
			s.reconScout = e
			break
		}
	}
	q.Close()
	if s.reconScout != (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] t=%.1fs SCOUT RELEASED\n", s.sceneID, elapsed)
	}
}

// watchESM records the range and bearing of the first passive intercept, and
// checks the bearing against the truth the harness alone knows.
func (s *aiTestState) watchESM(elapsed float32, pos components.WorldPos) {
	c := s.contactOn(s.reconEmitter)
	if c == nil {
		return
	}
	if s.reconESMAt < 0 {
		if !c.BearingOnly {
			return // ranged before it was ever heard — the claim failed
		}
		s.reconESMAt = elapsed
		s.reconESMRange = s.rangeTo(pos, s.reconEmitter)
		s.reconBearingErr = bearingError(pos, s.PosMap.Get(s.reconEmitter), c.Bearing)
		fmt.Printf("[ai-test %s] t=%.1fs ESM INTERCEPT at %.0f m, bearing err %.1f deg\n",
			s.sceneID, elapsed, s.reconESMRange, s.reconBearingErr*180/math.Pi)
	}
	if c.BearingOnly {
		s.reconLastIntercept = c.LastSeenTime
	} else if !s.reconRanged {
		s.reconRanged = true
		s.reconRangedAt = elapsed
		fmt.Printf("[ai-test %s] t=%.1fs BEARING RANGED by a second source at %.0f m\n",
			s.sceneID, elapsed, s.rangeTo(pos, s.reconEmitter))
	}
}

// watchOwnRadar records where the scout's own set first acquired the silent
// hull. Optical would have to close to 140 m; the radar reaches 420.
func (s *aiTestState) watchOwnRadar(elapsed float32, pos components.WorldPos) {
	if s.reconRadarAt >= 0 || !s.reconRadarOn {
		return
	}
	if c := s.contactOn(s.reconHull); c != nil && !c.BearingOnly {
		s.reconRadarAt = elapsed
		s.reconRadarRange = s.rangeTo(pos, s.reconHull)
		fmt.Printf("[ai-test %s] t=%.1fs RADAR ACQUIRED the hull at %.0f m\n",
			s.sceneID, elapsed, s.reconRadarRange)
	}
}

// watchAudio reads the ENEMY hull's own awareness of the scout. Beyond its
// 60 m optics, the only channel that can have put the airframe there is sound.
func (s *aiTestState) watchAudio(elapsed float32, pos components.WorldPos) {
	if s.reconAudioAt >= 0 {
		return
	}
	aware := s.AwarenessMap.Get(s.reconHull)
	if aware == nil {
		return
	}
	for i := range aware.LastSeen {
		if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == s.reconScout {
			s.reconAudioAt = elapsed
			s.reconAudioRange = s.rangeTo(pos, s.reconHull)
			fmt.Printf("[ai-test %s] t=%.1fs HULL HEARD the scout at %.0f m\n",
				s.sceneID, elapsed, s.reconAudioRange)
			return
		}
	}
}

// driveEmissionsScript is the player's half of the dilemma, played on a timer
// so the run is reproducible: switch the scout's own radar on, then shut the
// enemy's down and watch the intercept die with it.
func (s *aiTestState) driveEmissionsScript(elapsed float32) {
	if !s.reconRadarOn && s.reconESMAt >= 0 && elapsed >= s.reconESMAt+2 {
		if sn := s.SensorsMap.Get(s.reconScout); sn != nil {
			if i := sn.FindChannel(components.SensorRadar); i >= 0 {
				sn.SetChannel(uint8(i), true)
				s.reconRadarOn = true
				fmt.Printf("[ai-test %s] t=%.1fs SCOUT RADAR ON\n", s.sceneID, elapsed)
			}
		}
	}
	if !s.reconSilenced && s.reconESMAt >= 0 && elapsed >= s.reconESMAt+reconSilenceAfter {
		if sn := s.SensorsMap.Get(s.reconEmitter); sn != nil {
			if i := sn.FindChannel(components.SensorRadar); i >= 0 {
				sn.SetChannel(uint8(i), false)
				s.reconSilenced = true
				s.reconSilencedAt = elapsed
				s.reconInterceptAtHush = s.reconLastIntercept
				fmt.Printf("[ai-test %s] t=%.1fs EMITTER WENT SILENT\n", s.sceneID, elapsed)
			}
		}
	}
}

func (s *aiTestState) reconVerdict(elapsed float32) {
	esm := s.reconESMAt >= 0 && s.reconESMRange >= reconESMFloorM &&
		s.reconBearingErr <= reconBearingTolR
	// Silence is proven by the intercept clock standing still: the emitter
	// stopped radiating, so nothing has refreshed the track since.
	silent := s.reconSilenced &&
		elapsed >= s.reconSilencedAt+reconSilenceHold &&
		s.reconLastIntercept <= s.reconInterceptAtHush+0.01
	radar := s.reconRadarAt >= 0 && s.reconRadarRange >= reconRadarFloorM
	audio := s.reconAudioAt >= 0 && s.reconAudioRange >= reconAudioFloorM

	pass := esm && silent && radar && audio
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (esm=%.0fm/%.1fdeg silent=%v ranged=%v radar=%.0fm audio=%.0fm t=%.1fs)\n",
		s.sceneID, verdict, s.reconESMRange, s.reconBearingErr*180/math.Pi,
		silent, s.reconRanged, s.reconRadarRange, s.reconAudioRange, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}

func (s *aiTestState) contactOn(target ecs.Entity) *components.Contact {
	reg := s.regRes.Get()
	if reg == nil || reg.Tracked == nil {
		return nil
	}
	ent, ok := reg.Tracked[target]
	if !ok || !s.World.Alive(ent) {
		return nil
	}
	return s.ContactMap.Get(ent)
}

func (s *aiTestState) rangeTo(from components.WorldPos, target ecs.Entity) float32 {
	p := s.PosMap.Get(target)
	if p == nil {
		return 0
	}
	d := p.Sub(from)
	return float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
}

// bearingError compares the reported bearing with the true one. The receiver
// has no range, so this is the only thing about the track that can be wrong.
func bearingError(from components.WorldPos, target *components.WorldPos, reported float32) float32 {
	if target == nil {
		return math.Pi
	}
	d := target.Sub(from)
	truth := float32(math.Atan2(float64(d.X), float64(d.Z)))
	err := math.Mod(float64(truth-reported)+3*math.Pi, 2*math.Pi) - math.Pi
	return float32(math.Abs(err))
}

func worldX(p components.WorldPos) float32 {
	return float32(p.Chunk.X)*components.ChunkSize + p.Local.X
}
