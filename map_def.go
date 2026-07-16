package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/gen/buildings"
	"rts-go/systems"
)

var mapFlag = flag.String("map", "", "map manifest name (maps/<name>.json); empty = built-in default")

// mapDef is the hand-authored world manifest: terrain params + generator
// specs. Built in code for the default world or loaded from maps/<name>.json
// via -map. Scenes (-scene) ignore -map entirely.
type mapDef struct {
	Name      string                `json:"name"`
	Terrain   systems.TerrainParams `json:"terrain"`
	Buildings []mapBuilding         `json:"buildings"`
	Roads     mapRoads              `json:"roads"`
	Trenches  []mapTrench           `json:"trenches"`
	Rivers    []mapRiver            `json:"rivers"`
}

type mapBuilding struct {
	Template string  `json:"template"` // house | office | compound | compound_plus
	Seed     uint64  `json:"seed"`
	X        float32 `json:"x"`
	Z        float32 `json:"z"`
	Stories  uint8   `json:"stories,omitempty"`
	SizeX    float32 `json:"sizeX,omitempty"`
	SizeZ    float32 `json:"sizeZ,omitempty"`
	Kind     string  `json:"kind,omitempty"` // "" = house, "bunker"
	DoorSide uint8   `json:"doorSide,omitempty"`
}

type mapRoads struct {
	Nodes [][2]float32  `json:"nodes"`
	Edges []mapRoadEdge `json:"edges"`
}

type mapRoadEdge struct {
	From  uint16  `json:"from"`
	To    uint16  `json:"to"`
	Kind  string  `json:"kind"` // highway | local | dirt
	Width float32 `json:"width"`
}

type mapTrench struct {
	Points [][2]float32 `json:"points"`
	Width  float32      `json:"width"`
	Depth  float32      `json:"depth"`
}

type mapRiver struct {
	Points [][2]float32 `json:"points"`
	Width  float32      `json:"width"`
	Depth  float32      `json:"depth"`
}

var worldMap mapDef

func loadMapDef() mapDef {
	if *mapFlag == "" || isAIScene() || isDoorScene() {
		return defaultMapDef()
	}
	return readMapFile(*mapFlag)
}

// loadMapDefByName resolves a map by the name stored in a snapshot header.
func loadMapDefByName(name string) mapDef {
	if name == "" || name == "world-default" {
		return defaultMapDef()
	}
	return readMapFile(name)
}

func readMapFile(name string) mapDef {
	path := filepath.Join("maps", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("map: %v\n", err)
		os.Exit(1)
	}
	var def mapDef
	if err := json.Unmarshal(data, &def); err != nil {
		fmt.Printf("map %s: %v\n", path, err)
		os.Exit(1)
	}
	if def.Name == "" {
		def.Name = name
	}
	fmt.Printf("map: %s (%s)\n", def.Name, path)
	return def
}

func defaultMapDef() mapDef {
	return mapDef{
		Name:    "world-default",
		Terrain: systems.DefaultTerrainParams(),
		Buildings: []mapBuilding{
			{Template: "house", Seed: 0xA1, X: -25, Z: -40, Stories: 1, SizeX: 8, SizeZ: 8, DoorSide: 4},
			{Template: "house", Seed: 0xB2, X: 40, Z: 30, Stories: 2, SizeX: 12, SizeZ: 10, DoorSide: 4},
			{Template: "house", Seed: 0xC3, X: -30, Z: 55, Stories: 1, SizeX: 10, SizeZ: 10, Kind: "bunker", DoorSide: 4},
			// 20m-wide house straddling the chunk (-1,0)/(0,0) boundary —
			// multi-chunk building smoke.
			{Template: "house", Seed: 0xD4, X: 5, Z: 20, Stories: 1, SizeX: 20, SizeZ: 8, DoorSide: 4},
			{Template: "office", Seed: 0xE5, X: 70, Z: -20},
			{Template: "compound", Seed: 0xF6, X: -60, Z: 10},
		},
		Roads: mapRoads{
			Nodes: [][2]float32{{0, -50}, {15, -15}, {15, 50}, {-50, 80}},
			Edges: []mapRoadEdge{
				{From: 0, To: 1, Kind: "highway", Width: 4.0},
				{From: 1, To: 2, Kind: "highway", Width: 4.0},
				{From: 2, To: 3, Kind: "dirt", Width: 2.5},
			},
		},
		Trenches: []mapTrench{
			{Points: [][2]float32{{-50, 40}, {-35, 50}, {-15, 55}}, Width: 1.5, Depth: 1.5},
		},
	}
}

func mapWP(x, z float32) components.WorldPos {
	return components.WorldPos{}.Add(rl.Vector3{X: x, Y: 0, Z: z})
}

func (d *mapDef) buildingPlans() []components.BuildingPlan {
	plans := make([]components.BuildingPlan, 0, len(d.Buildings))
	for _, b := range d.Buildings {
		pos := mapWP(b.X, b.Z)
		pos.Local.Y = systems.GroundHeight(b.X, b.Z)
		switch b.Template {
		case "office":
			plans = append(plans, *buildings.GenerateOffice(b.Seed, pos))
		case "compound":
			for _, p := range buildings.GenerateCompound(b.Seed, pos) {
				plans = append(plans, *p)
			}
		case "compound_plus":
			for _, p := range buildings.GenerateCompoundPlus(b.Seed, pos) {
				plans = append(plans, *p)
			}
		default:
			kind := components.BuildingHouse
			if b.Kind == "bunker" {
				kind = components.BuildingBunker
			}
			plans = append(plans, *buildings.GenerateHouse(b.Seed, buildings.HouseParams{
				Stories:  b.Stories,
				SizeX:    b.SizeX,
				SizeZ:    b.SizeZ,
				DoorSide: b.DoorSide,
			}, pos, kind))
		}
	}
	return plans
}

func (d *mapDef) roadGraph() components.RoadGraph {
	g := components.RoadGraph{}
	for _, n := range d.Roads.Nodes {
		g.Nodes = append(g.Nodes, components.RoadNode{Pos: mapWP(n[0], n[1])})
	}
	for _, e := range d.Roads.Edges {
		kind := components.RoadLocal
		switch e.Kind {
		case "highway":
			kind = components.RoadHighway
		case "dirt":
			kind = components.RoadDirtTrack
		}
		g.Edges = append(g.Edges, components.RoadEdge{From: e.From, To: e.To, Kind: kind, Width: e.Width})
	}
	return g
}

func (d *mapDef) trenchLines() []components.Trench {
	out := make([]components.Trench, 0, len(d.Trenches))
	for _, t := range d.Trenches {
		pts := make([]components.WorldPos, 0, len(t.Points))
		for _, p := range t.Points {
			pts = append(pts, mapWP(p[0], p[1]))
		}
		out = append(out, components.Trench{Points: pts, Width: t.Width, Depth: t.Depth})
	}
	return out
}

func (d *mapDef) riverLines() []components.RiverPolyline {
	out := make([]components.RiverPolyline, 0, len(d.Rivers))
	for _, r := range d.Rivers {
		pts := make([]components.WorldPos, 0, len(r.Points))
		for _, p := range r.Points {
			pts = append(pts, mapWP(p[0], p[1]))
		}
		out = append(out, components.RiverPolyline{Points: pts, Width: r.Width, Depth: r.Depth})
	}
	return out
}
