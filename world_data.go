package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/gen/buildings"
)

// makeStartingRoadGraph builds the test graph: a four-node chain where
// n1->n2 crosses the first river so PreprocessRoadGraph splits it and tags
// the middle sub-edge RoadBridge.
func makeStartingRoadGraph() components.RoadGraph {
	if isDoorScene() {
		return components.RoadGraph{}
	}
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return components.RoadGraph{
		Nodes: []components.RoadNode{
			{Pos: wp(0, -50)},
			{Pos: wp(15, -15)},
			{Pos: wp(15, 50)},
			{Pos: wp(-50, 80)},
		},
		Edges: []components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4.0},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4.0},
			{From: 2, To: 3, Kind: components.RoadDirtTrack, Width: 2.5},
		},
	}
}

func makeStartingBuildings() []components.BuildingPlan {
	if isDoorScene() {
		return doorSceneBuildings()
	}
	if isAIScene() {
		return aiSceneBuildings()
	}
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	makeHouse := func(seed uint64, x, z float32, stories uint8, sizeX, sizeZ float32, kind components.BuildingKind) components.BuildingPlan {
		pos := wp(x, z)
		pos.Local.Y = systems.GroundHeight(
			pos.Local.X+float32(pos.Chunk.X)*components.ChunkSize,
			pos.Local.Z+float32(pos.Chunk.Z)*components.ChunkSize,
		)
		params := buildings.HouseParams{
			Stories:  stories,
			SizeX:    sizeX,
			SizeZ:    sizeZ,
			DoorSide: 4,
		}
		return *buildings.GenerateHouse(seed, params, pos, kind)
	}
	plans := []components.BuildingPlan{
		makeHouse(0xA1, -25, -40, 1, 8, 8, components.BuildingHouse),
		makeHouse(0xB2, 40, 30, 2, 12, 10, components.BuildingHouse),
		makeHouse(0xC3, -30, 55, 1, 10, 10, components.BuildingBunker),
		// 20m-wide house straddling the chunk (-1,0)/(0,0) boundary —
		// multi-chunk building smoke.
		makeHouse(0xD4, 5, 20, 1, 20, 8, components.BuildingHouse),
	}

	officePos := wp(70, -20)
	officePos.Local.Y = systems.GroundHeight(
		officePos.Local.X+float32(officePos.Chunk.X)*components.ChunkSize,
		officePos.Local.Z+float32(officePos.Chunk.Z)*components.ChunkSize,
	)
	plans = append(plans, *buildings.GenerateOffice(0xE5, officePos))

	compoundCentre := wp(-60, 10)
	compoundCentre.Local.Y = systems.GroundHeight(
		compoundCentre.Local.X+float32(compoundCentre.Chunk.X)*components.ChunkSize,
		compoundCentre.Local.Z+float32(compoundCentre.Chunk.Z)*components.ChunkSize,
	)
	for _, p := range buildings.GenerateCompound(0xF6, compoundCentre) {
		plans = append(plans, *p)
	}

	return plans
}

func makeStartingTrenches() []components.Trench {
	if isDoorScene() {
		return nil
	}
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return []components.Trench{
		{
			Points: []components.WorldPos{
				wp(-50, 40),
				wp(-35, 50),
				wp(-15, 55),
			},
			Width: 1.5,
			Depth: 1.5,
		},
	}
}

func makeStartingRivers() []components.RiverPolyline {
	return []components.RiverPolyline{}
}
