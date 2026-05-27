package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/gen/buildings"
)

// makeStartingRoadGraph builds the Phase-4 test graph: a four-node chain
// (n0 -> n1 -> n2 -> n3) where n1->n2 is intentionally aimed across the first
// river so the preprocessor can split it and tag the middle sub-edge
// RoadBridge. n0->n1 is highway, n2->n3 is dirt track - visual proof that
// kinds survive preprocessing.
func makeStartingRoadGraph() components.RoadGraph {
	if isDoorScene() {
		return components.RoadGraph{}
	}
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return components.RoadGraph{
		Nodes: []components.RoadNode{
			{Pos: wp(0, -50)},  // n0 - start, south of river 1
			{Pos: wp(15, -15)}, // n1 - south bank
			{Pos: wp(15, 50)},  // n2 - north bank (n1->n2 crosses river 1)
			{Pos: wp(-50, 80)}, // n3 - far NW
		},
		Edges: []components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4.0},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4.0},
			{From: 2, To: 3, Kind: components.RoadDirtTrack, Width: 2.5},
		},
	}
}

// makeStartingBuildings - test scene driven by the Phase 16.5 generator.
// Phase 16.A loader (.glb files via manifest) will replace this once art
// assets exist; until then the generator stands in.
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
		// Phase 16.B.0 multi-chunk smoke: 20m-wide house centred at X=5,
		// footprint spans X in [-5, 15] - crosses the chunk (-1,0)/(0,0)
		// boundary. Walking past either chunk should keep half the house
		// visible while the other half tears down with its host chunk.
		makeHouse(0xD4, 5, 20, 1, 20, 8, components.BuildingHouse),
	}

	// Phase 17 M17.D test placements: one Office (3 storeys + cascade stair +
	// interior partition + 2 entrances) and one Compound (L-shape with three
	// connected wings around an implied courtyard).
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

// makeStartingTrenches - one ~30 m defensive earthwork running between the
// bunker and the road, so persistence + clearance are exercised in one place.
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

// makeStartingRivers returns the hand-authored river polylines. World-unit
// coords. Two rivers cross the spawn area so the player sees water + cut +
// water-prop visuals without wandering far.
func makeStartingRivers() []components.RiverPolyline {
	// wp := func(wx, wz float32) components.WorldPos {
	// 	return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	// }
	// return []components.RiverPolyline{
	// 	// Diagonal river NW -> SE through chunk (0,0).
	// 	{
	// 		Points: []components.WorldPos{
	// 			wp(-100, -80),
	// 			wp(-30, -20),
	// 			wp(20, 30),
	// 			wp(80, 90),
	// 			wp(160, 150),
	// 		},
	// 		Width: 5.0,
	// 		Depth: 1.5,
	// 	},
	// 	// Smaller stream branching westward.
	// 	{
	// 		Points: []components.WorldPos{
	// 			wp(-90, 40),
	// 			wp(-30, 20),
	// 			wp(20, 30),
	// 		},
	// 		Width: 3.5,
	// 		Depth: 1.0,
	// 	},
	// }
	return []components.RiverPolyline{}
}
