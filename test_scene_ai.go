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
	}
	return components.WorldPos{}
}

func aiSceneBuildings() []components.BuildingPlan {
	switch aiSceneID() {
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

	// For compound scenes, target the closest wing to the spawn position so
	// we can exercise multi-section interior pathing. Non-compound scenes
	// keep the largest-footprint pick.
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
		if preferNearest {
			cx := b.Footprint.CenterX()
			cz := b.Footprint.CenterZ()
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

// countInside tallies live roster members inside the target Footprint.
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
