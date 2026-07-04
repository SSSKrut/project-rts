package main

import (
	"rts-go/components"
)

func makeStartingRoadGraph() components.RoadGraph {
	if isDoorScene() {
		return components.RoadGraph{}
	}
	return worldMap.roadGraph()
}

func makeStartingBuildings() []components.BuildingPlan {
	if isDoorScene() {
		return doorSceneBuildings()
	}
	if isAIScene() {
		return aiSceneBuildings()
	}
	return worldMap.buildingPlans()
}

// mainWorldBuildings is the default-map building set. The ai_main_* test
// scenes reuse it verbatim so pathing regressions on the actual map are
// caught by the headless suite.
func mainWorldBuildings() []components.BuildingPlan {
	d := defaultMapDef()
	return d.buildingPlans()
}

func makeStartingTrenches() []components.Trench {
	if isDoorScene() {
		return nil
	}
	if isAIScene() {
		return aiSceneTrenches()
	}
	return worldMap.trenchLines()
}

func makeStartingRivers() []components.RiverPolyline {
	if isDoorScene() || isAIScene() {
		return nil
	}
	return worldMap.riverLines()
}
