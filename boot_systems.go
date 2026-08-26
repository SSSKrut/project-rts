package main

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// registerSystems constructs every sim system, runs its InitUI (Filters/Maps
// need a live world), and adds it to the app in per-tick pipeline order.
// This is the single seam for adding a system: nothing here escapes to the
// caller, so the frame loop never sees a system handle.
//
// Order is load-bearing — see the per-tick pipeline section in CLAUDE.md.
func (g *Game) registerSystems() {
	app := g.App
	workerPool := g.workerPool
	navService := g.Svc.Nav
	squadService := g.Svc.Squad
	damageService := g.Svc.Damage

	terrainStreamingSys := &systems.TerrainStreamingSystem{}
	terrainStreamingSys.InitUI(app.World)

	terrainLoadSys := &systems.TerrainLoadSystem{}
	terrainLoadSys.InitUI(app.World)

	terrainGenSys := &systems.TerrainGenSystem{}
	terrainGenSys.InitUI(app.World)

	riverSys := &systems.RiverSystem{}
	riverSys.InitUI(app.World)

	roadSys := &systems.RoadSystem{}
	roadSys.InitUI(app.World)

	buildingSys := &systems.BuildingSystem{}
	buildingSys.InitUI(app.World)

	trenchSys := &systems.TrenchSystem{}
	trenchSys.InitUI(app.World)

	propSpawnSys := &systems.PropSpawnSystem{}
	propSpawnSys.InitUI(app.World)

	spatialBakeSys := &systems.SpatialBakeSystem{}
	spatialBakeSys.InitUI(app.World)

	terrainMeshSys := &systems.TerrainMeshSystem{}
	terrainMeshSys.InitUI(app.World)

	groundStickSys := &systems.GroundStickSystem{}
	groundStickSys.InitUI(app.World)

	spatialHashRebuildSys := systems.NewSpatialHashRebuildSystem()
	spatialHashRebuildSys.InitUI(app.World)

	unitMovementSys := systems.NewUnitMovementSystem(workerPool)
	unitMovementSys.InitUI(app.World)

	vehicleDriverSys := &systems.VehicleDriverSystem{}
	vehicleDriverSys.InitUI(app.World)

	// Air traffic runs BEFORE the air driver: a released airframe flies its
	// first tick immediately, and one that reached its exit last tick is off
	// the map before anything else this tick can look at it.
	airTrafficSys := systems.NewAirTrafficSystem()
	airTrafficSys.InitUI(app.World)
	g.Svc.AirTraffic = airTrafficSys

	airDriverSys := &systems.AirDriverSystem{}
	airDriverSys.InitUI(app.World)

	contactSys := systems.NewContactSystem(workerPool)
	contactSys.InitUI(app.World)

	// Particle handles built before WeaponSystem so its constructor takes a non-nil ref.
	particleHandles := systems.NewSpawnHandles(app.World)
	particleSys := systems.NewParticleSystem()
	particleSys.InitUI(app.World)

	// WeaponSystem runs after Vision so it reads the freshest Awareness FIFO.
	weaponSys := systems.NewWeaponSystem(workerPool, damageService, particleHandles)
	weaponSys.InitUI(app.World)

	// ThreatSystem runs after WeaponSystem (which mutates Threat.Suppression /
	// ThreatDir) so SurvivalInstinct / StanceController read recomputed State.
	threatSys := systems.NewThreatSystem()
	threatSys.InitUI(app.World)

	stanceSys := systems.NewStanceControllerSystem()
	stanceSys.InitUI(app.World)

	circlePatrolSys := systems.NewCirclePatrolSystem()
	circlePatrolSys.InitUI(app.World)

	utilityEvalSys := systems.NewUtilityEvaluatorSystem()
	utilityEvalSys.InitUI(app.World)

	threatDecaySys := systems.NewThreatDecaySystem()
	threatDecaySys.InitUI(app.World)

	// After threat_decay (fresh Threat) and before order_resolver: arm vehicle
	// reflexes so the driver picks them up next tick.
	vehicleReflexSys := systems.NewVehicleReflexSystem()
	vehicleReflexSys.InitUI(app.World)

	mapPingDecaySys := systems.NewMapPingDecaySystem()
	mapPingDecaySys.InitUI(app.World)

	levelVisSys := systems.NewLevelVisibilitySystem()
	levelVisSys.InitUI(app.World)

	// SurvivalInstinct runs after WeaponSystem (fresh Threat.Suppression) and
	// before FormationSystem so override-driven ActionQueue writes survive.
	survivalSys := systems.NewSurvivalInstinctSystem()
	survivalSys.InitUI(app.World)

	orderResolverSys := systems.NewOrderResolverSystem(squadService)
	orderResolverSys.InitUI(app.World)

	// The brain reads the head order the resolver just advanced and writes the
	// SquadPlan that SurvivalInstinct and FormationSystem execute this tick.
	squadBrainSys := systems.NewSquadBrainSystem()
	squadBrainSys.InitUI(app.World)

	squadMacroPathSys := systems.NewSquadMacroPathSystem(navService, workerPool)
	squadMacroPathSys.InitUI(app.World)

	formationSys := systems.NewFormationSystem(squadService, workerPool)
	formationSys.InitUI(app.World)

	// MicroPath runs after FormationSystem (writer of ActionQueue.Head.Target
	// + MicroPath.Dirty). Serial — NavService holds Filter handles not
	// concurrent-safe.
	microPathSys := systems.NewMicroPathSystem(navService, workerPool)
	microPathSys.InitUI(app.World)

	mapMarkerCacheSys := &systems.MapMarkerCacheSystem{}
	mapMarkerCacheSys.InitUI(app.World)

	lodSys := &systems.LODSystem{
		ActiveRadius:   60,
		RelevantRadius: 120,
		Hysteresis:     2,
	}
	lodSys.InitUI(app.World)

	movementSys := &systems.MovementSystem{}
	movementSys.InitUI(app.World)

	streamingSys := &systems.StreamingSystem{}
	streamingSys.InitUI(app.World)

	orbitSys := &systems.OrbitSystem{}
	orbitSys.InitUI(app.World)

	cameraSys := &systems.CameraSystem{}
	cameraSys.InitUI(app.World)

	// Phase 20 M2/M3 registrations come LAST on purpose: this is the first
	// registration of Missile / AircraftOverride / AirEngagement, and a
	// mid-order component ID would reshuffle every existing archetype (M0
	// finding #1). Pipeline position comes from the AddSystem block below.
	missileSys := systems.NewMissileSystem(damageService)
	missileSys.InitUI(app.World)
	airReflexSys := systems.NewAirReflexSystem()
	airReflexSys.InitUI(app.World)
	weaponSys.SetMissileMap(missileSys.MissileMap())
	airEngageMap := ecs.NewMap[components.AirEngagement](app.World)
	weaponSys.SetAirEngageMap(airEngageMap)
	airDriverSys.SetEngageMap(airEngageMap)
	// Block A: CommsState / Relay are first registered here, in the tail.
	commsSys := systems.NewCommsSystem()
	commsSys.InitUI(app.World)
	// Block B: ControlPoint's first registration, also in the tail.
	controlPointSys := systems.NewControlPointSystem()
	controlPointSys.InitUI(app.World)

	app.AddSystem(terrainStreamingSys)
	app.AddSystem(terrainLoadSys)
	app.AddSystem(terrainGenSys)
	app.AddSystem(riverSys)
	app.AddSystem(roadSys)
	app.AddSystem(buildingSys)
	app.AddSystem(trenchSys)
	app.AddSystem(propSpawnSys)
	app.AddSystem(spatialBakeSys)
	app.AddSystem(terrainMeshSys)
	app.AddSystem(groundStickSys)
	app.AddSystem(spatialHashRebuildSys)
	app.AddSystem(circlePatrolSys)
	app.AddSystem(unitMovementSys)
	app.AddSystem(vehicleDriverSys)
	app.AddSystem(airTrafficSys)
	app.AddSystem(airDriverSys)
	app.AddSystem(missileSys)
	app.AddSystem(contactSys)
	app.AddSystem(weaponSys)
	app.AddSystem(particleSys)
	app.AddSystem(threatSys)
	app.AddSystem(stanceSys)
	app.AddSystem(utilityEvalSys)
	app.AddSystem(threatDecaySys)
	app.AddSystem(vehicleReflexSys)
	app.AddSystem(airReflexSys)
	app.AddSystem(levelVisSys)
	app.AddSystem(mapPingDecaySys)
	app.AddSystem(controlPointSys)
	app.AddSystem(commsSys)
	app.AddSystem(orderResolverSys)
	app.AddSystem(squadBrainSys)
	app.AddSystem(survivalSys)
	app.AddSystem(squadMacroPathSys)
	app.AddSystem(formationSys)
	app.AddSystem(microPathSys)
	app.AddSystem(mapMarkerCacheSys)
	app.AddSystem(lodSys)
	app.AddSystem(movementSys)
	app.AddSystem(streamingSys)
	app.AddSystem(orbitSys)
	app.AddSystem(cameraSys)
}
