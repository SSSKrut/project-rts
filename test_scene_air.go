package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// ai_air_transit — Phase 20 M0. Nothing here is about combat; it is the four
// claims the flight half has to make good on before anything can be built on
// top of it:
//
//	release   an arrival filed at boot turns into an airframe on schedule,
//	          and only once (a schedule entry left alive respawns forever)
//	route     every leg of the ActionQueue is consumed, in order
//	terrain   an AGL dial is followed over rolling ground, not averaged
//	clearance the airframe never gets nearer the dirt than the floor, so a
//	          mis-set dial cannot fly it into a hill
//	egress    sent home, it leaves the map and stops existing
//
// The map is `hills` on purpose: over flat ground an AGL dial and an AMSL one
// produce the same run, and the terrain-following claim would be untested.
const (
	airArrivalAt float32 = 3.0
	airVerdictAt float32 = 220.0
	airAltDial   float32 = 35.0 // metres AGL — inside the NOE band
	// Dial speed. Above 30 m/s the sub-step split engages, so the suite
	// actually executes that path; below cruise so the airframe holds it.
	airDialSpeed float32 = 45.0
	// Tracking tolerance, set from measurement rather than taste: this route
	// over `hills` at 45 m/s lags the dial by 2.9 m at worst, so 6 m leaves
	// headroom for terrain noise without letting a real regression through.
	// A looser figure would pass a driver that had stopped following the
	// ground at all — the first guess here was 30 m and asserted nothing.
	airAltTolerance float32 = 6.0
	// Tracking is armed by STATE, not by a timer: the airframe is released at
	// transit altitude and has to fly down, and a fixed grace long enough to
	// cover that descent also swallows most of the NOE leg it was supposed to
	// measure (measured: a 32 s grace left a 6 s window and a meaningless
	// altErr=0.0). Arm on first capture of the dial instead.
	airCaptureBand float32 = 3.0
	// Minimum tracked window for the terrain claim to count.
	airTrackMinFor float32 = 10.0
)

func aiAirTransitSpawn(world *ecs.World, aircraftFactory *entities.AircraftFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if aircraftFactory == nil {
		fmt.Printf("[ai-test %s] NO AIRCRAFT FACTORY — aborting\n", aiSceneID())
		return nil
	}
	sampler := systems.NewHeightSampler(world)
	wp := func(x, z, agl float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z) + agl
		return p
	}
	// Enter high from the west, leave the way it came. The entry altitude is
	// deliberately far above the dial so the descent is part of the test.
	entry := wp(-140, 30, 220)
	exit := systems.AirExitFor(entry, 60)

	aircraftFactory.Arrival(components.AirArrival{
		At:         airArrivalAt,
		Kind:       components.AircraftHeliAttack,
		FactionID:  components.FactionPlayer,
		Controller: components.ControllerLocal,
		Entry:      entry,
		Exit:       exit,
		AltRef:     components.AltAGL,
		AltSet:     airAltDial,
		SpeedSet:   airDialSpeed,
	})

	return &aiTestState{
		sceneID:   aiSceneID(),
		airActive: true,
		// Legs of ~300 m: short ones never let the airframe reach its dial
		// speed, and a test that only measures acceleration is not a test of
		// terrain following.
		airWaypoints: []components.WorldPos{
			wp(60, 180, airAltDial),
			wp(320, -40, airAltDial),
			wp(40, -280, airAltDial),
			wp(-220, -60, airAltDial),
		},
		airLegsLeft:  4,
		airMinClear:  1e9,
		World:        world,
		PosMap:       posMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		AircraftMap:  ecs.NewMap[components.Aircraft](world),
		airSampler:   sampler,
		airFilter:    ecs.NewFilter1[components.Aircraft](world),
		orderAt:      airArrivalAt,
		verdictAt:    airVerdictAt,
		nextSampleAt: airArrivalAt + 5,
	}
}

func (s *aiTestState) updateAirTransit(elapsed float32) {
	if s.verdictDone {
		return
	}

	// Release watch. The airframe is not spawned by this harness — the
	// traffic system is, which is the point: the scene proves the schedule
	// path works, not that a factory call works.
	if s.airEnt == (ecs.Entity{}) {
		q := s.airFilter.Query()
		for q.Next() {
			s.airEnt = q.Entity()
			break
		}
		q.Close()
		if s.airEnt != (ecs.Entity{}) {
			s.airReleasedAt = elapsed
			fmt.Println("============================================================")
			fmt.Printf("== AI AIR SCENE: %s  released at t=%.1fs (scheduled %.1f)\n",
				s.sceneID, elapsed, airArrivalAt)
			fmt.Println("============================================================")
		}
		if elapsed >= s.verdictAt {
			s.airVerdict(elapsed)
		}
		return
	}

	if !s.World.Alive(s.airEnt) {
		if s.airDespawnedAt == 0 {
			s.airDespawnedAt = elapsed
			fmt.Printf("[ai-test %s] t=%.1fs CLEARED THE MAP\n", s.sceneID, elapsed)
		}
		s.airVerdict(elapsed)
		return
	}

	pos := s.PosMap.Get(s.airEnt)
	mot := s.MotionMap.Get(s.airEnt)
	aq := s.VehQueueMap.Get(s.airEnt)
	ac := s.AircraftMap.Get(s.airEnt)
	if pos == nil || mot == nil || aq == nil || ac == nil {
		return
	}

	if !s.airLegsPushed {
		for _, w := range s.airWaypoints {
			systems.PushAction(aq, components.Action{
				Kind: components.ActionMoveTo, Target: w,
			})
		}
		s.airLegsPushed = true
		fmt.Printf("[ai-test %s] t=%.1fs %d LEGS PUSHED\n",
			s.sceneID, elapsed, len(s.airWaypoints))
	}

	agl := systems.AircraftAGL(s.airSampler, *pos)
	if agl < s.airMinClear {
		s.airMinClear = agl
	}
	err := agl - ac.AltSet
	if err < 0 {
		err = -err
	}
	if !s.airCaptured && err <= airCaptureBand {
		s.airCaptured = true
		s.airCaptureAt = elapsed
		fmt.Printf("[ai-test %s] t=%.1fs CAPTURED %.0f m AGL\n",
			s.sceneID, elapsed, ac.AltSet)
	}
	if s.airCaptured && !ac.Egressing {
		s.airTrackedFor = elapsed - s.airCaptureAt
		if err > s.airAltErrMax {
			s.airAltErrMax = err
		}
	}
	if int(aq.Count) < s.airLegsLeft {
		s.airLegsLeft = int(aq.Count)
	}

	// Route flown: send it home. That is the one live command M0 owns, and
	// it must go through the shared verb so a pad-era abort behaves the same.
	if s.airLegsPushed && aq.Count == 0 && !s.airSentHome {
		systems.SendHome(ac, aq)
		s.airSentHome = true
		fmt.Printf("[ai-test %s] t=%.1fs ROUTE FLOWN, SENT HOME\n", s.sceneID, elapsed)
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		fmt.Printf("[ai-test %s] t=%.1fs pos=(%.0f,%.0f) agl=%.1f band=%s speed=%.1f legs=%d fuel=%.0f\n",
			s.sceneID, elapsed, wx, wz, agl, components.AltBandOf(agl),
			mot.Speed, aq.Count, ac.Fuel)
	}

	if elapsed >= s.verdictAt {
		s.airVerdict(elapsed)
	}
}

func (s *aiTestState) airVerdict(elapsed float32) {
	released := s.airReleasedAt > 0
	onTime := released && s.airReleasedAt >= airArrivalAt && s.airReleasedAt < airArrivalAt+1
	flew := s.airLegsLeft == 0
	// The window has to be long enough for the claim to mean something: a
	// capture on the last second would otherwise report a perfect altErr.
	tracked := s.airCaptured && s.airTrackedFor >= airTrackMinFor &&
		s.airAltErrMax <= airAltTolerance
	cleared := s.airMinClear >= 5.0
	gone := s.airDespawnedAt > 0

	pass := onTime && flew && tracked && cleared && gone
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (released=%.1f legsLeft=%d capture=%.1f tracked=%.1fs altErr=%.1f minClear=%.1f gone=%.1f t=%.1fs)\n",
		s.sceneID, verdict, s.airReleasedAt, s.airLegsLeft, s.airCaptureAt,
		s.airTrackedFor, s.airAltErrMax, s.airMinClear, s.airDespawnedAt, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}
