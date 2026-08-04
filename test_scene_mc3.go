package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// MC3 scene harnesses: the three squad-brain behaviours plus focus fire.
// Enemies here are fire SOURCES, not targets — a rifleman carries 30 rounds
// and no reload, so a scene that needs sustained pressure has to hand them a
// deep magazine and enough hit points to keep shooting for the whole run.

func aiSceneWP(x, z float32) components.WorldPos {
	p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
	p.Local.Y = systems.GroundHeight(x, z)
	return p
}

// aiSpawnGunner drops one hostile rifleman with `hp` health and `ammo` rounds.
func aiSpawnGunner(world *ecs.World, roleService *systems.RoleService,
	unitFactory aiUnitSpawn, pos components.WorldPos, hp float32, ammo uint16) ecs.Entity {

	ent := unitFactory(pos)
	if ent == (ecs.Entity{}) {
		return ent
	}
	roleService.AssignRole(ent, components.RoleRifleman)
	if f := ecs.NewMap[components.Faction](world).Get(ent); f != nil {
		f.ID = components.FactionEnemyRed
	}
	if c := ecs.NewMap[components.Controller](world).Get(ent); c != nil {
		c.Owner = components.ControllerAI
	}
	hpMap := ecs.NewMap[components.HP](world)
	if h := hpMap.Get(ent); h != nil {
		h.Current, h.Max = hp, hp
	} else {
		hpMap.Add(ent, &components.HP{Current: hp, Max: hp})
	}
	if ammo > 0 {
		eq := ecs.NewMap[components.Equipment](world).Get(ent)
		if eq != nil && eq.Primary != (ecs.Entity{}) && world.Alive(eq.Primary) {
			if w := ecs.NewMap[components.Weapon](world).Get(eq.Primary); w != nil {
				w.Ammo = ammo
			}
		}
	}
	return ent
}

// aiRefillSquad hands every member a deep magazine (no reload exists yet).
func aiRefillSquad(world *ecs.World, squadService *systems.SquadService,
	squad ecs.Entity, ammo uint16) {

	roster := ecs.NewMap[components.CommandRoster](world).Get(squad)
	if roster == nil {
		return
	}
	eqMap := ecs.NewMap[components.Equipment](world)
	wMap := ecs.NewMap[components.Weapon](world)
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		eq := eqMap.Get(mem)
		if eq == nil {
			continue
		}
		for _, w := range []ecs.Entity{eq.Primary, eq.Secondary} {
			if w == (ecs.Entity{}) || !world.Alive(w) {
				continue
			}
			if wp := wMap.Get(w); wp != nil {
				wp.Ammo = ammo
			}
		}
	}
}

// aiBoundingSpawn (MC3): a MotorRifle squad marches 120 m east across open
// ground past three dug-in gunners 35 m off the axis.
func aiBoundingSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSceneWP(25, 10),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	var foes []ecs.Entity
	for _, x := range []float32{60, 90, 120} {
		if e := aiSpawnGunner(world, roleService, unitFactory,
			aiSceneWP(x, 45), 350, 20000); e != (ecs.Entity{}) {
			foes = append(foes, e)
		}
	}
	return &aiTestState{
		sceneID:      aiSceneID(),
		squad:        squad,
		boundActive:  true,
		boundGoal:    aiSceneWP(145, 10),
		boundFoes:    foes,
		boundMinHold: 1,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		PlanMap:      ecs.NewMap[components.SquadPlan](world),
		HPMap:        ecs.NewMap[components.HP](world),
		OQMap:        ecs.NewMap[components.OrderQueueHead](world),
		StateMap:     ecs.NewMap[components.OrderState](world),
		orderAt:      aiOrderAt,
		verdictAt:    150,
		nextSampleAt: aiOrderAt + 5,
	}
}

// updateBounding (MC3): PASS = the brain actually bounded, the covering wave
// never dropped below 40% of the live roster, progress along the march axis
// never fell back, and the squad reached the goal.
func (s *aiTestState) updateBounding(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI BOUNDING: squad=%v marches (25,10) -> (145,10) past %d gunners\n",
			s.squad, len(s.boundFoes))
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.squad, components.OrderKindMoveTo,
			s.boundGoal, ecs.Entity{}, false, systems.OrderParams{})
		s.orderFired = true
		s.boundMaxX = -1e9
		return
	}

	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	plan := s.PlanMap.Get(s.squad)
	bounding := plan != nil && plan.Mode == components.SquadPlanBounding
	if bounding {
		s.boundSawMode = true
	}

	var live, holding float32
	var sumX, sumZ float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		p := s.PosMap.Get(mem)
		if p == nil {
			continue
		}
		live++
		x := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		z := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		sumX += x
		sumZ += z
		if bounding && !plan.InMovingWave(i) {
			holding++
		}
	}
	if live == 0 {
		s.finishBounding(elapsed, 0)
		return
	}
	cx, cz := sumX/live, sumZ/live

	// Monotonic progress: the centroid may stall while a wave holds, but a
	// squad that keeps losing ground is not bounding, it is being pushed back.
	if cx > s.boundMaxX {
		s.boundMaxX = cx
	} else if back := s.boundMaxX - cx; back > s.boundBackMax {
		s.boundBackMax = back
	}
	if bounding {
		if share := holding / live; share < s.boundMinHold {
			s.boundMinHold = share
		}
		s.boundHoldN++
	}

	gx := float32(s.boundGoal.Chunk.X)*components.ChunkSize + s.boundGoal.Local.X
	gz := float32(s.boundGoal.Chunk.Z)*components.ChunkSize + s.boundGoal.Local.Z
	if s.boundArrived == 0 {
		if dx, dz := cx-gx, cz-gz; dx*dx+dz*dz < 15*15 {
			s.boundArrived = elapsed
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		mode := "-"
		if plan != nil {
			if l := components.SquadPlanLabel(plan.Mode); l != "" {
				mode = fmt.Sprintf("%s w%d", l, plan.Phase)
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs x=%.1f live=%.0f mode=%s hold=%.0f minHold=%.2f back=%.1f\n",
			s.sceneID, elapsed, cx, live, mode, holding, s.boundMinHold, s.boundBackMax)
	}

	if s.boundArrived > 0 || elapsed >= s.verdictAt {
		s.finishBounding(elapsed, live)
	}
}

// foeHP totals the gunners' remaining health — how much the covering wave
// actually accomplished.
func (s *aiTestState) foeHP() float32 {
	var total float32
	for _, e := range s.boundFoes {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			continue
		}
		if h := s.HPMap.Get(e); h != nil {
			total += h.Current
		}
	}
	return total
}

func (s *aiTestState) finishBounding(elapsed, live float32) {
	holdOK := s.boundHoldN == 0 || s.boundMinHold >= 0.4
	// Half the roster has to come out the other side: bounding that gets the
	// squad killed is worse than the straight march it replaced (the A/B with
	// RTS_BRAIN_OFF=1 is the calibration, this is the floor).
	pass := s.boundSawMode && holdOK && s.boundBackMax < 12 &&
		s.boundArrived > 0 && live >= 4
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (bounded=%v minHold=%.2f back=%.1f arrived=%.1f live=%.0f foeHP=%.0f t=%.1fs)\n",
		s.sceneID, verdict, s.boundSawMode, s.boundMinHold, s.boundBackMax,
		s.boundArrived, live, s.foeHP(), elapsed)
	fmt.Println("============================================================")
	s.verdictDone = true
}

// aiClearBuildingSpawn (MC3): a 2-storey office with two hostiles on the
// ground floor and one upstairs; the squad forms up south of the door.
func aiClearBuildingSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSceneWP(32, 18),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	// The building roots exist before the scene spawns (spawnWorldRoots), so
	// the footprint is readable here even though the children are not.
	var building ecs.Entity
	var fp components.AABB2D
	bq := ecs.NewFilter1[components.Building](world).Query()
	for bq.Next() {
		b := bq.Get()
		if b.Footprint.CenterX() > 20 && b.Footprint.CenterX() < 45 {
			building = bq.Entity()
			fp = b.Footprint
		}
	}
	if building == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] NO BUILDING — aborting\n", aiSceneID())
		return nil
	}
	groundY := systems.GroundHeight(fp.CenterX(), fp.CenterZ())
	upY := groundY + components.FloorHeight

	var foes []ecs.Entity
	place := func(x, z, y float32) {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Y: y, Z: z})
		if e := aiSpawnGunner(world, roleService, unitFactory, p, 120, 400); e != (ecs.Entity{}) {
			foes = append(foes, e)
		}
	}
	place(fp.MinX+3, fp.MinZ+3, groundY)
	place(fp.MaxX-3, fp.CenterZ(), groundY)
	aiRefillSquad(world, squadService, squad, 400)
	_ = upY

	return &aiTestState{
		sceneID:       aiSceneID(),
		squad:         squad,
		clearActive:   true,
		clearBuilding: building,
		clearFoes:     foes,
		clearUpY:      groundY + components.FloorHeight*0.6,
		clearFP:       fp,
		clearRole:     roleService,
		clearSpawner:  unitFactory,
		clearChildRes: ecs.NewResource[systems.BuildingChildIndex](world),
		FloorMap:      ecs.NewMap[components.Floor](world),
		World:         world,
		SquadService:  squadService,
		PosMap:        posMap,
		RosterMap:     rosterMap,
		PlanMap:       ecs.NewMap[components.SquadPlan](world),
		HPMap:         ecs.NewMap[components.HP](world),
		OQMap:         ecs.NewMap[components.OrderQueueHead](world),
		StateMap:      ecs.NewMap[components.OrderState](world),
		KindMap:       ecs.NewMap[components.OrderKind](world),
		BuildingMap:   ecs.NewMap[components.Building](world),
		orderAt:       aiOrderAt,
		verdictAt:     140,
		nextSampleAt:  aiOrderAt + 5,
	}
}

// updateClearBuilding (MC3): PASS = nobody climbed while the ground floor
// still held a hostile, every hostile is down, and the resolver auto-chained
// into OccupyBuilding.
func (s *aiTestState) updateClearBuilding(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		// The upper storey only exists once the chunk streamed its children in,
		// so the hostile who makes the sweep gate meaningful is placed here and
		// not at spawn — dropped at boot he would sink to the ground floor and
		// the two-storey test would silently become a one-storey one.
		if !s.clearUpPlaced {
			if !s.placeUpstairsFoe(elapsed) {
				if elapsed > s.orderAt+8 {
					fmt.Printf("[ai-test %s] NO UPPER FLOOR — aborting\n", s.sceneID)
					s.verdictDone = true
				}
				return
			}
		}
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI CLEAR BUILDING: squad=%v building=%v foes=%d upY=%.1f\n",
			s.squad, s.clearBuilding, len(s.clearFoes), s.clearUpY)
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.squad, components.OrderKindClearBuilding,
			components.WorldPos{}, s.clearBuilding, false, systems.OrderParams{})
		s.orderFired = true
		return
	}

	if !s.World.Alive(s.squad) {
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: FAIL  (squad wiped at t=%.1fs)\n", s.sceneID, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
		return
	}

	if plan := s.PlanMap.Get(s.squad); plan != nil &&
		plan.Mode == components.SquadPlanClearSeq && int(plan.Phase) < len(s.clearPhases) {
		s.clearPhases[plan.Phase] = true
	}

	// Hostiles left, split by storey.
	downFoes, upFoes := 0, 0
	for _, e := range s.clearFoes {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			continue
		}
		p := s.PosMap.Get(e)
		if p == nil {
			continue
		}
		if p.Local.Y >= s.clearUpY {
			upFoes++
		} else {
			downFoes++
		}
	}
	if downFoes+upFoes == 0 && s.clearClearedT == 0 {
		s.clearClearedT = elapsed
	}

	// The gate: while the ground floor is dirty nobody may be upstairs.
	upMembers, liveMembers := 0, 0
	if roster := s.RosterMap.Get(s.squad); roster != nil {
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
				continue
			}
			liveMembers++
			if p := s.PosMap.Get(mem); p != nil && p.Local.Y >= s.clearUpY {
				upMembers++
			}
		}
	}
	if downFoes > 0 && upMembers > 0 {
		s.clearEarlyUp++
	}
	if upMembers > s.clearUpSeen {
		s.clearUpSeen = upMembers
	}

	// Auto-chain: the head order becomes OccupyBuilding once Clear is done.
	if h := s.OQMap.Get(s.squad); h != nil && h.First != (ecs.Entity{}) &&
		s.World.Alive(h.First) {
		if k := s.KindMap.Get(h.First); k != nil &&
			k.Code == components.OrderKindOccupyBuilding {
			s.clearChained = true
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		phase := "-"
		if plan := s.PlanMap.Get(s.squad); plan != nil &&
			plan.Mode == components.SquadPlanClearSeq {
			phase = fmt.Sprintf("%s L%d", components.ClearPhaseLabel(plan.Phase), plan.Floor)
		}
		fmt.Printf("[ai-test %s] t=%.1fs phase=%s foes=%d/%d live=%d up=%d(max %d) early=%d chained=%v\n",
			s.sceneID, elapsed, phase, downFoes, upFoes, liveMembers, upMembers,
			s.clearUpSeen, s.clearEarlyUp, s.clearChained)
	}

	if (s.clearClearedT > 0 && s.clearChained) || elapsed >= s.verdictAt {
		pass := s.clearClearedT > 0 && s.clearClearedT <= 90 &&
			s.clearChained && s.clearEarlyUp == 0
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (cleared=%.1f chained=%v earlyUp=%d upstairs=%d phases=%v t=%.1fs)\n",
			s.sceneID, verdict, s.clearClearedT, s.clearChained, s.clearEarlyUp,
			s.clearUpSeen, s.clearPhases, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// placeUpstairsFoe puts one hostile on storey 1 as soon as the floor plate is
// live. Returns false while the building's children are still unstreamed.
func (s *aiTestState) placeUpstairsFoe(elapsed float32) bool {
	idx := s.clearChildRes.Get()
	if idx == nil {
		return false
	}
	var floor0Y, floor1Y float32
	have0, have1 := false, false
	for _, ch := range idx.Loaded[s.clearBuilding] {
		if !s.World.Alive(ch) {
			continue
		}
		f := s.FloorMap.Get(ch)
		p := s.PosMap.Get(ch)
		if f == nil || p == nil {
			continue
		}
		switch f.Level {
		case 0:
			floor0Y, have0 = p.Local.Y, true
		case 1:
			floor1Y, have1 = p.Local.Y, true
		}
	}
	if !have0 || !have1 {
		return false
	}
	pos := components.WorldPos{}.Add(rl.Vector3{
		X: s.clearFP.CenterX(), Y: floor1Y + 0.2, Z: s.clearFP.MaxZ - 2.5})
	// Floors do not occlude yet, so this man is engaged from the storey below
	// as soon as the squad is inside; the sweep gate this scene measures is
	// "nobody upstairs while the ground floor is dirty", not the kill itself.
	e := aiSpawnGunner(s.World, s.clearRole, s.clearSpawner, pos, 120, 400)
	if e == (ecs.Entity{}) {
		return false
	}
	s.clearFoes = append(s.clearFoes, e)
	s.clearUpY = (floor0Y + floor1Y) * 0.5
	s.clearUpPlaced = true
	fmt.Printf("[ai-test %s] upstairs foe %v at y=%.2f (floors %.2f/%.2f) t=%.1fs\n",
		s.sceneID, e, floor1Y+0.2, floor0Y, floor1Y, elapsed)
	return true
}

// aiFocusFireSpawn (MC3): three hostiles abreast at 26 m; the order names the
// middle one.
func aiFocusFireSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSceneWP(30, 8),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	// Equal hit points across the group: the damage split IS the shot split
	// only while every target soaks the same per-hit damage.
	var foes []ecs.Entity
	for _, x := range []float32{26, 30, 34} {
		if e := aiSpawnGunner(world, roleService, unitFactory,
			aiSceneWP(x, 34), 500, 60); e != (ecs.Entity{}) {
			foes = append(foes, e)
		}
	}
	if len(foes) < 3 {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN FOES — aborting\n", aiSceneID())
		return nil
	}
	// The squad is the measured party: a rifleman's 30 rounds would end the
	// engagement before the concentration is worth reading.
	aiRefillSquad(world, squadService, squad, 400)
	maxHP := make([]float32, len(foes))
	hpMap := ecs.NewMap[components.HP](world)
	for i, e := range foes {
		if h := hpMap.Get(e); h != nil {
			maxHP[i] = h.Max
		}
	}
	return &aiTestState{
		sceneID:      aiSceneID(),
		squad:        squad,
		focusActive:  true,
		focusEnemy:   foes[1],
		focusFoes:    foes,
		focusMaxHP:   maxHP,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		HPMap:        hpMap,
		OQMap:        ecs.NewMap[components.OrderQueueHead](world),
		StateMap:     ecs.NewMap[components.OrderState](world),
		orderAt:      aiOrderAt,
		verdictAt:    90,
		nextSampleAt: aiOrderAt + 3,
	}
}

// updateFocusFire (MC3): PASS = at least 80% of the damage the squad dealt
// landed on the named enemy, and he went down.
func (s *aiTestState) updateFocusFire(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		tgtPos := components.WorldPos{}
		if p := s.PosMap.Get(s.focusEnemy); p != nil {
			tgtPos = *p
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI FOCUS FIRE: squad=%v attacks %v of %d abreast\n",
			s.squad, s.focusEnemy, len(s.focusFoes))
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.squad, components.OrderKindAttackTarget,
			tgtPos, s.focusEnemy, false, systems.OrderParams{})
		s.orderFired = true
		// Baseline at the order, not at spawn: the two seconds of free fire
		// before it are not what focus fire is being measured on.
		for i, e := range s.focusFoes {
			if h := s.HPMap.Get(e); h != nil {
				s.focusMaxHP[i] = h.Current
			}
		}
		return
	}

	var focusDmg, totalDmg float32
	for i, e := range s.focusFoes {
		dmg := s.focusMaxHP[i]
		if s.World.Alive(e) {
			if h := s.HPMap.Get(e); h != nil {
				dmg = s.focusMaxHP[i] - h.Current
			}
		}
		totalDmg += dmg
		if e == s.focusEnemy {
			focusDmg = dmg
			if !s.World.Alive(e) {
				s.focusKilled = true
			} else if h := s.HPMap.Get(e); h != nil && h.Current <= 0 {
				s.focusKilled = true
			}
		}
	}
	if totalDmg > 0 {
		s.focusShare = focusDmg / totalDmg
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		var per [3]float32
		for i := range s.focusFoes {
			if i < len(per) {
				per[i] = s.focusMaxHP[i]
				if s.World.Alive(s.focusFoes[i]) {
					if h := s.HPMap.Get(s.focusFoes[i]); h != nil {
						per[i] = s.focusMaxHP[i] - h.Current
					}
				}
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs focusDmg=%.0f total=%.0f share=%.2f killed=%v per=%.0f/%.0f/%.0f\n",
			s.sceneID, elapsed, focusDmg, totalDmg, s.focusShare, s.focusKilled,
			per[0], per[1], per[2])
	}

	if s.focusKilled || elapsed >= s.verdictAt {
		pass := s.focusKilled && s.focusShare >= 0.8
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (share=%.2f killed=%v dmg=%.0f/%.0f t=%.1fs)\n",
			s.sceneID, verdict, s.focusShare, s.focusKilled, focusDmg, totalDmg, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}
