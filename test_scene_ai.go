package main

// Automated AI test scenes. Run with `./bin/rts -scene=<id>`. Each scene
// spawns a minimal isolated world, auto-issues OccupyBuilding at t=aiOrderAt,
// samples insider count every aiSampleEvery, prints PASS/FAIL at aiVerdictAt.

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/gen/buildings"
	"rts-go/systems"
)

type aiUnitSpawn = func(components.WorldPos) ecs.Entity

const (
	aiSceneDoorSouth     = "ai_door_south"
	aiSceneDoorNorth     = "ai_door_north"
	aiSceneDoorEast      = "ai_door_east"
	aiSceneDoorWest      = "ai_door_west"
	aiSceneCompoundSouth = "ai_compound_south"
	aiSceneCompoundEast  = "ai_compound_east"
	aiSceneCompoundNorth = "ai_compound_north"
	aiSceneCompoundWest  = "ai_compound_west"
	aiSceneCompoundMain  = "ai_compound_main"
	aiSceneCompoundPlusSouth = "ai_compound_plus_south"
	aiSceneCompoundPlusEast  = "ai_compound_plus_east"
	aiSceneCompoundPlusNorth = "ai_compound_plus_north"
	aiSceneCompoundPlusWest  = "ai_compound_plus_west"
	aiSceneOfficeFront   = "ai_office_front"
	aiSceneFarBuilding   = "ai_far_building"

	// ai_main_* run on the REAL main-map world data (mainWorldBuildings +
	// roads + trenches) and send the squad into one specific section / storey
	// of the multi-wing compound at (-60, 10) — the building with the worst
	// pathing history.
	aiSceneMainM0 = "ai_main_m0" // main wing, ground floor
	aiSceneMainM1 = "ai_main_m1" // main wing, second storey (via stairs)
	aiSceneMainE  = "ai_main_e"  // east wing
	aiSceneMainN  = "ai_main_n"  // north wing

	// ai_los_* validate terrain-LOS + squad shared vision: one hostile 13 m
	// north of a HoldFire squad (optical: linear falloff over 40 m ⇒
	// detection needs ≤ ~20 m) — on open ground (contact + shared awareness
	// expected) or standing inside a trench cut (defilade, zero contacts).
	aiSceneLosOpen     = "ai_los_open"
	aiSceneLosDefilade = "ai_los_defilade"
	// ai_los_creep: prone stationary hostile — detection meter grants a
	// grace window (no contact by t=3 s, contact by t=15 s).
	aiSceneLosCreep = "ai_los_creep"
)

// aiMainSpec selects the target wing (by expected footprint centre) and the
// storey index for one ai_main_* scene.
type aiMainSpec struct {
	wingX, wingZ float32
	levelIdx     int
}

func aiMainSpecFor(id string) (aiMainSpec, bool) {
	switch id {
	case aiSceneMainM0:
		return aiMainSpec{wingX: -60, wingZ: 10, levelIdx: 0}, true
	case aiSceneMainM1:
		return aiMainSpec{wingX: -60, wingZ: 10, levelIdx: 1}, true
	case aiSceneMainE:
		return aiMainSpec{wingX: -50, wingZ: 10, levelIdx: 0}, true
	case aiSceneMainN:
		return aiMainSpec{wingX: -60, wingZ: 20, levelIdx: 0}, true
	}
	return aiMainSpec{}, false
}

const (
	aiOrderAt     float32 = 2.0
	aiVerdictAt   float32 = 60.0
	aiSampleEvery float32 = 5.0
)

func isAIScene() bool {
	if sceneFlag == nil {
		return false
	}
	v := *sceneFlag
	return len(v) >= 3 && v[:3] == "ai_"
}
func aiSceneID() string {
	if !isAIScene() {
		return ""
	}
	return *sceneFlag
}

func aiSceneAnchorPos() components.WorldPos {
	switch aiSceneID() {
	case aiSceneDoorSouth, aiSceneDoorNorth, aiSceneDoorEast, aiSceneDoorWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 26})
	case aiSceneCompoundSouth, aiSceneCompoundEast,
		aiSceneCompoundNorth, aiSceneCompoundWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 26})
	case aiSceneCompoundMain:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	case aiSceneCompoundPlusSouth, aiSceneCompoundPlusEast,
		aiSceneCompoundPlusNorth, aiSceneCompoundPlusWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 26})
	case aiSceneOfficeFront:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 22})
	case aiSceneFarBuilding:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	case aiSceneMainM0, aiSceneMainM1, aiSceneMainE, aiSceneMainN:
		return components.WorldPos{}.Add(rl.Vector3{X: -40, Z: 10})
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 20})
	}
	return components.WorldPos{}
}

// aiSceneTrenches: the defilade scene digs its own line under the enemy;
// other ai scenes keep the main-map trench (far away from all of them).
func aiSceneTrenches() []components.Trench {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	if aiSceneID() == aiSceneLosDefilade {
		return []components.Trench{{
			Points: []components.WorldPos{wp(20, 25), wp(40, 25)},
			Width:  2.0,
			Depth:  1.5,
		}}
	}
	return []components.Trench{{
		Points: []components.WorldPos{wp(-50, 40), wp(-35, 50), wp(-15, 55)},
		Width:  1.5,
		Depth:  1.5,
	}}
}

func aiSceneBuildings() []components.BuildingPlan {
	if _, ok := aiMainSpecFor(aiSceneID()); ok {
		return mainWorldBuildings()
	}
	switch aiSceneID() {
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep:
		return nil
	case aiSceneDoorSouth:
		return aiBuildingsSingleHouse(0)
	case aiSceneDoorNorth:
		return aiBuildingsSingleHouse(2)
	case aiSceneDoorEast:
		return aiBuildingsSingleHouse(1)
	case aiSceneDoorWest:
		return aiBuildingsSingleHouse(3)
	case aiSceneCompoundSouth, aiSceneCompoundEast,
		aiSceneCompoundNorth, aiSceneCompoundWest:
		return aiBuildingsCompound()
	case aiSceneCompoundMain:
		return aiBuildingsCompoundMain()
	case aiSceneCompoundPlusSouth, aiSceneCompoundPlusEast,
		aiSceneCompoundPlusNorth, aiSceneCompoundPlusWest:
		return aiBuildingsCompoundPlus()
	case aiSceneOfficeFront:
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		return []components.BuildingPlan{*buildings.GenerateOffice(0xE5, pos)}
	case aiSceneFarBuilding:
		pos := components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 50})
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		plan := buildings.GenerateHouse(0xA0, buildings.HouseParams{
			Stories:  1,
			SizeX:    8,
			SizeZ:    8,
			DoorSide: 0,
		}, pos, components.BuildingHouse)
		return []components.BuildingPlan{*plan}
	}
	return nil
}

// aiBuildingsSingleHouse returns one 8×8 1-storey house at (32, 32).
// doorSide: 0=south, 1=east, 2=north, 3=west.
func aiBuildingsSingleHouse(doorSide uint8) []components.BuildingPlan {
	pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
	pos.Local.Y = systems.GroundHeight(
		pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
		pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
	)
	plan := buildings.GenerateHouse(0xA0, buildings.HouseParams{
		Stories:  1,
		SizeX:    8,
		SizeZ:    8,
		DoorSide: doorSide,
	}, pos, components.BuildingHouse)
	return []components.BuildingPlan{*plan}
}

// aiBuildingsCompound returns a 3-wing L-shape compound centred at (32, 32).
func aiBuildingsCompound() []components.BuildingPlan {
	centre := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
	centre.Local.Y = systems.GroundHeight(
		centre.Local.X+float32(centre.Chunk.X)*components.ChunkSize,
		centre.Local.Z+float32(centre.Chunk.Z)*components.ChunkSize,
	)
	out := []components.BuildingPlan{}
	for _, p := range buildings.GenerateCompound(0xF6, centre) {
		out = append(out, *p)
	}
	return out
}

// aiBuildingsCompoundMain mirrors the multi-chunk compound placement from main.go.
func aiBuildingsCompoundMain() []components.BuildingPlan {
	centre := components.WorldPos{}.Add(rl.Vector3{X: -60, Z: 10})
	centre.Local.Y = systems.GroundHeight(
		centre.Local.X+float32(centre.Chunk.X)*components.ChunkSize,
		centre.Local.Z+float32(centre.Chunk.Z)*components.ChunkSize,
	)
	out := []components.BuildingPlan{}
	for _, p := range buildings.GenerateCompound(0xF6, centre) {
		out = append(out, *p)
	}
	return out
}

// aiBuildingsCompoundPlus returns a plus-shaped 5-wing compound centred at (32, 32).
func aiBuildingsCompoundPlus() []components.BuildingPlan {
	centre := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
	centre.Local.Y = systems.GroundHeight(
		centre.Local.X+float32(centre.Chunk.X)*components.ChunkSize,
		centre.Local.Z+float32(centre.Chunk.Z)*components.ChunkSize,
	)
	out := []components.BuildingPlan{}
	for _, p := range buildings.GenerateCompoundPlus(0xF7, centre) {
		out = append(out, *p)
	}
	return out
}

// aiSpawnPos returns the squad's start position for the active scene.
func aiSpawnPos() components.WorldPos {
	switch aiSceneID() {
	case aiSceneDoorSouth, aiSceneDoorNorth,
		aiSceneDoorEast, aiSceneDoorWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 20})
	case aiSceneCompoundSouth:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 20})
	case aiSceneCompoundEast:
		return components.WorldPos{}.Add(rl.Vector3{X: 58, Z: 30})
	case aiSceneCompoundNorth:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 58})
	case aiSceneCompoundWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 14, Z: 30})
	case aiSceneCompoundMain:
		return components.WorldPos{}.Add(rl.Vector3{X: -20, Z: 0})
	case aiSceneCompoundPlusSouth:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 18})
	case aiSceneCompoundPlusEast:
		return components.WorldPos{}.Add(rl.Vector3{X: 60, Z: 32})
	case aiSceneCompoundPlusNorth:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 60})
	case aiSceneCompoundPlusWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 10, Z: 32})
	case aiSceneOfficeFront:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 18})
	case aiSceneFarBuilding:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	case aiSceneMainM0, aiSceneMainM1, aiSceneMainE, aiSceneMainN:
		return components.WorldPos{}.Add(rl.Vector3{X: -44, Z: -4})
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 10})
	}
	return components.WorldPos{}
}

// aiSceneSpawn instantiates the squad + captures the entities the auto-
// verifier needs. Returns nil for non-AI scenes.
func aiSceneSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
	buildingMap *ecs.Map[components.Building],
) *aiTestState {
	if !isAIScene() {
		return nil
	}
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSpawnPos(),
		components.FormationLine, playerFaction, roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}

	if id := aiSceneID(); id == aiSceneLosOpen || id == aiSceneLosDefilade || id == aiSceneLosCreep {
		// HoldFire keeps the enemy alive: sweepAwareness on death would wipe
		// the very entries the verdict inspects.
		if rules := ecs.NewMap[components.EngagementRules](world).Get(squad); rules != nil {
			rules.Mode = components.HoldFire
		}
		enemyZ := float32(25)
		if id == aiSceneLosCreep {
			enemyZ = 30
		}
		enemyPos := components.WorldPos{}.Add(rl.Vector3{X: 30, Z: enemyZ})
		enemyPos.Local.Y = systems.GroundHeight(30, enemyZ)
		enemy := unitFactory(enemyPos)
		ecs.NewMap[components.Faction](world).Add(enemy, &components.Faction{ID: components.FactionEnemyRed})
		ecs.NewMap[components.HP](world).Add(enemy, &components.HP{Current: 100, Max: 100})
		if id == aiSceneLosCreep {
			// Prone + override so StanceController's Safe band doesn't stand
			// him back up.
			if st := ecs.NewMap[components.Stance](world).Get(enemy); st != nil {
				st.Code = components.StanceProne
				st.LockUntil = 1e9 // unit_movement's profile auto-stance respects the lock
			}
			ecs.NewMap[components.StanceOverride](world).Add(enemy, &components.StanceOverride{Until: 1e9})
		}
		// Short northward march so the settled formation faces the enemy —
		// the optical cone must not decide the verdict.
		squadService.IssueOrder(squad, components.OrderKindMoveTo,
			components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 12}), ecs.Entity{},
			false, systems.OrderParams{})
		fmt.Println("============================================================")
		fmt.Printf("== AI LOS SCENE: %s  squad=%v enemy=%v expectVisible=%v\n",
			id, squad, enemy, id == aiSceneLosOpen)
		fmt.Println("============================================================")
		earlyAt := float32(0)
		if id == aiSceneLosCreep {
			earlyAt = 3
		}
		return &aiTestState{
			sceneID:          id,
			squad:            squad,
			losTarget:        enemy,
			losExpectVisible: id != aiSceneLosDefilade,
			losEarlyAt:       earlyAt,
			World:            world,
			SquadService:     squadService,
			PosMap:           posMap,
			RosterMap:        rosterMap,
			BuildingMap:      buildingMap,
			MotionMap:        ecs.NewMap[components.Motion](world),
			BlackboardMap:    ecs.NewMap[components.LocalBlackboard](world),
			MicroPathMap:     ecs.NewMap[components.MicroPath](world),
			orderFired:       true,
			verdictAt:        20,
			nextSampleAt:     5,
		}
	}

	// For compound scenes, target the closest wing to the spawn position so
	// we can exercise multi-section interior pathing. ai_main_* scenes pick
	// an explicit wing by footprint centre. Non-compound scenes keep the
	// largest-footprint pick.
	mainSpec, isMainScene := aiMainSpecFor(aiSceneID())
	preferNearest := false
	switch aiSceneID() {
	case aiSceneCompoundSouth, aiSceneCompoundEast, aiSceneCompoundNorth, aiSceneCompoundWest,
		aiSceneCompoundPlusSouth, aiSceneCompoundPlusEast, aiSceneCompoundPlusNorth, aiSceneCompoundPlusWest:
		preferNearest = true
	}
	var target ecs.Entity
	var targetFP components.AABB2D
	bestDist := float32(0)
	maxArea := float32(0)
	spawn := aiSpawnPos()
	spawnX := float32(spawn.Chunk.X)*components.ChunkSize + spawn.Local.X
	spawnZ := float32(spawn.Chunk.Z)*components.ChunkSize + spawn.Local.Z
	bf := ecs.NewFilter1[components.Building](world)
	q := bf.Query()
	for q.Next() {
		b := q.Get()
		cx := b.Footprint.CenterX()
		cz := b.Footprint.CenterZ()
		if isMainScene {
			dx := cx - mainSpec.wingX
			dz := cz - mainSpec.wingZ
			if dx*dx+dz*dz < 2.25 {
				target = q.Entity()
				targetFP = b.Footprint
			}
			continue
		}
		if preferNearest {
			dx := cx - spawnX
			dz := cz - spawnZ
			dist := dx*dx + dz*dz
			if target == (ecs.Entity{}) || dist < bestDist {
				bestDist = dist
				target = q.Entity()
				targetFP = b.Footprint
			}
			continue
		}
		area := b.Footprint.SizeX() * b.Footprint.SizeZ()
		if area > maxArea {
			maxArea = area
			target = q.Entity()
			targetFP = b.Footprint
		}
	}
	q.Close()
	if target == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] NO BUILDING FOUND — aborting\n", aiSceneID())
		return nil
	}

	st := &aiTestState{
		sceneID:         aiSceneID(),
		squad:           squad,
		targetBuilding:  target,
		targetFootprint: targetFP,
		World:           world,
		SquadService:    squadService,
		PosMap:          posMap,
		RosterMap:       rosterMap,
		BuildingMap:     buildingMap,
		MotionMap:       ecs.NewMap[components.Motion](world),
		BlackboardMap:   ecs.NewMap[components.LocalBlackboard](world),
		MicroPathMap:    ecs.NewMap[components.MicroPath](world),
		orderAt:         aiOrderAt,
		verdictAt:       aiVerdictAt,
		nextSampleAt:    aiOrderAt + 1,
	}

	// ai_main_* scenes target one storey: resolve the Level entity via
	// BuildingPlanIndex (same path the in-game "Occupy L<n>" popup takes).
	if isMainScene {
		planIdxRes := ecs.NewResource[systems.BuildingPlanIndex](world)
		planIdx := planIdxRes.Get()
		levelMap := ecs.NewMap[components.Level](world)
		if planIdx == nil {
			fmt.Printf("[ai-test %s] NO BuildingPlanIndex — aborting\n", aiSceneID())
			return nil
		}
		levels := planIdx.Levels[target]
		if mainSpec.levelIdx >= len(levels) {
			fmt.Printf("[ai-test %s] wing has %d levels, need idx %d — aborting\n",
				aiSceneID(), len(levels), mainSpec.levelIdx)
			return nil
		}
		levelEnt := levels[mainSpec.levelIdx]
		lvl := levelMap.Get(levelEnt)
		if lvl == nil {
			fmt.Printf("[ai-test %s] level entity %v has no Level component — aborting\n",
				aiSceneID(), levelEnt)
			return nil
		}
		st.targetLevel = levelEnt
		st.targetLevelMinY = lvl.AABB.MinY
		st.targetLevelAABB = lvl.AABB
	}
	return st
}

type aiTestState struct {
	sceneID         string
	squad           ecs.Entity
	targetBuilding  ecs.Entity
	targetFootprint components.AABB2D

	// Set for ai_main_* scenes: the goal is one specific storey, the order
	// is MoveTo on the Level entity, and the verdict additionally checks
	// each member's Y against the level band.
	targetLevel     ecs.Entity
	targetLevelMinY float32
	targetLevelAABB components.AABB3D

	// Set for ai_los_* scenes: verdict counts Contacts on this entity and
	// tallies Direct/Shared awareness across the roster.
	losTarget         ecs.Entity
	losExpectVisible  bool
	losEarlyAt        float32 // >0: contacts must still be 0 at this time
	losEarlyDone      bool
	losEarlyContacts  int

	elapsed      float32
	orderAt      float32
	verdictAt    float32
	nextSampleAt float32

	orderFired  bool
	verdictDone bool

	World        *ecs.World
	SquadService *systems.SquadService
	PosMap       *ecs.Map[components.WorldPos]
	RosterMap    *ecs.Map[components.CommandRoster]
	BuildingMap  *ecs.Map[components.Building]
	MotionMap    *ecs.Map[components.Motion]
	BlackboardMap *ecs.Map[components.LocalBlackboard]
	MicroPathMap *ecs.Map[components.MicroPath]
}

// EnsureInit prints a one-time scene banner.
func (s *aiTestState) EnsureInit() {
	if s == nil || s.orderFired {
		return
	}
	if s.elapsed > 0.05 {
		return
	}
	roster := s.RosterMap.Get(s.squad)
	memN := 0
	if roster != nil {
		memN = int(roster.Count)
	}
	fp := s.targetFootprint
	fmt.Println("============================================================")
	fmt.Printf("== AI SCENE: %s\n", s.sceneID)
	fmt.Printf("== squad=%v (%d units)  target_building=%v\n",
		s.squad, memN, s.targetBuilding)
	fmt.Printf("== footprint X[%.1f..%.1f] Z[%.1f..%.1f]\n",
		fp.MinX, fp.MaxX, fp.MinZ, fp.MaxZ)
	if s.targetLevel != (ecs.Entity{}) {
		fmt.Printf("== storey goal: level=%v floorY=%.2f\n",
			s.targetLevel, s.targetLevelMinY)
	}
	fmt.Printf("== order at t=%.1fs, verdict at t=%.1fs\n",
		s.orderAt, s.verdictAt)
	fmt.Println("============================================================")
}

// Update drives the auto-test lifecycle: issue order, sample, verdict.
func (s *aiTestState) Update(elapsed float32) {
	if s == nil {
		return
	}
	s.elapsed = elapsed
	s.EnsureInit()

	if s.losTarget != (ecs.Entity{}) {
		s.updateLos(elapsed)
		return
	}

	if !s.orderFired && elapsed >= s.orderAt {
		bld := s.BuildingMap.Get(s.targetBuilding)
		if bld == nil {
			fmt.Printf("[ai-test %s] target building gone before order — aborting\n", s.sceneID)
			s.verdictDone = true
			return
		}
		if s.targetLevel != (ecs.Entity{}) {
			// Storey goal: MoveTo on the Level entity, target at the level
			// AABB centre — the exact order the building-popup "Occupy L<n>"
			// emits, so the test exercises the real player mechanic.
			targetPos := components.WorldPos{}.Add(rl.Vector3{
				X: s.targetLevelAABB.CenterX(),
				Z: s.targetLevelAABB.CenterZ(),
			})
			targetPos.Local.Y = s.targetLevelMinY
			s.SquadService.IssueOrder(s.squad,
				components.OrderKindMoveTo, targetPos, s.targetLevel,
				false, systems.OrderParams{})
			s.orderFired = true
			fmt.Printf("[ai-test %s] t=%.1fs ORDER ISSUED MoveTo level=%v Y=%.1f\n",
				s.sceneID, elapsed, s.targetLevel, s.targetLevelMinY)
			return
		}
		targetPos := components.WorldPos{}.Add(rl.Vector3{
			X: bld.Footprint.CenterX(),
			Z: bld.Footprint.CenterZ(),
		})
		s.SquadService.IssueOrder(s.squad,
			components.OrderKindOccupyBuilding, targetPos, s.targetBuilding,
			false, systems.OrderParams{})
		s.orderFired = true
		fmt.Printf("[ai-test %s] t=%.1fs ORDER ISSUED OccupyBuilding target=%v\n",
			s.sceneID, elapsed, s.targetBuilding)
	}

	if s.orderFired && !s.verdictDone && elapsed >= s.nextSampleAt {
		inside, alive := s.countInside()
		roster := s.RosterMap.Get(s.squad)
		var diag string
		if roster != nil && roster.Count > 0 {
			// Track the first member still outside the goal (the straggler
			// is who needs diagnosing); fall back to the leader.
			watch := roster.Members[0]
			watchIdx := uint8(0)
			fp := s.targetFootprint
			for i := uint8(0); i < roster.Count; i++ {
				mem := roster.Members[i]
				if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
					continue
				}
				pos := s.PosMap.Get(mem)
				if pos == nil {
					continue
				}
				mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
				mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
				bad := !fp.Contains(mx, mz)
				if !bad && s.targetLevel != (ecs.Entity{}) {
					dy := pos.Local.Y - s.targetLevelMinY
					bad = dy < -0.8 || dy > 0.8
				}
				if bad {
					watch = mem
					watchIdx = i
					break
				}
			}
			if watch != (ecs.Entity{}) && s.World.Alive(watch) {
				pos := s.PosMap.Get(watch)
				mot := s.MotionMap.Get(watch)
				bb := s.BlackboardMap.Get(watch)
				mp := s.MicroPathMap.Get(watch)
				if pos != nil && mot != nil {
					mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
					mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
					mode := "?"
					if bb != nil {
						mode = components.ModeName(bb.CurrentMode)
					}
					mpLen := uint8(0)
					mpHead := uint8(0)
					wps := ""
					if mp != nil {
						mpLen = mp.Count
						mpHead = mp.Head
						for k := mp.Head; k < mp.Count && k < mp.Head+3; k++ {
							wp := mp.Waypoints[k]
							wps += fmt.Sprintf(" wp%d=(%.1f,%.1f,Y%.1f)", k,
								float32(wp.Chunk.X)*components.ChunkSize+wp.Local.X,
								float32(wp.Chunk.Z)*components.ChunkSize+wp.Local.Z,
								wp.Local.Y)
						}
					}
					diag = fmt.Sprintf(" m%d=(%.1f,%.1f) speed=%.2f mode=%s path=%d/%d%s",
						watchIdx, mx, mz, mot.Speed, mode, mpHead, mpLen, wps)
				}
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs sample: inside=%d/%d%s\n",
			s.sceneID, elapsed, inside, alive, diag)
		s.nextSampleAt = elapsed + aiSampleEvery
	}

	if !s.verdictDone && elapsed >= s.verdictAt {
		inside, alive := s.countInside()
		verdict := "FAIL"
		if inside >= alive && alive > 0 {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (%d/%d members inside)\n",
			s.sceneID, verdict, inside, alive)
		s.dumpPositions()
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

func (s *aiTestState) updateLos(elapsed float32) {
	if s.verdictDone {
		return
	}
	if s.losEarlyAt > 0 && !s.losEarlyDone && elapsed >= s.losEarlyAt {
		s.losEarlyDone = true
		s.losEarlyContacts = s.losContactCount()
		fmt.Printf("[ai-test %s] t=%.1fs early: contacts=%d\n",
			s.sceneID, elapsed, s.losEarlyContacts)
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt += aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs sample: contacts=%d\n",
			s.sceneID, elapsed, s.losContactCount())
	}
	if elapsed < s.verdictAt {
		return
	}
	s.verdictDone = true
	contacts := s.losContactCount()
	direct, shared, members := s.losAwarenessTally()
	pass := contacts == 0
	if s.losExpectVisible {
		pass = contacts >= 1 && direct >= 1 && direct+shared == members
		if s.losEarlyAt > 0 && s.losEarlyContacts != 0 {
			pass = false
		}
	}
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (contacts=%d direct=%d shared=%d members=%d early=%d expectVisible=%v)\n",
		s.sceneID, verdict, contacts, direct, shared, members, s.losEarlyContacts, s.losExpectVisible)
	fmt.Println("============================================================")
}

func (s *aiTestState) losContactCount() int {
	regRes := ecs.NewResource[components.ContactRegistry](s.World)
	reg := regRes.Get()
	if reg == nil || reg.Tracked == nil {
		return 0
	}
	n := 0
	for tracked := range reg.Tracked {
		if tracked == s.losTarget {
			n++
		}
	}
	return n
}

func (s *aiTestState) losAwarenessTally() (direct, shared, members int) {
	awareMap := ecs.NewMap[components.Awareness](s.World)
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		members++
		aw := awareMap.Get(mem)
		if aw == nil {
			continue
		}
		for j := range aw.LastSeen {
			e := &aw.LastSeen[j]
			if e.Time == 0 || e.Target != s.losTarget {
				continue
			}
			if e.Flags&components.AwareDirect != 0 {
				direct++
			} else {
				shared++
			}
			break
		}
	}
	return
}

// countInside tallies live roster members inside the target Footprint (and,
// for storey-goal scenes, standing on the target level: |Y - MinY| <= 0.8).
func (s *aiTestState) countInside() (inside, alive uint8) {
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return 0, 0
	}
	fp := s.targetFootprint
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		alive++
		pos := s.PosMap.Get(mem)
		if pos == nil {
			continue
		}
		mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		if !fp.Contains(mx, mz) {
			continue
		}
		if s.targetLevel != (ecs.Entity{}) {
			dy := pos.Local.Y - s.targetLevelMinY
			if dy < -0.8 || dy > 0.8 {
				continue
			}
		}
		inside++
	}
	return inside, alive
}

// dumpPositions prints each member's XZ + inside-footprint flag.
func (s *aiTestState) dumpPositions() {
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	fp := s.targetFootprint
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) {
			fmt.Printf("    member[%d]: zero\n", i)
			continue
		}
		if !s.World.Alive(mem) {
			fmt.Printf("    member[%d]=%v: dead\n", i, mem)
			continue
		}
		pos := s.PosMap.Get(mem)
		if pos == nil {
			fmt.Printf("    member[%d]=%v: no WorldPos\n", i, mem)
			continue
		}
		mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		flag := "OUTSIDE"
		if fp.Contains(mx, mz) {
			flag = "inside"
		}
		fmt.Printf("    member[%d]=%v: (%.1f, %.1f, Y=%.2f) [%s]\n",
			i, mem, mx, mz, pos.Local.Y, flag)
	}
}
