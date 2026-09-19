package main

import (
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

// spawnAnchorAndCamera creates the camera target and the orbit camera.
// Skipped under -load: the snapshot carries both, and loadSnapshot re-binds them.
func (g *Game) spawnAnchorAndCamera() {
	g.Maps.Pos = ecs.NewMap[components.WorldPos](g.App.World)
	g.Maps.LODActive = ecs.NewMap[components.LODActive](g.App.World)
	g.Maps.LODAnchor = ecs.NewMap[components.LODAnchor](g.App.World)
	g.Maps.AlwaysActive = ecs.NewMap[components.AlwaysActive](g.App.World)

	g.anchor = ecs.Entity{}
	if *loadFlag == "" {
		g.anchor = g.App.World.NewEntity()
		anchorPos := components.WorldPos{}
		if isDoorScene() {
			anchorPos = doorSceneAnchorPos()
		} else if isAIScene() {
			anchorPos = aiSceneAnchorPos()
		}
		g.Maps.Pos.Add(g.anchor, &anchorPos)
		g.Maps.LODActive.Add(g.anchor, &components.LODActive{})
		g.Maps.LODAnchor.Add(g.anchor, &components.LODAnchor{})
		g.Maps.AlwaysActive.Add(g.anchor, &components.AlwaysActive{})
		ecs.NewMap[components.OnGround](g.App.World).Add(g.anchor, &components.OnGround{})
	}

	g.Maps.Camera = ecs.NewMap[components.Camera](g.App.World)
	g.Maps.Orbit = ecs.NewMap[components.OrbitController](g.App.World)
	g.Maps.ActiveCam = ecs.NewMap[components.ActiveCamera](g.App.World)

	g.camEnt = ecs.Entity{}
	if *loadFlag == "" {
		g.camEnt = g.App.World.NewEntity()
		g.Maps.Pos.Add(g.camEnt, &components.WorldPos{Local: rl.Vector3{X: 0, Y: 15.0, Z: 20.0}})
		g.Maps.Camera.Add(g.camEnt, &components.Camera{Fovy: 75.0, Perspective: true})
		g.Maps.Orbit.Add(g.camEnt, &components.OrbitController{
			Target:           g.anchor,
			Yaw:              shotCamYaw(0),
			Pitch:            shotCamPitch(0.6),
			Radius:           shotCamRadius(25.0),
			MinRadius:        5.0,
			MaxRadius:        max(100.0, shotCamRadius(25.0)),
			SensitivityYaw:   0.01,
			SensitivityPitch: 0.01,
			SensitivityZoom:  4.0,
			Smooth:           0,
			ViewLift:         float32(*shotLiftFlag),
		})
		g.Maps.ActiveCam.Add(g.camEnt, &components.ActiveCamera{})
	}
}

// spawnWorldRoots creates the AlwaysActive roots that outlive chunk eviction:
// one per building (plus its Level entities) and one per trench polyline.
func (g *Game) spawnWorldRoots() {
	g.Maps.Building = ecs.NewMap[components.Building](g.App.World)
	g.Maps.BuildingMember = ecs.NewMap[components.BuildingMember](g.App.World)
	g.Maps.Level = ecs.NewMap[components.Level](g.App.World)
	g.Maps.BuildingViewMode = ecs.NewMap[components.BuildingViewMode](g.App.World)
	g.Maps.LevelVisibility = ecs.NewMap[components.LevelVisibility](g.App.World)
	plansToSpawn := g.Res.BuildingPlans.Plans
	if *loadFlag != "" {
		plansToSpawn = nil // snapshot restores roots/levels; PostLoadRebuild refills the index
	}
	for i := range plansToSpawn {
		p := &plansToSpawn[i]
		root := g.App.World.NewEntity()
		fp := components.AABB2D{
			MinX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize - p.Size.X*0.5,
			MinZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize - p.Size.Y*0.5,
			MaxX: p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize + p.Size.X*0.5,
			MaxZ: p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize + p.Size.Y*0.5,
		}
		g.Maps.Pos.Add(root, &p.Pos)
		g.Maps.Building.Add(root, &components.Building{
			Kind:      p.Kind,
			Stories:   p.Stories,
			Yaw:       p.Yaw,
			Footprint: fp,
			Seed:      p.Seed,
		})
		g.Maps.AlwaysActive.Add(root, &components.AlwaysActive{})
		// Ownership rides on the root from the start: ControlPointSystem only
		// ever writes it, never adds it mid-query.
		ecs.NewMap[components.BuildingControl](g.App.World).Add(root,
			&components.BuildingControl{Owner: components.FactionNone})
		g.Res.BuildingPlanIx.Plans[root] = p

		// Level entities live for the building's whole life independent of
		// chunk lifecycle (AlwaysActive); chunk-spawned children reference
		// them by stable entity handle.
		levels := make([]ecs.Entity, len(p.Levels))
		for li := range p.Levels {
			ls := &p.Levels[li]
			lev := g.App.World.NewEntity()
			lwx := ls.AABB.CenterX()
			lwz := ls.AABB.CenterZ()
			lcx := int32(math.Floor(float64(lwx) / float64(components.ChunkSize)))
			lcz := int32(math.Floor(float64(lwz) / float64(components.ChunkSize)))
			g.Maps.Pos.Add(lev, &components.WorldPos{
				Chunk: components.ChunkCoord{X: lcx, Z: lcz},
				Local: rl.Vector3{
					X: lwx - float32(lcx)*components.ChunkSize,
					Y: ls.AABB.CenterY(),
					Z: lwz - float32(lcz)*components.ChunkSize,
				},
			})
			g.Maps.BuildingMember.Add(lev, &components.BuildingMember{Building: root})
			g.Maps.AlwaysActive.Add(lev, &components.AlwaysActive{})
			lc := components.Level{
				AABB:         ls.AABB,
				Name:         components.LevelName(ls.Name),
				DisplayOrder: ls.DisplayOrder,
			}
			for _, room := range ls.Rooms {
				if lc.RoomCount >= components.MaxRoomsPerLevel {
					break
				}
				lc.Rooms[lc.RoomCount] = room
				lc.RoomCount++
			}
			g.Maps.Level.Add(lev, &lc)
			g.Maps.LevelVisibility.Add(lev, &components.LevelVisibility{})
			levels[li] = lev
		}
		g.Res.BuildingPlanIx.Levels[root] = levels
		fmt.Printf("[startup] building %d kind=%d stories=%d levels=%d footprint=(%.0f..%.0f, %.0f..%.0f)\n",
			i, p.Kind, p.Stories, len(levels), fp.MinX, fp.MaxX, fp.MinZ, fp.MaxZ)

		var currentLevel ecs.Entity
		if len(levels) > 0 {
			currentLevel = levels[0]
		}
		g.Maps.BuildingViewMode.Add(root, &components.BuildingViewMode{
			InteriorOpen: false,
			CurrentLevel: currentLevel,
			WallMode:     components.WallRenderAll,
		})
	}

	// One TrenchRoot entity per polyline so the hit-test resolver can return
	// an ecs.Entity in OrderTarget.Entity for OccupyTrench.
	g.Maps.TrenchRoot = ecs.NewMap[components.TrenchRoot](g.App.World)
	trenchLinesToSpawn := g.Res.Trenches.Lines
	if *loadFlag != "" {
		trenchLinesToSpawn = nil
	}
	for i := range trenchLinesToSpawn {
		pts := trenchLinesToSpawn[i].Points
		if len(pts) == 0 {
			continue
		}
		ent := g.App.World.NewEntity()
		mid := pts[len(pts)/2]
		g.Maps.Pos.Add(ent, &mid)
		g.Maps.TrenchRoot.Add(ent, &components.TrenchRoot{Index: i})
		g.Maps.AlwaysActive.Add(ent, &components.AlwaysActive{})
	}
}

// initGameplayHandles builds the unit / order / squad component handles the
// UI and input halves read, plus the spawn factories.
func (g *Game) initGameplayHandles() {
	g.Svc.UnitFactory = entities.NewUnitFactory(g.App.World, g.Maps.Pos)
	g.Maps.ActionQueue = g.Svc.UnitFactory.ActionQueueMap
	g.Svc.VehicleFactory = entities.NewVehicleFactory(g.App.World, g.Maps.Pos)
	g.Svc.AircraftFactory = entities.NewAircraftFactory(g.App.World, g.Maps.Pos)
	if g.Svc.AirTraffic != nil {
		g.Svc.AirTraffic.SetSpawner(g.Svc.AircraftFactory)
	}

	g.Ctx.LOS = newLOSPreview(g.App.World)
	g.Ctx.Coverage = newCoverageState(g.App.World)

	g.Ctx.Inspector = ui.NewInspectorMaps(g.App.World)
	g.Ctx.Behavior = ui.NewBehaviorMaps(g.App.World)
	weaponMap := ecs.NewMap[components.Weapon](g.App.World)
	_ = weaponMap
	g.Maps.Role = ecs.NewMap[components.UnitRole](g.App.World)
	g.Maps.SquadMember = ecs.NewMap[components.SquadMember](g.App.World)
	g.Maps.Roster = ecs.NewMap[components.CommandRoster](g.App.World)
	g.Maps.FormationData = ecs.NewMap[components.FormationData](g.App.World)
	g.Maps.OrderQueue = ecs.NewMap[components.OrderQueueHead](g.App.World)
	g.Maps.OrderKind = ecs.NewMap[components.OrderKind](g.App.World)
	g.Maps.OrderTarget = ecs.NewMap[components.OrderTarget](g.App.World)
	g.Maps.OrderChain = ecs.NewMap[components.OrderChain](g.App.World)
	g.Maps.OrderState = ecs.NewMap[components.OrderState](g.App.World)
	g.Maps.OrderProgress = ecs.NewMap[components.OrderProgress](g.App.World)
	g.Maps.OrderIssuedAt = ecs.NewMap[components.OrderIssuedAt](g.App.World)
	g.Maps.MovementProfile = ecs.NewMap[components.MovementProfile](g.App.World)
	g.Maps.Stamina = ecs.NewMap[components.Stamina](g.App.World)
	g.Maps.HP = ecs.NewMap[components.HP](g.App.World)
	g.Maps.Faction = ecs.NewMap[components.Faction](g.App.World)
	g.Maps.Controller = ecs.NewMap[components.Controller](g.App.World)
	g.Maps.Detectability = ecs.NewMap[components.Detectability](g.App.World)
	g.Maps.CirclePatrol = ecs.NewMap[components.CirclePatrol](g.App.World)
	g.Maps.IndividualPos = ecs.NewMap[components.IndividualPosition](g.App.World)

	// RoleService owns UnitRole + per-role Equipment sub-entities.
	g.Svc.Role = systems.NewRoleService(g.App.World)
	// After RoleService exists, not before: a nil spawner made every scheduled
	// squad silently fail to appear.
	if g.Svc.ForceTraffic != nil {
		g.Svc.ForceTraffic.SetSpawners(g.Svc.Squad, g.Svc.Role,
			g.Svc.VehicleFactory, g.unitFactory)
	}
}

// spawnScene populates the world: a scripted -scene harness, or the default
// playground squads. No-op under -load.
func (g *Game) spawnScene() {
	playerFaction := components.Faction{ID: components.FactionPlayer}
	playerController := components.Controller{Owner: components.ControllerLocal}
	aiController := components.Controller{Owner: components.ControllerAI}
	if *loadFlag != "" {
		// Snapshot restores all entities; scene / default spawns skipped.
	} else if isMission() {
		g.spawnMission(activeMission())
	} else if isAIScene() {
		g.Scene.AI = aiSceneSpawn(g.App.World, g.Svc.Squad, g.Svc.Role, g.unitFactory,
			g.Svc.VehicleFactory, g.Svc.AircraftFactory, g.Svc.Damage, playerFaction,
			g.Maps.Pos, g.Maps.Roster, g.Maps.Building)
	} else if isDoorScene() {
		testSquad := g.Svc.Squad.CreateFromTemplate(
			systems.TmplMotorRifle, doorSceneSquadSpawn(),
			components.FormationLine, playerFaction, playerController, g.Svc.Role, g.unitFactory)
		var firstBuilding, firstLevel ecs.Entity
		var firstLevelAABB components.AABB3D
		var firstFootprint components.AABB2D
		buildingScan := ecs.NewFilter1[components.Building](g.App.World)
		bq := buildingScan.Query()
		for bq.Next() {
			b := bq.Get()
			firstBuilding = bq.Entity()
			firstFootprint = b.Footprint
			break
		}
		bq.Close()
		if levels, ok := g.Res.BuildingPlanIx.Levels[firstBuilding]; ok && len(levels) > 0 {
			firstLevel = levels[0]
			if lv := g.Maps.Level.Get(firstLevel); lv != nil {
				firstLevelAABB = lv.AABB
			}
		}
		g.Scene.Door = &doorSceneState{
			Squad:        testSquad,
			Building:     firstBuilding,
			Level:        firstLevel,
			LevelAABB:    firstLevelAABB,
			Footprint:    firstFootprint,
			World:        g.App.World,
			SquadService: g.Svc.Squad,
			PosMap:       g.Maps.Pos,
			RosterMap:    g.Maps.Roster,
		}
	} else {
		// A base relay, or every squad in the playground reads Silent and the
		// core mechanic looks broken before it has done anything.
		systems.SpawnRelay(g.App.World, components.WorldPos{}, playerFaction.ID,
			components.RelayRangeSpawnM)
		// Three points to stand on: one held, one the enemy holds, one nobody
		// does. The playground is where the mechanic has to be legible at a
		// glance, and one point would not show what the colours mean.
		for _, cp := range []struct {
			x, z  float32
			owner uint8
		}{
			{60, -40, playerFaction.ID},
			{160, 120, components.FactionEnemyRed},
			{-90, 90, components.FactionNone},
		} {
			at := components.WorldPos{}.Add(rl.Vector3{X: cp.x, Z: cp.z})
			at.Local.Y = systems.GroundHeight(cp.x, cp.z)
			systems.SpawnControlPoint(g.App.World, at, cp.owner,
				components.ControlPointRadiusM, components.RelayRangePointM)
		}
		// Squads spawn OUTSIDE buildings so formation slots don't land on
		// wall-rasterised surface cells (which would block path planning).
		g.Svc.Squad.CreateFromTemplate(
			systems.TmplLightInfantry,
			components.WorldPos{}.Add(rl.Vector3{X: -25, Z: -55}),
			components.FormationLine, playerFaction, playerController, g.Svc.Role, g.unitFactory)
		g.Svc.Squad.CreateFromTemplate(
			systems.TmplMGTeam,
			components.WorldPos{}.Add(rl.Vector3{X: 40, Z: 15}),
			components.FormationWedge, playerFaction, playerController, g.Svc.Role, g.unitFactory)
		g.Svc.Squad.CreateFromTemplate(
			systems.TmplATTeam,
			components.WorldPos{}.Add(rl.Vector3{X: -30, Z: 40}),
			components.FormationColumn, playerFaction, playerController, g.Svc.Role, g.unitFactory)
		g.Svc.Squad.CreateFromTemplate(
			systems.TmplMotorRifle,
			components.WorldPos{}.Add(rl.Vector3{X: -20, Z: 0}),
			components.FormationLoose, playerFaction, playerController, g.Svc.Role, g.unitFactory)

		// Hostile MotorRifle squad parked via DefendPosition.
		enemySpawn := components.WorldPos{}.Add(rl.Vector3{X: 5, Z: -90})
		enemySquad := g.Svc.Squad.CreateFromTemplate(
			systems.TmplMotorRifle, enemySpawn,
			components.FormationLine, components.Faction{ID: components.FactionEnemyRed},
			aiController, g.Svc.Role, g.unitFactory)
		if enemySquad != (ecs.Entity{}) {
			g.Svc.Squad.IssueOrder(enemySquad,
				components.OrderKindDefendPosition, enemySpawn, ecs.Entity{},
				false, systems.OrderParams{})
		}

		// Phase 18.5 FoW test dummies: stationary + patrolling EnemyFaction +
		// one WildlifeFaction circle-walker. Not in any squad — exercises
		// per-unit Faction path. Replace with proper enemy spawners later.
		spawnDummy := func(pos components.WorldPos, faction uint8, patrol *components.CirclePatrol) {
			ent := g.unitFactory(pos)
			if ent == (ecs.Entity{}) {
				return
			}
			// Factory stamps FactionPlayer/ControllerLocal defaults; overwrite.
			if f := g.Maps.Faction.Get(ent); f != nil {
				f.ID = faction
			}
			if c := g.Maps.Controller.Get(ent); c != nil {
				c.Owner = components.ControllerAI
			}
			if patrol != nil {
				g.Maps.CirclePatrol.Add(ent, patrol)
			}
		}
		// 3 stationary enemies in a small cluster.
		spawnDummy(components.WorldPos{}.Add(rl.Vector3{X: 200, Z: 200}), components.FactionEnemyRed, nil)
		spawnDummy(components.WorldPos{}.Add(rl.Vector3{X: 210, Z: 195}), components.FactionEnemyRed, nil)
		spawnDummy(components.WorldPos{}.Add(rl.Vector3{X: 220, Z: 205}), components.FactionEnemyRed, nil)
		// 2 patrolling enemies around different centres.
		enemyCenterA := components.WorldPos{}.Add(rl.Vector3{X: 180, Z: 180})
		spawnDummy(enemyCenterA, components.FactionEnemyRed,
			&components.CirclePatrol{Center: enemyCenterA, RadiusM: 15, Speed: 1.5})
		enemyCenterB := components.WorldPos{}.Add(rl.Vector3{X: 250, Z: 220})
		spawnDummy(enemyCenterB, components.FactionEnemyRed,
			&components.CirclePatrol{Center: enemyCenterB, RadiusM: 20, Speed: 1.2, Phase: 1.5})
		// 1 wildlife (deer placeholder) circling slowly.
		wildCenter := components.WorldPos{}.Add(rl.Vector3{X: 200, Z: 260})
		spawnDummy(wildCenter, components.FactionWildlife,
			&components.CirclePatrol{Center: wildCenter, RadiusM: 30, Speed: 0.8})

		// Phase 19 M0: one vehicle per class parked west of the squads.
		for i, vk := range []components.VehicleKind{
			components.VehicleTruck, components.VehicleBTR, components.VehicleBMP,
			components.VehicleTank, components.VehicleATCarrier,
		} {
			g.Svc.VehicleFactory.Spawn(
				components.WorldPos{}.Add(rl.Vector3{X: -55, Z: -12 + float32(i)*10}),
				vk, components.FactionPlayer, components.ControllerLocal)
		}
		// The Car parks in front of the starting camera: it is the one class
		// with a real model, so it is what a look at the playground is for.
		g.Svc.VehicleFactory.Spawn(components.WorldPos{}.Add(rl.Vector3{X: -14, Z: 4}),
			components.VehicleCar, components.FactionPlayer, components.ControllerLocal)

		// Phase 20 M0: one of each rotary class, arriving on schedule rather
		// than spawned, so the playground exercises the same release path the
		// gate does. They hold station where they appear until ordered.
		for i, ak := range []components.AircraftKind{
			components.AircraftHeliAttack, components.AircraftHeliTransport,
		} {
			entry := components.WorldPos{}.Add(
				rl.Vector3{X: -30 + float32(i)*40, Y: 60, Z: 55})
			g.Svc.AircraftFactory.Arrival(components.AirArrival{
				At:         2 + float32(i),
				Kind:       ak,
				FactionID:  components.FactionPlayer,
				Controller: components.ControllerLocal,
				Entry:      entry,
				Exit:       systems.AirExitFor(entry, 400),
				AltRef:     components.AltAGL,
				AltSet:     components.SpecForAircraft(ak).DefaultAltAGL,
			})
		}
	}
}

// loadSnapshot restores a full-world snapshot and re-binds anchor + camera,
// whose entity handles the snapshot does not preserve by name.
func (g *Game) loadSnapshot() {
	if *loadFlag != "" {
		meta, err := systems.LoadWorld(g.App.World, *loadFlag)
		if err != nil {
			fmt.Printf("load: %v\n", err)
			os.Exit(1)
		}
		g.App.RestoreClock(meta.TickIndex, uint32(meta.FrameIndex))
		systems.PostLoadRebuild(g.App.World)
		g.App.NotifyLoaded()
		qa := ecs.NewFilter1[components.LODAnchor](g.App.World).Query()
		for qa.Next() {
			g.anchor = qa.Entity()
		}
		qcam := ecs.NewFilter1[components.ActiveCamera](g.App.World).Query()
		for qcam.Next() {
			g.camEnt = qcam.Entity()
		}
		fmt.Printf("load: %s map=%s tick=%d\n", *loadFlag, worldMap.Name, meta.TickIndex)
		writeReplayHash(g.App)
	}
}

// initRenderHandles builds the query handles and context structs the render
// and input halves use. Order matches the old main(): ctx structs read the
// maps declared above them.
func (g *Game) initRenderHandles() {
	g.Filt.UnitRender = ecs.NewFilter3[components.WorldPos, components.Unit, components.Stance](g.App.World)
	g.Filt.ControlPoint = ecs.NewFilter2[components.ControlPoint, components.WorldPos](g.App.World)
	g.Filt.VehicleRender = ecs.NewFilter2[components.WorldPos, components.Vehicle](g.App.World)
	g.Filt.AircraftRender = ecs.NewFilter2[components.WorldPos, components.Aircraft](g.App.World)
	g.Filt.MissileRender = ecs.NewFilter2[components.Missile, components.WorldPos](g.App.World)
	g.Filt.SmokeRender = ecs.NewFilter2[components.WorldPos, components.SmokeField](g.App.World)
	g.Maps.Turret = ecs.NewMap[components.Turret](g.App.World)
	g.Filt.ChunkActive = ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODActive](g.App.World)
	g.Filt.ChunkRelevant = ecs.NewFilter3[components.WorldPos, components.ChunkMesh, components.LODRelevant](g.App.World)
	g.Filt.Prop = ecs.NewFilter2[components.WorldPos, components.Prop](g.App.World)
	g.Filt.WallRender = ecs.NewFilter2[components.WorldPos, components.WallSegment](g.App.World)
	g.Filt.FloorRender = ecs.NewFilter2[components.WorldPos, components.Floor](g.App.World)
	g.Filt.StairsRender = ecs.NewFilter2[components.WorldPos, components.Stairs](g.App.World)
	g.Filt.RoofRender = ecs.NewFilter2[components.WorldPos, components.Roof](g.App.World)
	g.Filt.LevelCutaway = ecs.NewFilter2[components.Level, components.BuildingMember](g.App.World)
	g.Maps.LevelMember = ecs.NewMap[components.LevelMember](g.App.World)
	g.Maps.CoverDirRead = ecs.NewMap[components.CoverDirection](g.App.World)
	g.Maps.LevelVisRead = ecs.NewMap[components.LevelVisibility](g.App.World)
	g.Filt.NavOverlay = ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap](g.App.World).
		With(ecs.C[components.LODActive]())
	g.Filt.CoverOverlay = ecs.NewFilter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap](g.App.World).
		With(ecs.C[components.LODActive]())
	g.Filt.CoverSlot = ecs.NewFilter2[components.WorldPos, components.CoverSlot](g.App.World)
	g.Filt.MapPing = ecs.NewFilter2[components.WorldPos, components.MapPing](g.App.World)
	g.Filt.NavGridChunk = ecs.NewFilter1[components.NavGrid](g.App.World)
	g.Filt.FloorNav = ecs.NewFilter3[components.WorldPos, components.Level, components.LevelNavGrid](g.App.World)
	g.Filt.VisionAware = ecs.NewFilter2[components.WorldPos, components.Awareness](g.App.World).
		With(ecs.C[components.Unit]())
	// drawUnitPaths dedupes squad members from the solo filter via g.Maps.SquadMember.Has.
	g.Filt.UnitPathSquad = ecs.NewFilter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember](g.App.World)
	g.Filt.UnitPathSolo = ecs.NewFilter3[components.Unit, components.WorldPos, components.MicroPath](g.App.World)

	g.Filt.ChunkAll = ecs.NewFilter1[components.TerrainChunk](g.App.World)
	g.Filt.Weapon = ecs.NewFilter1[components.Weapon](g.App.World)
	g.Filt.Unit = ecs.NewFilter1[components.Unit](g.App.World)
	g.Filt.StairsCount = ecs.NewFilter1[components.Stairs](g.App.World)
	g.Filt.Squad = ecs.NewFilter2[components.Squad, components.CommandRoster](g.App.World)
	g.Filt.Commander = ecs.NewFilter2[components.CommandRoster, components.OrderQueueHead](g.App.World)
	g.Filt.Contact = ecs.NewFilter1[components.Contact](g.App.World)
	g.Maps.Contact = ecs.NewMap[components.Contact](g.App.World)
	g.Maps.ContactOverride = ecs.NewMap[components.ContactSymbolOverride](g.App.World)
	g.Maps.ContactPlayerSet = ecs.NewMap[components.ContactPlayerSet](g.App.World)
	g.Maps.UnitOverride = ecs.NewMap[components.UnitSymbolOverride](g.App.World)
	g.Maps.SquadOverride = ecs.NewMap[components.SquadSymbolOverride](g.App.World)
	g.Maps.Vehicle = ecs.NewMap[components.Vehicle](g.App.World)
	g.Maps.Aircraft = ecs.NewMap[components.Aircraft](g.App.World)
	g.Maps.SquadMarker = ecs.NewMap[components.Squad](g.App.World)

	g.Filt.Building = ecs.NewFilter1[components.Building](g.App.World)
	g.Filt.TrenchRoot = ecs.NewFilter1[components.TrenchRoot](g.App.World)
	// 1.5 m snap radius = standing collider (0.35 m) + ~1 m forgiveness margin.
	g.Filt.UnitHit = ecs.NewFilter2[components.Unit, components.WorldPos](g.App.World)
	g.Ctx.HitTest = &HitTester{
		BuildingFilter:  g.Filt.Building,
		BuildingMap:     g.Maps.Building,
		TrenchRootMap:   g.Maps.TrenchRoot,
		TrenchRoots:     g.Filt.TrenchRoot,
		Trenches:        &g.Res.Trenches,
		TrenchHitRadius: 2.5,
		UnitFilter:      g.Filt.UnitHit,
		FactionMap:      g.Maps.Faction,
		UnitHitRadius:   1.5,
		OwnFaction:      components.FactionPlayer,
	}

	g.Ctx.Ghost = &ghostContext{
		world:            g.App.World,
		posMap:           g.Maps.Pos,
		rosterMap:        g.Maps.Roster,
		formationDataMap: g.Maps.FormationData,
		stanceMap:        g.Ctx.Inspector.StanceMap,
		movementMap:      g.Maps.MovementProfile,
		squadMemberMap:   g.Maps.SquadMember,
		hitTester:        g.Ctx.HitTest,
		slotPlanner:      systems.NewBuildingSlotPlanner(g.App.World),
		levelMap:         g.Maps.Level,
		trenches:         &g.Res.Trenches,
		trenchRootMap:    g.Maps.TrenchRoot,
		vehicleMap:       ecs.NewMap[components.Vehicle](g.App.World),
		squadColor:       g.squadColor,

		orderQueueMap:      g.Maps.OrderQueue,
		orderKindMap:       g.Maps.OrderKind,
		orderTargetMap:     g.Maps.OrderTarget,
		orderFacingMap:     ecs.NewMap[components.OrderParamFacing](g.App.World),
		orderEngagementMap: ecs.NewMap[components.OrderParamEngagementOverride](g.App.World),
	}
	g.Ctx.Route = &routePreviewCtx{
		world:      g.App.World,
		router:     systems.NewRoadRouter(g.App.World),
		vehicleMap: g.Ctx.Ghost.vehicleMap,
		routeMap:   ecs.NewMap[components.RoadRoute](g.App.World),
		posMap:     g.Maps.Pos,
		memberMap:  g.Maps.SquadMember,
		squadColor: g.squadColor,
	}

	g.Ctx.Points = controlPointCtx{
		filter:  g.Filt.ControlPoint,
		sampler: systems.NewHeightSampler(g.App.World),
	}

	g.Ctx.OrderMarker = orderMarkerCtx{
		world:          g.App.World,
		posMap:         g.Maps.Pos,
		rosterMap:      g.Maps.Roster,
		squadMemberMap: g.Maps.SquadMember,
		orderQueueMap:  g.Maps.OrderQueue,
		orderKindMap:   g.Maps.OrderKind,
		orderTargetMap: g.Maps.OrderTarget,
		orderChainMap:  g.Maps.OrderChain,
		orderFacingMap: ecs.NewMap[components.OrderParamFacing](g.App.World),
		commsMap:       ecs.NewMap[components.CommsState](g.App.World),
		squadColor:     g.squadColor,
	}

	g.Ctx.Particle = ParticleRenderCtx{
		Filter: ecs.NewFilter3[components.Particle, components.WorldPos, components.ParticleVisual](g.App.World),
		EndMap: ecs.NewMap[components.ParticleEnd](g.App.World),
		Smoke:  g.Filt.SmokeRender,
	}
}
