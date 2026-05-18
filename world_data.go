package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// makeStartingRoadGraph builds the Phase-4 test graph: a four-node chain
// (n0 -> n1 -> n2 -> n3) where n1->n2 is intentionally aimed across the first
// river so the preprocessor can split it and tag the middle sub-edge
// RoadBridge. n0->n1 is highway, n2->n3 is dirt track - visual proof that
// kinds survive preprocessing.
func makeStartingRoadGraph() components.RoadGraph {
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

// makeStartingBuildings - Phase 5 hardcoded test scene: a single-storey house,
// a two-storey house (verifies stairs), and a sunken bunker (verifies
// RectCut). Footprints are sized to fit cleanly inside their host chunks (P5).
func makeStartingBuildings() []components.BuildingPlan {
	wp := func(wx, wz float32) components.WorldPos {
		return components.WorldPos{}.Add(rl.Vector3{X: wx, Y: 0, Z: wz})
	}
	return []components.BuildingPlan{
		{
			Pos:     wp(-25, -40),
			Kind:    components.BuildingHouse,
			Stories: 1,
			Size:    rl.Vector2{X: 8, Y: 8},
			Yaw:     0,
			Seed:    0xA1,
		},
		{
			Pos:     wp(40, 30),
			Kind:    components.BuildingHouse,
			Stories: 2,
			Size:    rl.Vector2{X: 12, Y: 10},
			Yaw:     0,
			Seed:    0xB2,
		},
		{
			Pos:     wp(-30, 55),
			Kind:    components.BuildingBunker,
			Stories: 1,
			Size:    rl.Vector2{X: 10, Y: 10},
			Yaw:     0,
			Seed:    0xC3,
		},
	}
}

// makeStartingTrenches - one ~30 m defensive earthwork running between the
// bunker and the road, so persistence + clearance are exercised in one place.
func makeStartingTrenches() []components.Trench {
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
