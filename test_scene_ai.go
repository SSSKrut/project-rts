package main

// Automated AI test scenes. Run with `./bin/rts -scene=<id>`. Each scene
// spawns a minimal isolated world, auto-issues OccupyBuilding at t=aiOrderAt,
// samples insider count every aiSampleEvery, prints PASS/FAIL at aiVerdictAt.

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
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
	// ai_office_l2 (ISSUES #20): the office is 3 storeys, so its stairs are
	// CASCADES — the only template that exercises them. Sending a squad to the
	// top storey is the integration half of the cascade-anchor fix: with the
	// old single-anchor flights the bake wired stairs to the exterior surface
	// and no path to L2 existed at all.
	aiSceneOfficeL2    = "ai_office_l2"
	aiSceneFarBuilding = "ai_far_building"

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

	// ai_vehicle_move (Phase 19 M1): truck + tank drive a 3-leg off-road
	// route via ActionQueue waypoints; PASS when both park at the final
	// point (early verdict on arrival).
	aiSceneVehMove = "ai_vehicle_move"
	// ai_vehicle_road (Phase 19 M2): two trucks patrol west↔east on `valley`
	// across the auto-tagged bridge; PASS = both parked at the final point
	// AND at least one tick spent on a RoadBridge edge (validates RoadGraph
	// A* + RoadFollower + deck Y end to end).
	aiSceneVehRoad = "ai_vehicle_road"

	// ai_march_* (ISSUES #12/#13): one MotorRifle squad marches a straight
	// ~100 m MoveTo and the verdict scores movement hygiene — formation-yaw
	// churn + sideways-walking ticks (#12), member clearance vs the live
	// heightmap (#13). _line runs on the default map, _slope on `hills`.
	aiSceneMarchLine  = "ai_march_line"
	aiSceneMarchSlope = "ai_march_slope"

	// ai_cover_side (ISSUES #18): a 2-man squad stands BETWEEN a lone oak
	// and a synthetic threat pulsing from the north (DangerBuffer injection,
	// no bullets). The scramble must relocate both men to slots on the far
	// (south) side of the trunk. PASS = both assigned slots south of the
	// tree AND both units parked on them.
	aiSceneCoverSide = "ai_cover_side"

	// ai_vehicle_combat: tank+BTR (player) vs BMP+ATCarrier (enemy AI) at
	// ~50 m. Gunners detect, slew and fire on their own; the cannon's
	// weapon-vs-class preference must delete both light hulls while the
	// tank shrugs off 30mm/ATGM frontal hits. PASS = enemy side destroyed,
	// at least one player vehicle alive.
	aiSceneVehCombat = "ai_vehicle_combat"

	// ai_vehicle_reflex (Phase 19 M4): three vehicles take synthetic fire
	// from one point and each runs its class reflex — the tank (spawned
	// side-on) pivots its hull to face the threat, the BMP drops a smoke
	// field and reverses, the truck flees. PASS = all three conditions in
	// one run.
	aiSceneVehReflex = "ai_vehicle_reflex"
)

// aiSceneMapName lets a scene demand a specific map manifest ("" = default).
func aiSceneMapName() string {
	switch aiSceneID() {
	case aiSceneMarchSlope:
		return "hills"
	case aiSceneVehRoad:
		return "valley"
	}
	return ""
}

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
	// The office on the REAL main map (70, -20) — 3 storeys, cascade stairs.
	case aiSceneOfficeL2:
		return aiMainSpec{wingX: 70, wingZ: -20, levelIdx: 2}, true
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
	case aiSceneOfficeL2:
		return components.WorldPos{}.Add(rl.Vector3{X: 66, Z: -29})
	case aiSceneFarBuilding:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	case aiSceneMainM0, aiSceneMainM1, aiSceneMainE, aiSceneMainN:
		return components.WorldPos{}.Add(rl.Vector3{X: -40, Z: 10})
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 20})
	case aiSceneVehMove:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 0})
	case aiSceneVehRoad:
		return components.WorldPos{}.Add(rl.Vector3{X: 20, Z: -8})
	case aiSceneMarchLine:
		return components.WorldPos{}.Add(rl.Vector3{X: 75, Z: 10})
	case aiSceneMarchSlope:
		return components.WorldPos{}.Add(rl.Vector3{X: 20, Z: -10})
	case aiSceneCoverSide:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -30})
	case aiSceneVehCombat:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -30})
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
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep,
		aiSceneMarchLine, aiSceneMarchSlope:
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
	case aiSceneOfficeL2:
		return components.WorldPos{}.Add(rl.Vector3{X: 70, Z: -33})
	case aiSceneFarBuilding:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	case aiSceneMainM0, aiSceneMainM1, aiSceneMainE, aiSceneMainN:
		return components.WorldPos{}.Add(rl.Vector3{X: -44, Z: -4})
	case aiSceneLosOpen, aiSceneLosDefilade, aiSceneLosCreep:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 10})
	case aiSceneMarchLine:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 10})
	case aiSceneMarchSlope:
		return components.WorldPos{}.Add(rl.Vector3{X: -20, Z: -40})
	}
	return components.WorldPos{}
}

// aiMarchSceneSpawn (#12/#13): one MotorRifle squad in Line, straight MoveTo.
func aiMarchSceneSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSpawnPos(),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	goal := components.WorldPos{}.Add(rl.Vector3{X: 120, Z: 10})
	// Flat straight march must hold a near-constant heading; the hills route
	// legitimately re-turns on path-exhaustion replans (16-wp cap = 64 m).
	churnMax := float32(40)
	if aiSceneID() == aiSceneMarchSlope {
		goal = components.WorldPos{}.Add(rl.Vector3{X: 60, Z: 20})
		churnMax = 150
	}
	goal.Local.Y = systems.GroundHeight(
		goal.Local.X+float32(goal.Chunk.X)*components.ChunkSize,
		goal.Local.Z+float32(goal.Chunk.Z)*components.ChunkSize,
	)
	// Two non-squad soloists exercise the pushSoloMove arm (#13's worst
	// offender: far direct MoveTo).
	spawn := aiSpawnPos()
	solo1 := unitFactory(spawn.Add(rl.Vector3{X: -5, Z: -4}))
	solo2 := unitFactory(spawn.Add(rl.Vector3{X: 5, Z: -4}))
	return &aiTestState{
		sceneID:       aiSceneID(),
		squad:         squad,
		marchActive:   true,
		marchGoal:     goal,
		marchChurnMax: churnMax,
		soloEnts:      []ecs.Entity{solo1, solo2},
		AQMap:         ecs.NewMap[components.ActionQueue](world),
		World:         world,
		SquadService:  squadService,
		PosMap:        posMap,
		RosterMap:     rosterMap,
		MotionMap:     ecs.NewMap[components.Motion](world),
		FdMap:         ecs.NewMap[components.FormationData](world),
		sampler:       systems.NewHeightSampler(world),
		roadSurface:   ecs.NewResource[components.RoadSurface](world),
		marchMinClear: 1e9,
		marchMaxClear: -1e9,
		orderAt:       aiOrderAt,
		verdictAt:     120,
		nextSampleAt:  aiOrderAt + 5,
	}
}

// aiVehicleSceneSpawn: truck + tank at (20, 8..16), three off-road legs.
func aiVehicleSceneSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	truck := vehicleFactory.Spawn(wp(20, 8), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	tank := vehicleFactory.Spawn(wp(20, 16), components.VehicleTank,
		components.FactionPlayer, components.ControllerLocal)
	return &aiTestState{
		sceneID:      aiSceneID(),
		vehEnts:      []ecs.Entity{truck, tank},
		// Leg 4 sits ~160° behind the leg-3 arrival heading: the truck
		// (4×TurnRadius = 32 m > 23 m) backs out, the tank pivots.
		vehWaypoints: []components.WorldPos{wp(55, 28), wp(80, -5), wp(45, -30), wp(60, -12)},
		World:        world,
		PosMap:       posMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		orderAt:      aiOrderAt,
		verdictAt:    90,
		nextSampleAt: aiOrderAt + 3,
	}
}

// aiVehicleRoadSceneSpawn: two trucks on `valley`, west↔east patrol. The
// time-optimal plan for a truck (road 16 / offroad 6 m/s) enters the highway
// and crosses the bridge both ways; off-road would be a straight swim through
// the river cut, so the bridge-tick check proves routing actually engaged.
func aiVehicleRoadSceneSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	t1 := vehicleFactory.Spawn(wp(-40, -30), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	t2 := vehicleFactory.Spawn(wp(-40, -24), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	return &aiTestState{
		sceneID:          aiSceneID(),
		vehEnts:          []ecs.Entity{t1, t2},
		vehWaypoints:     []components.WorldPos{wp(70, 8), wp(-45, -25)},
		vehRequireBridge: true,
		VehFollowerMap:   ecs.NewMap[components.RoadFollower](world),
		vehGraphRes:      ecs.NewResource[components.RoadGraph](world),
		World:            world,
		PosMap:           posMap,
		MotionMap:        ecs.NewMap[components.Motion](world),
		VehQueueMap:      ecs.NewMap[components.ActionQueue](world),
		orderAt:          aiOrderAt,
		verdictAt:        120,
		nextSampleAt:     aiOrderAt + 3,
	}
}

// aiCoverSceneSpawn (#18): lone oak at (40,-38), 2-man squad north of it at
// z=-30, synthetic shooter position further north at (40,-10).
func aiCoverSceneSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	unitFactory aiUnitSpawn,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	treePos := wp(40, -38)
	tree := world.NewEntity()
	posMap.Add(tree, &treePos)
	ecs.NewMap[components.Prop](world).Add(tree,
		&components.Prop{Type: components.PropOak, Yaw: 0, Scale: 1})
	ecs.NewMap[components.LODRelevant](world).Add(tree, &components.LODRelevant{})
	// Registering in PropChunkIndex is what makes the bake emit cover slots
	// (and eviction tear them down with the chunk).
	propIdxRes := ecs.NewResource[systems.PropChunkIndex](world)
	if idx := propIdxRes.Get(); idx != nil {
		idx.Loaded[treePos.Chunk] = append(idx.Loaded[treePos.Chunk], tree)
	}

	u1 := unitFactory(wp(36, -30))
	u2 := unitFactory(wp(44, -30))
	squad := squadService.CreateFromUnits([]ecs.Entity{u1, u2}, components.FormationLine)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO FORM SQUAD — aborting\n", aiSceneID())
		return nil
	}
	return &aiTestState{
		sceneID:         aiSceneID(),
		squad:           squad,
		coverActive:     true,
		coverMembers:    []ecs.Entity{u1, u2},
		coverTreeX:      40,
		coverTreeZ:      -38,
		coverShooter:    wp(40, -10),
		coverSlotZ:      map[ecs.Entity]float32{},
		coverSlotOf:     map[ecs.Entity]ecs.Entity{},
		DangerMap:       ecs.NewMap[components.DangerBuffer](world),
		OverrideMap:     ecs.NewMap[components.TacticalOverride](world),
		World:           world,
		SquadService:    squadService,
		PosMap:          posMap,
		RosterMap:       rosterMap,
		MotionMap:       ecs.NewMap[components.Motion](world),
		orderAt:         aiOrderAt,
		verdictAt:       30,
		nextSampleAt:    aiOrderAt + 3,
		coverNextInject: aiOrderAt,
	}
}

// aiVehicleCombatSpawn: tank+BTR (player, west) vs BMP+ATCarrier (enemy AI,
// east), ~50 m apart on open ground. No orders — the Gunner layer does the
// rest.
func aiVehicleCombatSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	tank := vehicleFactory.Spawn(wp(15, -35), components.VehicleTank,
		components.FactionPlayer, components.ControllerLocal)
	btr := vehicleFactory.Spawn(wp(25, -35), components.VehicleBTR,
		components.FactionPlayer, components.ControllerLocal)
	bmp := vehicleFactory.Spawn(wp(58, -32), components.VehicleBMP,
		components.FactionEnemyRed, components.ControllerAI)
	atc := vehicleFactory.Spawn(wp(64, -28), components.VehicleATCarrier,
		components.FactionEnemyRed, components.ControllerAI)
	// Sides face each other — the M4 FaceThreat reflex will own this later.
	motMap := ecs.NewMap[components.Motion](world)
	for _, e := range []ecs.Entity{tank, btr} {
		if m := motMap.Get(e); m != nil {
			m.Yaw = math.Pi / 2
		}
	}
	for _, e := range []ecs.Entity{bmp, atc} {
		if m := motMap.Get(e); m != nil {
			m.Yaw = -math.Pi / 2
		}
	}
	return &aiTestState{
		sceneID:         aiSceneID(),
		vehCombatActive: true,
		vehEnts:         []ecs.Entity{tank, btr},
		vehFoes:         []ecs.Entity{bmp, atc},
		HPMap:           ecs.NewMap[components.HP](world),
		World:           world,
		PosMap:          posMap,
		MotionMap:       ecs.NewMap[components.Motion](world),
		VehQueueMap:     ecs.NewMap[components.ActionQueue](world),
		orderAt:         aiOrderAt,
		verdictAt:       90,
		nextSampleAt:    aiOrderAt + 3,
	}
}

// aiVehicleReflexSpawn: a tank (side-on), a BMP and a truck, all player-side,
// each fed synthetic bullet-impact danger from one northern point. No real
// shooter — the reflex arbitration is what we measure.
func aiVehicleReflexSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
	posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	tankPos := wp(0, -35)
	bmpPos := wp(25, -35)
	truckPos := wp(-15, -20)
	tank := vehicleFactory.Spawn(tankPos, components.VehicleTank,
		components.FactionPlayer, components.ControllerLocal)
	bmp := vehicleFactory.Spawn(bmpPos, components.VehicleBMP,
		components.FactionPlayer, components.ControllerLocal)
	truck := vehicleFactory.Spawn(truckPos, components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	// Tank starts broadside to the threat so FaceThreat has 90° to close.
	motMap := ecs.NewMap[components.Motion](world)
	if m := motMap.Get(tank); m != nil {
		m.Yaw = math.Pi / 2
	}
	if m := motMap.Get(bmp); m != nil {
		m.Yaw = -math.Pi / 2
	}
	return &aiTestState{
		sceneID:          aiSceneID(),
		reflexActive:     true,
		reflexTank:       tank,
		reflexBmp:        bmp,
		reflexTruck:      truck,
		reflexSource:     wp(0, 0),
		reflexBmpSpawn:   bmpPos,
		reflexVehicles:   []ecs.Entity{tank, bmp, truck},
		DangerMap:        ecs.NewMap[components.DangerBuffer](world),
		SmokeFilter:      ecs.NewFilter2[components.SmokeField, components.WorldPos](world),
		World:            world,
		PosMap:           posMap,
		MotionMap:        motMap,
		orderAt:          aiOrderAt,
		verdictAt:        30,
		nextSampleAt:     aiOrderAt + 3,
		reflexNextInject: aiOrderAt,
	}
}

// aiSceneSpawn instantiates the squad + captures the entities the auto-
// verifier needs. Returns nil for non-AI scenes.
func aiSceneSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	vehicleFactory *entities.VehicleFactory,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
	buildingMap *ecs.Map[components.Building],
) *aiTestState {
	if !isAIScene() {
		return nil
	}
	if aiSceneID() == aiSceneVehMove {
		return aiVehicleSceneSpawn(world, vehicleFactory, posMap)
	}
	if aiSceneID() == aiSceneVehRoad {
		return aiVehicleRoadSceneSpawn(world, vehicleFactory, posMap)
	}
	if id := aiSceneID(); id == aiSceneMarchLine || id == aiSceneMarchSlope {
		return aiMarchSceneSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneCoverSide {
		return aiCoverSceneSpawn(world, squadService, unitFactory, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneVehCombat {
		return aiVehicleCombatSpawn(world, vehicleFactory, posMap)
	}
	if aiSceneID() == aiSceneVehReflex {
		return aiVehicleReflexSpawn(world, vehicleFactory, posMap)
	}
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, aiSpawnPos(),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
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
		if f := ecs.NewMap[components.Faction](world).Get(enemy); f != nil {
			f.ID = components.FactionEnemyRed
		}
		if c := ecs.NewMap[components.Controller](world).Get(enemy); c != nil {
			c.Owner = components.ControllerAI
		}
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

	// Set for ai_vehicle_* scenes: waypoints go straight into each
	// vehicle's ActionQueue; verdict = all parked at the final point.
	// vehRequireBridge additionally demands ≥1 tick on a RoadBridge edge.
	vehEnts          []ecs.Entity
	vehWaypoints     []components.WorldPos
	VehQueueMap      *ecs.Map[components.ActionQueue]
	vehRequireBridge bool
	vehBridgeTicks   int
	VehFollowerMap   *ecs.Map[components.RoadFollower]
	vehGraphRes      ecs.Resource[components.RoadGraph]

	// Set for ai_vehicle_combat: two sides duel, verdict = enemy side dead.
	vehCombatActive bool
	vehFoes         []ecs.Entity
	HPMap           *ecs.Map[components.HP]

	// Set for ai_vehicle_reflex (M4): each vehicle runs its class reflex
	// under synthetic fire from reflexSource.
	reflexActive     bool
	reflexTank       ecs.Entity
	reflexBmp        ecs.Entity
	reflexTruck      ecs.Entity
	reflexVehicles   []ecs.Entity
	reflexSource     components.WorldPos
	reflexBmpSpawn   components.WorldPos
	reflexNextInject float32
	reflexBmpSmoked  bool
	SmokeFilter      *ecs.Filter2[components.SmokeField, components.WorldPos]

	// Set for ai_cover_side (#18): synthetic-threat cover-side metric.
	coverActive     bool
	coverMembers    []ecs.Entity
	coverTreeX      float32
	coverTreeZ      float32
	coverShooter    components.WorldPos
	coverNextInject float32
	coverSlotZ      map[ecs.Entity]float32
	coverSlotOf     map[ecs.Entity]ecs.Entity
	DangerMap       *ecs.Map[components.DangerBuffer]
	OverrideMap     *ecs.Map[components.TacticalOverride]

	// Set for ai_march_* scenes (#12/#13): movement-hygiene metrics.
	marchActive   bool
	marchGoal     components.WorldPos
	marchChurnMax float32
	soloEnts      []ecs.Entity
	AQMap         *ecs.Map[components.ActionQueue]
	FdMap         *ecs.Map[components.FormationData]
	sampler       *systems.HeightSampler
	roadSurface   ecs.Resource[components.RoadSurface]
	marchMoveN    int
	marchSideN    int
	marchChurnDeg float32
	marchLastFYaw float32
	marchHaveFYaw bool
	marchMinClear float32
	marchMaxClear float32

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
	if s.coverActive {
		s.updateCoverSide(elapsed)
		return
	}
	if s.vehCombatActive {
		s.updateVehCombat(elapsed)
		return
	}
	if s.reflexActive {
		s.updateVehReflex(elapsed)
		return
	}
	if s.marchActive {
		s.updateMarch(elapsed)
		return
	}
	if len(s.vehEnts) > 0 {
		s.updateVehicles(elapsed)
		return
	}
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

// angDiffAbs — |a-b| folded into [0, pi].
func angDiffAbs(a, b float32) float32 {
	d := a - b
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d < -math.Pi {
		d += 2 * math.Pi
	}
	if d < 0 {
		d = -d
	}
	return d
}

func (s *aiTestState) updateMarch(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI MARCH SCENE: %s  squad=%v goal=(%.0f,%.0f)\n",
			s.sceneID, s.squad,
			float32(s.marchGoal.Chunk.X)*components.ChunkSize+s.marchGoal.Local.X,
			float32(s.marchGoal.Chunk.Z)*components.ChunkSize+s.marchGoal.Local.Z)
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.squad, components.OrderKindMoveTo,
			s.marchGoal, ecs.Entity{}, false, systems.OrderParams{})
		for _, e := range s.soloEnts {
			pushSoloMove(s.AQMap, s.PosMap, e, s.marchGoal, false)
		}
		s.orderFired = true
		return
	}

	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	sampleOne := func(mem ecs.Entity) {
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			return
		}
		pos := s.PosMap.Get(mem)
		mot := s.MotionMap.Get(mem)
		if pos == nil || mot == nil {
			return
		}
		if mot.Speed > 1.5 {
			s.marchMoveN++
			if angDiffAbs(mot.VelocityYaw, mot.Yaw) > math.Pi/3 {
				s.marchSideN++
			}
		}
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		// Clearance is measured against the surface a walker actually stands
		// on — heightmap, or the road deck where an embankment carries it.
		surf := s.sampler.Sample(wx, wz)
		if rs := s.roadSurface.Get(); rs != nil {
			surf = rs.SurfaceY(surf, wx, wz)
		}
		clear := pos.Local.Y - surf
		if clear < s.marchMinClear {
			s.marchMinClear = clear
		}
		if clear > s.marchMaxClear {
			s.marchMaxClear = clear
		}
	}
	// Per-frame metrics over live members + soloists.
	for i := uint8(0); i < roster.Count; i++ {
		sampleOne(roster.Members[i])
	}
	for _, e := range s.soloEnts {
		sampleOne(e)
	}
	// Churn accumulates after a 5 s alignment window: the initial in-place
	// turn toward the march heading is legitimate rotation, not noise.
	if fd := s.FdMap.Get(s.squad); fd != nil && (fd.Forward.X != 0 || fd.Forward.Z != 0) &&
		elapsed > s.orderAt+5 {
		yaw := float32(math.Atan2(float64(fd.Forward.X), float64(fd.Forward.Z)))
		if s.marchHaveFYaw {
			s.marchChurnDeg += angDiffAbs(yaw, s.marchLastFYaw) * (180 / math.Pi)
		}
		s.marchLastFYaw = yaw
		s.marchHaveFYaw = true
	}

	center, okC := systems.SquadCenter(s.World, roster, s.PosMap)
	arrived := false
	if okC {
		d := center.Sub(s.marchGoal)
		arrived = d.X*d.X+d.Z*d.Z < 9
	}
	for _, e := range s.soloEnts {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			continue
		}
		if p := s.PosMap.Get(e); p != nil {
			d := p.Sub(s.marchGoal)
			if d.X*d.X+d.Z*d.Z > 36 {
				arrived = false
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt && !arrived {
		s.nextSampleAt = elapsed + aiSampleEvery
		sideFrac := float32(0)
		if s.marchMoveN > 0 {
			sideFrac = float32(s.marchSideN) / float32(s.marchMoveN)
		}
		fmt.Printf("[ai-test %s] t=%.1fs side=%.3f churn=%.0fdeg clear=[%.2f..%.2f]\n",
			s.sceneID, elapsed, sideFrac, s.marchChurnDeg, s.marchMinClear, s.marchMaxClear)
	}

	if arrived || elapsed >= s.verdictAt {
		sideFrac := float32(1)
		if s.marchMoveN > 0 {
			sideFrac = float32(s.marchSideN) / float32(s.marchMoveN)
		}
		// Post-fix baseline: line 0deg side 0.01-0.03 clear ±0.14; slope
		// 88deg side 0.02-0.05 clear -0.22..0.18. Pre-fix: churn 410deg,
		// side-storms, clear -0.32 (and unbounded on far solo orders).
		pass := arrived &&
			sideFrac <= 0.08 &&
			s.marchChurnDeg <= s.marchChurnMax &&
			s.marchMinClear >= -0.3 &&
			s.marchMaxClear <= 0.5
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%v t=%.1fs side=%.3f churn=%.0fdeg clear=[%.2f..%.2f])\n",
			s.sceneID, verdict, arrived, elapsed, sideFrac, s.marchChurnDeg,
			s.marchMinClear, s.marchMaxClear)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateCoverSide (#18): pulse suppression events from the north shooter
// position into both members' DangerBuffers (t=2..12, every 0.5 s), track
// TacticalOverride.AssignedSlot, and pass when both men park on slots south
// of the trunk. Early verdict once both are within 1.5 m of their slots —
// after the threat decays the override clears and formation pulls them back.
func (s *aiTestState) updateCoverSide(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI COVER SCENE: %s  tree=(%.0f,%.0f) shooter north\n",
			s.sceneID, s.coverTreeX, s.coverTreeZ)
		fmt.Println("============================================================")
		s.orderFired = true
	}
	if elapsed <= 12 && elapsed >= s.coverNextInject {
		s.coverNextInject = elapsed + 0.5
		for _, u := range s.coverMembers {
			if u == (ecs.Entity{}) || !s.World.Alive(u) {
				continue
			}
			if buf := s.DangerMap.Get(u); buf != nil {
				components.PushDanger(buf, components.DangerEvent{
					Kind:     components.DangerBulletImpact,
					Pos:      s.coverShooter,
					Strength: 0.35,
					Time:     elapsed,
				})
			}
		}
	}

	assigned := 0
	parked := 0
	southSlots := 0
	for _, u := range s.coverMembers {
		if u == (ecs.Entity{}) || !s.World.Alive(u) {
			continue
		}
		if ov := s.OverrideMap.Get(u); ov != nil && ov.AssignedSlot != (ecs.Entity{}) {
			if sp := s.PosMap.Get(ov.AssignedSlot); sp != nil {
				s.coverSlotOf[u] = ov.AssignedSlot
				s.coverSlotZ[u] = float32(sp.Chunk.Z)*components.ChunkSize + sp.Local.Z
			}
		}
		slot, ok := s.coverSlotOf[u]
		if !ok {
			continue
		}
		assigned++
		if s.coverSlotZ[u] < s.coverTreeZ-0.5 {
			southSlots++
		}
		if sp := s.PosMap.Get(slot); sp != nil && s.World.Alive(slot) {
			up := s.PosMap.Get(u)
			if up != nil {
				d := up.Sub(*sp)
				if d.X*d.X+d.Z*d.Z < 1.5*1.5 {
					parked++
				}
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs assigned=%d south=%d parked=%d\n",
			s.sceneID, elapsed, assigned, southSlots, parked)
	}

	n := len(s.coverMembers)
	done := assigned == n && parked == n
	if done || elapsed >= s.verdictAt {
		pass := assigned == n && southSlots == n && parked == n
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (assigned=%d/%d southSlots=%d parked=%d treeZ=%.1f)\n",
			s.sceneID, verdict, assigned, n, southSlots, parked, s.coverTreeZ)
		for _, u := range s.coverMembers {
			if z, ok := s.coverSlotZ[u]; ok {
				fmt.Printf("==   unit %v slotZ=%.1f dz=%.1f\n", u, z, z-s.coverTreeZ)
			}
		}
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

func (s *aiTestState) sideStatus(side []ecs.Entity) (alive int, hpSum float32) {
	for _, e := range side {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			continue
		}
		alive++
		if hp := s.HPMap.Get(e); hp != nil {
			hpSum += hp.Current
		}
	}
	return alive, hpSum
}

func (s *aiTestState) updateVehCombat(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE COMBAT: %s  players=%d foes=%d\n",
			s.sceneID, len(s.vehEnts), len(s.vehFoes))
		fmt.Println("============================================================")
		s.orderFired = true
	}
	playersAlive, playersHP := s.sideStatus(s.vehEnts)
	foesAlive, foesHP := s.sideStatus(s.vehFoes)
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs players=%d hp=%.0f foes=%d hp=%.0f\n",
			s.sceneID, elapsed, playersAlive, playersHP, foesAlive, foesHP)
	}
	if !s.orderFired {
		return
	}
	if foesAlive == 0 || playersAlive == 0 || elapsed >= s.verdictAt {
		pass := foesAlive == 0 && playersAlive > 0
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (t=%.1fs players=%d/%d hp=%.0f foes=%d/%d)\n",
			s.sceneID, verdict, elapsed, playersAlive, len(s.vehEnts), playersHP,
			foesAlive, len(s.vehFoes))
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

func (s *aiTestState) updateVehReflex(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE REFLEX: %s  tank=side-on bmp=smoke+reverse truck=flee\n",
			s.sceneID)
		fmt.Println("============================================================")
		s.orderFired = true
	}
	// Pulse synthetic bullet impacts into each vehicle's DangerBuffer.
	if elapsed <= 12 && elapsed >= s.reflexNextInject {
		s.reflexNextInject = elapsed + 0.5
		for _, v := range s.reflexVehicles {
			if v == (ecs.Entity{}) || !s.World.Alive(v) {
				continue
			}
			if buf := s.DangerMap.Get(v); buf != nil {
				components.PushDanger(buf, components.DangerEvent{
					Kind:     components.DangerBulletImpact,
					Pos:      s.reflexSource,
					Strength: 0.35,
					Time:     elapsed,
				})
			}
		}
	}

	// Latch the BMP smoke observation while a field is live near its spawn.
	if !s.reflexBmpSmoked {
		q := s.SmokeFilter.Query()
		for q.Next() {
			_, sp := q.Get()
			d := sp.Sub(s.reflexBmpSpawn)
			if d.X*d.X+d.Z*d.Z < 20*20 {
				s.reflexBmpSmoked = true
			}
		}
		q.Close()
	}

	tankFaced := false
	if s.World.Alive(s.reflexTank) {
		if m := s.MotionMap.Get(s.reflexTank); m != nil {
			if tp := s.PosMap.Get(s.reflexTank); tp != nil {
				d := s.reflexSource.Sub(*tp)
				bearing := float32(math.Atan2(float64(d.X), float64(d.Z)))
				if absReflexAngle(reflexNormAngle(m.Yaw-bearing)) < 30*math.Pi/180 {
					tankFaced = true
				}
			}
		}
	}
	bmpRetreat := reflexDist(s.PosMap, s.reflexBmp, s.reflexBmpSpawn)
	bmpMoved := bmpRetreat >= 8
	truckDist := reflexDist(s.PosMap, s.reflexTruck, s.reflexSource)
	truckFled := truckDist >= 30

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs tankFaced=%v bmpBack=%.1fm smoked=%v truckDist=%.1fm\n",
			s.sceneID, elapsed, tankFaced, bmpRetreat, s.reflexBmpSmoked, truckDist)
	}

	done := tankFaced && bmpMoved && s.reflexBmpSmoked && truckFled
	if done || elapsed >= s.verdictAt {
		pass := tankFaced && bmpMoved && s.reflexBmpSmoked && truckFled
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (t=%.1fs tankFaced=%v bmpBack=%.1fm smoked=%v truckDist=%.1fm)\n",
			s.sceneID, verdict, elapsed, tankFaced, bmpRetreat, s.reflexBmpSmoked, truckDist)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// reflexDist returns the horizontal distance from `ent` to `to`.
func reflexDist(posMap *ecs.Map[components.WorldPos], ent ecs.Entity, to components.WorldPos) float32 {
	p := posMap.Get(ent)
	if p == nil {
		return 0
	}
	d := p.Sub(to)
	return float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
}

func reflexNormAngle(a float32) float32 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func absReflexAngle(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func (s *aiTestState) updateVehicles(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE SCENE: %s  vehicles=%d legs=%d\n",
			s.sceneID, len(s.vehEnts), len(s.vehWaypoints))
		fmt.Println("============================================================")
		for _, v := range s.vehEnts {
			if aq := s.VehQueueMap.Get(v); aq != nil {
				for _, wpt := range s.vehWaypoints {
					systems.PushAction(aq, components.Action{
						Kind: components.ActionMoveTo, Target: wpt,
					})
				}
			}
		}
		s.orderFired = true
		fmt.Printf("[ai-test %s] t=%.1fs WAYPOINTS PUSHED\n", s.sceneID, elapsed)
		return
	}
	if s.vehRequireBridge && s.vehOnBridge() {
		s.vehBridgeTicks++
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		for i, v := range s.vehEnts {
			if v == (ecs.Entity{}) || !s.World.Alive(v) {
				continue
			}
			pos := s.PosMap.Get(v)
			mot := s.MotionMap.Get(v)
			aq := s.VehQueueMap.Get(v)
			if pos == nil || mot == nil || aq == nil {
				continue
			}
			wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
			wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
			fmt.Printf("[ai-test %s] t=%.1fs veh%d=(%.1f,%.1f) speed=%.2f queue=%d\n",
				s.sceneID, elapsed, i, wx, wz, mot.Speed, aq.Count)
		}
	}
	arrived := s.vehArrivedCount()
	if arrived == len(s.vehEnts) || elapsed >= s.verdictAt {
		pass := arrived == len(s.vehEnts) && len(s.vehEnts) > 0 &&
			(!s.vehRequireBridge || s.vehBridgeTicks > 0)
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		if s.vehRequireBridge {
			fmt.Printf("== VERDICT [%s]: %s  (%d/%d vehicles arrived, bridge_ticks=%d)\n",
				s.sceneID, verdict, arrived, len(s.vehEnts), s.vehBridgeTicks)
		} else {
			fmt.Printf("== VERDICT [%s]: %s  (%d/%d vehicles arrived)\n",
				s.sceneID, verdict, arrived, len(s.vehEnts))
		}
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// vehOnBridge: true when any scene vehicle's RoadFollower sits on a
// RoadBridge edge this tick.
func (s *aiTestState) vehOnBridge() bool {
	g := s.vehGraphRes.Get()
	if g == nil {
		return false
	}
	for _, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		f := s.VehFollowerMap.Get(v)
		if f == nil || f.Edge < 0 || int(f.Edge) >= len(g.Edges) {
			continue
		}
		if g.Edges[f.Edge].Kind == components.RoadBridge {
			return true
		}
	}
	return false
}

func (s *aiTestState) vehArrivedCount() int {
	if len(s.vehWaypoints) == 0 {
		return 0
	}
	final := s.vehWaypoints[len(s.vehWaypoints)-1]
	fx := float32(final.Chunk.X)*components.ChunkSize + final.Local.X
	fz := float32(final.Chunk.Z)*components.ChunkSize + final.Local.Z
	n := 0
	for _, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		aq := s.VehQueueMap.Get(v)
		pos := s.PosMap.Get(v)
		if aq == nil || pos == nil || aq.Count != 0 {
			continue
		}
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		dx, dz := wx-fx, wz-fz
		if dx*dx+dz*dz < 36 {
			n++
		}
	}
	return n
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
