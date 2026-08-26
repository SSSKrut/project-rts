package main

// Automated AI test scenes. Run with `./bin/rts -scene=<id>`. Each scene
// spawns a minimal isolated world, auto-issues OccupyBuilding at t=aiOrderAt,
// samples insider count every aiSampleEvery, prints PASS/FAIL at aiVerdictAt.

import (
	"fmt"
	"math"
	"os"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/gen/buildings"
	"rts-go/systems"
)

type aiUnitSpawn = func(components.WorldPos) ecs.Entity

const (
	aiSceneDoorSouth         = "ai_door_south"
	aiSceneDoorNorth         = "ai_door_north"
	aiSceneDoorEast          = "ai_door_east"
	aiSceneDoorWest          = "ai_door_west"
	aiSceneCompoundSouth     = "ai_compound_south"
	aiSceneCompoundEast      = "ai_compound_east"
	aiSceneCompoundNorth     = "ai_compound_north"
	aiSceneCompoundWest      = "ai_compound_west"
	aiSceneCompoundMain      = "ai_compound_main"
	aiSceneCompoundPlusSouth = "ai_compound_plus_south"
	aiSceneCompoundPlusEast  = "ai_compound_plus_east"
	aiSceneCompoundPlusNorth = "ai_compound_plus_north"
	aiSceneCompoundPlusWest  = "ai_compound_plus_west"
	aiSceneOfficeFront       = "ai_office_front"
	// ai_office_l2 (ISSUES #20): the office is 3 storeys, so its stairs are
	// CASCADES — the only template that exercises them. Sending a squad to the
	// top storey is the integration half of the cascade-anchor fix: with the
	// old single-anchor flights the bake wired stairs to the exterior surface
	// and no path to L2 existed at all.
	aiSceneOfficeL2 = "ai_office_l2"
	// ai_office_rooms: OccupyBuilding on the 3-storey office must land every
	// room of every storey (Level.Rooms round-robin split): 8 members over
	// 3 floors x 2 rooms.
	aiSceneOfficeRooms = "ai_office_rooms"
	// ai_house2_north: 2-storey house, south door, squad approaches from the
	// NW so its path hugs the west wall — right through the strip where the
	// padded stair footprint pokes outside. Verdict adds max lift above the
	// surface while OUTSIDE the footprint (owner playtest: an MG rode the
	// invisible outer ramp up the wall face).
	aiSceneHouse2North = "ai_house2_north"
	// ai_garrison_windows: Garrison on the office must MAN THE WINDOWS —
	// every planned window slot (BuildingSlotPlanner SlotWindows) holds a
	// member facing the opening; overflow waits inside as reserve.
	aiSceneGarrisonWin = "ai_garrison_windows"
	aiSceneFarBuilding = "ai_far_building"

	// ai_main_* run on the REAL main-map world data (mainWorldBuildings +
	// roads + trenches) and send the squad into one specific section / storey
	// of the multi-wing compound at (-60, 10) — the building with the worst
	// pathing history.
	aiSceneMainM0 = "ai_main_m0" // main wing, ground floor
	aiSceneMainM1 = "ai_main_m1" // main wing, second storey (via stairs)
	// Live-map "Occupy L1" probes on the two multi-storey buildings the
	// ai_main_*/office scenes do not cover: the 2-storey house at (40, 30)
	// and the toroidal courtyard at (-20, -60).
	aiSceneHouseL1 = "ai_house_l1"
	aiSceneCourtL1 = "ai_courtyard_l1"
	// Live-map building-popup probes on the same house: "Attacking position
	// at windows" (Garrison) and "Hidden position" (OccupyBuilding +
	// Stealth preset + HoldFire override — everyone inside, crouched, and
	// nobody silhouetted at a window slot).
	aiSceneGarrisonHouse = "ai_garrison_house"
	aiSceneHiddenHouse   = "ai_hidden_house"
	aiSceneMainE         = "ai_main_e" // east wing
	aiSceneMainN         = "ai_main_n" // north wing

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

	// ai_march_column (MA1): a Column squad of 8 marches three chained legs
	// with two 90° turns. Verdict = arrival + churn-replan budget + no
	// follower collapse onto one waypoint (never 3+ men bunched in a 1.2 m
	// circle past the alignment window).
	aiSceneMarchColumn = "ai_march_column"

	// ai_vehicle_forest (MB1): truck + tank drive parallel lanes through a
	// planted pine grove with rocks. The truck must round the grove (0 ticks
	// with its centre inside any prop), the tank crushes through at ×0.4
	// (>= 1 pine flattened) but never enters a rock.
	aiSceneVehForest = "ai_vehicle_forest"
	// ai_vehicle_slope (MB1): a tank ordered through a stamped 45°+ ridge
	// must refuse the grade (0 ticks on slope >= 0.6) and detour around the
	// open flank, arriving <= 60 s.
	aiSceneVehSlope = "ai_vehicle_slope"

	// ai_crowd_cross (MA3): a Line squad's straight MoveTo runs through a
	// standing crowd of 12. Verdict = arrival + max pairwise penetration
	// <= 0.05 m (movers fully avoid standing bodies, Resp=1) + bounded
	// per-tick |dv| (no rescale jerks).
	aiSceneCrowdCross = "ai_crowd_cross"

	// ai_wall_glide (MA2): a Column squad's straight MoveTo crosses the union
	// footprint of two flush 20×10 houses — the route must round the 40 m
	// south facade — then a queued OccupyBuilding sends everyone through the
	// east house's south door. Verdict = facade clearance ≥ 0.25 m (leg 1),
	// escape-spring < 5% of steering ticks, all inside, ≤ 2 entries per man
	// (door-jamb oscillation re-crosses the wall plane).
	aiSceneWallGlide = "ai_wall_glide"

	// ai_cover_side (ISSUES #18): a 2-man squad stands BETWEEN a lone oak
	// and a synthetic threat pulsing from the north (DangerBuffer injection,
	// no bullets). The scramble must relocate both men to slots on the far
	// (south) side of the trunk. PASS = both assigned slots south of the
	// tree AND both units parked on them.
	aiSceneCoverSide = "ai_cover_side"

	// ai_shellfire (MC1): a squad holds DefendPosition on open ground while
	// synthetic shells walk across it (BlastMark entities + DangerExplosion
	// pulses, no weapon involved). The barrage must raise an UnsafeArea, the
	// roster must vacate it, the ORDER must stay InProgress throughout (P1 —
	// autonomy changes "how", never "what"), and once the shelling stops the
	// squad must return to the position it was told to hold.
	aiSceneShellfire = "ai_shellfire"

	// MC3 — squad brain: bounding overwatch, ClearBuilding sequencing, focus
	// fire on the named enemy.
	aiSceneBounding  = "ai_bounding"
	aiSceneClearBld  = "ai_clear_building"
	aiSceneFocusFire = "ai_focus_fire"
	// Mass scene: 100 combatants, the only one past core.SerialThresholdHint.
	aiSceneMass = "ai_mass"

	// MC2 position-scoring family (P4). One scorer ranks slots, trench cells,
	// terrain defilade and hull shadow against every live threat bearing;
	// each scene isolates one candidate kind, and _none proves the mandatory
	// fallback (nothing worth taking → get out of the fire lane).
	aiSceneCoverTrench   = "ai_cover_trench"
	aiSceneCoverHull     = "ai_cover_hull"
	aiSceneCoverDefilade = "ai_cover_defilade"
	aiSceneCoverNone     = "ai_cover_none"

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

	// ai_vehicle_* M6 collision scenes. Per-vehicle goals; the verdict
	// tracks per-tick collision metrics on top of arrival.
	// _avoid: two BTRs swap positions head-on — min pairwise distance must
	// stay above the collider sum (ID priority + tangent steer).
	aiSceneVehAvoid = "ai_vehicle_avoid"
	// _building: a truck ordered to a point 5 m BEHIND a house must loop
	// around — the detour must arm even with the goal right past the far wall
	// (MB2 P8-c) — with zero ticks inside the footprint.
	aiSceneVehBuilding = "ai_vehicle_building"
	// _stuck (MB2): two houses whose inflated bands seal a 6 m slit. Truck A
	// is ordered INTO the slit centre — unreachable, the progress watchdog
	// must fail it honestly (reason in the EventLog) within 20 s. Truck B is
	// ordered past the pair — the committed-side detour frees it around, with
	// the corner pick not flickering (AvoidSide sign alternations bounded).
	aiSceneVehStuck = "ai_vehicle_stuck"
	// _yield: a BTR drives through a standing rifle line — corridor sidestep
	// + shove keep every unit outside the hull radius (P5: infantry yields).
	aiSceneVehYield = "ai_vehicle_yield"
	// _group: three trucks share one goal — regression for the owner-reported
	// spontaneous three-point turn (zero reverse ticks allowed after spin-up)
	// and adjacent parking around the occupied point.
	aiSceneVehGroup = "ai_vehicle_group"
	// _convoy (M7): BMP leader (11 m/s) + two trucks (6 m/s) merged into one
	// squad march 110 m — squadPaceCap must hold the column together
	// (owner repro 2026-07-31: fast members «укатывают вперёд»). PASS = all
	// parked at the goal AND max member spread over the run stays bounded.
	aiSceneVehConvoy = "ai_vehicle_convoy"
	// _flee (MB3): a squadded unarmed truck takes fire it cannot answer while
	// the squad marches. FormationSystem must yield the hull to the reflex
	// (before P8-f the slot push overwrote Flee within 100 ms and it never
	// left), and the player's order must survive the retreat and resume.
	aiSceneVehFlee = "ai_vehicle_flee"
	// _convoy_road (MB3): the same three-hull column as _convoy, but on
	// `valley` between two points a highway connects. The squad macro path
	// must route over the RoadGraph and the members must actually ride at
	// road speed — offroad the river cut is impassable, so the road is the
	// only way east.
	aiSceneVehConvoyRoad = "ai_vehicle_convoy_road"

	// _air_transit (Phase 20 M0): a gunship arrives on schedule, flies four
	// legs across `hills` at a nap-of-the-earth AGL dial, then is sent home
	// and clears the map. PASS demands the release fired, every leg was
	// consumed, the airframe tracked the terrain within tolerance, never got
	// closer to it than the clearance floor, and actually despawned.
	aiSceneAirTransit = "ai_air_transit"

	// _air_recon (Phase 20 M1): a silent scout flies past an enemy hull toward
	// an enemy gunship whose radar is running. PASS demands the passive
	// intercept fired far beyond any imaging sensor and as a bearing, that it
	// died when the emitter shut down, that the scout's own radar bought
	// acquisition past optical range, and that the hull HEARD the scout long
	// before it could see one.
	aiSceneAirRecon = "ai_air_recon"

	// _solo_orders (Phase 20.7 L0): a lone vehicle takes a two-leg order
	// chain. PASS demands it became an order-holder WITHOUT becoming a squad,
	// that the order reached its queue in the issuing tick, that both legs
	// completed, that the history ring kept them under the vehicle, and that
	// joining a squad stripped the solo command.
	aiSceneSoloOrders = "ai_solo_orders"

	// _air_aa_gun (Phase 20 M2): a player gunship transits over an enemy AA
	// squad whose FireOnAir starts OFF. PASS demands radar-range acquisition
	// (past anything optics could do), zero rounds while the gate is down,
	// fire once it flips, real damage landing, and near-miss danger reaching
	// the airframe's own Threat.
	aiSceneAirAAGun = "ai_air_aa_gun"

	// _air_manpads (Phase 20 M2): a MANPADS gunner against a transiting
	// gunship. PASS demands a Missile entity launched, the flare override
	// armed while the round was still flying, an honest resolution (decoyed
	// or damage landed), and the abort rule holding at the HP floor.
	aiSceneAirManpads = "ai_air_manpads"

	// _air_cas (Phase 20 M3): a weapons-tight gunship is NAMED a hull to kill.
	// PASS demands radar-range acquisition, a launch from past anything a
	// hitscan weapon could reach, an approach that stops instead of overflying,
	// borrowed altitude that comes back, and a dead hull.
	aiSceneAirCAS = "ai_air_cas"

	// _comms (lite block A M0): a squad marches away from its relay and the
	// radio quality follows it down through every band; killing the radioman
	// halves the strength from the same spot.
	aiSceneComms = "lite_comms_range"

	// _comms_orders (lite block A M1): the delivery gate, one rung per band,
	// plus a plan made while cut off that arrives by itself when the net comes
	// back. See test_scene_comms.go.
	aiSceneCommsOrders = "lite_comms_orders"

	// _balance_* (lite, 2026-08-26): the balance statement is the PAIR of the
	// first two — armour breaks a squad caught in the open, the same squad
	// lying in wait breaks armour. _eyes fires no shot and asserts the ORDER
	// of four first-contact ranges. See test_scene_balance.go.
	aiSceneBalOpen   = "lite_balance_open"
	aiSceneBalAmbush = "lite_balance_ambush"
	aiSceneBalEyes   = "lite_balance_eyes"
)

// aiSceneMapName lets a scene demand a specific map manifest ("" = default).
func aiSceneMapName() string {
	switch aiSceneID() {
	case aiSceneMarchSlope:
		return "hills"
	case aiSceneVehRoad, aiSceneVehConvoyRoad:
		return "valley"
	case aiSceneAirTransit:
		// Terrain following is the point: on flat ground an AGL dial and an
		// AMSL one are the same run.
		return "hills"
	case aiSceneAirRecon:
		// Flat: the claims are about ranges, and a ridge that masks the hull
		// turns a measurement into a coin toss.
		return "flat"
	case aiSceneSoloOrders:
		// Flat: the claims are about the order lifecycle. A hill that stalls
		// the hull would fail the scene for a reason it does not test.
		return "flat"
	case aiSceneAirAAGun, aiSceneAirManpads:
		// Flat: the claims are about ranges and gates; a masking ridge would
		// turn either into a coin toss.
		return "flat"
	case aiSceneAirCAS:
		// Relief IS a claim here: without a ridge in the way there is nothing
		// for the pop-up to unmask from.
		return "hills"
	case aiSceneComms:
		// The claims are distances; a hill that slows the march only stretches
		// the clock and tells us nothing.
		return "flat"
	case aiSceneCommsOrders, aiSceneBalOpen, aiSceneBalAmbush, aiSceneBalEyes:
		// Balance is a number, and terrain is the loudest way to hide one:
		// a ridge that masks half the squad turns a measurement into a story.
		return "flat"
	case aiSceneCoverNone:
		return "flat"
	case aiSceneCoverDefilade:
		// Natural relief: a player-stamped ridge would never reach the cover
		// bake (CoverBaked is one-shot per chunk), so the scene must test the
		// DirMask consumer on terrain the bake actually saw.
		return "hills"
	}
	return ""
}

// aiMainSpec selects the target wing (by expected footprint centre) and the
// storey index for one ai_main_* scene.
type aiMainSpec struct {
	wingX, wingZ float32
	levelIdx     int
}

// aiRoomBand is one room rect + its storey floor Y (ai_office_rooms verdict).
type aiRoomBand struct {
	room components.AABB2D
	minY float32
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
	case aiSceneHouseL1:
		return aiMainSpec{wingX: 40, wingZ: 30, levelIdx: 1}, true
	case aiSceneCourtL1:
		return aiMainSpec{wingX: -20, wingZ: -60, levelIdx: 1}, true
	// levelIdx -1 = a building order (Garrison / OccupyBuilding), not a
	// storey MoveTo; the spec only picks the target building.
	case aiSceneGarrisonHouse, aiSceneHiddenHouse:
		return aiMainSpec{wingX: 40, wingZ: 30, levelIdx: -1}, true
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
	// "lite_" is the pivot's namespace; it goes through the same harness —
	// headless run, pristine SaveDir, verdict line, replay hash.
	return strings.HasPrefix(v, "ai_") || strings.HasPrefix(v, "lite_")
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
	case aiSceneOfficeFront, aiSceneOfficeRooms, aiSceneGarrisonWin:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 22})
	case aiSceneHouse2North:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 40})
	case aiSceneOfficeL2:
		return components.WorldPos{}.Add(rl.Vector3{X: 66, Z: -29})
	case aiSceneHouseL1, aiSceneGarrisonHouse, aiSceneHiddenHouse:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 18})
	case aiSceneCourtL1:
		return components.WorldPos{}.Add(rl.Vector3{X: -20, Z: -78})
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
	case aiSceneMarchColumn:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -5})
	case aiSceneWallGlide:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 25})
	case aiSceneCrowdCross:
		return components.WorldPos{}.Add(rl.Vector3{X: 45, Z: 0})
	case aiSceneVehForest:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -50})
	case aiSceneVehSlope:
		return components.WorldPos{}.Add(rl.Vector3{X: 45, Z: 0})
	case aiSceneCoverSide:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -30})
	case aiSceneShellfire:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 40})
	case aiSceneBounding:
		return components.WorldPos{}.Add(rl.Vector3{X: 60, Z: 20})
	case aiSceneClearBld:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 22})
	case aiSceneFocusFire:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 16})
	case aiSceneMass:
		return components.WorldPos{}.Add(rl.Vector3{X: 45, Z: 10})
	case aiSceneCoverTrench:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 38})
	case aiSceneCoverHull:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -4})
	case aiSceneCoverDefilade:
		return components.WorldPos{}.Add(rl.Vector3{X: 56, Z: 0})
	case aiSceneCoverNone:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 6})
	case aiSceneVehCombat:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -30})
	case aiSceneVehAvoid:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: -20})
	case aiSceneVehBuilding:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 10})
	case aiSceneVehStuck:
		return components.WorldPos{}.Add(rl.Vector3{X: 30, Z: 0})
	case aiSceneVehYield:
		return components.WorldPos{}.Add(rl.Vector3{X: 35, Z: 5})
	case aiSceneVehGroup:
		return components.WorldPos{}.Add(rl.Vector3{X: 50, Z: -18})
	case aiSceneVehConvoy:
		return components.WorldPos{}.Add(rl.Vector3{X: 70, Z: -24})
	case aiSceneVehFlee:
		return components.WorldPos{}.Add(rl.Vector3{X: 45, Z: -20})
	case aiSceneVehConvoyRoad:
		return components.WorldPos{}.Add(rl.Vector3{X: 10, Z: -12})
	case aiSceneAirTransit, aiSceneAirRecon, aiSceneSoloOrders, aiSceneAirAAGun,
		aiSceneAirManpads, aiSceneAirCAS, aiSceneComms:
		return components.WorldPos{}.Add(rl.Vector3{X: 0, Z: 0})
	}
	return components.WorldPos{}
}

// aiSceneTrenches: the defilade scene digs its own line under the enemy;
// other ai scenes keep the main-map trench (far away from all of them).
func aiSceneTrenches() []components.Trench {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	if aiSceneID() == aiSceneCoverTrench {
		return []components.Trench{{
			Points: []components.WorldPos{wp(20, 44), wp(60, 44)},
			Width:  2.0,
			Depth:  1.5,
		}}
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
		aiSceneMarchLine, aiSceneMarchSlope, aiSceneMarchColumn,
		aiSceneCrowdCross, aiSceneVehForest, aiSceneVehSlope, aiSceneShellfire,
		aiSceneCoverTrench, aiSceneCoverHull, aiSceneCoverDefilade, aiSceneCoverNone,
		aiSceneBounding, aiSceneFocusFire, aiSceneMass:
		return nil
	case aiSceneClearBld:
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
		pos.Local.Y = systems.GroundHeight(32, 32)
		plan := buildings.GenerateHouse(0xC3, buildings.HouseParams{
			Stories:   2,
			SizeX:     14,
			SizeZ:     10,
			DoorSides: []uint8{0},
			Interior:  true,
		}, pos, components.BuildingHouse)
		return []components.BuildingPlan{*plan}
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
	case aiSceneOfficeFront, aiSceneOfficeRooms, aiSceneGarrisonWin:
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		return []components.BuildingPlan{*buildings.GenerateOffice(0xE5, pos)}
	case aiSceneHouse2North:
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 32})
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		plan := buildings.GenerateHouse(0xB2, buildings.HouseParams{
			Stories:  2,
			SizeX:    8,
			SizeZ:    8,
			DoorSide: 0,
		}, pos, components.BuildingHouse)
		return []components.BuildingPlan{*plan}
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
	case aiSceneVehBuilding:
		// Dead centre on the truck's straight line to its goal.
		pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 0})
		pos.Local.Y = systems.GroundHeight(32, 0)
		plan := buildings.GenerateHouse(0xC7, buildings.HouseParams{
			Stories:  1,
			SizeX:    10,
			SizeZ:    10,
			DoorSide: 0,
		}, pos, components.BuildingHouse)
		return []components.BuildingPlan{*plan}
	case aiSceneVehStuck:
		// Footprints X[27..37], Z[-13..-3] and Z[3..13]: the 6 m slit between
		// them is narrower than a truck's doubled steering inflate — sealed.
		mk := func(seed uint64, cz float32, doorSide uint8) components.BuildingPlan {
			pos := components.WorldPos{}.Add(rl.Vector3{X: 32, Z: cz})
			pos.Local.Y = systems.GroundHeight(32, cz)
			return *buildings.GenerateHouse(seed, buildings.HouseParams{
				Stories:  1,
				SizeX:    10,
				SizeZ:    10,
				DoorSide: doorSide,
			}, pos, components.BuildingHouse)
		}
		return []components.BuildingPlan{mk(0xE1, -8, 0), mk(0xE2, 8, 2)}
	case aiSceneWallGlide:
		// Two flush houses form one continuous 40 m south facade; the west
		// house's door faces north so the facade has exactly one opening.
		mk := func(seed uint64, cx float32, doorSide uint8) components.BuildingPlan {
			pos := components.WorldPos{}.Add(rl.Vector3{X: cx, Z: 32})
			pos.Local.Y = systems.GroundHeight(cx, 32)
			return *buildings.GenerateHouse(seed, buildings.HouseParams{
				Stories:  1,
				SizeX:    20,
				SizeZ:    10,
				DoorSide: doorSide,
			}, pos, components.BuildingHouse)
		}
		return []components.BuildingPlan{mk(0xD1, 22, 2), mk(0xD2, 42, 0)}
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
	case aiSceneOfficeFront, aiSceneOfficeRooms, aiSceneGarrisonWin:
		return components.WorldPos{}.Add(rl.Vector3{X: 32, Z: 18})
	case aiSceneHouse2North:
		return components.WorldPos{}.Add(rl.Vector3{X: 27, Z: 44})
	case aiSceneOfficeL2:
		return components.WorldPos{}.Add(rl.Vector3{X: 70, Z: -33})
	case aiSceneHouseL1, aiSceneGarrisonHouse, aiSceneHiddenHouse:
		return components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 12})
	case aiSceneCourtL1:
		return components.WorldPos{}.Add(rl.Vector3{X: -20, Z: -82})
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
	case aiSceneMarchColumn:
		return components.WorldPos{}.Add(rl.Vector3{X: 20, Z: -25})
	case aiSceneWallGlide:
		return components.WorldPos{}.Add(rl.Vector3{X: 8, Z: 30})
	case aiSceneCrowdCross:
		return components.WorldPos{}.Add(rl.Vector3{X: 28, Z: 0})
	}
	return components.WorldPos{}
}

// aiMarchColumnSpawn (MA1): one MotorRifle squad in Column, three chained
// legs with two 90° turns on open default-map terrain.
func aiMarchColumnSpawn(
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
		components.FormationColumn, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	goal := func(wx, wz float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: wx, Z: wz})
		p.Local.Y = systems.GroundHeight(wx, wz)
		return p
	}
	return &aiTestState{
		sceneID:      aiSceneID(),
		squad:        squad,
		columnActive: true,
		columnGoals: []components.WorldPos{
			goal(60, -25), goal(60, 15), goal(20, 15),
		},
		// Turn responses legitimately churn (target sweeps with the Forward
		// slew); the budget catches the storm class (43+/u/min pre-P7a).
		// 18, not 15: the 2026-08-26 roster (AT gunner + radioman, both on the
		// small stamina tank) stretches the column and reads 15.1 — the same
		// class of churn, with the margin the budget is supposed to have.
		marchReplanMax: 18,
		World:          world,
		SquadService:   squadService,
		PosMap:         posMap,
		RosterMap:      rosterMap,
		MicroPathMap:   ecs.NewMap[components.MicroPath](world),
		AQMap:          ecs.NewMap[components.ActionQueue](world),
		orderAt:        aiOrderAt,
		verdictAt:      90,
	}
}

// aiWallGlideSpawn (MA2): Column squad; leg 1 straight through the flush
// houses' union footprint (forces the 40 m facade round), leg 2 queued
// OccupyBuilding on the east house.
func aiWallGlideSpawn(
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
		components.FormationColumn, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	var east ecs.Entity
	var eastFP, union components.AABB2D
	first := true
	bf := ecs.NewFilter1[components.Building](world)
	q := bf.Query()
	for q.Next() {
		b := q.Get()
		fp := b.Footprint
		if first {
			union = fp
			first = false
		} else {
			if fp.MinX < union.MinX {
				union.MinX = fp.MinX
			}
			if fp.MaxX > union.MaxX {
				union.MaxX = fp.MaxX
			}
			if fp.MinZ < union.MinZ {
				union.MinZ = fp.MinZ
			}
			if fp.MaxZ > union.MaxZ {
				union.MaxZ = fp.MaxZ
			}
		}
		if fp.CenterX() > 32 {
			east = q.Entity()
			eastFP = fp
		}
	}
	q.Close()
	if east == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] NO EAST HOUSE — aborting\n", aiSceneID())
		return nil
	}
	return &aiTestState{
		sceneID:         aiSceneID(),
		squad:           squad,
		glideActive:     true,
		targetBuilding:  east,
		targetFootprint: eastFP,
		glideUnion:      union,
		glideMinClear:   1e9,
		glideInside:     map[ecs.Entity]bool{},
		glideEnter:      map[ecs.Entity]int{},
		World:           world,
		SquadService:    squadService,
		PosMap:          posMap,
		RosterMap:       rosterMap,
		MicroPathMap:    ecs.NewMap[components.MicroPath](world),
		AQMap:           ecs.NewMap[components.ActionQueue](world),
		OQMap:           ecs.NewMap[components.OrderQueueHead](world),
		orderAt:         aiOrderAt,
		verdictAt:       90,
		nextSampleAt:    aiOrderAt + 5,
	}
}

// aiScenePropRec — one hand-planted prop for MB1 metrics.
type aiScenePropRec struct {
	x, z, r float32
	rock    bool
	ent     ecs.Entity
}

// aiPlantProp spawns a prop the same way PropSpawnSystem does (WorldPos +
// Prop + LODRelevant + PropChunkIndex registration, so nav bake and chunk
// evict both see it).
func aiPlantProp(world *ecs.World, posMap *ecs.Map[components.WorldPos],
	propMap *ecs.Map[components.Prop], lodMap *ecs.Map[components.LODRelevant],
	propIdx *systems.PropChunkIndex, registry *components.PropTypeRegistry,
	t components.PropType, x, z float32) aiScenePropRec {
	e := world.NewEntity()
	p := components.WorldPos{}.Add(rl.Vector3{X: x, Y: systems.GroundHeight(x, z), Z: z})
	posMap.Add(e, &p)
	propMap.Add(e, &components.Prop{Type: t, Scale: 1})
	lodMap.Add(e, &components.LODRelevant{})
	if propIdx != nil {
		propIdx.Loaded[p.Chunk] = append(propIdx.Loaded[p.Chunk], e)
	}
	r := float32(1)
	if registry != nil {
		r = registry.Metas[t].BBoxRadius
	}
	return aiScenePropRec{x: x, z: z, r: r, rock: t == components.PropRock, ent: e}
}

// aiVehicleForestSpawn (MB1): pine grove across both lanes + rocks on the
// tank's lane.
func aiVehicleForestSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
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
	truck := vehicleFactory.Spawn(wp(18, -44), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	tank := vehicleFactory.Spawn(wp(18, -56), components.VehicleTank,
		components.FactionPlayer, components.ControllerLocal)
	propMap := ecs.NewMap[components.Prop](world)
	lodMap := ecs.NewMap[components.LODRelevant](world)
	propIdxRes := ecs.NewResource[systems.PropChunkIndex](world)
	propIdx := propIdxRes.Get()
	registryRes := ecs.NewResource[components.PropTypeRegistry](world)
	registry := registryRes.Get()
	var props []aiScenePropRec
	for x := float32(30); x <= 50; x += 4 {
		for z := float32(-58); z <= -42; z += 4 {
			props = append(props, aiPlantProp(world, posMap, propMap, lodMap,
				propIdx, registry, components.PropPine, x, z))
		}
	}
	props = append(props, aiPlantProp(world, posMap, propMap, lodMap,
		propIdx, registry, components.PropRock, 36, -56))
	props = append(props, aiPlantProp(world, posMap, propMap, lodMap,
		propIdx, registry, components.PropRock, 44, -54))
	return &aiTestState{
		sceneID:      aiSceneID(),
		forestActive: true,
		forestTruck:  truck,
		forestTank:   tank,
		forestGoalTr: wp(62, -44),
		forestGoalTk: wp(62, -56),
		forestProps:  props,
		World:        world,
		PosMap:       posMap,
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		orderAt:      aiOrderAt,
		verdictAt:    90,
		nextSampleAt: aiOrderAt + 5,
	}
}

// aiVehicleSlopeSpawn (MB1): tank vs a stamped ridge with an open flank.
func aiVehicleSlopeSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
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
	tank := vehicleFactory.Spawn(wp(25, 0), components.VehicleTank,
		components.FactionPlayer, components.ControllerLocal)
	return &aiTestState{
		sceneID:      aiSceneID(),
		slopeActive:  true,
		slopeTank:    tank,
		slopeGoal:    wp(62, 0),
		Stamper:      systems.NewStamper(world),
		World:        world,
		PosMap:       posMap,
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		sampler:      systems.NewHeightSampler(world),
		orderAt:      2.5,
		verdictAt:    60,
		nextSampleAt: aiOrderAt + 5,
	}
}

// aiCrowdCrossSpawn (MA3): Line squad marches straight through a standing
// 4×3 crowd; the crowd never gets orders.
func aiCrowdCrossSpawn(
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
	var crowd []ecs.Entity
	for i := 0; i < 4; i++ {
		for j := 0; j < 3; j++ {
			wx := 42.8 + float32(i)*1.8
			wz := -1.8 + float32(j)*1.8
			p := components.WorldPos{}.Add(rl.Vector3{X: wx, Z: wz})
			p.Local.Y = systems.GroundHeight(wx, wz)
			crowd = append(crowd, unitFactory(p))
		}
	}
	goal := components.WorldPos{}.Add(rl.Vector3{X: 62, Z: 0})
	goal.Local.Y = systems.GroundHeight(62, 0)
	return &aiTestState{
		sceneID:      aiSceneID(),
		squad:        squad,
		crowdActive:  true,
		crowdEnts:    crowd,
		crowdGoal:    goal,
		crowdPrev:    map[ecs.Entity][2]float32{},
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		MicroPathMap: ecs.NewMap[components.MicroPath](world),
		orderAt:      aiOrderAt,
		verdictAt:    60,
		nextSampleAt: aiOrderAt + 5,
	}
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
	replanMax := float32(2)
	clearFloor := float32(-0.3)
	if aiSceneID() == aiSceneMarchSlope {
		// Threshold applies to the straight march only (plan MA1); the
		// hills route legitimately churns on terrain detours — report, no gate.
		replanMax = 0
		clearFloor = -0.35
	}
	return &aiTestState{
		sceneID:         aiSceneID(),
		squad:           squad,
		marchActive:     true,
		marchGoal:       goal,
		marchChurnMax:   churnMax,
		marchReplanMax:  replanMax,
		marchClearFloor: clearFloor,
		soloEnts:        []ecs.Entity{solo1, solo2},
		AQMap:           ecs.NewMap[components.ActionQueue](world),
		World:           world,
		SquadService:    squadService,
		PosMap:          posMap,
		RosterMap:       rosterMap,
		MotionMap:       ecs.NewMap[components.Motion](world),
		FdMap:           ecs.NewMap[components.FormationData](world),
		MicroPathMap:    ecs.NewMap[components.MicroPath](world),
		sampler:         systems.NewHeightSampler(world),
		roadSurface:     ecs.NewResource[components.RoadSurface](world),
		marchMinClear:   1e9,
		marchMaxClear:   -1e9,
		orderAt:         aiOrderAt,
		verdictAt:       120,
		nextSampleAt:    aiOrderAt + 5,
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
		sceneID: aiSceneID(),
		vehEnts: []ecs.Entity{truck, tank},
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

// aiVehicleAvoidSpawn covers the four M6 collision scenes; the layout
// switches on the scene ID, the metrics + verdict live in updateVehAvoid.
func aiVehicleAvoidSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
	unitFactory aiUnitSpawn, posMap *ecs.Map[components.WorldPos]) *aiTestState {
	if vehicleFactory == nil {
		fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	s := &aiTestState{
		sceneID:      aiSceneID(),
		avoidActive:  true,
		avoidMinPair: 1e9,
		World:        world,
		PosMap:       posMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		orderAt:      aiOrderAt,
		verdictAt:    75,
		nextSampleAt: aiOrderAt + 3,
	}
	spawn := func(x, z float32, kind components.VehicleKind, gx, gz float32) {
		v := vehicleFactory.Spawn(wp(x, z), kind,
			components.FactionPlayer, components.ControllerLocal)
		s.vehEnts = append(s.vehEnts, v)
		s.avoidGoals = append(s.avoidGoals, wp(gx, gz))
		s.avoidRadii = append(s.avoidRadii, components.SpecForVehicle(kind).ColliderR)
	}
	switch aiSceneID() {
	case aiSceneVehAvoid:
		// Head-on swap on one lane: A (lower ID) holds course, B yields.
		spawn(10, -20, components.VehicleBTR, 70, -20)
		spawn(70, -20, components.VehicleBTR, 10, -20)
	case aiSceneVehBuilding:
		// The 10×10 house at (32, 0) sits dead centre on the straight line;
		// the goal is 5 m past the far wall (MB2) — the detour must still arm.
		spawn(4, 0, components.VehicleTruck, 42, 0)
		s.avoidFoots = append(s.avoidFoots,
			components.AABB2D{MinX: 27, MinZ: -5, MaxX: 37, MaxZ: 5})
	case aiSceneVehYield:
		// Standing rifle line across the BTR's path at x=35.
		spawn(5, 12, components.VehicleBTR, 65, 12)
		for i := 0; i < 6; i++ {
			u := unitFactory(wp(35, 12+(-3.0+1.2*float32(i))))
			s.avoidYield = append(s.avoidYield, u)
		}
	case aiSceneVehGroup:
		// Three trucks, one shared goal: regression for the spontaneous
		// three-point turn; reversals allowed only during the first 2 s.
		spawn(15, -28, components.VehicleTruck, 85, -18)
		spawn(15, -18, components.VehicleTruck, 85, -18)
		spawn(15, -8, components.VehicleTruck, 85, -18)
		s.avoidRevFree = aiOrderAt + 2
	}
	return s
}

// aiCoverPosSpawn (MC2): the four position-scoring scenes. Each drops a small
// squad under synthetic fire from one bearing with exactly one good answer
// nearby — a trench, a parked hull, a ridge, or nothing at all.
func aiCoverPosSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory,
	unitFactory aiUnitSpawn,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	st := &aiTestState{
		sceneID:      aiSceneID(),
		covActive:    true,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		OverrideMap:  ecs.NewMap[components.TacticalOverride](world),
		DangerMap:    ecs.NewMap[components.DangerBuffer](world),
		StanceMap:    ecs.NewMap[components.Stance](world),
		SlotFilter:   ecs.NewFilter2[components.WorldPos, components.CoverSlot](world),
		sampler:      systems.NewHeightSampler(world),
		orderAt:      aiOrderAt,
		verdictAt:    60,
		nextSampleAt: aiOrderAt + 3,
	}
	var spawnPts [2]components.WorldPos
	switch aiSceneID() {
	case aiSceneCoverTrench:
		// Trench 10 m north, a lone oak 24 m south, fire from the east: both
		// answers are lateral, so only quality decides.
		spawnPts = [2]components.WorldPos{wp(38, 34), wp(42, 34)}
		st.covShooter = wp(80, 34)
		st.covRefZ = 44
		propMap := ecs.NewMap[components.Prop](world)
		lodMap := ecs.NewMap[components.LODRelevant](world)
		propIdxRes := ecs.NewResource[systems.PropChunkIndex](world)
		registryRes := ecs.NewResource[components.PropTypeRegistry](world)
		aiPlantProp(world, posMap, propMap, lodMap, propIdxRes.Get(),
			registryRes.Get(), components.PropOak, 40, 10)
	case aiSceneCoverHull:
		// A parked friendly BTR between the men and the shooter.
		spawnPts = [2]components.WorldPos{wp(38, 2), wp(42, 2)}
		st.covShooter = wp(40, 22)
		st.covRefX, st.covRefZ = 40, -6
		if vehicleFactory != nil {
			vehicleFactory.Spawn(wp(40, -6), components.VehicleBTR,
				components.FactionPlayer, components.ControllerLocal)
		}
	case aiSceneCoverDefilade:
		// Open ground on `hills` with a shooter across the valley: the answer
		// is the reverse slope, and the verdict tests the sightline itself
		// rather than a hard-coded crest.
		spawnPts = [2]components.WorldPos{wp(38, -2), wp(42, 2)}
		st.covShooter = wp(40, 60)
	case aiSceneCoverNone:
		// Flat map, no cover of any kind: the fallback must fire.
		spawnPts = [2]components.WorldPos{wp(-2, 0), wp(2, 0)}
		st.covShooter = wp(0, 40)
	}
	u1 := unitFactory(spawnPts[0])
	u2 := unitFactory(spawnPts[1])
	st.covUnits = []ecs.Entity{u1, u2}
	st.covStart = spawnPts[0]
	squad := squadService.CreateFromUnits([]ecs.Entity{u1, u2}, components.FormationLine)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO FORM SQUAD — aborting\n", aiSceneID())
		return nil
	}
	st.squad = squad
	st.covStopAt = aiOrderAt + 20
	return st
}

// aiShellfireSpawn (MC1): a MotorRifle squad holds a position on open ground;
// updateShellfire walks synthetic shells across it.
func aiShellfireSpawn(
	world *ecs.World,
	squadService *systems.SquadService,
	roleService *systems.RoleService,
	unitFactory aiUnitSpawn,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
) *aiTestState {
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	squad := squadService.CreateFromTemplate(
		systems.TmplMotorRifle, wp(40, 40),
		components.FormationLine, playerFaction,
		components.Controller{Owner: components.ControllerLocal},
		roleService, unitFactory,
	)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO SPAWN SQUAD — aborting\n", aiSceneID())
		return nil
	}
	return &aiTestState{
		sceneID:     aiSceneID(),
		shellActive: true,
		shellSquad:  squad,
		squad:       squad,
		shellHold:   wp(40, 40),
		shellCentre: wp(40, 40),
		// Every scene-driven mutation must land BEFORE the saveload gate's
		// save tick (1000 = 16.7 s): the loaded run has no harness, so a shell
		// after that point can never replay (SAVELOAD MISMATCH).
		shellStopAt:  aiOrderAt + 12,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		OQMap:        ecs.NewMap[components.OrderQueueHead](world),
		StateMap:     ecs.NewMap[components.OrderState](world),
		OverrideMap:  ecs.NewMap[components.TacticalOverride](world),
		DangerMap:    ecs.NewMap[components.DangerBuffer](world),
		BlastMap:     ecs.NewMap[components.BlastMark](world),
		UnsafeFilter: ecs.NewFilter2[components.UnsafeArea, components.WorldPos](world),
		AQMap:        ecs.NewMap[components.ActionQueue](world),
		orderAt:      aiOrderAt,
		verdictAt:    90,
		nextSampleAt: aiOrderAt + 3,
	}
}

// aiVehicleFleeSpawn (MB3): two trucks in one squad marching east; the rear
// one takes synthetic fire from the south it cannot answer.
func aiVehicleFleeSpawn(world *ecs.World, squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory, posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY/SERVICE — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	lead := vehicleFactory.Spawn(wp(20, -20), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	truck := vehicleFactory.Spawn(wp(12, -20), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	squad := squadService.CreateFromUnits([]ecs.Entity{lead, truck},
		components.FormationColumn)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO MERGE SQUAD — aborting\n", aiSceneID())
		return nil
	}
	return &aiTestState{
		sceneID:      aiSceneID(),
		fleeActive:   true,
		fleeSquad:    squad,
		fleeTruck:    truck,
		fleeGoal:     wp(95, -20),
		fleeSource:   wp(12, 10),
		fleeSpawn:    wp(12, -20),
		vehEnts:      []ecs.Entity{lead, truck},
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		RosterMap:    rosterMap,
		MotionMap:    ecs.NewMap[components.Motion](world),
		VehQueueMap:  ecs.NewMap[components.ActionQueue](world),
		DangerMap:    ecs.NewMap[components.DangerBuffer](world),
		OverrideVMap: ecs.NewMap[components.VehicleOverride](world),
		OQMap:        ecs.NewMap[components.OrderQueueHead](world),
		FdMap:        ecs.NewMap[components.FormationData](world),
		orderAt:      aiOrderAt,
		verdictAt:    60,
		nextSampleAt: aiOrderAt + 3,
	}
}

// aiVehicleStuckSpawn (MB2): two houses seal a 6 m slit between their
// inflated bands. Truck A's goal sits inside the seal (unreachable — the
// watchdog must fail it), truck B's beyond the pair (detour must free it).
func aiVehicleStuckSpawn(world *ecs.World, vehicleFactory *entities.VehicleFactory,
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
	a := vehicleFactory.Spawn(wp(8, 0), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	b := vehicleFactory.Spawn(wp(5, -14), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	return &aiTestState{
		sceneID:       aiSceneID(),
		stuckActive:   true,
		stuckFailT:    a,
		stuckFreeT:    b,
		stuckGoalFail: wp(32, 0),
		stuckGoalFree: wp(58, 8),
		stuckFoots: []components.AABB2D{
			{MinX: 27, MinZ: -13, MaxX: 37, MaxZ: -3},
			{MinX: 27, MinZ: 3, MaxX: 37, MaxZ: 13},
		},
		World:          world,
		PosMap:         posMap,
		VehQueueMap:    ecs.NewMap[components.ActionQueue](world),
		VehFollowerMap: ecs.NewMap[components.RoadFollower](world),
		stuckEvRes:     ecs.NewResource[components.EventLog](world),
		orderAt:        aiOrderAt,
		verdictAt:      45,
		nextSampleAt:   aiOrderAt + 5,
	}
}

// aiVehicleConvoySpawn (M7): fast leader + slow followers in one squad.
func aiVehicleConvoySpawn(world *ecs.World, squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory, posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY/SERVICE — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	// BMP first → roster slot 0 → leader; the fast hull must be the one
	// that has to hold back.
	bmp := vehicleFactory.Spawn(wp(15, -24), components.VehicleBMP,
		components.FactionPlayer, components.ControllerLocal)
	t1 := vehicleFactory.Spawn(wp(8, -30), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	t2 := vehicleFactory.Spawn(wp(8, -18), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	// Stale personal MoveTos: a T-merge on the move must wipe these, or the
	// fresh squad (no order yet) scatters racing them (owner repro).
	aqMap := ecs.NewMap[components.ActionQueue](world)
	for _, v := range []ecs.Entity{bmp, t1, t2} {
		if aq := aqMap.Get(v); aq != nil {
			systems.PushAction(aq, components.Action{
				Kind: components.ActionMoveTo, Target: wp(-60, 40),
			})
		}
	}
	squad := squadService.CreateFromUnits([]ecs.Entity{bmp, t1, t2},
		components.FormationColumn)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO MERGE SQUAD — aborting\n", aiSceneID())
		return nil
	}
	mergeOK := true
	for _, v := range []ecs.Entity{bmp, t1, t2} {
		if aq := aqMap.Get(v); aq == nil || aq.Count != 0 {
			mergeOK = false
		}
	}
	return &aiTestState{
		sceneID:       aiSceneID(),
		convoyActive:  true,
		convoySquad:   squad,
		convoyGoal:    wp(125, -24),
		convoyMergeOK: mergeOK,
		vehEnts:       []ecs.Entity{bmp, t1, t2},
		World:         world,
		SquadService:  squadService,
		PosMap:        posMap,
		RosterMap:     rosterMap,
		MotionMap:     ecs.NewMap[components.Motion](world),
		VehQueueMap:   aqMap,
		FdMap:         ecs.NewMap[components.FormationData](world),
		CSMap:         ecs.NewMap[components.FormationCustomSlots](world),
		orderAt:       aiOrderAt,
		verdictAt:     90,
		nextSampleAt:  aiOrderAt + 3,
	}
}

// aiVehicleConvoyRoadSpawn (MB3): the same BMP+2-truck column on `valley`,
// west of the river; the goal sits east of it. Off-road the river cut is
// impassable (P8-b water refusal), so the highway bridge is the only way —
// the macro path has to route over the RoadGraph and the members have to
// ride the carriageway at road speed.
func aiVehicleConvoyRoadSpawn(world *ecs.World, squadService *systems.SquadService,
	vehicleFactory *entities.VehicleFactory, posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if vehicleFactory == nil || squadService == nil {
		fmt.Printf("[ai-test %s] NO FACTORY/SERVICE — aborting\n", aiSceneID())
		return nil
	}
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}
	bmp := vehicleFactory.Spawn(wp(-40, -30), components.VehicleBMP,
		components.FactionPlayer, components.ControllerLocal)
	t1 := vehicleFactory.Spawn(wp(-47, -36), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	t2 := vehicleFactory.Spawn(wp(-47, -24), components.VehicleTruck,
		components.FactionPlayer, components.ControllerLocal)
	squad := squadService.CreateFromUnits([]ecs.Entity{bmp, t1, t2},
		components.FormationColumn)
	if squad == (ecs.Entity{}) {
		fmt.Printf("[ai-test %s] FAILED TO MERGE SQUAD — aborting\n", aiSceneID())
		return nil
	}
	return &aiTestState{
		sceneID:          aiSceneID(),
		convoyRoadActive: true,
		convoySquad:      squad,
		convoyGoal:       wp(70, 8),
		vehEnts:          []ecs.Entity{bmp, t1, t2},
		World:            world,
		SquadService:     squadService,
		PosMap:           posMap,
		RosterMap:        rosterMap,
		MotionMap:        ecs.NewMap[components.Motion](world),
		VehQueueMap:      ecs.NewMap[components.ActionQueue](world),
		VehFollowerMap:   ecs.NewMap[components.RoadFollower](world),
		vehGraphRes:      ecs.NewResource[components.RoadGraph](world),
		OQMap:            ecs.NewMap[components.OrderQueueHead](world),
		orderAt:          aiOrderAt,
		verdictAt:        120,
		nextSampleAt:     aiOrderAt + 5,
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
	aircraftFactory *entities.AircraftFactory,
	damageService *systems.DamageService,
	playerFaction components.Faction,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster],
	buildingMap *ecs.Map[components.Building],
) *aiTestState {
	if !isAIScene() {
		return nil
	}
	// A scene that is not about comms gets a working net, the same way it gets
	// terrain: since block A M1 an undelivered order simply does not run, and
	// without this every scene in the suite would be measuring radio silence
	// instead of what it was written for. Comms scenes place their own nodes.
	// Both sides: the bot commands over the same net the player does (block E),
	// so a scene where only one faction can be reached is not the neutral
	// background it looks like.
	if !strings.HasPrefix(aiSceneID(), "lite_comms") {
		for _, f := range [2]uint8{playerFaction.ID, components.FactionEnemyRed} {
			systems.SpawnRelay(world, components.WorldPos{}, f, components.RelayRangeSpawnM)
		}
	}
	if aiSceneID() == aiSceneAirTransit {
		return aiAirTransitSpawn(world, aircraftFactory, posMap)
	}
	if aiSceneID() == aiSceneAirRecon {
		return aiAirReconSpawn(world, aircraftFactory, vehicleFactory, posMap)
	}
	if aiSceneID() == aiSceneAirAAGun {
		return aiAirAASpawn(world, squadService, unitFactory, vehicleFactory,
			aircraftFactory, posMap)
	}
	if aiSceneID() == aiSceneAirManpads {
		return aiAirManpadsSpawn(world, unitFactory, aircraftFactory, posMap)
	}
	if aiSceneID() == aiSceneAirCAS {
		return aiAirCASSpawn(world, squadService, vehicleFactory, aircraftFactory, posMap)
	}
	if aiSceneID() == aiSceneComms {
		return aiCommsSpawn(world, squadService, roleService, unitFactory, damageService,
			playerFaction.ID, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneCommsOrders {
		return aiCommsOrdersSpawn(world, squadService, roleService, unitFactory,
			playerFaction.ID, posMap, rosterMap)
	}
	switch aiSceneID() {
	case aiSceneBalOpen, aiSceneBalAmbush, aiSceneBalEyes:
		mode := balanceOpen
		if aiSceneID() == aiSceneBalAmbush {
			mode = balanceAmbush
		} else if aiSceneID() == aiSceneBalEyes {
			mode = balanceEyes
		}
		return aiBalanceSpawn(world, squadService, roleService, unitFactory,
			vehicleFactory, mode, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneSoloOrders {
		return aiSoloOrderSceneSpawn(world, squadService, vehicleFactory, posMap)
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
	if aiSceneID() == aiSceneMarchColumn {
		return aiMarchColumnSpawn(world, squadService, roleService, unitFactory,
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
	if id := aiSceneID(); id == aiSceneVehAvoid || id == aiSceneVehBuilding ||
		id == aiSceneVehYield || id == aiSceneVehGroup {
		return aiVehicleAvoidSpawn(world, vehicleFactory, unitFactory, posMap)
	}
	if aiSceneID() == aiSceneVehStuck {
		return aiVehicleStuckSpawn(world, vehicleFactory, posMap)
	}
	if id := aiSceneID(); id == aiSceneCoverTrench || id == aiSceneCoverHull ||
		id == aiSceneCoverDefilade || id == aiSceneCoverNone {
		return aiCoverPosSpawn(world, squadService, vehicleFactory, unitFactory,
			posMap, rosterMap)
	}
	if aiSceneID() == aiSceneShellfire {
		return aiShellfireSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneBounding {
		return aiBoundingSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneClearBld {
		return aiClearBuildingSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneFocusFire {
		return aiFocusFireSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneMass {
		return aiMassSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneVehFlee {
		return aiVehicleFleeSpawn(world, squadService, vehicleFactory, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneVehConvoyRoad {
		return aiVehicleConvoyRoadSpawn(world, squadService, vehicleFactory, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneVehConvoy {
		return aiVehicleConvoySpawn(world, squadService, vehicleFactory, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneWallGlide {
		return aiWallGlideSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneCrowdCross {
		return aiCrowdCrossSpawn(world, squadService, roleService, unitFactory,
			playerFaction, posMap, rosterMap)
	}
	if aiSceneID() == aiSceneVehForest {
		return aiVehicleForestSpawn(world, vehicleFactory, posMap)
	}
	if aiSceneID() == aiSceneVehSlope {
		return aiVehicleSlopeSpawn(world, vehicleFactory, posMap)
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

	if aiSceneID() == aiSceneHouse2North {
		st.liftTrack = true
		st.sampler = systems.NewHeightSampler(world)
	}
	if id := aiSceneID(); id == aiSceneGarrisonWin || id == aiSceneGarrisonHouse {
		st.garrisonCheck = true
		st.slotPlanner = systems.NewBuildingSlotPlanner(world)
	}
	if aiSceneID() == aiSceneHiddenHouse {
		st.hiddenCheck = true
		st.slotPlanner = systems.NewBuildingSlotPlanner(world)
	}

	// ai_office_rooms: snapshot every room band (rect + storey floor Y) of
	// the target building — the verdict demands each one occupied.
	if aiSceneID() == aiSceneOfficeRooms {
		planIdxRes := ecs.NewResource[systems.BuildingPlanIndex](world)
		levelMap := ecs.NewMap[components.Level](world)
		if planIdx := planIdxRes.Get(); planIdx != nil {
			for _, levEnt := range planIdx.Levels[target] {
				lvl := levelMap.Get(levEnt)
				if lvl == nil {
					continue
				}
				for r := uint8(0); r < lvl.RoomCount; r++ {
					st.roomBands = append(st.roomBands, aiRoomBand{
						room: lvl.Rooms[r],
						minY: lvl.AABB.MinY,
					})
				}
			}
		}
		if len(st.roomBands) == 0 {
			fmt.Printf("[ai-test %s] NO ROOM DATA on target building — aborting\n", aiSceneID())
			return nil
		}
	}

	// ai_main_* scenes target one storey: resolve the Level entity via
	// BuildingPlanIndex (same path the in-game "Occupy L<n>" popup takes).
	// levelIdx -1 = a building order — the spec only picked the target.
	if isMainScene && mainSpec.levelIdx >= 0 {
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
		// The exact goal the popup would commit: pickRoomTarget with the raw
		// press in the building's middle. On a ring building the AABB centre
		// is the open well — the room fallback is the mechanic under test.
		raw := components.WorldPos{}.Add(rl.Vector3{
			X: lvl.AABB.CenterX(), Z: lvl.AABB.CenterZ()})
		st.targetLevelGoal = pickRoomTarget(lvl, raw)
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
	targetLevelGoal components.WorldPos

	// Set for ai_office_rooms: every band must hold >=1 member at verdict.
	roomBands []aiRoomBand

	// Set for ai_house2_north: per-frame max of (Y - surface) over members
	// whose XZ is OUTSIDE the target footprint. A padded stair ramp poking
	// through the wall hoists outside walkers metres into the air.
	liftTrack bool
	liftMax   float32

	// Set for ai_garrison_windows: order kind = Garrison, verdict demands
	// every planned window slot manned by a member facing the opening.
	garrisonCheck bool
	// hiddenCheck: OccupyBuilding + Stealth + HoldFire ("Hidden position") —
	// verdict adds all-crouched and nobody parked at a window slot.
	hiddenCheck bool
	slotPlanner *systems.BuildingSlotPlanner

	// Set for ai_los_* scenes: verdict counts Contacts on this entity and
	// tallies Direct/Shared awareness across the roster.
	losTarget        ecs.Entity
	losExpectVisible bool
	losEarlyAt       float32 // >0: contacts must still be 0 at this time
	losEarlyDone     bool
	losEarlyContacts int

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

	// Set for ai_air_transit (Phase 20 M0). See test_scene_air.go.
	airActive      bool
	airEnt         ecs.Entity
	airWaypoints   []components.WorldPos
	airLegsPushed  bool
	airReleasedAt  float32
	airLegsLeft    int
	airMinClear    float32
	airAltErrMax   float32
	airCaptured    bool
	airCaptureAt   float32
	airTrackedFor  float32
	airDespawnedAt float32
	airSentHome    bool
	AircraftMap    *ecs.Map[components.Aircraft]
	airSampler     *systems.HeightSampler
	airFilter      *ecs.Filter1[components.Aircraft]

	// Set for ai_air_recon (Phase 20 M1). See test_scene_air_recon.go.
	reconActive          bool
	reconScout           ecs.Entity
	reconEmitter         ecs.Entity
	reconHull            ecs.Entity
	reconWaypoint        components.WorldPos
	reconLegPushed       bool
	reconESMAt           float32
	reconESMRange        float32
	reconBearingErr      float32
	reconLastIntercept   float32
	reconRanged          bool
	reconRangedAt        float32
	reconRadarOn         bool
	reconRadarAt         float32
	reconRadarRange      float32
	reconAudioAt         float32
	reconAudioRange      float32
	reconSilenced        bool
	reconSilencedAt      float32
	reconInterceptAtHush float32
	// Set for ai_solo_orders (Phase 20.7 L0). See test_scene_solo_orders.go.
	soloActive          bool
	soloVeh             ecs.Entity
	soloMate            ecs.Entity
	soloLegA            components.WorldPos
	soloLegB            components.WorldPos
	soloIssued          bool
	soloBecameCommander bool
	soloStayedNonSquad  bool
	soloDroveSameTick   bool
	soloLastHead        ecs.Entity
	soloLegsDone        int
	soloHistCount       int
	soloMerged          bool
	soloCommandDropped  bool
	soloSvc             *systems.SquadService
	soloHeadMap         *ecs.Map[components.OrderQueueHead]
	soloHistRes         ecs.Resource[components.OrderHistory]

	SensorsMap   *ecs.Map[components.Sensors]
	AwarenessMap *ecs.Map[components.Awareness]
	ContactMap   *ecs.Map[components.Contact]
	regRes       ecs.Resource[components.ContactRegistry]

	// Set for ai_air_aa_gun (Phase 20 M2). See test_scene_air_aa.go.
	aaActive          bool
	aaGun             ecs.Entity
	aaSquad           ecs.Entity
	aaScout           ecs.Entity
	aaLegPushed       bool
	aaGunWeapon       ecs.Entity
	aaAmmoStart       uint16
	aaAmmoNow         uint16
	aaShotsBeforeGate bool
	aaEnabled         bool
	aaAcquireAt       float32
	aaAcquireD        float32
	aaHPStart         float32
	aaHPNow           float32
	aaSuppSeen        float32
	aaDownAt          float32
	EquipMap          *ecs.Map[components.Equipment]
	WeaponMap         *ecs.Map[components.Weapon]
	ThreatMap         *ecs.Map[components.Threat]
	logRes            ecs.Resource[components.EventLog]

	// Set for ai_air_manpads (Phase 20 M2). See test_scene_air_manpads.go.
	manActive      bool
	manGunner      ecs.Entity
	manTube        ecs.Entity
	manScout       ecs.Entity
	manLegPushed   bool
	manAmmoStart   uint16
	manAmmoNow     uint16
	manMissileSeen bool
	manFlareSeen   bool
	manDecoySeen   bool
	manAbortSeen   bool
	manEgressSeen  bool
	manHPStart     float32
	manHPNow       float32
	manMinDist     float32
	manDownAt      float32
	manLeft        bool
	AirOvMap       *ecs.Map[components.AircraftOverride]
	MissileF       *ecs.Filter2[components.Missile, components.WorldPos]

	// Set for ai_air_cas (Phase 20 M3). See test_scene_air_cas.go.
	casActive    bool
	casHull      ecs.Entity
	casScout     ecs.Entity
	casTube      ecs.Entity
	casOrdered   bool
	casAmmoStart uint16
	casAmmoNow   uint16
	casAcquireD  float32
	casLaunchD   float32
	casMinD      float32
	casMaxUnmask float32
	casHullHP0   float32
	casHullHP    float32
	casKillAt    float32
	casSankAt    float32

	// Set for lite_comms_range (block A M0). See test_scene_comms.go.
	commsActive  bool
	commsKilled  bool
	commsRescued bool
	commsProbes  []commsProbe
	commsOrders  []commsOrderProbe
	playerFac    uint8
	Damage       *systems.DamageService
	CommsMap     *ecs.Map[components.CommsState]
	casRidgeUp   bool
	casStamper   *systems.Stamper

	// Set for lite_balance_* (2026-08-26). See test_scene_balance.go.
	balanceActive      bool
	balMode            balanceMode
	balHulls           []ecs.Entity
	balSquad           ecs.Entity
	balInf             []ecs.Entity
	balTriggered       bool
	balTriggerAt       float32
	balTriggerM        float32
	balWipeAt          float32
	balFirstHullLossAt float32
	balEyesVehFoe      ecs.Entity
	balEyesVehFoeUnits []ecs.Entity
	balEyesInfA        ecs.Entity
	balEyesInfAUnits   []ecs.Entity
	balEyesInfB        ecs.Entity
	balEyesInfBUnits   []ecs.Entity
	balSpottedAt       float32
	balContactMap      *ecs.Map[components.Contact]
	balRegistry        ecs.Resource[components.ContactRegistry]
	balEyesVehSees     float32
	balEyesInfSeesVeh  float32
	balEyesInfSeesInf  float32
	AwareMap           *ecs.Map[components.Awareness]
	RulesMap           *ecs.Map[components.EngagementRules]

	// Set for ai_vehicle_combat: two sides duel, verdict = enemy side dead.
	vehCombatActive bool
	vehFoes         []ecs.Entity
	HPMap           *ecs.Map[components.HP]

	// Set for ai_vehicle_{avoid,building,yield,group} (M6): per-vehicle
	// goals + per-tick collision metrics.
	avoidActive   bool
	avoidGoals    []components.WorldPos // parallel to vehEnts
	avoidRadii    []float32             // spec ColliderR, parallel to vehEnts
	avoidFoots    []components.AABB2D   // building footprints to stay out of
	avoidYield    []ecs.Entity          // infantry that must stay clear
	avoidMinPair  float32               // min over run: pairwise dist − ΣR
	avoidFootBad  int                   // ticks with a hull centre inside a footprint
	avoidYieldBad int                   // ticks with a unit inside a hull radius
	avoidRevTicks int                   // reverse ticks past avoidRevFree
	avoidRevFree  float32               // >0: count reversals after this elapsed

	// Set for the MC2 cover-position family: synthetic fire from one point,
	// one candidate kind per scene, latched success + a scene-specific number.
	covActive  bool
	covUnits   []ecs.Entity
	covShooter components.WorldPos
	covNextInj float32
	covStopAt  float32
	covRefX    float32
	covRefZ    float32
	covOK      bool
	covNote    float32
	covStamped bool
	covProne   bool
	covLateral float32
	covStart   components.WorldPos
	SlotFilter *ecs.Filter2[components.WorldPos, components.CoverSlot]
	StanceMap  *ecs.Map[components.Stance]

	// Set for ai_shellfire (MC1): DefendPosition under a walking barrage.
	shellActive     bool
	shellSquad      ecs.Entity
	shellHold       components.WorldPos
	shellCentre     components.WorldPos
	shellNextAt     float32
	shellShots      int
	shellStopAt     float32
	shellZoneR      float32
	shellVacatedAt  float32
	shellReturnedAt float32
	shellOrderBroke bool
	shellSawZone    bool
	BlastMap        *ecs.Map[components.BlastMark]
	UnsafeFilter    *ecs.Filter2[components.UnsafeArea, components.WorldPos]
	StateMap        *ecs.Map[components.OrderState]

	// Set for ai_bounding (MC3): a 120 m march under sustained flank fire.
	boundActive  bool
	boundGoal    components.WorldPos
	boundFoes    []ecs.Entity
	boundMaxX    float32
	boundBackMax float32
	boundSawMode bool
	boundMinHold float32
	boundHoldN   int
	boundArrived float32
	PlanMap      *ecs.Map[components.SquadPlan]

	// Set for ai_clear_building (MC3): storey-by-storey sweep of a 2-floor
	// office; the gate is that nobody goes upstairs while the ground floor
	// still holds a hostile.
	clearActive   bool
	clearBuilding ecs.Entity
	clearFoes     []ecs.Entity
	clearUpY      float32
	clearEarlyUp  int
	clearClearedT float32
	clearChained  bool
	clearPhases   [4]bool
	clearFP       components.AABB2D
	clearUpPlaced bool
	clearUpSeen   int
	clearRole     *systems.RoleService
	clearSpawner  aiUnitSpawn
	clearChildRes ecs.Resource[systems.BuildingChildIndex]
	KindMap       *ecs.Map[components.OrderKind]
	FloorMap      *ecs.Map[components.Floor]

	// Set for ai_focus_fire (MC3): share of the squad's damage that lands on
	// the enemy the AttackTarget order names.
	focusActive bool
	focusEnemy  ecs.Entity
	focusFoes   []ecs.Entity
	focusMaxHP  []float32
	focusShare  float32
	focusKilled bool

	// Set for ai_mass (MZ): 12 squads under fire — parallel-path coverage and
	// the phase's perf measurement.
	massActive  bool
	massSquads  []ecs.Entity
	massGoalZ   float32
	massLiveMax int
	massPlanMax int

	// Set for ai_vehicle_flee (MB3): squadded truck under unanswerable fire —
	// retreat distance, order survival across the reflex, resumption after.
	fleeActive    bool
	fleeSquad     ecs.Entity
	fleeTruck     ecs.Entity
	fleeGoal      components.WorldPos
	fleeSource    components.WorldPos
	fleeSpawn     components.WorldPos
	fleeNextInj   float32
	fleeMaxDist   float32
	fleeSawReflex bool
	fleeOrderKept bool
	fleeResumed   bool
	OverrideVMap  *ecs.Map[components.VehicleOverride]

	// Set for ai_vehicle_convoy_road (MB3): road usage + column timing.
	convoyRoadActive bool
	convoyRoadTicks  int
	convoyRoadArrive float32

	// Set for ai_vehicle_stuck (MB2): watchdog Failed arm (truck A, sealed
	// slit goal) + committed-detour escape arm (truck B) + AvoidSide flip
	// metric on B.
	stuckActive   bool
	stuckFailT    ecs.Entity
	stuckFreeT    ecs.Entity
	stuckGoalFail components.WorldPos
	stuckGoalFree components.WorldPos
	stuckFoots    []components.AABB2D
	stuckFootBad  int
	stuckFlips    int
	stuckLastSide int8
	stuckFailedAt float32
	stuckFreedAt  float32
	stuckEvRes    ecs.Resource[components.EventLog]

	// Set for ai_vehicle_convoy (M7): squad march cohesion metric, then an
	// in-place reform via CustomSlots+ReformPending (owner 2026-07-31).
	convoyActive    bool
	convoySquad     ecs.Entity
	convoyGoal      components.WorldPos
	convoyMaxSpread float32
	convoyPhase     uint8
	convoyMarchAt   float32
	convoyMergeOK   bool
	convoyReformOK  bool
	CSMap           *ecs.Map[components.FormationCustomSlots]

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
	// Churn-replan budget (replans/unit/min over roster+soloists); 0 = report only.
	// Measured from orderAt+5 s — the same alignment window the churn-deg
	// metric uses (the initial turn/shake-out is legitimate work, not storm).
	marchReplanMax  float32
	marchReplanBase float32
	replanBaseSet   bool

	// Set for ai_vehicle_forest (MB1): prop-intrusion ticks + crush proof.
	forestActive   bool
	forestTruck    ecs.Entity
	forestTank     ecs.Entity
	forestGoalTr   components.WorldPos
	forestGoalTk   components.WorldPos
	forestProps    []aiScenePropRec
	forestTruckBad int
	forestRockBad  int

	// Set for ai_vehicle_slope (MB1): stamped ridge + steep-tick metric.
	slopeActive     bool
	slopeTank       ecs.Entity
	slopeGoal       components.WorldPos
	slopeStamped    bool
	slopeSteepTicks int
	Stamper         *systems.Stamper

	// Set for ai_crowd_cross (MA3): pairwise penetration + per-tick |dv|
	// over squad members and the standing crowd.
	crowdActive bool
	crowdEnts   []ecs.Entity
	crowdGoal   components.WorldPos
	crowdMaxPen float32
	crowdMaxDv  float32
	crowdPrev   map[ecs.Entity][2]float32

	// Set for ai_wall_glide (MA2): facade-clearance (leg 1 only — the door
	// approach afterwards legitimately touches the wall line), escape-spring
	// ratio, and door-entry oscillation metrics.
	glideActive   bool
	glideUnion    components.AABB2D
	glideMinClear float32
	glideLeg1     ecs.Entity
	glideLeg1Done bool
	glideInside   map[ecs.Entity]bool
	glideEnter    map[ecs.Entity]int
	OQMap         *ecs.Map[components.OrderQueueHead]

	// Set for ai_march_column (MA1): chained legs + follower-collapse metric.
	// Collapse = 3+ men inside a 1.2 m circle SUSTAINED (transient corner
	// proximity is normal — ORCA keeps bodies apart; parking on one shared
	// waypoint is not). columnClusterWorst = longest sustained violation.
	columnActive       bool
	columnGoals        []components.WorldPos
	columnClusterMax   int
	columnClusterSince float32
	columnClusterWorst float32
	columnLastT        float32
	// Min-clearance floor. The metric samples post-movement Y at the NEW xz
	// before the next GroundStick clamp: on a steep carve-skirt cell
	// (gradient ≥ 2 m/m) the one-frame reading is ±(gradient·step + yLerp)
	// ≈ 0.33 m, so hills routes crossing the road cut get a looser floor.
	marchClearFloor float32
	soloEnts        []ecs.Entity
	AQMap           *ecs.Map[components.ActionQueue]
	FdMap           *ecs.Map[components.FormationData]
	sampler         *systems.HeightSampler
	roadSurface     ecs.Resource[components.RoadSurface]
	marchMoveN      int
	marchSideN      int
	marchChurnDeg   float32
	marchLastFYaw   float32
	marchHaveFYaw   bool
	marchMinClear   float32
	marchMaxClear   float32

	elapsed      float32
	orderAt      float32
	verdictAt    float32
	nextSampleAt float32

	orderFired  bool
	verdictDone bool

	World         *ecs.World
	SquadService  *systems.SquadService
	PosMap        *ecs.Map[components.WorldPos]
	RosterMap     *ecs.Map[components.CommandRoster]
	BuildingMap   *ecs.Map[components.Building]
	MotionMap     *ecs.Map[components.Motion]
	BlackboardMap *ecs.Map[components.LocalBlackboard]
	MicroPathMap  *ecs.Map[components.MicroPath]
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
	if s.airActive {
		s.updateAirTransit(elapsed)
		return
	}
	if s.reconActive {
		s.updateAirRecon(elapsed)
		return
	}
	if s.aaActive {
		s.updateAirAA(elapsed)
		return
	}
	if s.manActive {
		s.updateAirManpads(elapsed)
		return
	}
	if s.casActive {
		s.updateAirCAS(elapsed)
		return
	}
	if s.commsActive {
		s.updateComms(elapsed)
		return
	}
	if len(s.commsOrders) > 0 {
		s.updateCommsOrders(elapsed)
		return
	}
	if s.balanceActive {
		s.updateBalance(elapsed)
		return
	}
	if s.soloActive {
		s.updateSoloOrders(elapsed)
		return
	}
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
	if s.columnActive {
		s.updateMarchColumn(elapsed)
		return
	}
	if s.glideActive {
		s.updateWallGlide(elapsed)
		return
	}
	if s.crowdActive {
		s.updateCrowdCross(elapsed)
		return
	}
	if s.forestActive {
		s.updateVehForest(elapsed)
		return
	}
	if s.slopeActive {
		s.updateVehSlope(elapsed)
		return
	}
	if s.stuckActive {
		s.updateVehStuck(elapsed)
		return
	}
	if s.covActive {
		s.updateCoverPos(elapsed)
		return
	}
	if s.shellActive {
		s.updateShellfire(elapsed)
		return
	}
	if s.boundActive {
		s.updateBounding(elapsed)
		return
	}
	if s.clearActive {
		s.updateClearBuilding(elapsed)
		return
	}
	if s.focusActive {
		s.updateFocusFire(elapsed)
		return
	}
	if s.massActive {
		s.updateMass(elapsed)
		return
	}
	if s.fleeActive {
		s.updateVehFlee(elapsed)
		return
	}
	if s.convoyRoadActive {
		s.updateConvoyRoad(elapsed)
		return
	}
	if s.avoidActive {
		s.updateVehAvoid(elapsed)
		return
	}
	if s.convoyActive {
		s.updateConvoy(elapsed)
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

	if s.liftTrack && s.orderFired && !s.verdictDone && s.sampler != nil {
		roster := s.RosterMap.Get(s.squad)
		if roster != nil {
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
				if fp.Contains(mx, mz) {
					continue
				}
				if lift := pos.Local.Y - s.sampler.Sample(mx, mz); lift > s.liftMax {
					s.liftMax = lift
				}
			}
		}
	}

	if !s.orderFired && elapsed >= s.orderAt {
		bld := s.BuildingMap.Get(s.targetBuilding)
		if bld == nil {
			fmt.Printf("[ai-test %s] target building gone before order — aborting\n", s.sceneID)
			s.verdictDone = true
			return
		}
		if s.targetLevel != (ecs.Entity{}) {
			// Storey goal: MoveTo on the Level entity, target from
			// pickRoomTarget — the exact order the building-popup
			// "Occupy L<n>" emits, so the test exercises the real player
			// mechanic (including the nearest-room fallback on ring shapes).
			s.SquadService.IssueOrder(s.squad,
				components.OrderKindMoveTo, s.targetLevelGoal, s.targetLevel,
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
		kind := components.OrderKindOccupyBuilding
		kindName := "OccupyBuilding"
		params := systems.OrderParams{}
		if s.garrisonCheck {
			kind = components.OrderKindGarrison
			kindName = "Garrison"
		}
		if s.hiddenCheck {
			// The exact call the popup's "Hidden position" item commits —
			// order params for the approach + standing rules for the hold.
			s.SquadService.IssueOrderHidden(s.squad, targetPos, s.targetBuilding, false)
			s.orderFired = true
			fmt.Printf("[ai-test %s] t=%.1fs ORDER ISSUED OccupyBuilding+Hidden target=%v\n",
				s.sceneID, elapsed, s.targetBuilding)
			return
		}
		s.SquadService.IssueOrder(s.squad,
			kind, targetPos, s.targetBuilding,
			false, params)
		s.orderFired = true
		fmt.Printf("[ai-test %s] t=%.1fs ORDER ISSUED %s target=%v\n",
			s.sceneID, elapsed, kindName, s.targetBuilding)
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
			var garrisonSlots []systems.BuildingSlot
			if s.garrisonCheck && s.slotPlanner != nil {
				garrisonSlots = s.slotPlanner.PlanSlots(s.targetBuilding,
					systems.SlotWindows, int(roster.Count), false, 0)
			}
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
				if !bad && int(i) < len(garrisonSlots) && garrisonSlots[i].Window {
					d := pos.Sub(garrisonSlots[i].Pos)
					r := systems.SlotParkRadius + 0.3
					bad = d.X*d.X+d.Z*d.Z > r*r || d.Y < -0.8 || d.Y > 0.8
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
					diag = fmt.Sprintf(" m%d=(%.1f,%.1f,Y%.1f) speed=%.2f mode=%s path=%d/%d%s",
						watchIdx, mx, mz, pos.Local.Y, mot.Speed, mode, mpHead, mpLen, wps)
				}
			}
		}
		if os.Getenv("RTS_WALL_DUMP") != "" && elapsed > 21 && elapsed < 25 {
			tempDumpWalls(s.World, 26, 40, 30, 40, 1.6)
		}
		if os.Getenv("RTS_MACRO_DUMP") != "" {
			if mpq := ecs.NewMap[components.MacroPath](s.World).Get(s.squad); mpq != nil {
				wp := ""
				if mpq.Head < mpq.Count {
					w := mpq.Waypoints[mpq.Head]
					wp = fmt.Sprintf(" wp=(%.1f,%.1f)",
						float32(w.Chunk.X)*components.ChunkSize+w.Local.X,
						float32(w.Chunk.Z)*components.ChunkSize+w.Local.Z)
				}
				center, okC := systems.SquadCenter(s.World, roster, s.PosMap)
				cs := ""
				if okC {
					cs = fmt.Sprintf(" center=(%.1f,%.1f)",
						float32(center.Chunk.X)*components.ChunkSize+center.Local.X,
						float32(center.Chunk.Z)*components.ChunkSize+center.Local.Z)
				}
				diag += fmt.Sprintf(" macro=%d/%d has=%v goal=(%.1f,%.1f)%s%s",
					mpq.Head, mpq.Count, mpq.HasGoal,
					float32(mpq.Goal.Chunk.X)*components.ChunkSize+mpq.Goal.Local.X,
					float32(mpq.Goal.Chunk.Z)*components.ChunkSize+mpq.Goal.Local.Z, wp, cs)
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
		roomsInfo := ""
		if len(s.roomBands) > 0 {
			counts := s.countRoomOccupancy()
			for _, c := range counts {
				if c == 0 {
					verdict = "FAIL"
				}
			}
			roomsInfo = fmt.Sprintf("  rooms=%v", counts)
		}
		if s.liftTrack {
			if s.liftMax > 0.5 {
				verdict = "FAIL"
			}
			roomsInfo += fmt.Sprintf("  outsideLift=%.2fm", s.liftMax)
		}
		if s.garrisonCheck {
			manned, faced, expected := s.countWindowSlots()
			if manned < expected || faced < manned {
				verdict = "FAIL"
			}
			roomsInfo += fmt.Sprintf("  windows=%d/%d faced=%d", manned, expected, faced)
		}
		if s.hiddenCheck {
			crouched, atWindow, total := s.countHidden()
			if total == 0 || crouched < total || atWindow > 0 {
				verdict = "FAIL"
			}
			roomsInfo += fmt.Sprintf("  crouched=%d/%d atWindow=%d", crouched, total, atWindow)
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (%d/%d members inside)%s\n",
			s.sceneID, verdict, inside, alive, roomsInfo)
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

// updateMarchColumn (MA1): chained-leg column march. Verdict = arrival at
// the final leg + churn-replan budget + no follower collapse (never 3+ men
// inside a 1.2 m circle past the alignment window — the discrete leader-wake
// used to converge every follower onto one waypoint at the leg's end).
func (s *aiTestState) updateMarchColumn(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI MARCH COLUMN: %s  squad=%v legs=%d\n",
			s.sceneID, s.squad, len(s.columnGoals))
		fmt.Println("============================================================")
		for li, g := range s.columnGoals {
			s.SquadService.IssueOrder(s.squad, components.OrderKindMoveTo,
				g, ecs.Entity{}, li > 0, systems.OrderParams{})
		}
		s.orderFired = true
		return
	}

	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}

	if os.Getenv("RTS_CLUSTER_DUMP") != "" && int(elapsed*100)%100 < 2 {
		fmt.Printf("[col] t=%.1f", elapsed)
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
				continue
			}
			p := s.PosMap.Get(mem)
			mp := s.MicroPathMap.Get(mem)
			if p == nil || mp == nil {
				continue
			}
			tgt := ""
			if s.AQMap != nil {
				if aq := s.AQMap.Get(mem); aq != nil && aq.Count > 0 {
					a := aq.Actions[aq.Head]
					tgt = fmt.Sprintf(" T%d(%.1f,%.1f)", a.Kind,
						float32(a.Target.Chunk.X)*components.ChunkSize+a.Target.Local.X,
						float32(a.Target.Chunk.Z)*components.ChunkSize+a.Target.Local.Z)
				}
			}
			fmt.Printf(" [%d](%.1f,%.1f h%d/%d%s)", i,
				float32(p.Chunk.X)*components.ChunkSize+p.Local.X,
				float32(p.Chunk.Z)*components.ChunkSize+p.Local.Z,
				mp.Head, mp.Count, tgt)
		}
		fmt.Println()
	}
	// Follower-collapse metric: for every live member, count live members
	// (itself included) inside 1.2 m; track the run maximum. The alignment
	// window is 8 s (replan metric keeps 5): a TRUE-spacing column is 14 m
	// deep and the start-line lane-sorting legitimately packs laggards for
	// ~1 s around t≈7 — the window exists precisely to skip that phase.
	if elapsed > s.orderAt+8 {
		var xs, zs [16]float32
		n := 0
		for i := uint8(0); i < roster.Count && n < 16; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
				continue
			}
			if p := s.PosMap.Get(mem); p != nil {
				xs[n] = float32(p.Chunk.X)*components.ChunkSize + p.Local.X
				zs[n] = float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
				n++
			}
		}
		const clusterR = 1.2
		cur := 0
		for a := 0; a < n; a++ {
			c := 0
			for b := 0; b < n; b++ {
				dx, dz := xs[a]-xs[b], zs[a]-zs[b]
				if dx*dx+dz*dz <= clusterR*clusterR {
					c++
				}
			}
			if c > cur {
				cur = c
			}
		}
		if cur > s.columnClusterMax {
			s.columnClusterMax = cur
		}
		if cur >= 3 {
			if s.columnClusterSince == 0 {
				s.columnClusterSince = elapsed
			}
			if d := elapsed - s.columnClusterSince; d > s.columnClusterWorst {
				s.columnClusterWorst = d
			}
			if os.Getenv("RTS_CLUSTER_DUMP") != "" && int(elapsed*10)%5 == 0 {
				fmt.Printf("[cluster] t=%.1f", elapsed)
				for a := 0; a < n; a++ {
					fmt.Printf(" (%.1f,%.1f)", xs[a], zs[a])
				}
				fmt.Println()
			}
		} else {
			s.columnClusterSince = 0
		}
	}

	// Arrival keys on the LEADER: a column's member centroid legitimately
	// trails the goal by half the column depth.
	final := s.columnGoals[len(s.columnGoals)-1]
	arrived := false
	if leader := roster.Members[0]; leader != (ecs.Entity{}) && s.World.Alive(leader) {
		if p := s.PosMap.Get(leader); p != nil {
			d := p.Sub(final)
			arrived = d.X*d.X+d.Z*d.Z < 4*4 && elapsed > s.orderAt+10
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt && !arrived {
		s.nextSampleAt = elapsed + aiSampleEvery
		distToFinal := float32(-1)
		if center, ok := systems.SquadCenter(s.World, roster, s.PosMap); ok {
			d := center.Sub(final)
			distToFinal = float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
		}
		fmt.Printf("[ai-test %s] t=%.1fs cluster=%d replan=%.1f/u/min distFinal=%.1f\n",
			s.sceneID, elapsed, s.columnClusterMax, s.replanRate(roster, elapsed), distToFinal)
	}

	if arrived || elapsed >= s.verdictAt {
		replanRate := s.replanRate(roster, elapsed)
		pass := arrived &&
			s.columnClusterWorst < 1.0 &&
			(s.marchReplanMax == 0 || replanRate <= s.marchReplanMax)
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%v t=%.1fs cluster=%d clusterHold=%.2fs replan=%.1f/u/min)\n",
			s.sceneID, verdict, arrived, elapsed, s.columnClusterMax,
			s.columnClusterWorst, replanRate)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// tempDumpWalls — one-shot forensics behind RTS_WALL_DUMP: prints wall
// segments (with openings and door state) inside a world-XZ box at a storey Y.
func tempDumpWalls(world *ecs.World, minX, maxX, minZ, maxZ, y float32) {
	wf := ecs.NewFilter2[components.WallSegment, components.WorldPos](world)
	doorMap := ecs.NewMap[components.Door](world)
	q := wf.Query()
	for q.Next() {
		w, pos := q.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		if wx < minX || wx > maxX || wz < minZ || wz > maxZ {
			continue
		}
		if pos.Local.Y < y-1 || pos.Local.Y > y+1 {
			continue
		}
		ds := "-"
		if d := doorMap.Get(q.Entity()); d != nil {
			ds = fmt.Sprintf("door:%d", d.State)
		}
		fmt.Printf("[wall] (%.1f,%.1f Y%.1f) yaw=%.2f len=%.1f open=%d w=%.1f t=%.2f %s\n",
			wx, wz, pos.Local.Y, w.Yaw, w.Length, w.OpeningKind,
			w.OpeningWidth*w.Length*0+w.OpeningWidth, w.OpeningCenterT, ds)
	}
	q.Close()
}

// updateWallGlide (MA2): leg 1 MoveTo whose straight line crosses the union
// footprint — the planner must round the 40 m facade at body clearance —
// then a queued OccupyBuilding through the east house's south door.
func (s *aiTestState) updateWallGlide(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		goal := components.WorldPos{}.Add(rl.Vector3{X: 70, Z: 30})
		goal.Local.Y = systems.GroundHeight(70, 30)
		fmt.Println("============================================================")
		fmt.Printf("== AI WALL GLIDE: %s  squad=%v east=%v union X[%.0f..%.0f] Z[%.0f..%.0f]\n",
			s.sceneID, s.squad, s.targetBuilding,
			s.glideUnion.MinX, s.glideUnion.MaxX, s.glideUnion.MinZ, s.glideUnion.MaxZ)
		fmt.Println("============================================================")
		s.glideLeg1 = s.SquadService.IssueOrder(s.squad, components.OrderKindMoveTo,
			goal, ecs.Entity{}, false, systems.OrderParams{})
		s.SquadService.IssueOrder(s.squad, components.OrderKindOccupyBuilding,
			components.WorldPos{}.Add(rl.Vector3{
				X: s.targetFootprint.CenterX(), Z: s.targetFootprint.CenterZ()}),
			s.targetBuilding, true, systems.OrderParams{})
		s.orderFired = true
		return
	}
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	if !s.glideLeg1Done {
		if head := s.OQMap.Get(s.squad); head == nil || head.First != s.glideLeg1 {
			s.glideLeg1Done = true
		}
	}
	insideCnt, alive, maxEnter := 0, 0, 0
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		p := s.PosMap.Get(mem)
		if p == nil {
			continue
		}
		alive++
		mx := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		mz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		if !s.glideLeg1Done {
			if c := rectOutsideDist(s.glideUnion, mx, mz); c < s.glideMinClear {
				s.glideMinClear = c
			}
		}
		in := s.targetFootprint.Contains(mx, mz)
		if in && !s.glideInside[mem] {
			s.glideEnter[mem]++
		}
		s.glideInside[mem] = in
		if in {
			insideCnt++
		}
		if s.glideEnter[mem] > maxEnter {
			maxEnter = s.glideEnter[mem]
		}
	}
	escFrac := s.escapeFrac(roster)
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs inside=%d/%d clear=%.2f esc=%.3f enterMax=%d leg1done=%v\n",
			s.sceneID, elapsed, insideCnt, alive, s.glideMinClear, escFrac,
			maxEnter, s.glideLeg1Done)
	}
	done := alive > 0 && insideCnt >= alive && elapsed > s.orderAt+10
	if done || elapsed >= s.verdictAt {
		// 0.15, not the 0.25 this was first calibrated at: the metric is the
		// single closest approach of any man over a whole march, and it swings
		// 0.07 m on a pure entity-ID shift (measured 2026-08-26 by spawning one
		// extra relay: 0.19 -> 0.26, same behaviour). A bound set at the value
		// of the day detects ID order, not walls. What it must still catch is a
		// man pressed INTO the facade, which reads near zero.
		pass := done &&
			s.glideMinClear >= 0.15 &&
			escFrac < 0.05 &&
			maxEnter <= 2
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (inside=%d/%d t=%.1fs clear=%.2f esc=%.3f enterMax=%d replan=%.1f/u/min)\n",
			s.sceneID, verdict, insideCnt, alive, elapsed, s.glideMinClear,
			escFrac, maxEnter, s.replanRate(roster, elapsed))
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateVehForest (MB1): parallel-lane drive through the grove. Verdict =
// both arrived + truck never inside any prop + tank never inside a rock +
// at least one pine flattened.
func (s *aiTestState) updateVehForest(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE FOREST: truck=%v tank=%v props=%d\n",
			s.forestTruck, s.forestTank, len(s.forestProps))
		fmt.Println("============================================================")
		for _, v := range [2]struct {
			e ecs.Entity
			g components.WorldPos
		}{{s.forestTruck, s.forestGoalTr}, {s.forestTank, s.forestGoalTk}} {
			if aq := s.VehQueueMap.Get(v.e); aq != nil {
				systems.ClearActions(aq)
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: v.g})
			}
		}
		s.orderFired = true
		return
	}
	inside := func(e ecs.Entity, rockOnly bool) bool {
		p := s.PosMap.Get(e)
		if p == nil {
			return false
		}
		wx := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		wz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		for i := range s.forestProps {
			rec := &s.forestProps[i]
			if rockOnly && !rec.rock {
				continue
			}
			if !s.World.Alive(rec.ent) {
				continue
			}
			dx, dz := wx-rec.x, wz-rec.z
			if dx*dx+dz*dz < rec.r*rec.r {
				return true
			}
		}
		return false
	}
	if inside(s.forestTruck, false) {
		s.forestTruckBad++
	}
	if inside(s.forestTank, true) {
		s.forestRockBad++
	}
	crushed := 0
	for i := range s.forestProps {
		if !s.forestProps[i].rock && !s.World.Alive(s.forestProps[i].ent) {
			crushed++
		}
	}
	arrivedN := 0
	for _, v := range [2]struct {
		e ecs.Entity
		g components.WorldPos
	}{{s.forestTruck, s.forestGoalTr}, {s.forestTank, s.forestGoalTk}} {
		if p := s.PosMap.Get(v.e); p != nil {
			d := p.Sub(v.g)
			if d.X*d.X+d.Z*d.Z < 36 {
				arrivedN++
			}
		}
	}
	arrived := arrivedN == 2 && elapsed > s.orderAt+5
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt && !arrived {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs arrived=%d/2 truckBad=%d rockBad=%d crushed=%d\n",
			s.sceneID, elapsed, arrivedN, s.forestTruckBad, s.forestRockBad, crushed)
	}
	if arrived || elapsed >= s.verdictAt {
		pass := arrived && s.forestTruckBad == 0 && s.forestRockBad == 0 && crushed >= 1
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%d/2 t=%.1fs truckBad=%d rockBad=%d crushed=%d)\n",
			s.sceneID, verdict, arrivedN, elapsed, s.forestTruckBad, s.forestRockBad, crushed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateVehSlope (MB1): stamp the ridge once the chunks are live, order the
// tank across, count hull ticks on impassable grade.
func (s *aiTestState) updateVehSlope(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.slopeStamped {
		if elapsed < 1.2 {
			return
		}
		for z := float32(-14); z <= 6; z += 4 {
			c := components.WorldPos{}.Add(rl.Vector3{X: 48, Z: z})
			s.Stamper.StampHeightmap(c, systems.Crater(-7, 5), 5)
		}
		s.slopeStamped = true
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE SLOPE: tank=%v ridge x=48 z[-19..11]\n", s.slopeTank)
		fmt.Println("============================================================")
		if aq := s.VehQueueMap.Get(s.slopeTank); aq != nil {
			systems.ClearActions(aq)
			systems.PushAction(aq, components.Action{
				Kind: components.ActionMoveTo, Target: s.slopeGoal})
		}
		s.orderFired = true
		return
	}
	p := s.PosMap.Get(s.slopeTank)
	if p == nil {
		return
	}
	wx := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
	wz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
	gx := (s.sampler.Sample(wx+1, wz) - s.sampler.Sample(wx-1, wz)) * 0.5
	gz := (s.sampler.Sample(wx, wz+1) - s.sampler.Sample(wx, wz-1)) * 0.5
	if gx < 0 {
		gx = -gx
	}
	if gz < 0 {
		gz = -gz
	}
	if gx >= 0.6 || gz >= 0.6 {
		s.slopeSteepTicks++
	}
	d := p.Sub(s.slopeGoal)
	distSq := d.X*d.X + d.Z*d.Z
	arrived := distSq < 36 && elapsed > s.orderAt+3
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt && !arrived {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs at=(%.1f,%.1f) distGoal=%.1f steep=%d\n",
			s.sceneID, elapsed, wx, wz, float32(math.Sqrt(float64(distSq))), s.slopeSteepTicks)
	}
	if arrived || elapsed >= s.verdictAt {
		pass := arrived && s.slopeSteepTicks == 0
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%v t=%.1fs steep=%d)\n",
			s.sceneID, verdict, arrived, elapsed, s.slopeSteepTicks)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateCrowdCross (MA3): straight march through a standing crowd. Verdict =
// arrival + max pairwise penetration ≤ 0.05 m + bounded per-tick |dv|.
func (s *aiTestState) updateCrowdCross(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI CROWD CROSS: %s  squad=%v crowd=%d\n",
			s.sceneID, s.squad, len(s.crowdEnts))
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.squad, components.OrderKindMoveTo,
			s.crowdGoal, ecs.Entity{}, false, systems.OrderParams{})
		s.orderFired = true
		return
	}
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return
	}
	// Metrics accrue past the shake-out window (alignment precedent): the
	// scene's subject is the crowd crossing, and the start-line lane sort has
	// its own transient contacts before anyone reaches the crowd.
	if elapsed < s.orderAt+4 {
		return
	}
	var xs, zs [24]float32
	ents := make([]ecs.Entity, 0, 20)
	n := 0
	collect := func(e ecs.Entity) {
		if e == (ecs.Entity{}) || !s.World.Alive(e) || n >= len(xs) {
			return
		}
		p := s.PosMap.Get(e)
		if p == nil {
			return
		}
		xs[n] = float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		zs[n] = float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		ents = append(ents, e)
		n++
	}
	for i := uint8(0); i < roster.Count; i++ {
		collect(roster.Members[i])
	}
	for _, e := range s.crowdEnts {
		collect(e)
	}
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			dx, dz := xs[a]-xs[b], zs[a]-zs[b]
			// Collider radius is 0.35 (UnitFactory) — contact ring 0.7.
			pen := 0.7 - float32(math.Sqrt(float64(dx*dx+dz*dz)))
			if pen > s.crowdMaxPen {
				s.crowdMaxPen = pen
				if os.Getenv("RTS_CROWD_DUMP") != "" && pen > 0.05 {
					fmt.Printf("[pen] t=%.2f %v-%v pen=%.3f at=(%.1f,%.1f)\n",
						elapsed, ents[a], ents[b], pen, xs[a], zs[a])
				}
			}
		}
	}
	for _, e := range ents {
		mot := s.MotionMap.Get(e)
		if mot == nil {
			continue
		}
		vx := float32(math.Sin(float64(mot.VelocityYaw))) * mot.Speed
		vz := float32(math.Cos(float64(mot.VelocityYaw))) * mot.Speed
		if prev, ok := s.crowdPrev[e]; ok {
			dx, dz := vx-prev[0], vz-prev[1]
			if dv := float32(math.Sqrt(float64(dx*dx + dz*dz))); dv > s.crowdMaxDv {
				s.crowdMaxDv = dv
				if os.Getenv("RTS_CROWD_DUMP") != "" && dv > 1.8 {
					p := s.PosMap.Get(e)
					fmt.Printf("[dv] t=%.2f ent=%v dv=%.2f v=(%.1f,%.1f)->(%.1f,%.1f) at=(%.1f,%.1f)\n",
						elapsed, e, dv, prev[0], prev[1], vx, vz,
						float32(p.Chunk.X)*components.ChunkSize+p.Local.X,
						float32(p.Chunk.Z)*components.ChunkSize+p.Local.Z)
				}
			}
		}
		s.crowdPrev[e] = [2]float32{vx, vz}
	}

	arrived := false
	if leader := roster.Members[0]; leader != (ecs.Entity{}) && s.World.Alive(leader) {
		if p := s.PosMap.Get(leader); p != nil {
			d := p.Sub(s.crowdGoal)
			arrived = d.X*d.X+d.Z*d.Z < 16 && elapsed > s.orderAt+5
		}
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt && !arrived {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs maxPen=%.3f maxDv=%.2f\n",
			s.sceneID, elapsed, s.crowdMaxPen, s.crowdMaxDv)
	}
	if arrived || elapsed >= s.verdictAt {
		pass := arrived && s.crowdMaxPen <= 0.05 && s.crowdMaxDv <= crowdDvMax
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%v t=%.1fs maxPen=%.3f maxDv=%.2f replan=%.1f/u/min)\n",
			s.sceneID, verdict, arrived, elapsed, s.crowdMaxPen, s.crowdMaxDv,
			s.replanRate(roster, elapsed))
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// crowdDvMax — per-tick |dv| ceiling for ai_crowd_cross.
const crowdDvMax float32 = 2.0

// escapeFrac — squad-wide ratio of escape-spring ticks to MoveTo steering
// ticks (MicroPath telemetry, MA2).
func (s *aiTestState) escapeFrac(roster *components.CommandRoster) float32 {
	var esc, move float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		if mp := s.MicroPathMap.Get(mem); mp != nil {
			esc += float32(mp.EscapeTicks)
			move += float32(mp.MoveTicks)
		}
	}
	if move == 0 {
		return 0
	}
	return esc / move
}

// rectOutsideDist — distance from (x, z) to the rect's boundary: positive
// outside, negative penetration depth inside.
func rectOutsideDist(r components.AABB2D, x, z float32) float32 {
	var dx, dz float32
	if x < r.MinX {
		dx = r.MinX - x
	} else if x > r.MaxX {
		dx = x - r.MaxX
	}
	if z < r.MinZ {
		dz = r.MinZ - z
	} else if z > r.MaxZ {
		dz = z - r.MaxZ
	}
	if dx == 0 && dz == 0 {
		pen := x - r.MinX
		if v := r.MaxX - x; v < pen {
			pen = v
		}
		if v := z - r.MinZ; v < pen {
			pen = v
		}
		if v := r.MaxZ - z; v < pen {
			pen = v
		}
		return -pen
	}
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
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

	// Arrival keys on the LEADER (MA1 precedent): order completion is
	// anchor-based, so the parked line's centroid legitimately sits at the
	// leader's rank, up to the completion radius short of the point.
	arrived := false
	if leader := roster.Members[0]; leader != (ecs.Entity{}) && s.World.Alive(leader) {
		if p := s.PosMap.Get(leader); p != nil {
			d := p.Sub(s.marchGoal)
			arrived = d.X*d.X+d.Z*d.Z < 16
		}
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
		fmt.Printf("[ai-test %s] t=%.1fs side=%.3f churn=%.0fdeg clear=[%.2f..%.2f] replan=%.1f/u/min\n",
			s.sceneID, elapsed, sideFrac, s.marchChurnDeg, s.marchMinClear, s.marchMaxClear,
			s.replanRate(roster, elapsed))
	}

	if arrived || elapsed >= s.verdictAt {
		sideFrac := float32(1)
		if s.marchMoveN > 0 {
			sideFrac = float32(s.marchSideN) / float32(s.marchMoveN)
		}
		replanRate := s.replanRate(roster, elapsed)
		// Post-fix baseline: line 0deg side 0.01-0.03 clear ±0.14; slope
		// 88deg side 0.02-0.05 clear -0.22..0.18. Pre-fix: churn 410deg,
		// side-storms, clear -0.32 (and unbounded on far solo orders).
		pass := arrived &&
			sideFrac <= 0.08 &&
			s.marchChurnDeg <= s.marchChurnMax &&
			s.marchMinClear >= s.marchClearFloor &&
			s.marchMaxClear <= 0.5 &&
			(s.marchReplanMax == 0 || replanRate <= s.marchReplanMax)
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%v t=%.1fs side=%.3f churn=%.0fdeg clear=[%.2f..%.2f] replan=%.1f/u/min)\n",
			s.sceneID, verdict, arrived, elapsed, sideFrac, s.marchChurnDeg,
			s.marchMinClear, s.marchMaxClear, replanRate)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// replanRate returns churn replans per unit per minute past the 5 s
// alignment window, over live roster members + soloists.
func (s *aiTestState) replanRate(roster *components.CommandRoster, elapsed float32) float32 {
	var total, units float32
	count := func(e ecs.Entity) {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			return
		}
		if mp := s.MicroPathMap.Get(e); mp != nil {
			total += float32(mp.ReplanCount)
			units++
		}
	}
	if roster != nil {
		for i := uint8(0); i < roster.Count; i++ {
			count(roster.Members[i])
		}
	}
	for _, e := range s.soloEnts {
		count(e)
	}
	if units == 0 {
		return 0
	}
	if elapsed < s.orderAt+5 {
		return 0
	}
	if !s.replanBaseSet {
		s.replanBaseSet = true
		s.marchReplanBase = total
	}
	minutes := (elapsed - s.orderAt - 5) / 60
	if minutes <= 0.01 {
		return 0
	}
	return (total - s.marchReplanBase) / units / minutes
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

// updateVehAvoid drives the M6 collision scenes: per-vehicle goals, per-tick
// metrics (pairwise clearance / footprint intrusion / infantry inside a hull
// / reverse ticks), verdict on all-arrived or timeout.
// updateConvoyRoad (MB3): one squad MoveTo across the river. PASS = all three
// hulls parked at the goal, the column actually rode the carriageway (deck
// ticks) and crossed on the bridge — off-road the water refusal stops it, so
// a swim is not an alternative route but a livelock.
func (s *aiTestState) updateConvoyRoad(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE CONVOY ROAD: squad=%v members=%d goal=east of river\n",
			s.convoySquad, len(s.vehEnts))
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.convoySquad, components.OrderKindMoveTo,
			s.convoyGoal, ecs.Entity{}, false, systems.OrderParams{})
		s.orderFired = true
		return
	}
	g := s.vehGraphRes.Get()
	onRoad := 0
	for _, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		f := s.VehFollowerMap.Get(v)
		if f == nil || f.Edge < 0 || g == nil || int(f.Edge) >= len(g.Edges) {
			continue
		}
		onRoad++
		if g.Edges[f.Edge].Kind == components.RoadBridge {
			s.vehBridgeTicks++
		}
	}
	if onRoad >= 2 {
		s.convoyRoadTicks++
	}

	// Arrival is the ORDER completing (anchor-based, invariant 58) — a Column
	// legitimately parks its tail a column depth behind the goal, so a
	// per-member radius would never close. The tail is checked separately:
	// nobody may be left on the far bank.
	maxDist := float32(0)
	for _, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		if p := s.PosMap.Get(v); p != nil {
			d := p.Sub(s.convoyGoal)
			if dist := d.X*d.X + d.Z*d.Z; dist > maxDist {
				maxDist = dist
			}
		}
	}
	maxDist = float32(math.Sqrt(float64(maxDist)))
	arrived := 0
	if h := s.OQMap.Get(s.convoySquad); h != nil && h.First == (ecs.Entity{}) {
		arrived = len(s.vehEnts)
		if s.convoyRoadArrive == 0 && maxDist < 45 {
			s.convoyRoadArrive = elapsed
		}
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs done=%v maxDist=%.1f roadTicks=%d bridge=%d",
			s.sceneID, elapsed, arrived == len(s.vehEnts), maxDist,
			s.convoyRoadTicks, s.vehBridgeTicks)
		for i, v := range s.vehEnts {
			p := s.PosMap.Get(v)
			if p == nil {
				continue
			}
			edge := int32(-1)
			if f := s.VehFollowerMap.Get(v); f != nil {
				edge = f.Edge
			}
			fmt.Printf(" m%d=(%.1f,%.1f,e%d)", i,
				float32(p.Chunk.X)*components.ChunkSize+p.Local.X,
				float32(p.Chunk.Z)*components.ChunkSize+p.Local.Z, edge)
		}
		fmt.Println()
	}
	if s.convoyRoadArrive > 0 || elapsed >= s.verdictAt {
		pass := s.convoyRoadArrive > 0 && s.convoyRoadTicks >= 300 && s.vehBridgeTicks > 0
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (done=%v maxDist=%.1f t=%.1fs roadTicks=%d bridge=%d)\n",
			s.sceneID, verdict, s.convoyRoadArrive > 0, maxDist, elapsed,
			s.convoyRoadTicks, s.vehBridgeTicks)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// aiSightBlocked reports the terrain between (ax,az) and (bx,bz) rising above
// the eye line — the cover bake's own test, so the defilade verdict does not
// depend on where the bake happened to store its mask.
func aiSightBlocked(sampler *systems.HeightSampler, ax, az, bx, bz float32) bool {
	if sampler == nil {
		return false
	}
	const eye = 1.7
	dx, dz := bx-ax, bz-az
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if dist < 1 {
		return false
	}
	ay := sampler.Sample(ax, az) + eye
	by := sampler.Sample(bx, bz) + eye
	for t := float32(2); t < dist; t += 2 {
		f := t / dist
		gx, gz := ax+dx*f, az+dz*f
		if sampler.Sample(gx, gz) > ay+(by-ay)*f+0.3 {
			return true
		}
	}
	return false
}

// updateCoverPos (MC2): pulse synthetic fire from one bearing and check that
// the scorer took the one good answer the scene planted — or, in _none, that
// the fallback got the men out of the fire lane instead of freezing them.
func (s *aiTestState) updateCoverPos(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI COVER POSITION: %s  units=%d shooter=(%.0f,%.0f)\n",
			s.sceneID, len(s.covUnits),
			float32(s.covShooter.Chunk.X)*components.ChunkSize+s.covShooter.Local.X,
			float32(s.covShooter.Chunk.Z)*components.ChunkSize+s.covShooter.Local.Z)
		fmt.Println("============================================================")
		s.orderFired = true
		s.covNextInj = elapsed
		if s.sceneID == aiSceneCoverDefilade {
			// The scene is only a test if the men START in the open: a spawn
			// already in defilade would pass without anyone deciding anything.
			for _, u := range s.covUnits {
				p := s.PosMap.Get(u)
				if p == nil {
					continue
				}
				ux := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
				uz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
				if aiSightBlocked(s.sampler, ux, uz,
					float32(s.covShooter.Chunk.X)*components.ChunkSize+s.covShooter.Local.X,
					float32(s.covShooter.Chunk.Z)*components.ChunkSize+s.covShooter.Local.Z) {
					fmt.Printf("[ai-test %s] SPAWN ALREADY IN DEFILADE at (%.1f,%.1f) — aborting\n",
						s.sceneID, ux, uz)
					s.verdictDone = true
					return
				}
			}
		}
	}
	if elapsed >= s.covNextInj && elapsed <= s.covStopAt {
		s.covNextInj = elapsed + 0.5
		for _, u := range s.covUnits {
			if u == (ecs.Entity{}) || !s.World.Alive(u) {
				continue
			}
			if buf := s.DangerMap.Get(u); buf != nil {
				components.PushDanger(buf, components.DangerEvent{
					Kind:     components.DangerBulletImpact,
					Pos:      s.covShooter,
					Strength: 0.35,
					Time:     elapsed,
				})
			}
		}
	}

	// Per-scene success test, latched: reaching the answer once is the
	// behaviour under test; drifting later is a different milestone.
	good := 0
	proneNow := false
	var lateralMax float32
	sx := float32(s.covShooter.Chunk.X)*components.ChunkSize + s.covShooter.Local.X
	sz := float32(s.covShooter.Chunk.Z)*components.ChunkSize + s.covShooter.Local.Z
	for _, u := range s.covUnits {
		if u == (ecs.Entity{}) || !s.World.Alive(u) {
			continue
		}
		p := s.PosMap.Get(u)
		if p == nil {
			continue
		}
		ux := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		uz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		switch s.sceneID {
		case aiSceneCoverTrench:
			if dz := uz - s.covRefZ; dz > -3 && dz < 3 {
				good++
			}
			// Report how deep the ditch actually holds a man (the
			// GroundStick-on-Modified tail the plan asks to verify).
			if s.sampler != nil {
				if sink := s.sampler.Sample(ux, uz+8) - p.Local.Y; sink > s.covNote {
					s.covNote = sink
				}
			}
		case aiSceneCoverHull:
			dx, dz := ux-s.covRefX, uz-s.covRefZ
			if uz < s.covRefZ && dx*dx+dz*dz < 36 {
				good++
			}
		case aiSceneCoverDefilade:
			// In defilade = the ground breaks the line from the shooter to
			// this man's head — the same predicate the cover bake uses.
			if aiSightBlocked(s.sampler, ux, uz, sx, sz) {
				good++
			}
			startX := float32(s.covStart.Chunk.X)*components.ChunkSize + s.covStart.Local.X
			startZ := float32(s.covStart.Chunk.Z)*components.ChunkSize + s.covStart.Local.Z
			if d := float32(math.Sqrt(float64((ux-startX)*(ux-startX) +
				(uz-startZ)*(uz-startZ)))); d > s.covNote {
				s.covNote = d
			}
		case aiSceneCoverNone:
			if st := s.StanceMap.Get(u); st != nil && st.Code == components.StanceProne {
				proneNow = true
			}
			// Lateral = displacement perpendicular to the shooter bearing.
			bx, bz := ux-sx, uz-sz
			bl := float32(math.Sqrt(float64(bx*bx + bz*bz)))
			if bl > 1e-3 {
				startX := float32(s.covStart.Chunk.X)*components.ChunkSize + s.covStart.Local.X
				startZ := float32(s.covStart.Chunk.Z)*components.ChunkSize + s.covStart.Local.Z
				mx, mz := ux-startX, uz-startZ
				lat := mx*(-bz/bl) + mz*(bx/bl)
				if lat < 0 {
					lat = -lat
				}
				if lat > lateralMax {
					lateralMax = lat
				}
			}
		}
	}
	if proneNow {
		s.covProne = true
	}
	if lateralMax > s.covLateral {
		s.covLateral = lateralMax
	}
	if s.sceneID == aiSceneCoverNone {
		if s.covProne && s.covLateral >= 5 {
			s.covOK = true
		}
	} else if good == len(s.covUnits) {
		s.covOK = true
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs good=%d/%d ok=%v note=%.2f prone=%v lat=%.1f",
			s.sceneID, elapsed, good, len(s.covUnits), s.covOK,
			s.covNote, s.covProne, s.covLateral)
		for _, u := range s.covUnits {
			if p := s.PosMap.Get(u); p != nil {
				ovr := -1
				if ov := s.OverrideMap.Get(u); ov != nil {
					ovr = int(ov.Reason)
				}
				fmt.Printf(" u=(%.1f,%.1f,r%d)",
					float32(p.Chunk.X)*components.ChunkSize+p.Local.X,
					float32(p.Chunk.Z)*components.ChunkSize+p.Local.Z, ovr)
			}
		}
		fmt.Println()
	}
	if s.covOK || elapsed >= s.verdictAt {
		slots := 0
		q := s.SlotFilter.Query()
		for q.Next() {
			sp, _ := q.Get()
			d := sp.Sub(s.covStart)
			if d.X*d.X+d.Z*d.Z < 900 {
				slots++
			}
		}
		q.Close()
		verdict := "FAIL"
		if s.covOK {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (ok=%v good=%d/%d slotsNear=%d note=%.2f prone=%v lat=%.1f t=%.1fs)\n",
			s.sceneID, verdict, s.covOK, good, len(s.covUnits), slots,
			s.covNote, s.covProne, s.covLateral, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateShellfire (MC1): DefendPosition + a walking barrage. PASS = a zone
// was raised, ≥80% of the roster left it within 10 s of the first shell, the
// order never left InProgress, and ≥80% are back on the held position within
// 30 s of the last shell.
func (s *aiTestState) updateShellfire(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI SHELLFIRE: squad=%v holds (40,40), barrage %.0f..%.0fs\n",
			s.shellSquad, s.orderAt+2, s.shellStopAt)
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.shellSquad, components.OrderKindDefendPosition,
			s.shellHold, ecs.Entity{}, false, systems.OrderParams{})
		s.orderFired = true
		s.shellNextAt = s.orderAt + 2
		return
	}
	// Walk shells across the held position: alternating offsets so the
	// aggregator sees a barrage, not one crater.
	if elapsed >= s.shellNextAt && elapsed < s.shellStopAt {
		s.shellNextAt = elapsed + 1.0
		off := float32(s.shellShots%3)*4 - 4
		centre := s.shellCentre.Add(rl.Vector3{X: off, Z: -off})
		systems.SpawnBlastMark(s.World, s.PosMap, s.BlastMap, centre, 8, 0.8, elapsed)
		if roster := s.RosterMap.Get(s.shellSquad); roster != nil {
			for i := uint8(0); i < roster.Count; i++ {
				mem := roster.Members[i]
				if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
					continue
				}
				if buf := s.DangerMap.Get(mem); buf != nil {
					components.PushDanger(buf, components.DangerEvent{
						Kind:     components.DangerExplosion,
						Pos:      centre,
						Strength: 0.5,
						Time:     elapsed,
					})
				}
			}
		}
		s.shellShots++
	}

	// Live zone (radius drives the vacate metric).
	zoneR := float32(0)
	var zoneX, zoneZ float32
	qz := s.UnsafeFilter.Query()
	for qz.Next() {
		area, pos := qz.Get()
		if area.Radius > zoneR {
			zoneR = area.Radius
			zoneX = float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
			zoneZ = float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		}
	}
	qz.Close()
	if zoneR > 0 {
		s.shellSawZone = true
		s.shellZoneR = zoneR
	} else {
		zoneX = float32(s.shellCentre.Chunk.X)*components.ChunkSize + s.shellCentre.Local.X
		zoneZ = float32(s.shellCentre.Chunk.Z)*components.ChunkSize + s.shellCentre.Local.Z
		zoneR = s.shellZoneR
	}

	// The player's order must survive the whole barrage.
	live := false
	if h := s.OQMap.Get(s.shellSquad); h != nil && h.First != (ecs.Entity{}) &&
		s.World.Alive(h.First) {
		if st := s.StateMap.Get(h.First); st != nil &&
			st.Code == components.OrderStateInProgress {
			live = true
		}
	}
	if !live && elapsed > s.orderAt+1 {
		s.shellOrderBroke = true
	}

	roster := s.RosterMap.Get(s.shellSquad)
	if roster == nil {
		return
	}
	var total, outside, home float32
	hx := float32(s.shellHold.Chunk.X)*components.ChunkSize + s.shellHold.Local.X
	hz := float32(s.shellHold.Chunk.Z)*components.ChunkSize + s.shellHold.Local.Z
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		p := s.PosMap.Get(mem)
		if p == nil {
			continue
		}
		total++
		mx := float32(p.Chunk.X)*components.ChunkSize + p.Local.X
		mz := float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
		if dx, dz := mx-zoneX, mz-zoneZ; zoneR <= 0 || dx*dx+dz*dz > zoneR*zoneR {
			outside++
		}
		// "Home" = inside the formation footprint around the held point.
		if dx, dz := mx-hx, mz-hz; dx*dx+dz*dz < 20*20 {
			home++
		}
	}
	if total > 0 {
		if s.shellVacatedAt == 0 && s.shellSawZone && outside/total >= 0.8 {
			s.shellVacatedAt = elapsed
		}
		if s.shellVacatedAt > 0 && s.shellReturnedAt == 0 &&
			elapsed > s.shellStopAt && home/total >= 0.8 {
			s.shellReturnedAt = elapsed
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		ovN, ovShell := 0, 0
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
				continue
			}
			if ov := s.OverrideMap.Get(mem); ov != nil {
				ovN++
				if ov.Reason == components.TacticalOverrideShellfire {
					ovShell++
				}
			}
		}
		fmt.Printf("[ai-test %s] t=%.1fs shells=%d zoneR=%.1f outside=%.0f/%.0f home=%.0f ov=%d(shell=%d) vacated=%.1f returned=%.1f order=%v\n",
			s.sceneID, elapsed, s.shellShots, zoneR, outside, total, home,
			ovN, ovShell, s.shellVacatedAt, s.shellReturnedAt, live)
	}

	if s.shellReturnedAt > 0 || elapsed >= s.verdictAt {
		vacateOK := s.shellVacatedAt > 0 && s.shellVacatedAt <= s.orderAt+2+10
		returnOK := s.shellReturnedAt > 0 && s.shellReturnedAt <= s.shellStopAt+30
		pass := s.shellSawZone && vacateOK && returnOK && !s.shellOrderBroke
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (zone=%v vacatedAt=%.1f returnedAt=%.1f orderHeld=%v shells=%d t=%.1fs)\n",
			s.sceneID, verdict, s.shellSawZone, s.shellVacatedAt, s.shellReturnedAt,
			!s.shellOrderBroke, s.shellShots, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateVehFlee (MB3): the rear truck of a marching squad takes fire from the
// north it cannot answer. It must actually retreat southward (the march runs
// east, so southward displacement is flee-only), the player's order must
// survive the reflex, and the truck must rejoin and finish the march.
func (s *aiTestState) updateVehFlee(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE FLEE: squad=%v truck=%v (unarmed, fire from north)\n",
			s.fleeSquad, s.fleeTruck)
		fmt.Println("============================================================")
		s.SquadService.IssueOrder(s.fleeSquad, components.OrderKindMoveTo,
			s.fleeGoal, ecs.Entity{}, false, systems.OrderParams{})
		s.orderFired = true
		s.fleeNextInj = elapsed + 2
		return
	}
	if elapsed >= s.fleeNextInj && elapsed <= s.orderAt+12 {
		s.fleeNextInj = elapsed + 0.5
		if buf := s.DangerMap.Get(s.fleeTruck); buf != nil {
			components.PushDanger(buf, components.DangerEvent{
				Kind:     components.DangerBulletImpact,
				Pos:      s.fleeSource,
				Strength: 0.35,
				Time:     elapsed,
			})
		}
	}

	reflexing := false
	if ov := s.OverrideVMap.Get(s.fleeTruck); ov != nil &&
		ov.Kind != components.VehicleReflexNone {
		reflexing = true
		s.fleeSawReflex = true
	}
	if p := s.PosMap.Get(s.fleeTruck); p != nil {
		if away := s.fleeSpawn.Sub(*p).Z; away > s.fleeMaxDist {
			s.fleeMaxDist = away
		}
		if d := p.Sub(s.fleeGoal); d.X*d.X+d.Z*d.Z < 400 {
			s.fleeResumed = true
		}
	}
	// The order must outlive the retreat: sampled on the tick the override
	// releases, when the pre-P8-f code would have left a wiped queue behind.
	if s.fleeSawReflex && !reflexing && !s.fleeOrderKept {
		if h := s.OQMap.Get(s.fleeSquad); h != nil && h.First != (ecs.Entity{}) &&
			s.World.Alive(h.First) {
			s.fleeOrderKept = true
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs reflex=%v away=%.1fm orderKept=%v resumed=%v\n",
			s.sceneID, elapsed, reflexing, s.fleeMaxDist, s.fleeOrderKept, s.fleeResumed)
	}
	done := s.fleeSawReflex && s.fleeOrderKept && s.fleeResumed && s.fleeMaxDist >= 20
	if done || elapsed >= s.verdictAt {
		verdict := "FAIL"
		if done {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (reflex=%v away=%.1fm orderKept=%v resumed=%v t=%.1fs)\n",
			s.sceneID, verdict, s.fleeSawReflex, s.fleeMaxDist,
			s.fleeOrderKept, s.fleeResumed, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateVehStuck (MB2): truck A must be FAILED by the driver watchdog (queue
// cleared + reason in the EventLog) within 20 s of the order; truck B must
// arrive past the pair with a committed (non-flickering) detour side and
// neither hull may enter a footprint.
func (s *aiTestState) updateVehStuck(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE STUCK: failT=%v (goal in sealed slit) freeT=%v\n",
			s.stuckFailT, s.stuckFreeT)
		fmt.Println("============================================================")
		for _, v := range [2]struct {
			e ecs.Entity
			g components.WorldPos
		}{{s.stuckFailT, s.stuckGoalFail}, {s.stuckFreeT, s.stuckGoalFree}} {
			if aq := s.VehQueueMap.Get(v.e); aq != nil {
				systems.ClearActions(aq)
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: v.g})
			}
		}
		s.orderFired = true
		return
	}
	hull := func(e ecs.Entity) (float32, float32, bool) {
		if e == (ecs.Entity{}) || !s.World.Alive(e) {
			return 0, 0, false
		}
		p := s.PosMap.Get(e)
		if p == nil {
			return 0, 0, false
		}
		return float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
			float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z, true
	}
	for _, e := range [2]ecs.Entity{s.stuckFailT, s.stuckFreeT} {
		if x, z, ok := hull(e); ok {
			for _, fp := range s.stuckFoots {
				if x >= fp.MinX && x <= fp.MaxX && z >= fp.MinZ && z <= fp.MaxZ {
					s.stuckFootBad++
				}
			}
		}
	}
	if f := s.VehFollowerMap.Get(s.stuckFreeT); f != nil && f.AvoidSide != 0 {
		if s.stuckLastSide != 0 && f.AvoidSide != s.stuckLastSide {
			s.stuckFlips++
		}
		s.stuckLastSide = f.AvoidSide
	}
	if s.stuckFailedAt == 0 {
		if aq := s.VehQueueMap.Get(s.stuckFailT); aq != nil && aq.Count == 0 {
			s.stuckFailedAt = elapsed
		}
	}
	if s.stuckFreedAt == 0 {
		if aq := s.VehQueueMap.Get(s.stuckFreeT); aq != nil && aq.Count == 0 {
			if x, z, ok := hull(s.stuckFreeT); ok {
				gx := float32(s.stuckGoalFree.Chunk.X)*components.ChunkSize + s.stuckGoalFree.Local.X
				gz := float32(s.stuckGoalFree.Chunk.Z)*components.ChunkSize + s.stuckGoalFree.Local.Z
				if dx, dz := x-gx, z-gz; dx*dx+dz*dz < 225 {
					s.stuckFreedAt = elapsed
				}
			}
		}
	}
	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		ax, az, _ := hull(s.stuckFailT)
		bx, bz, _ := hull(s.stuckFreeT)
		fmt.Printf("[ai-test %s] t=%.1fs A=(%.1f,%.1f) B=(%.1f,%.1f) failedAt=%.1f freedAt=%.1f foot=%d flips=%d\n",
			s.sceneID, elapsed, ax, az, bx, bz,
			s.stuckFailedAt, s.stuckFreedAt, s.stuckFootBad, s.stuckFlips)
	}
	if (s.stuckFailedAt > 0 && s.stuckFreedAt > 0) || elapsed >= s.verdictAt {
		reason := false
		if log := s.stuckEvRes.Get(); log != nil {
			for _, e := range log.Latest(10) {
				if e.Kind == components.EventOrderFailed && e.Text == "Vehicle stuck: no path" {
					reason = true
				}
			}
		}
		pass := s.stuckFailedAt > 0 && s.stuckFailedAt <= s.orderAt+20 &&
			s.stuckFreedAt > 0 && reason &&
			s.stuckFootBad == 0 && s.stuckFlips <= 3
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (failedAt=%.1f freedAt=%.1f reason=%v foot=%d flips=%d t=%.1fs)\n",
			s.sceneID, verdict, s.stuckFailedAt, s.stuckFreedAt, reason,
			s.stuckFootBad, s.stuckFlips, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

func (s *aiTestState) updateVehAvoid(elapsed float32) {
	if s.verdictDone {
		return
	}
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE AVOID SCENE: %s  vehicles=%d\n", s.sceneID, len(s.vehEnts))
		fmt.Println("============================================================")
		for i, v := range s.vehEnts {
			if aq := s.VehQueueMap.Get(v); aq != nil {
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: s.avoidGoals[i],
				})
			}
		}
		s.orderFired = true
		return
	}

	type xz struct{ x, z float32 }
	hulls := make([]xz, len(s.vehEnts))
	for i, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			hulls[i] = xz{1e9, 1e9}
			continue
		}
		p := s.PosMap.Get(v)
		if p == nil {
			hulls[i] = xz{1e9, 1e9}
			continue
		}
		hulls[i] = xz{
			float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
			float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z,
		}
	}
	for i := 0; i < len(hulls); i++ {
		for j := i + 1; j < len(hulls); j++ {
			dx, dz := hulls[j].x-hulls[i].x, hulls[j].z-hulls[i].z
			clear := float32(math.Sqrt(float64(dx*dx+dz*dz))) -
				s.avoidRadii[i] - s.avoidRadii[j]
			if clear < s.avoidMinPair {
				s.avoidMinPair = clear
			}
		}
	}
	for i := range hulls {
		for _, fp := range s.avoidFoots {
			if hulls[i].x >= fp.MinX && hulls[i].x <= fp.MaxX &&
				hulls[i].z >= fp.MinZ && hulls[i].z <= fp.MaxZ {
				s.avoidFootBad++
			}
		}
		for _, u := range s.avoidYield {
			if u == (ecs.Entity{}) || !s.World.Alive(u) {
				continue
			}
			up := s.PosMap.Get(u)
			if up == nil {
				continue
			}
			ux := float32(up.Chunk.X)*components.ChunkSize + up.Local.X
			uz := float32(up.Chunk.Z)*components.ChunkSize + up.Local.Z
			dx, dz := ux-hulls[i].x, uz-hulls[i].z
			lim := s.avoidRadii[i] - 0.3
			if dx*dx+dz*dz < lim*lim {
				s.avoidYieldBad++
			}
		}
	}
	if s.avoidRevFree > 0 && elapsed >= s.avoidRevFree {
		for _, v := range s.vehEnts {
			if m := s.MotionMap.Get(v); m != nil && m.Speed < -0.05 {
				s.avoidRevTicks++
			}
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		for i := range hulls {
			fmt.Printf("[ai-test %s] t=%.1fs veh%d=(%.1f,%.1f) minPair=%.2f foot=%d yield=%d rev=%d\n",
				s.sceneID, elapsed, i, hulls[i].x, hulls[i].z,
				s.avoidMinPair, s.avoidFootBad, s.avoidYieldBad, s.avoidRevTicks)
		}
	}

	arrived := 0
	for i, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		aq := s.VehQueueMap.Get(v)
		if aq == nil || aq.Count != 0 {
			continue
		}
		g := s.avoidGoals[i]
		gx := float32(g.Chunk.X)*components.ChunkSize + g.Local.X
		gz := float32(g.Chunk.Z)*components.ChunkSize + g.Local.Z
		dx, dz := hulls[i].x-gx, hulls[i].z-gz
		// 15 m: TurnRadius-scaled arrival + goalCrowded adjacent parking.
		if dx*dx+dz*dz < 225 {
			arrived++
		}
	}
	if arrived == len(s.vehEnts) || elapsed >= s.verdictAt {
		pass := arrived == len(s.vehEnts) &&
			s.avoidMinPair >= -0.3 &&
			s.avoidFootBad == 0 &&
			s.avoidYieldBad == 0 &&
			s.avoidRevTicks == 0
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%d/%d minPair=%.2f foot=%d yield=%d rev=%d t=%.1fs)\n",
			s.sceneID, verdict, arrived, len(s.vehEnts),
			s.avoidMinPair, s.avoidFootBad, s.avoidYieldBad, s.avoidRevTicks, elapsed)
		fmt.Println("============================================================")
		s.verdictDone = true
	}
}

// updateConvoy (M7): one squad MoveTo; the metric is the max pairwise member
// distance over the whole march — without pacing the BMP leader (11 m/s)
// leaves the trucks (6 m/s) ~60 m behind, with pacing the column stays inside
// formation depth + drift.
func (s *aiTestState) updateConvoy(elapsed float32) {
	if s.verdictDone {
		return
	}
	// All scene-driven sim mutations happen BEFORE the saveload gate's
	// SAVE_AT tick (1000): the loaded run has no scene harness, so a
	// post-save mutation can never replay (SAVELOAD MISMATCH). Hence the
	// order: reform in place first (t=2), march second (fixed t=12).
	if !s.orderFired {
		if elapsed < s.orderAt {
			return
		}
		fmt.Println("============================================================")
		fmt.Printf("== AI VEHICLE CONVOY SCENE: %s  members=%d\n", s.sceneID, len(s.vehEnts))
		fmt.Println("============================================================")
		var cs components.FormationCustomSlots
		cs.Slots[0] = rl.Vector2{X: 0, Y: 0}
		cs.Slots[1] = rl.Vector2{X: -24, Y: 0}
		cs.Slots[2] = rl.Vector2{X: 24, Y: 0}
		if s.CSMap.Has(s.convoySquad) {
			*s.CSMap.Get(s.convoySquad) = cs
		} else {
			s.CSMap.Add(s.convoySquad, &cs)
		}
		if fd := s.FdMap.Get(s.convoySquad); fd != nil {
			fd.ReformPending = true
		}
		s.convoyPhase = 1
		fmt.Printf("[ai-test %s] t=%.1fs REFORM APPLIED (line abreast +/-24, no order)\n", s.sceneID, elapsed)
		s.orderFired = true
		return
	}

	type xz struct{ x, z float32 }
	pts := make([]xz, 0, len(s.vehEnts))
	for _, v := range s.vehEnts {
		if v == (ecs.Entity{}) || !s.World.Alive(v) {
			continue
		}
		p := s.PosMap.Get(v)
		if p == nil {
			continue
		}
		pts = append(pts, xz{
			float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
			float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z,
		})
	}
	// March-cohesion metric: only in phase 2, after a grace window that lets
	// the line-abreast start fold back into the column.
	if s.convoyPhase == 2 && elapsed > s.convoyMarchAt+12 {
		for i := 0; i < len(pts); i++ {
			for j := i + 1; j < len(pts); j++ {
				dx, dz := pts[j].x-pts[i].x, pts[j].z-pts[i].z
				if d := float32(math.Sqrt(float64(dx*dx + dz*dz))); d > s.convoyMaxSpread {
					s.convoyMaxSpread = d
				}
			}
		}
	}

	gx := float32(s.convoyGoal.Chunk.X)*components.ChunkSize + s.convoyGoal.Local.X
	gz := float32(s.convoyGoal.Chunk.Z)*components.ChunkSize + s.convoyGoal.Local.Z
	arrived := 0
	for _, p := range pts {
		dx, dz := p.x-gx, p.z-gz
		if dx*dx+dz*dz < 625 { // 25 m: column depth behind the parked leader
			arrived++
		}
	}

	if elapsed >= s.nextSampleAt && elapsed < s.verdictAt {
		s.nextSampleAt = elapsed + aiSampleEvery
		fmt.Printf("[ai-test %s] t=%.1fs ph=%d arrived=%d/%d spread=%.1f",
			s.sceneID, elapsed, s.convoyPhase, arrived, len(pts), s.convoyMaxSpread)
		for i, p := range pts {
			spd, qn := float32(0), uint8(0)
			if i < len(s.vehEnts) && s.World.Alive(s.vehEnts[i]) {
				if m := s.MotionMap.Get(s.vehEnts[i]); m != nil {
					spd = m.Speed
				}
				if aq := s.VehQueueMap.Get(s.vehEnts[i]); aq != nil {
					qn = aq.Count
				}
			}
			fmt.Printf(" v%d=(%.0f,%.0f|%.1f,q%d)", i, p.x, p.z, spd, qn)
		}
		rc := -1
		if s.World.Alive(s.convoySquad) {
			if r := s.RosterMap.Get(s.convoySquad); r != nil {
				rc = int(r.Count)
			}
		}
		fmt.Printf(" roster=%d\n", rc)
	}

	// Phase 1: watch the in-place reform succeed (line abreast formed
	// without any order), then at FIXED t=12 clear the custom layout and
	// order the march — a deterministic pre-save mutation time.
	if s.convoyPhase == 1 {
		if len(pts) == 3 && !s.convoyReformOK {
			d01 := dist2Dxz(pts[0].x, pts[0].z, pts[1].x, pts[1].z)
			d02 := dist2Dxz(pts[0].x, pts[0].z, pts[2].x, pts[2].z)
			d12 := dist2Dxz(pts[1].x, pts[1].z, pts[2].x, pts[2].z)
			if d12 > 32 && d01 > 15 && d02 > 15 {
				s.convoyReformOK = true
				fmt.Printf("[ai-test %s] t=%.1fs REFORM OK (d12=%.1f)\n", s.sceneID, elapsed, d12)
			}
		}
		if elapsed >= s.orderAt+10 {
			if s.CSMap.Has(s.convoySquad) {
				s.CSMap.Remove(s.convoySquad)
			}
			s.SquadService.OrderMoveTo(s.convoySquad, s.convoyGoal)
			s.convoyPhase = 2
			s.convoyMarchAt = elapsed
			fmt.Printf("[ai-test %s] t=%.1fs MARCH ORDERED (reform=%v)\n",
				s.sceneID, elapsed, s.convoyReformOK)
		}
		return
	}

	if (s.convoyPhase == 2 && arrived == len(pts) && len(pts) > 0 &&
		elapsed > s.convoyMarchAt+13) || elapsed >= s.verdictAt {
		pass := arrived == len(pts) && len(pts) > 0 &&
			s.convoyReformOK && s.convoyMaxSpread < 30 && s.convoyMergeOK
		verdict := "FAIL"
		if pass {
			verdict = "PASS"
		}
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (arrived=%d/%d reform=%v mergeClear=%v maxSpread=%.1f t=%.1fs)\n",
			s.sceneID, verdict, arrived, len(pts), s.convoyReformOK, s.convoyMergeOK, s.convoyMaxSpread, elapsed)
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
		// 13 m: arrival scales with TurnRadius since M6 and a shared goal
		// parks followers adjacent to the first hull (goalCrowded), not on
		// the point.
		if dx*dx+dz*dz < 169 {
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

// countRoomOccupancy tallies live members per roomBand (XZ in rect, Y within
// the storey band).
func (s *aiTestState) countRoomOccupancy() []int {
	counts := make([]int, len(s.roomBands))
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return counts
	}
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
		for b := range s.roomBands {
			band := &s.roomBands[b]
			if !band.room.Contains(mx, mz) {
				continue
			}
			dy := pos.Local.Y - band.minY
			if dy < -0.8 || dy > 0.8 {
				continue
			}
			counts[b]++
		}
	}
	return counts
}

// countHidden tallies live members that hold StanceCrouch and how many stand
// within park radius of a window fire slot — a hidden squad must show zero
// silhouettes in the openings.
func (s *aiTestState) countHidden() (crouched, atWindow, total int) {
	roster := s.RosterMap.Get(s.squad)
	if roster == nil {
		return 0, 0, 0
	}
	stanceMap := ecs.NewMap[components.Stance](s.World)
	var windows []systems.BuildingSlot
	if s.slotPlanner != nil {
		for _, slot := range s.slotPlanner.PlanSlots(s.targetBuilding,
			systems.SlotWindows, int(roster.Count), false, 0) {
			if slot.Window {
				windows = append(windows, slot)
			}
		}
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
			continue
		}
		total++
		if st := stanceMap.Get(mem); st != nil && st.Code == components.StanceCrouch {
			crouched++
		}
		pos := s.PosMap.Get(mem)
		if pos == nil {
			continue
		}
		for _, slot := range windows {
			d := pos.Sub(slot.Pos)
			if d.Y < -0.8 || d.Y > 0.8 {
				continue
			}
			r := systems.SlotParkRadius
			if d.X*d.X+d.Z*d.Z <= r*r {
				atWindow++
				break
			}
		}
	}
	return crouched, atWindow, total
}

// countWindowSlots recomputes the Garrison window plan and tallies how many
// planned window slots hold a member (XZ within SlotParkRadius+0.3, Y within
// the storey band) and how many of those face the opening (yaw within
// ~34 deg of outward).
func (s *aiTestState) countWindowSlots() (manned, faced, expected int) {
	roster := s.RosterMap.Get(s.squad)
	if roster == nil || s.slotPlanner == nil {
		return 0, 0, 0
	}
	slots := s.slotPlanner.PlanSlots(s.targetBuilding, systems.SlotWindows,
		int(roster.Count), false, 0)
	for si, slot := range slots {
		if !slot.Window {
			continue
		}
		expected++
		bestD := float32(1e9)
		bestIdx := -1
		var bestYawDiff float32
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem == (ecs.Entity{}) || !s.World.Alive(mem) {
				continue
			}
			pos := s.PosMap.Get(mem)
			mot := s.MotionMap.Get(mem)
			if pos == nil || mot == nil {
				continue
			}
			d := pos.Sub(slot.Pos)
			if d.Y < -0.8 || d.Y > 0.8 {
				continue
			}
			distSq := d.X*d.X + d.Z*d.Z
			if distSq < bestD {
				bestD = distSq
				bestIdx = int(i)
				bestYawDiff = angDiffAbs(mot.Yaw, slot.Yaw)
			}
		}
		r := systems.SlotParkRadius + 0.3
		ok := bestIdx >= 0 && bestD <= r*r
		if ok {
			manned++
			if bestYawDiff < 0.6 {
				faced++
			}
		}
		sx := float32(slot.Pos.Chunk.X)*components.ChunkSize + slot.Pos.Local.X
		sz := float32(slot.Pos.Chunk.Z)*components.ChunkSize + slot.Pos.Local.Z
		fmt.Printf("    window[%d]=(%.1f,%.1f,Y%.1f) yaw=%.2f nearest=m%d dist=%.2f yawDiff=%.2f manned=%v\n",
			si, sx, sz, slot.Pos.Local.Y, slot.Yaw, bestIdx,
			float32(math.Sqrt(float64(bestD))), bestYawDiff, ok)
	}
	return manned, faced, expected
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
