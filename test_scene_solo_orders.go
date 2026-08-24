package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// ai_solo_orders — Phase 20.7 L0. A lone vehicle is not a squad, and until now
// it could not hold an order either: the RMB path wrote its ActionQueue
// directly and every surface that answers "what is this doing" reads orders.
// This scene locks the lifecycle that replaced it:
//
//	commandable  an order makes the soloist an order-holder — and it must NOT
//	             make it a squad, or formation and the squad brain would take
//	             over a truck
//	executes     the head order reaches its ActionQueue in the same tick, so it
//	             drives exactly as the old direct push did
//	completes    arrival ends the order and the chain advances to the next leg
//	remembered   the finished order lands in OrderHistory under the vehicle
//	yields       joining a squad strips the solo command — two commanders for
//	             one body is how an order outlives its own cancellation
const (
	soloVerdictAt float32 = 45.0
	soloOrderAt   float32 = 2.0
	// Legs are short on purpose. Everything this scene DOES to the world —
	// the orders and the merge — has to land before the save/load gate's
	// snapshot at tick 1000, because the loaded run has no harness and would
	// simply never do it. That is a rule for any scripted scene.
	soloLegAX float32 = 55
	soloLegBZ float32 = 55
)

func aiSoloOrderSceneSpawn(world *ecs.World, squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory, posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY — aborting\n", aiSceneID())
		return nil
	}
	at := func(x, z float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: x, Y: systems.GroundHeight(x, z), Z: z})
	}
	veh := vehicleFactory.Spawn(at(0, 0), components.VehicleBTR,
		components.FactionPlayer, components.ControllerLocal)
	mate := vehicleFactory.Spawn(at(-14, 0), components.VehicleBTR,
		components.FactionPlayer, components.ControllerLocal)

	return &aiTestState{
		sceneID:      aiSceneID(),
		soloActive:   true,
		soloVeh:      veh,
		soloMate:     mate,
		soloLegA:     at(soloLegAX, 0),
		soloLegB:     at(soloLegAX, soloLegBZ),
		soloSvc:      squadService,
		World:        world,
		PosMap:       posMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		soloHeadMap:  ecs.NewMap[components.OrderQueueHead](world),
		soloHistRes:  ecs.NewResource[components.OrderHistory](world),
		orderAt:      soloOrderAt,
		verdictAt:    soloVerdictAt,
		nextSampleAt: soloOrderAt + 5,
	}
}

func (s *aiTestState) updateSoloOrders(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.World.Alive(s.soloVeh) {
		s.soloVerdict(elapsed)
		return
	}

	if !s.soloIssued && elapsed >= s.orderAt {
		s.soloIssued = true
		// Two legs through the same entry point the player's RMB uses, the
		// second appended — a chain is the thing an ActionQueue could not
		// express, so the scene has to exercise one.
		s.soloSvc.IssueOrder(s.soloVeh, components.OrderKindMoveTo, s.soloLegA,
			ecs.Entity{}, false, systems.OrderParams{})
		s.soloSvc.IssueOrder(s.soloVeh, components.OrderKindMoveTo, s.soloLegB,
			ecs.Entity{}, true, systems.OrderParams{})
		s.soloBecameCommander = s.soloHeadMap.Has(s.soloVeh)
		s.soloStayedNonSquad = !s.soloSvc.IsSquad(s.soloVeh)
		// The order must reach the queue NOW, not next tick: a tick of delay
		// is a behaviour change hiding inside a refactor.
		if aq := s.VehQueueMap.Get(s.soloVeh); aq != nil && aq.Count > 0 {
			s.soloDroveSameTick = true
		}
		fmt.Printf("[ai-test %s] t=%.1fs ORDERED  commandable=%v nonSquad=%v drove=%v\n",
			s.sceneID, elapsed, s.soloBecameCommander, s.soloStayedNonSquad, s.soloDroveSameTick)
	}
	if !s.soloIssued {
		return
	}

	s.trackSoloChain(elapsed)

	// Route flown: merge into a squad and confirm the solo command is gone.
	if s.soloLegsDone >= 2 && !s.soloMerged {
		s.soloMerged = true
		s.soloSvc.CreateFromUnits([]ecs.Entity{s.soloVeh, s.soloMate}, components.FormationLine)
		s.soloCommandDropped = !s.soloHeadMap.Has(s.soloVeh)
		fmt.Printf("[ai-test %s] t=%.1fs MERGED  soloCommandDropped=%v\n",
			s.sceneID, elapsed, s.soloCommandDropped)
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		p := s.PosMap.Get(s.soloVeh)
		queued := 0
		if aq := s.VehQueueMap.Get(s.soloVeh); aq != nil {
			queued = int(aq.Count)
		}
		fmt.Printf("[ai-test %s] t=%.1fs pos=(%.0f,%.0f) legsDone=%d hist=%d queued=%d\n",
			s.sceneID, elapsed, worldX(*p),
			float32(p.Chunk.Z)*components.ChunkSize+p.Local.Z,
			s.soloLegsDone, s.soloHistCount, queued)
	}

	if s.soloMerged && s.soloCommandDropped {
		s.soloVerdict(elapsed)
		return
	}
	if elapsed >= s.verdictAt {
		s.soloVerdict(elapsed)
	}
}

// trackSoloChain counts completed legs by watching the head pointer move, and
// tallies what the history ring kept for this vehicle.
func (s *aiTestState) trackSoloChain(elapsed float32) {
	head := s.soloHeadMap.Get(s.soloVeh)
	if head == nil {
		return
	}
	if head.First != s.soloLastHead {
		if s.soloLastHead != (ecs.Entity{}) {
			s.soloLegsDone++
			fmt.Printf("[ai-test %s] t=%.1fs LEG %d DONE\n", s.sceneID, elapsed, s.soloLegsDone)
		}
		s.soloLastHead = head.First
	}
	if hist := s.soloHistRes.Get(); hist != nil {
		n := 0
		hist.Each(func(r components.OrderRecord) {
			if r.Commander == s.soloVeh {
				n++
			}
		})
		s.soloHistCount = n
	}
}

func (s *aiTestState) soloVerdict(elapsed float32) {
	pass := s.soloBecameCommander && s.soloStayedNonSquad && s.soloDroveSameTick &&
		s.soloLegsDone >= 2 && s.soloHistCount >= 2 && s.soloCommandDropped
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (commandable=%v nonSquad=%v sameTick=%v legs=%d hist=%d dropped=%v t=%.1fs)\n",
		s.sceneID, verdict, s.soloBecameCommander, s.soloStayedNonSquad,
		s.soloDroveSameTick, s.soloLegsDone, s.soloHistCount, s.soloCommandDropped, elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}
