package main

import (
	"fmt"
	"os"
	"runtime"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

// bootGame opens the window and builds the world's resources and services.
// Teardown is NOT deferred here — it belongs to main's lifetime, so the
// caller installs `defer g.Shutdown()` instead.
func bootGame() *Game {
	worldMap = loadMapDef()
	if *loadFlag != "" {
		meta, err := systems.LoadSnapshotMeta(*loadFlag)
		if err != nil {
			fmt.Printf("load: %v\n", err)
			os.Exit(1)
		}
		worldMap = loadMapDefByName(meta.MapName)
	}
	systems.SetTerrainParams(worldMap.Terrain)
	if (*mapFlag != "" || *loadFlag != "") && !isAIScene() && !isDoorScene() {
		systems.SaveDir = "./save/" + worldMap.Name
	}

	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(initialScreenWidth, initialScreenHeight, "RTS/FPS 3D ECS Prototype")

	g := &Game{}
	g.hudFont, g.hudFontIsCustom = loadHUDFont()

	g.headless = isAIScene()
	if g.headless {
		rl.SetTargetFPS(0)
	} else {
		rl.SetTargetFPS(60)
	}

	g.App = core.NewApp()
	g.World = g.App.World

	initTrace(g.App)

	// Worker pool feeds the parallel hot-path systems (UnitMovement / Vision /
	// Formation / SquadMacroPath).
	workerCount := *workersFlag
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	g.workerPool = core.NewWorkerPool(workerCount)
	fmt.Printf("worker pool: %d workers\n", g.workerPool.Workers())

	g.initResources()
	g.saveDir = systems.SaveDir
	g.initServices()
	return g
}

func (g *Game) initResources() {
	r := &g.Res
	r.Streaming = components.NewStreamingMap()
	ecs.AddResource(g.World, &r.Streaming)
	r.TerrainIndex = systems.NewTerrainChunkIndex()
	ecs.AddResource(g.World, &r.TerrainIndex)
	r.PropRegistry = systems.NewPropTypeRegistry()
	ecs.AddResource(g.World, r.PropRegistry)
	r.PropIndex = systems.NewPropChunkIndex()
	ecs.AddResource(g.World, &r.PropIndex)
	r.Rivers = components.Rivers{Polylines: makeStartingRivers()}
	ecs.AddResource(g.World, &r.Rivers)
	r.RoadGraph = makeStartingRoadGraph()
	systems.PreprocessRoadGraph(&r.RoadGraph, &r.Rivers)
	ecs.AddResource(g.World, &r.RoadGraph)
	for _, e := range r.RoadGraph.Edges {
		if e.Kind == components.RoadBridge {
			r.BridgeEdges++
		}
	}
	fmt.Printf("road graph: nodes=%d edges=%d bridges=%d\n",
		len(r.RoadGraph.Nodes), len(r.RoadGraph.Edges), r.BridgeEdges)

	r.BuildingPlans = components.BuildingPlanList{Plans: makeStartingBuildings()}
	ecs.AddResource(g.World, &r.BuildingPlans)
	r.BuildingIndex = systems.NewBuildingChildIndex()
	ecs.AddResource(g.World, &r.BuildingIndex)
	r.BuildingPlanIx = systems.NewBuildingPlanIndex()
	ecs.AddResource(g.World, &r.BuildingPlanIx)
	r.Trenches = components.TrenchNetwork{Lines: makeStartingTrenches()}
	ecs.AddResource(g.World, &r.Trenches)
	r.CoverSlotIndex = systems.NewCoverSlotIndex()
	ecs.AddResource(g.World, &r.CoverSlotIndex)
	r.Transitions = components.NewTransitionRegistry()
	ecs.AddResource(g.World, &r.Transitions)
	r.MapMarkerCache = components.NewMapMarkerCache()
	ecs.AddResource(g.World, &r.MapMarkerCache)
	r.FormationPresets = components.FormationPresets{}
	ecs.AddResource(g.World, &r.FormationPresets)
	// SpatialHash for Unit XZ positions; rebuilt serially before UnitMovement
	// so this tick's separation steering reads fresh positions. Consumers:
	// UnitMovement.separation, WeaponSystem.resolveShot/propagateSuppression.
	r.UnitHash = core.NewSpatialHash(32.0)
	ecs.AddResource(g.World, r.UnitHash)
	// Second hash per Locomotion class (WS-E sh.1): vehicles only.
	r.VehicleHash = core.NewVehicleSpatialHash(32.0)
	ecs.AddResource(g.World, r.VehicleHash)

	r.EventLog = components.NewEventLog()
	ecs.AddResource(g.World, r.EventLog)

	r.ContactRegistry = components.NewContactRegistry()
	ecs.AddResource(g.World, &r.ContactRegistry)

	r.Symbology = components.SymbologyPresets{All: ui.BuiltinSymbologyPresets()}
	if persisted, ok := systems.LoadSymbologySidecar(); ok {
		r.Symbology = persisted
	}
	ecs.AddResource(g.World, &r.Symbology)
}

func (g *Game) initServices() {
	g.Svc.Stamper = systems.NewStamper(g.World)
	g.Svc.Nav = systems.NewNavService(g.World)
	g.Svc.Squad = systems.NewSquadService(g.World)
	// DamageService constructed before WeaponSystem.InitUI so the handle is live.
	g.Svc.Damage = systems.NewDamageService(g.World, g.Svc.Squad)
	g.Svc.Damage.SetClock(func() float32 { return g.Svc.Squad.Clock() })
	g.Svc.MapPing = systems.NewMapPingService(g.World, func() float32 { return g.Svc.Squad.Clock() })
	g.Svc.Damage.SetMapPings(g.Svc.MapPing)
}

// Shutdown replaces the five boot-time defers of the old main(). Order is
// load-bearing and matches what LIFO used to produce: chunk flush must land
// before the pool stops, and the window closes last.
func (g *Game) Shutdown() {
	systems.FlushModifiedChunks(g.World, g.saveDir)
	g.workerPool.Stop()
	_ = g.App.Trace.Close()
	if g.hudFontIsCustom {
		rl.UnloadFont(g.hudFont)
	}
	rl.CloseWindow()
}
