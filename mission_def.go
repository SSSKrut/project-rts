package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
)

var missionFlag = flag.String("mission", "", "mission manifest name (missions/<name>.json)")

// missionDef is the hand-authored scenario manifest. It REFERENCES a map (P1)
// rather than containing one, so maps stay reusable and a mission is not a fork
// of the world.
type missionDef struct {
	Name        string         `json:"name"`
	Map         string         `json:"map"`
	TimeSec     float32        `json:"timeSec"`
	Points      []missionPoint `json:"points"`
	Forces      []missionForce `json:"forces"`
	Victory     missionVictory `json:"victory"`
	Relays      []missionRelay `json:"relays"`
	BotVehicles bool           `json:"botVehicles,omitempty"`
}

type missionPoint struct {
	X           float32 `json:"x"`
	Z           float32 `json:"z"`
	Radius      float32 `json:"radius,omitempty"`
	Owner       string  `json:"owner,omitempty"` // player | enemy | none
	RelayRangeM float32 `json:"relayRangeM,omitempty"`
}

type missionRelay struct {
	X      float32 `json:"x"`
	Z      float32 `json:"z"`
	Side   string  `json:"side"`
	RangeM float32 `json:"rangeM,omitempty"`
}

type missionForce struct {
	Side     string  `json:"side"`
	AtSec    float32 `json:"atSec"`
	Kind     string  `json:"kind"`     // squad | vehicle
	Template string  `json:"template"` // squad template or vehicle class
	X        float32 `json:"x"`
	Z        float32 `json:"z"`
	Ctrl     string  `json:"ctrl,omitempty"` // "ai" hands a player-side force to the bot
}

type missionVictory struct {
	HoldPoints uint8   `json:"holdPoints"`
	OfTotal    uint8   `json:"ofTotal"`
	ForSec     float32 `json:"forSec"`
}

func isMission() bool { return missionFlag != nil && *missionFlag != "" }

// missionDefCache: the manifest is read by the map loader and again by the
// spawner, and reading a file twice is how the two halves start disagreeing.
var missionDefCache *missionDef

func activeMission() missionDef {
	if missionDefCache == nil {
		d := readMissionFile(*missionFlag)
		missionDefCache = &d
	}
	return *missionDefCache
}

func readMissionFile(name string) missionDef {
	path := filepath.Join("missions", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("mission: %v\n", err)
		os.Exit(1)
	}
	var def missionDef
	if err := json.Unmarshal(data, &def); err != nil {
		fmt.Printf("mission %s: %v\n", path, err)
		os.Exit(1)
	}
	if def.Name == "" {
		def.Name = name
	}
	fmt.Printf("mission: %s (%s) map=%s\n", def.Name, path, def.Map)
	return def
}

func missionFactionID(s string) uint8 {
	switch s {
	case "enemy", "red":
		return components.FactionEnemyRed
	case "none", "":
		return components.FactionNone
	}
	return components.FactionPlayer
}

func missionSquadTemplate(s string) systems.SquadTemplate {
	switch s {
	case "light":
		return systems.TmplLightInfantry
	case "recon":
		return systems.TmplRecon
	case "at":
		return systems.TmplATTeam
	case "mg":
		return systems.TmplMGTeam
	}
	return systems.TmplMotorRifle
}

func missionVehicleKind(s string) components.VehicleKind {
	switch s {
	case "truck":
		return components.VehicleTruck
	case "btr":
		return components.VehicleBTR
	case "tank":
		return components.VehicleTank
	case "atc":
		return components.VehicleATCarrier
	case "command":
		return components.VehicleCommand
	}
	return components.VehicleBMP
}

// spawnMission lays the scenario down: points, relays and every force as a
// scheduled arrival. Nothing here is "initial" — a force at t=0 goes through
// the same release path as one at t=300 (P2).
func (g *Game) spawnMission(def missionDef) {
	world := g.App.World
	wp := func(x, z float32) components.WorldPos {
		p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		p.Local.Y = systems.GroundHeight(x, z)
		return p
	}

	for i := range def.Points {
		pt := &def.Points[i]
		r := pt.Radius
		if r <= 0 {
			r = components.ControlPointRadiusM
		}
		systems.SpawnControlPoint(world, wp(pt.X, pt.Z), missionFactionID(pt.Owner),
			r, pt.RelayRangeM)
	}
	for i := range def.Relays {
		node := &def.Relays[i]
		r := node.RangeM
		if r <= 0 {
			r = components.RelayRangeSpawnM
		}
		systems.SpawnRelay(world, wp(node.X, node.Z), missionFactionID(node.Side), r)
	}
	for i := range def.Forces {
		f := &def.Forces[i]
		a := components.ForceArrival{
			At:   f.AtSec,
			Side: missionFactionID(f.Side),
			Ctrl: components.ControllerAI,
			Pos:  wp(f.X, f.Z),
		}
		if a.Side == components.FactionPlayer && f.Ctrl != "ai" {
			a.Ctrl = components.ControllerLocal
		}
		switch f.Kind {
		case "vehicle":
			a.Kind = components.ForceVehicle
			a.Vehicle = missionVehicleKind(f.Template)
		default:
			a.Kind = components.ForceSquad
			a.Template = uint8(missionSquadTemplate(f.Template))
		}
		systems.SpawnForceArrival(world, a)
	}

	g.Res.Mission = components.Mission{
		Name:        def.Name,
		TimeSec:     def.TimeSec,
		HoldPoints:  def.Victory.HoldPoints,
		OfTotal:     def.Victory.OfTotal,
		ForSec:      def.Victory.ForSec,
		Loaded:      true,
		BotVehicles: def.BotVehicles,
	}
	fmt.Printf("mission: %d points, %d forces, hold %d/%d for %.0fs, limit %.0fs\n",
		len(def.Points), len(def.Forces), def.Victory.HoldPoints,
		def.Victory.OfTotal, def.Victory.ForSec, def.TimeSec)
}

// missionOver stops the world the moment the mission is decided (P5). Headless
// prints the verdict and leaves; a windowed run freezes the clock and keeps the
// outcome on screen — a result you can keep playing is not a result.
func (g *Game) missionOver() bool {
	if !g.Res.Mission.Loaded || !g.Res.MissionState.Over() {
		return false
	}
	if !g.Dev.MissionReported {
		g.Dev.MissionReported = true
		printMissionVerdict(&g.Res.Mission, &g.Res.MissionState)
	}
	if g.headless {
		return true
	}
	g.App.TimeScale = 0
	return false
}

func printMissionVerdict(m *components.Mission, st *components.MissionState) {
	fmt.Println("============================================================")
	fmt.Printf("== MISSION [%s]: %s  (t=%.0fs of %.0f | points %d-%d | losses %d-%d | linked %.0fs)\n",
		m.Name, st.Outcome, st.EndedAt, m.TimeSec,
		st.Held[components.FactionPlayer], st.Held[components.FactionEnemyRed],
		st.Losses[components.FactionPlayer], st.Losses[components.FactionEnemyRed],
		st.LinkedSec)
	fmt.Println("============================================================")
}
