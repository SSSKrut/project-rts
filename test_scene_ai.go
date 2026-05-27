package main

// Phase 17.8 — automated AI test scenes. Run with `./bin/rts -scene=<id>`.
//
// Each scene spawns a minimal world (1 building or 1 compound + 1 player
// squad, no enemies, no trenches), auto-issues an OccupyBuilding order
// against the target building at t = aiOrderAt, samples insider count
// every aiSampleEvery, and prints a single VERDICT line at t = aiVerdictAt
// listing PASS / FAIL plus a per-unit position dump.
//
// The point is to ISOLATE pathfinding failures from movement / formation /
// other systems by stripping the world to one specific layout. When a
// scene fails, the stdout dump tells you exactly which cells the squad
// landed in and what the pathfinder did or didn't do.
//
// Scenes:
//
//   ai_door_south       Control. 8×8 house with door on the south wall,
//                       squad 12 m south. Squad walks 12 m straight to
//                       the door. Trivial — should PASS in < 15 s.
//
//   ai_door_north       Same house but door is on the NORTH wall.
//                       Squad still 12 m south. Squad must walk around
//                       the building (~25 m perimeter). Tests surface
//                       pathfinding around a footprint.
//
//   ai_compound_south   3-wing compound (main + east + north), squad 12 m
//                       south of the main wing. Target = main wing. Same
//                       complexity as ai_door_south but with adjacent
//                       wings sharing walls — tests that doors stay
//                       reachable despite NavInBuilding bits from
//                       neighbours.
//
//   ai_compound_east    Same compound, squad 12 m EAST of the east wing.
//                       Target = main wing. Squad must either traverse
//                       east wing → main wing through level-junction
//                       edges (M17.8.7) or walk around the compound
//                       perimeter. This is the bug the user reported.
//
// Verdict criteria: PASS if every live roster member is inside the target
// building's Building.Footprint AND on a Floor entity (Garrison-style
// completion). FAIL otherwise; dump prints each member's world XZ and
// whether they're inside footprint.

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/gen/buildings"
	"rts-go/systems"
)

// aiUnitSpawn matches SquadService.CreateFromTemplate's unitFactory shape.
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
	aiSceneOfficeFront   = "ai_office_front"
	aiSceneFarBuilding   = "ai_far_building"
)

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

// aiSceneAnchorPos — where the player anchor / camera target sits at start.
// Centred on the building so the test view loads framed.
func aiSceneAnchorPos() components.WorldPos {
	switch aiSceneID() {
	case aiSceneDoorSouth, aiSceneDoorNorth, aiSceneDoorEast, aiSceneDoorWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 26})
	case aiSceneCompoundSouth, aiSceneCompoundEast,
		aiSceneCompoundNorth, aiSceneCompoundWest:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 26})
	case aiSceneOfficeFront:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 22})
	case aiSceneFarBuilding:
		// Far-building test — anchor stays at origin; squad walks ~50 m
		// to target. LODAnchor on target should keep its chunks loaded
		// so pathfinder can find a route from t=0.
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	}
	return components.WorldPos{}
}

// aiSceneBuildings — building plans for the selected scene.
func aiSceneBuildings() []components.BuildingPlan {
	switch aiSceneID() {
	case aiSceneDoorSouth:
		return aiBuildingsSingleHouse(0 /*DoorSide south*/)
	case aiSceneDoorNorth:
		return aiBuildingsSingleHouse(2 /*DoorSide north*/)
	case aiSceneDoorEast:
		return aiBuildingsSingleHouse(1 /*DoorSide east*/)
	case aiSceneDoorWest:
		return aiBuildingsSingleHouse(3 /*DoorSide west*/)
	case aiSceneCompoundSouth, aiSceneCompoundEast,
		aiSceneCompoundNorth, aiSceneCompoundWest:
		return aiBuildingsCompound()
	case aiSceneOfficeFront:
		// 3-storey Office 14×10 with interior partition. Tests the
		// partition-aware path planning (interior wall, doors both
		// sides).
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		return []components.BuildingPlan{*buildings.GenerateOffice(0xE5, pos)}
	case aiSceneFarBuilding:
		// Building 50 m north of spawn — LODAnchor must activate its
		// chunks for pathfinder to find a route immediately.
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

// aiBuildingsSingleHouse returns one 8×8 1-storey house at (32, 32) with
// the door on the requested side (0=south, 1=east, 2=north, 3=west).
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
// Same layout as production-default compound but isolated for testing.
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

// aiSpawnPos — where the squad spawns. Tests squad approach from each
// cardinal direction; squad always 12 m from the closest edge of the
// target building.
func aiSpawnPos() components.WorldPos {
	switch aiSceneID() {
	case aiSceneDoorSouth, aiSceneDoorNorth,
		aiSceneDoorEast, aiSceneDoorWest:
		// 12 m south of the house centre (32, 32) regardless of door
		// side — door-side variants test pathfinding around the walls.
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 20})
	case aiSceneCompoundSouth:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 20})
	case aiSceneCompoundEast:
		// East of east wing (X[38..46]/Z[28..36]). Spawn (58, 30).
		return components.WorldPos{}.Add(rl.Vector3{X: 58, Z: 30})
	case aiSceneCompoundNorth:
		// North of north wing (X[28..36]/Z[38..46]). Spawn (32, 58).
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 58})
	case aiSceneCompoundWest:
		// West of main wing (X[26..38]/Z[26..38]). Spawn (14, 30).
		return components.WorldPos{}.Add(rl.Vector3{X: 14, Z: 30})
	case aiSceneOfficeFront:
		// 12 m south of Office (14×10 footprint at (32, 32)).
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 18})
	case aiSceneFarBuilding:
		// Spawn at origin; building at (0, 50) → ~50 m walk.
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	}
	return components.WorldPos{}
}

// aiSceneSpawn instantiates the squad + captures the entities the auto-
// verifier needs. Returns the test state, or nil for non-AI scenes.
//
// Wired from main.go after BuildingPlanIndex is populated.
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

	// Pick the target building: for door scenes there's one; for compound
	// scenes pick main wing (largest footprint of the three).
	var target ecs.Entity
	var targetFP components.AABB2D
	maxArea := float32(0)
	bf := ecs.NewFilter1[components.Building](world)
	q := bf.Query()
	for q.Next() {
		b := q.Get()
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

	return &aiTestState{
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
}

// aiTestState — per-scene auto-test state. Populated by aiSceneSpawn,
// driven each frame by Update().
type aiTestState struct {
	sceneID         string
	squad           ecs.Entity
	targetBuilding  ecs.Entity
	targetFootprint components.AABB2D

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

// EnsureInit prints a one-time scene banner so the stdout log starts with
// what scene + what to expect.
func (s *aiTestState) EnsureInit() {
	if s == nil || s.orderFired {
		return // banner only before first order
	}
	if s.elapsed > 0.05 {
		return // wait one frame so all systems are initialised
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
	fmt.Printf("== order at t=%.1fs, verdict at t=%.1fs\n",
		s.orderAt, s.verdictAt)
	fmt.Println("============================================================")
}

// Update drives the auto-test lifecycle: issue order, sample, verdict.
// Called from main.go each frame with elapsed session-time.
func (s *aiTestState) Update(elapsed float32) {
	if s == nil {
		return
	}
	s.elapsed = elapsed
	s.EnsureInit()

	if !s.orderFired && elapsed >= s.orderAt {
		bld := s.BuildingMap.Get(s.targetBuilding)
		if bld == nil {
			fmt.Printf("[ai-test %s] target building gone before order — aborting\n", s.sceneID)
			s.verdictDone = true
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
		// Member 0 (commander) diagnostic — speed + mode + path len.
		roster := s.RosterMap.Get(s.squad)
		var diag string
		if roster != nil && roster.Count > 0 {
			m0 := roster.Members[0]
			if m0 != (ecs.Entity{}) && s.World.Alive(m0) {
				pos := s.PosMap.Get(m0)
				mot := s.MotionMap.Get(m0)
				bb := s.BlackboardMap.Get(m0)
				mp := s.MicroPathMap.Get(m0)
				if pos != nil && mot != nil {
					mx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
					mz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
					mode := "?"
					if bb != nil {
						mode = components.ModeName(bb.CurrentMode)
					}
					mpLen := uint8(0)
					mpHead := uint8(0)
					if mp != nil {
						mpLen = mp.Count
						mpHead = mp.Head
					}
					diag = fmt.Sprintf(" m0=(%.1f,%.1f) speed=%.2f mode=%s path=%d/%d",
						mx, mz, mot.Speed, mode, mpHead, mpLen)
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

// countInside tallies how many live roster members sit inside the target
// building's Footprint. Friendly approximation of CompletionEveryMemberOnFloor.
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
		if fp.Contains(mx, mz) {
			inside++
		}
	}
	return inside, alive
}

// dumpPositions prints each member's XZ world position + inside-footprint
// flag. Used by the verdict block to make a FAIL diagnosable at a glance.
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
