package main

import (
	"flag"
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/gen/buildings"
)

// -scene=door spawns a minimal isolated world: one 8×8 south-door house +
// one 5-unit Recon squad 12 m south. Garrison auto-issued at t=2s; PASS/FAIL
// printed at t=30s.
var sceneFlag = flag.String("scene", "", "test scene id ('door' = door-entry isolation)")

func isDoorScene() bool { return *sceneFlag == "door" }

// World (32, 32) is dead-centre of chunk (0,0) so the test runs inside a
// single NavGrid — eliminates multi-chunk wall rasterisation as a variable.
const (
	doorSceneBuildingX float32 = 32
	doorSceneBuildingZ float32 = 32
	doorSceneSquadX    float32 = 32
	doorSceneSquadZ    float32 = 20
	doorSceneAnchorX   float32 = 32
	doorSceneAnchorZ   float32 = 26
)

// doorSceneBuildings — single house with door on the south wall.
// Seed 0xA0 ⇒ DoorSide = 0 (south). Footprint fully inside chunk (0,0).
func doorSceneBuildings() []components.BuildingPlan {
	wp := components.WorldPos{}.Add(rl.Vector3{X: doorSceneBuildingX, Z: doorSceneBuildingZ})
	wp.Local.Y = systems.GroundHeight(
		wp.Local.X+float32(wp.Chunk.X)*components.ChunkSize,
		wp.Local.Z+float32(wp.Chunk.Z)*components.ChunkSize,
	)
	plan := buildings.GenerateHouse(0xA0, buildings.HouseParams{
		Stories:  1,
		SizeX:    8,
		SizeZ:    8,
		DoorSide: 0,
	}, wp, components.BuildingHouse)
	return []components.BuildingPlan{*plan}
}

func doorSceneSquadSpawn() components.WorldPos {
	return components.WorldPos{}.Add(rl.Vector3{X: doorSceneSquadX, Z: doorSceneSquadZ})
}

func doorSceneAnchorPos() components.WorldPos {
	return components.WorldPos{}.Add(rl.Vector3{X: doorSceneAnchorX, Z: doorSceneAnchorZ})
}

// doorSceneState — captured handles + computed targets used by the auto-
// verifier and the O/I/K/U hotkeys. Populated lazily once BuildingSystem
// has spawned wall entities.
type doorSceneState struct {
	Squad     ecs.Entity
	Building  ecs.Entity
	Level     ecs.Entity
	LevelAABB components.AABB3D
	Footprint components.AABB2D

	DoorEntity ecs.Entity
	DoorPos    components.WorldPos

	World        *ecs.World
	SquadService *systems.SquadService
	PosMap       *ecs.Map[components.WorldPos]
	RosterMap    *ecs.Map[components.CommandRoster]

	initialised bool

	elapsedSec      float32
	autoOrderAt     float32
	verdictAt       float32
	autoOrderFired  bool
	verdictPrinted  bool
	nextSampleAt    float32
	lastInsideCount int
}

// EnsureInit looks up the door entity once BuildingSystem has spawned walls,
// then prints the scene banner. Safe to call every frame.
func (s *doorSceneState) EnsureInit() {
	if s.initialised || s.Building == (ecs.Entity{}) {
		return
	}
	dp, de, ok := s.findDoor()
	if !ok {
		return
	}
	s.DoorEntity = de
	s.DoorPos = dp
	s.initialised = true

	if s.autoOrderAt == 0 {
		s.autoOrderAt = 2.0
	}
	if s.verdictAt == 0 {
		s.verdictAt = 30.0
	}
	s.nextSampleAt = s.autoOrderAt + 1.0

	doorWX, doorWZ := worldXZ(dp)
	roster := s.RosterMap.Get(s.Squad)
	memberN := 0
	if roster != nil {
		memberN = int(roster.Count)
	}
	fmt.Println("==================== SCENE: door ====================")
	fmt.Printf("  squad         = %v  (Recon, %d units)\n", s.Squad, memberN)
	fmt.Printf("  building      = %v  footprint=(%.1f..%.1f, %.1f..%.1f)\n",
		s.Building, s.Footprint.MinX, s.Footprint.MaxX, s.Footprint.MinZ, s.Footprint.MaxZ)
	fmt.Printf("  level (L0)    = %v  aabb_y=(%.2f..%.2f)\n",
		s.Level, s.LevelAABB.MinY, s.LevelAABB.MaxY)
	fmt.Printf("  door wall     = %v  world=(%.2f, %.2f)  (south wall, opens -Z)\n",
		s.DoorEntity, doorWX, doorWZ)
	fmt.Println("  auto-verifier:")
	fmt.Printf("    t=%.1fs - issue Garrison(building) automatically\n", s.autoOrderAt)
	fmt.Println("    each 1 s after - log members inside Level AABB")
	fmt.Printf("    t=%.1fs - print PASS (all inside) or FAIL (dump positions)\n", s.verdictAt)
	fmt.Println("  manual hotkeys:")
	fmt.Println("    O = Garrison(building)         I = MoveTo(2 m outside door)")
	fmt.Println("    K = MoveTo(2 m inside door)    U = diagnostic dump")
	fmt.Println("    N / F / J = nav / level-nav / transitions overlays (hold)")
	fmt.Println("  tip: rm -rf ./save before first run so stale Modified chunks")
	fmt.Println("       from a previous session don't override the test heightmap.")
	fmt.Println("=====================================================")
}

// findDoor scans Wall entities for the first OpeningDoor of this building
// and computes its world position at OpeningCenterT along the wall.
func (s *doorSceneState) findDoor() (components.WorldPos, ecs.Entity, bool) {
	f := ecs.NewFilter3[components.WorldPos, components.WallSegment, components.BuildingMember](s.World)
	q := f.Query()
	for q.Next() {
		pos, ws, m := q.Get()
		if m.Building != s.Building || ws.OpeningKind != components.OpeningDoor {
			continue
		}
		// Wall.Yaw=0 ⇒ +Z; step OpeningCenterT * Length along (sin, cos).
		sinY, cosY := math.Sincos(float64(ws.Yaw))
		dx := float32(sinY) * ws.Length * ws.OpeningCenterT
		dz := float32(cosY) * ws.Length * ws.OpeningCenterT
		worldP := *pos
		worldP.Local.X += dx
		worldP.Local.Z += dz
		worldP = components.Normalize(worldP)
		ent := q.Entity()
		q.Close()
		return worldP, ent, true
	}
	return components.WorldPos{}, ecs.Entity{}, false
}

// Update is called once per frame with sim time. Using sim time means pause /
// timescale Just Work.
func (s *doorSceneState) Update(simSec float32) {
	if !s.initialised {
		return
	}
	s.elapsedSec = simSec

	if !s.autoOrderFired && s.elapsedSec >= s.autoOrderAt {
		s.autoOrderFired = true
		s.Dump("BEFORE auto-Garrison")
		s.SquadService.IssueOrder(s.Squad, components.OrderKindGarrison,
			s.DoorPos, s.Building, false, systems.OrderParams{})
		fmt.Printf("[scene-door] t=%.1fs -> auto Garrison(building) issued\n", s.elapsedSec)
	}

	if s.autoOrderFired && s.elapsedSec >= s.nextSampleAt {
		inside, total := s.countInside()
		s.lastInsideCount = inside
		fmt.Printf("[scene-door] t=%.1fs   inside Level=%d/%d\n", s.elapsedSec, inside, total)
		s.nextSampleAt = s.elapsedSec + 1.0
	}

	if !s.verdictPrinted && s.elapsedSec >= s.verdictAt {
		s.verdictPrinted = true
		inside, total := s.countInside()
		fmt.Println("------------------- VERDICT -------------------")
		if total > 0 && inside == total {
			fmt.Printf("[scene-door] PASS  all %d/%d members inside Level AABB at t=%.1fs\n",
				inside, total, s.elapsedSec)
		} else {
			fmt.Printf("[scene-door] FAIL  %d/%d members inside Level AABB at t=%.1fs\n",
				inside, total, s.elapsedSec)
			s.Dump("FAIL state")
		}
		fmt.Println("-----------------------------------------------")
	}
}

// countInside walks roster.Members[:Count] and returns (inside, alive).
// "Inside" = XZ in Level AABB AND Y within [MinY-0.6, MaxY+0.6].
// Reading roster.Members past Count returns recycled IDs that crash PosMap.Get.
func (s *doorSceneState) countInside() (int, int) {
	roster := s.RosterMap.Get(s.Squad)
	if roster == nil {
		return 0, 0
	}
	const yPad float32 = 0.6
	inside, total := 0, 0
	for _, m := range roster.Members[:roster.Count] {
		if !s.World.Alive(m) {
			continue
		}
		mp := s.PosMap.Get(m)
		if mp == nil {
			continue
		}
		total++
		wx, wz := worldXZ(*mp)
		if !s.LevelAABB.ContainsXZ(wx, wz) {
			continue
		}
		if mp.Local.Y < s.LevelAABB.MinY-yPad || mp.Local.Y > s.LevelAABB.MaxY+yPad {
			continue
		}
		inside++
	}
	return inside, total
}

// HandleHotkeys reacts to O / I / K / U when the 3D panel is focused.
func (s *doorSceneState) HandleHotkeys(panel3DFocused bool) {
	if !s.initialised || !panel3DFocused {
		return
	}
	if rl.IsKeyPressed(rl.KeyO) {
		s.Dump("BEFORE Garrison")
		s.SquadService.IssueOrder(s.Squad, components.OrderKindGarrison,
			s.DoorPos, s.Building, false, systems.OrderParams{})
		fmt.Println("[scene-door] -> Garrison(building) issued")
	}
	if rl.IsKeyPressed(rl.KeyI) {
		target := s.DoorPos.Add(rl.Vector3{X: 0, Z: -2})
		s.Dump("BEFORE MoveTo-outside")
		s.SquadService.IssueOrder(s.Squad, components.OrderKindMoveTo,
			target, ecs.Entity{}, false, systems.OrderParams{})
		fmt.Println("[scene-door] -> MoveTo(door - 2m south) issued")
	}
	if rl.IsKeyPressed(rl.KeyK) {
		target := s.DoorPos.Add(rl.Vector3{X: 0, Z: 2})
		s.Dump("BEFORE MoveTo-inside")
		s.SquadService.IssueOrder(s.Squad, components.OrderKindMoveTo,
			target, ecs.Entity{}, false, systems.OrderParams{})
		fmt.Println("[scene-door] -> MoveTo(door + 2m north, inside) issued")
	}
	if rl.IsKeyPressed(rl.KeyU) {
		s.Dump("manual U")
	}
}

func (s *doorSceneState) Dump(label string) {
	if !s.initialised {
		fmt.Printf("[scene-door] %s: state not yet initialised\n", label)
		return
	}
	roster := s.RosterMap.Get(s.Squad)
	if roster == nil {
		fmt.Printf("[scene-door] %s: roster missing on squad %v\n", label, s.Squad)
		return
	}
	center, ok := systems.SquadCenter(s.World, roster, s.PosMap)
	if !ok {
		fmt.Printf("[scene-door] %s: SquadCenter failed\n", label)
		return
	}
	cwx, cwz := worldXZ(center)
	dwx, dwz := worldXZ(s.DoorPos)
	dist := float32(math.Sqrt(float64((cwx-dwx)*(cwx-dwx) + (cwz-dwz)*(cwz-dwz))))
	fmt.Printf("[scene-door] %s\n", label)
	fmt.Printf("    squad center  world=(%.2f, %.2f)  chunk=%v  local=(%.2f, %.2f, %.2f)\n",
		cwx, cwz, center.Chunk, center.Local.X, center.Local.Y, center.Local.Z)
	fmt.Printf("    door target   world=(%.2f, %.2f)  chunk=%v  local=(%.2f, %.2f, %.2f)\n",
		dwx, dwz, s.DoorPos.Chunk, s.DoorPos.Local.X, s.DoorPos.Local.Y, s.DoorPos.Local.Z)
	fmt.Printf("    squad->door   %.2f m\n", dist)
	fmt.Printf("    member positions:\n")
	for i, m := range roster.Members[:roster.Count] {
		if !s.World.Alive(m) {
			continue
		}
		mp := s.PosMap.Get(m)
		if mp == nil {
			continue
		}
		wx, wz := worldXZ(*mp)
		insideTag := ""
		if s.LevelAABB.ContainsXZ(wx, wz) {
			insideTag = "  INSIDE"
		}
		fmt.Printf("      [%d] %v  world=(%.2f, %.2f)  local=(%.2f, %.2f, %.2f)%s\n",
			i, m, wx, wz, mp.Local.X, mp.Local.Y, mp.Local.Z, insideTag)
	}
}

func worldXZ(p components.WorldPos) (float32, float32) {
	return p.Local.X + float32(p.Chunk.X)*components.ChunkSize,
		p.Local.Z + float32(p.Chunk.Z)*components.ChunkSize
}
