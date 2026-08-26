package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// Balance scenes. They exist to MEASURE, not to be won.
//
// The statement is the PAIR, never one scene: armour must break a squad caught
// in the open, and the same squad lying in wait must break armour. One green
// scene proves only that the numbers point one way — which is exactly how a
// balance pass goes wrong.
//
//	lite_balance_open    three BMP roll over a standing squad; hulls win, and
//	                     cheaply
//	lite_balance_ambush  same forces, squad prone and holding fire to knife
//	                     range; the ambush costs the column hulls
//	lite_balance_eyes    nobody fires — four first-contact ranges, and the
//	                     ORDER between them is the whole claim
//
// Every verdict line prints its numbers whether it passes or not: a balance
// gate that only says PASS/FAIL cannot be used to tune anything.

type balanceMode uint8

const (
	balanceOpen balanceMode = iota
	balanceAmbush
	balanceEyes
)

const (
	balOrderAt       float32 = 1.0
	balVerdictAt     float32 = 55.0
	balEyesVerdictAt float32 = 35.0

	// Open field: hulls start outside every optic and drive straight at the
	// squad, stopping short so the verdict measures gunnery, not a ram.
	balHullStartX float32 = -85
	balHullGoalX  float32 = -22
	balInfX       float32 = 0

	// Ambush: the column TRANSITS instead of assaulting, and the squad lies
	// off to one side. Holding fire buys nothing against a hull that could
	// not see them anyway — what an ambush actually buys is the flank
	// (ArmorSide 0.6 against ArmorFront 0.45) at a range it picked.
	balAmbInfZ    float32 = 32
	balAmbHullEnd float32 = 85

	// Weapons free once the lead hull is this close — the ambush's whole
	// advantage is the first volley at a range it chose.
	balAmbushTriggerM float32 = 40

	// How long after the first volley the player is allowed to still have no
	// marker anywhere. The complaint this answers is "infantry opens fire and
	// nothing appears on the map"; a shot IS an emission, so the answer has to
	// arrive in about the time it takes to notice men dying.
	balSpotWindow float32 = 3.0

	// Thresholds are deliberately loose. A gate that pins the exact second of
	// a wipe goes red on every number change, which is the opposite of what a
	// balance gate is for.
	balOpenWipeBy   float32 = 40
	balOpenHullsMin int     = 2
	balAmbHullsKill int     = 1
	balAmbInfMin    int     = 4

	// Eyes lanes, far enough apart that neither hears the other (audio is
	// capped at 64 m, the wall window at one chunk).
	balEyesVehZ float32 = 0
	balEyesInfZ float32 = 400
	balEyesGap  float32 = 150
	// Infantry-vs-infantry first contact WANTED, not asserted. 40 m nominal
	// optics buy 24 m of it, and closing that gap is a combat-range pass
	// (measured: at 80 m a defended building stops being assaultable), so the
	// scene reports the number every run and gates on the claims the current
	// model actually supports. See .claude/lite/PLAYTEST.md.
	balEyesInfMi float32 = 40
)

// aiBalanceSpawn builds whichever balance scene is selected.
func aiBalanceSpawn(world *ecs.World, squadService *systems.SquadService,
	roleService *systems.RoleService, unitFactory aiUnitSpawn,
	vehicleFactory *entities.VehicleFactory, mode balanceMode,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if squadService == nil || roleService == nil || vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO SERVICE — aborting\n", aiSceneID())
		return nil
	}
	s := &aiTestState{
		sceneID:       aiSceneID(),
		balanceActive: true,
		balMode:       mode,
		World:         world,
		SquadService:  squadService,
		PosMap:        posMap,
		HPMap:         ecs.NewMap[components.HP](world),
		MotionMap:     ecs.NewMap[components.Motion](world),
		StanceMap:     ecs.NewMap[components.Stance](world),
		VehQueueMap:   ecs.NewMap[components.ActionQueue](world),
		AwareMap:      ecs.NewMap[components.Awareness](world),
		RulesMap:      ecs.NewMap[components.EngagementRules](world),
		RosterMap:     rosterMap,
		balContactMap: ecs.NewMap[components.Contact](world),
		balRegistry:   ecs.NewResource[components.ContactRegistry](world),
		orderAt:       balOrderAt,
		verdictAt:     balVerdictAt,
		nextSampleAt:  balOrderAt + 2,
	}
	if mode == balanceEyes {
		s.verdictAt = balEyesVerdictAt
		aiBalanceEyesSpawn(roleService, unitFactory, vehicleFactory, s)
		return s
	}
	aiBalanceFightSpawn(world, roleService, unitFactory, vehicleFactory, s)
	return s
}

func balWP(x, z float32) components.WorldPos {
	p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
	p.Local.Y = systems.GroundHeight(x, z)
	return p
}

func balXZ(a, b components.WorldPos) float32 {
	ax := float32(a.Chunk.X)*components.ChunkSize + a.Local.X
	az := float32(a.Chunk.Z)*components.ChunkSize + a.Local.Z
	bx := float32(b.Chunk.X)*components.ChunkSize + b.Local.X
	bz := float32(b.Chunk.Z)*components.ChunkSize + b.Local.Z
	dx, dz := ax-bx, az-bz
	return float32(rl.Vector2Length(rl.Vector2{X: dx, Y: dz}))
}

// balMotorRifle spawns one motor-rifle squad and returns it plus its bodies.
func (s *aiTestState) balMotorRifle(roleService *systems.RoleService,
	unitFactory aiUnitSpawn, at components.WorldPos, faction components.Faction,
	ctrl uint8) (ecs.Entity, []ecs.Entity) {
	sq := s.SquadService.CreateFromTemplate(systems.TmplMotorRifle, at,
		components.FormationLine, faction,
		components.Controller{Owner: ctrl}, roleService, unitFactory)
	if sq == (ecs.Entity{}) {
		return sq, nil
	}
	var bodies []ecs.Entity
	if r := s.RosterMap.Get(sq); r != nil {
		for i := uint8(0); i < r.Count; i++ {
			if m := r.Members[i]; m != (ecs.Entity{}) {
				bodies = append(bodies, m)
			}
		}
	}
	return sq, bodies
}

func aiBalanceFightSpawn(world *ecs.World, roleService *systems.RoleService,
	unitFactory aiUnitSpawn, vehicleFactory *entities.VehicleFactory, s *aiTestState) {
	for _, dz := range [3]float32{-14, 0, 14} {
		h := vehicleFactory.Spawn(balWP(balHullStartX, dz), components.VehicleBMP,
			components.FactionPlayer, components.ControllerLocal)
		if m := s.MotionMap.Get(h); m != nil {
			m.Yaw = rl.Pi / 2 // nose east, toward the squad
		}
		s.balHulls = append(s.balHulls, h)
	}

	infZ := float32(0)
	if s.balMode == balanceAmbush {
		infZ = balAmbInfZ
	}
	sq, bodies := s.balMotorRifle(roleService, unitFactory,
		balWP(balInfX, infZ), components.Faction{ID: components.FactionEnemyRed},
		components.ControllerAI)
	s.balSquad, s.balInf = sq, bodies

	if s.balMode == balanceAmbush {
		// Prone with the lock the StanceController honours, and silent until
		// the trigger. Without the lock the Safe band stands them straight
		// back up and there is no ambush to measure.
		for _, u := range bodies {
			if st := s.StanceMap.Get(u); st != nil {
				st.Code = components.StanceProne
				st.LockUntil = 1e9
			}
			ecs.NewMap[components.StanceOverride](world).Add(u,
				&components.StanceOverride{Until: 1e9})
		}
		if r := s.RulesMap.Get(sq); r != nil {
			r.Mode = components.HoldFire
		}
	}
	fmt.Println("============================================================")
	fmt.Printf("== BALANCE %s: hulls=%d squad=%d ambush=%v\n",
		s.sceneID, len(s.balHulls), len(s.balInf), s.balMode == balanceAmbush)
	fmt.Println("============================================================")
}

func aiBalanceEyesSpawn(roleService *systems.RoleService,
	unitFactory aiUnitSpawn, vehicleFactory *entities.VehicleFactory, s *aiTestState) {
	// Lane V — one BMP and one squad closing head-on. Both are MOVING: a
	// parked hull is a quarter as loud, and the question is about a hull that
	// is going somewhere.
	hull := vehicleFactory.Spawn(balWP(-balEyesGap/2, balEyesVehZ),
		components.VehicleBMP, components.FactionPlayer, components.ControllerLocal)
	if m := s.MotionMap.Get(hull); m != nil {
		m.Yaw = rl.Pi / 2
	}
	s.RulesMap.Add(hull, &components.EngagementRules{Mode: components.HoldFire})
	s.balHulls = append(s.balHulls, hull)

	sqV, bodyV := s.balMotorRifle(roleService, unitFactory,
		balWP(balEyesGap/2, balEyesVehZ),
		components.Faction{ID: components.FactionEnemyRed}, components.ControllerAI)
	s.balEyesVehFoe, s.balEyesVehFoeUnits = sqV, bodyV

	// Lane I — two squads closing on each other, 400 m away from lane V.
	sqA, bodyA := s.balMotorRifle(roleService, unitFactory,
		balWP(-balEyesGap/2, balEyesInfZ),
		components.Faction{ID: components.FactionPlayer}, components.ControllerLocal)
	sqB, bodyB := s.balMotorRifle(roleService, unitFactory,
		balWP(balEyesGap/2, balEyesInfZ),
		components.Faction{ID: components.FactionEnemyRed}, components.ControllerAI)
	s.balEyesInfA, s.balEyesInfAUnits = sqA, bodyA
	s.balEyesInfB, s.balEyesInfBUnits = sqB, bodyB

	// Nobody fires: the scene measures sight, and a dead target stops being
	// looked at. HoldFire is the real gate, not an empty magazine.
	for _, sq := range []ecs.Entity{sqV, sqA, sqB} {
		if r := s.RulesMap.Get(sq); r != nil {
			r.Mode = components.HoldFire
		}
	}
	fmt.Println("============================================================")
	fmt.Printf("== BALANCE %s: lane V hull vs squad, lane I squad vs squad, gap=%.0f m\n",
		s.sceneID, balEyesGap)
	fmt.Println("============================================================")
}

func (s *aiTestState) updateBalance(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		s.orderFired = true
		s.balIssueOrders()
	}
	if s.balMode == balanceEyes {
		s.balSampleEyes(elapsed)
	} else {
		s.balSampleFight(elapsed)
	}
	if elapsed >= s.verdictAt {
		s.balVerdict(elapsed)
	}
}

func (s *aiTestState) balIssueOrders() {
	if s.balMode == balanceEyes {
		for _, h := range s.balHulls {
			s.balDriveHull(h, balWP(balEyesGap/2-10, balEyesVehZ))
		}
		s.balMarch(s.balEyesVehFoe, balWP(-balEyesGap/2+10, balEyesVehZ))
		s.balMarch(s.balEyesInfA, balWP(balEyesGap/2-10, balEyesInfZ))
		s.balMarch(s.balEyesInfB, balWP(-balEyesGap/2+10, balEyesInfZ))
		return
	}
	goalX := balHullGoalX
	if s.balMode == balanceAmbush {
		goalX = balAmbHullEnd
	}
	for i, h := range s.balHulls {
		dz := float32(i-1) * 14
		s.balDriveHull(h, balWP(goalX, dz))
	}
}

// balDriveHull writes the hull's own ActionQueue instead of issuing an order:
// these scenes measure gunnery, and an order would drag the whole command
// chain (and, after block A M1, the radio) into a combat measurement.
func (s *aiTestState) balDriveHull(h ecs.Entity, to components.WorldPos) {
	if h == (ecs.Entity{}) || !s.World.Alive(h) {
		return
	}
	if aq := s.VehQueueMap.Get(h); aq != nil {
		systems.PushAction(aq, components.Action{
			Kind: components.ActionMoveTo, Target: to,
		})
	}
}

func (s *aiTestState) balMarch(sq ecs.Entity, to components.WorldPos) {
	if sq == (ecs.Entity{}) || !s.World.Alive(sq) {
		return
	}
	s.SquadService.IssueOrder(sq, components.OrderKindMoveTo, to, ecs.Entity{},
		false, systems.OrderParams{})
}

func (s *aiTestState) balAlive(ents []ecs.Entity) (int, float32) {
	n := 0
	hp := float32(0)
	for _, e := range ents {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			continue
		}
		if h := s.HPMap.Get(e); h != nil && h.Current > 0 {
			n++
			hp += h.Current
		}
	}
	return n, hp
}

func (s *aiTestState) balSampleFight(elapsed float32) {
	hullsN, hullsHP := s.balAlive(s.balHulls)
	infN, infHP := s.balAlive(s.balInf)

	if s.balMode == balanceAmbush && !s.balTriggered {
		if d := s.balNearestHullRange(); d > 0 && d <= balAmbushTriggerM {
			s.balTriggered = true
			s.balTriggerAt, s.balTriggerM = elapsed, d
			if r := s.RulesMap.Get(s.balSquad); r != nil {
				r.Mode = components.FreeFire
			}
			fmt.Printf("[ai-test %s] t=%.1fs AMBUSH SPRUNG at %.0f m\n",
				s.sceneID, elapsed, d)
		}
	}
	if s.balTriggered && s.balSpottedAt == 0 && s.balContacts() > 0 {
		s.balSpottedAt = elapsed
		fmt.Printf("[ai-test %s] t=%.1fs AMBUSHER ON THE MAP (%.1fs after the volley)\n",
			s.sceneID, elapsed, elapsed-s.balTriggerAt)
	}
	if infN == 0 && s.balWipeAt == 0 {
		s.balWipeAt = elapsed
	}
	if hullsN < len(s.balHulls) && s.balFirstHullLossAt == 0 {
		s.balFirstHullLossAt = elapsed
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs hulls=%d hp=%.0f inf=%d hp=%.0f\n",
			s.sceneID, elapsed, hullsN, hullsHP, infN, infHP)
	}
	if infN == 0 || hullsN == 0 {
		s.verdictAt = elapsed
	}
}

// balContacts counts ambushers the PLAYER has a live contact on. Firing is the
// only thing that can produce one here: the hulls never see the prone squad.
func (s *aiTestState) balContacts() int {
	reg := s.balRegistry.Get()
	if reg == nil || reg.Tracked == nil {
		return 0
	}
	n := 0
	for _, u := range s.balInf {
		ent, ok := reg.Tracked[u]
		if !ok || !s.World.Alive(ent) {
			continue
		}
		if c := s.balContactMap.Get(ent); c != nil && !c.BearingOnly {
			n++
		}
	}
	return n
}

func (s *aiTestState) balNearestHullRange() float32 {
	best := float32(0)
	for _, h := range s.balHulls {
		if h == (ecs.Entity{}) || !s.World.Alive(h) {
			continue
		}
		hp := s.PosMap.Get(h)
		if hp == nil {
			continue
		}
		for _, u := range s.balInf {
			if u == (ecs.Entity{}) || !s.World.Alive(u) {
				continue
			}
			up := s.PosMap.Get(u)
			if up == nil {
				continue
			}
			d := balXZ(*hp, *up)
			if best == 0 || d < best {
				best = d
			}
		}
	}
	return best
}

// balFirstSight latches the range at which any observer first files an
// Awareness entry naming any of `targets`. Range is measured to that target,
// not to the group: the claim is about one pair of eyes and one body.
func (s *aiTestState) balFirstSight(observers, targets []ecs.Entity, latch *float32) {
	if *latch > 0 {
		return
	}
	for _, o := range observers {
		if o == (ecs.Entity{}) || !s.World.Alive(o) {
			continue
		}
		aw := s.AwareMap.Get(o)
		op := s.PosMap.Get(o)
		if aw == nil || op == nil {
			continue
		}
		for i := range aw.LastSeen {
			e := aw.LastSeen[i]
			if e.Target == (ecs.Entity{}) || e.Time <= 0 {
				continue
			}
			for _, t := range targets {
				if e.Target != t || !s.World.Alive(t) {
					continue
				}
				tp := s.PosMap.Get(t)
				if tp == nil {
					continue
				}
				*latch = balXZ(*op, *tp)
				return
			}
		}
	}
}

func (s *aiTestState) balSampleEyes(elapsed float32) {
	s.balFirstSight(s.balHulls, s.balEyesVehFoeUnits, &s.balEyesVehSees)
	s.balFirstSight(s.balEyesVehFoeUnits, s.balHulls, &s.balEyesInfSeesVeh)
	s.balFirstSight(s.balEyesInfAUnits, s.balEyesInfBUnits, &s.balEyesInfSeesInf)

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs vehSeesInf=%.0f infSeesVeh=%.0f infSeesInf=%.0f\n",
			s.sceneID, elapsed, s.balEyesVehSees, s.balEyesInfSeesVeh, s.balEyesInfSeesInf)
	}
	if s.balEyesVehSees > 0 && s.balEyesInfSeesVeh > 0 && s.balEyesInfSeesInf > 0 {
		s.verdictAt = elapsed
	}
}

func (s *aiTestState) balVerdict(elapsed float32) {
	s.verdictDone = true
	if s.balMode == balanceEyes {
		all := s.balEyesVehSees > 0 && s.balEyesInfSeesVeh > 0 && s.balEyesInfSeesInf > 0
		harderForHull := s.balEyesInfSeesVeh > s.balEyesVehSees
		pass := all && harderForHull
		open := "met"
		if s.balEyesInfSeesInf < balEyesInfMi {
			open = "OPEN"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (vehSeesInf=%.0fm infSeesVeh=%.0fm | allSeen=%v harderForHull=%v | infSeesInf=%.0fm want>=%.0f %s t=%.1fs)\n",
			s.sceneID, balVerdictWord(pass), s.balEyesVehSees, s.balEyesInfSeesVeh,
			all, harderForHull, s.balEyesInfSeesInf, balEyesInfMi, open, elapsed)
		fmt.Println("============================================================")
		return
	}

	hullsN, hullsHP := s.balAlive(s.balHulls)
	infN, infHP := s.balAlive(s.balInf)
	hullsLost := len(s.balHulls) - hullsN

	var pass bool
	var detail string
	if s.balMode == balanceOpen {
		wiped := infN == 0
		quick := s.balWipeAt > 0 && s.balWipeAt <= balOpenWipeBy
		cheap := hullsN >= balOpenHullsMin
		pass = wiped && quick && cheap
		detail = fmt.Sprintf("wiped=%v quick=%v(t=%.1fs) cheap=%v", wiped, quick, s.balWipeAt, cheap)
	} else {
		// The contrast against the open field is the whole claim: the same
		// hull is paid for either way, but here the squad is still standing
		// afterwards.
		hurts := hullsLost >= balAmbHullsKill
		survives := infN >= balAmbInfMin
		sprung := s.balTriggered
		spotted := s.balSpottedAt > 0 && s.balSpottedAt-s.balTriggerAt <= balSpotWindow
		pass = hurts && survives && sprung && spotted
		detail = fmt.Sprintf("hullsLost=%d/%d survivors=%d/%d sprung=%v(%.0fm t=%.1fs) spotted=%v(+%.1fs)",
			hullsLost, balAmbHullsKill, infN, balAmbInfMin, sprung, s.balTriggerM,
			s.balTriggerAt, spotted, s.balSpottedAt-s.balTriggerAt)
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (hulls=%d/%d hp=%.0f inf=%d/%d hp=%.0f | %s t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), hullsN, len(s.balHulls), hullsHP,
		infN, len(s.balInf), infHP, detail, elapsed)
	fmt.Println("============================================================")
}

func balVerdictWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
