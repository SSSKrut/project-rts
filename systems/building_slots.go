package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SlotPolicy selects the placement rule for one building order kind.
type SlotPolicy uint8

const (
	// SlotRooms — storeys ceil(N/M) ascending, rooms round-robin within a
	// storey (OccupyBuilding default).
	SlotRooms SlotPolicy = iota
	// SlotWindows — fire positions at window openings facing outward;
	// overflow falls back to SlotRooms as the interior reserve (Garrison).
	SlotWindows
	// SlotHidden — SlotRooms pulled tight to room centres, away from the
	// openings (Hidden position).
	SlotHidden
	// SlotGroundFloor — SlotRooms restricted to the lowest storey (Clear
	// sweep phase).
	SlotGroundFloor
)

// BuildingSlot is one planned interior position for one roster index.
type BuildingSlot struct {
	Pos    components.WorldPos
	Yaw    float32
	HasYaw bool
	Window bool
}

// BuildingSlotPlanner resolves building-order placements. Shared by
// FormationSystem (execution), the ghost preview and the slot markers so the
// three cannot diverge. Stateless — read-only against the world, safe from
// FormationSystem's parallel workers.
type BuildingSlotPlanner struct {
	world          *ecs.World
	childIndex     ecs.Resource[BuildingChildIndex]
	posMap         *ecs.Map[components.WorldPos]
	floorMap       *ecs.Map[components.Floor]
	wallMap        *ecs.Map[components.WallSegment]
	windowMap      *ecs.Map[components.Window]
	coverDirMap    *ecs.Map[components.CoverDirection]
	levelMemberMap *ecs.Map[components.LevelMember]
	levelMap       *ecs.Map[components.Level]
	doorMap        *ecs.Map[components.Door]
}

func NewBuildingSlotPlanner(w *ecs.World) *BuildingSlotPlanner {
	return &BuildingSlotPlanner{
		world:          w,
		childIndex:     ecs.NewResource[BuildingChildIndex](w),
		posMap:         ecs.NewMap[components.WorldPos](w),
		floorMap:       ecs.NewMap[components.Floor](w),
		wallMap:        ecs.NewMap[components.WallSegment](w),
		windowMap:      ecs.NewMap[components.Window](w),
		coverDirMap:    ecs.NewMap[components.CoverDirection](w),
		levelMemberMap: ecs.NewMap[components.LevelMember](w),
		levelMap:       ecs.NewMap[components.Level](w),
		doorMap:        ecs.NewMap[components.Door](w),
	}
}

const (
	// Fire slot sits this far inside the opening plane.
	slotWindowInset float32 = 0.6
	// A member parked within this radius of a window slot snaps its yaw
	// to the opening's outward normal.
	SlotParkRadius    float32 = 0.8
	slotRoomSpacing   float32 = 1.2
	slotHiddenSpacing float32 = 0.8
	// Pre-entry stack beside a door (ClearSeq stack-up).
	stackStandoff   float32 = 1.1
	stackSpacing    float32 = 1.1
	stackSideOffset float32 = 1.2
)

type slotFloor struct {
	pos    components.WorldPos
	level  uint8
	rooms  [components.MaxRoomsPerLevel]components.AABB2D
	roomsN uint8
}

// PlanSlots returns `count` slots for `building` under `policy`; slot i is
// roster index i's goal. nil when the building's floor children aren't
// streamed in yet (caller falls back to plain interior spread). `facing`
// prioritises windows whose outward normal matches the sector; without it
// windows fill in spawn order — all-round defense.
func (p *BuildingSlotPlanner) PlanSlots(
	building ecs.Entity, policy SlotPolicy, count int,
	hasFacing bool, facingYaw float32,
) []BuildingSlot {
	if building == (ecs.Entity{}) || count <= 0 || !p.world.Alive(building) {
		return nil
	}
	idx := p.childIndex.Get()
	if idx == nil {
		return nil
	}
	children := idx.Loaded[building]
	if len(children) == 0 {
		return nil
	}

	floors := p.collectFloors(children)
	if len(floors) == 0 {
		return nil
	}
	if policy == SlotGroundFloor {
		floors = floors[:1]
	}

	out := make([]BuildingSlot, count)
	windowsN := 0
	if policy == SlotWindows {
		windows := p.collectWindowSlots(children, hasFacing, facingYaw)
		for i := 0; i < count && i < len(windows); i++ {
			out[i] = windows[i]
			windowsN++
		}
	}

	spacing := slotRoomSpacing
	if policy == SlotHidden {
		spacing = slotHiddenSpacing
	}
	fillRoomSlots(out, windowsN, floors, spacing)
	return out
}

// PlanFloorSlots spreads `count` slots across the rooms of ONE storey. The
// ClearSeq sweep drives every man from this list, which is what keeps the
// roster off storey k+1 until storey k is clean.
func (p *BuildingSlotPlanner) PlanFloorSlots(building ecs.Entity, level uint8, count int) []BuildingSlot {
	if building == (ecs.Entity{}) || count <= 0 || !p.world.Alive(building) {
		return nil
	}
	idx := p.childIndex.Get()
	if idx == nil {
		return nil
	}
	floors := p.collectFloors(idx.Loaded[building])
	if len(floors) == 0 {
		return nil
	}
	pick := 0
	for i := range floors {
		if floors[i].level == level {
			pick = i
			break
		}
	}
	out := make([]BuildingSlot, count)
	fillRoomSlots(out, 0, floors[pick:pick+1], slotRoomSpacing)
	return out
}

// PlanStackUp returns the pre-entry stack at the door nearest `from`: the men
// press against the facade to either side of the opening, facing it.
func (p *BuildingSlotPlanner) PlanStackUp(building ecs.Entity, from components.WorldPos, count int) []BuildingSlot {
	if building == (ecs.Entity{}) || count <= 0 || !p.world.Alive(building) {
		return nil
	}
	idx := p.childIndex.Get()
	if idx == nil {
		return nil
	}
	doorPos, outward, ok := p.nearestDoor(idx.Loaded[building], from)
	if !ok {
		return nil
	}
	tanX, tanZ := outward.Z, -outward.X
	yaw := float32(math.Atan2(float64(-outward.X), float64(-outward.Z)))
	out := make([]BuildingSlot, count)
	for i := 0; i < count; i++ {
		// Beside the opening, hugging the facade — a file straight out from the
		// door is the fatal funnel, and the defenders inside shoot down it.
		side := stackSideOffset + stackSpacing*float32(i/2)
		if i%2 == 1 {
			side = -side
		}
		out[i] = BuildingSlot{
			Pos: doorPos.Add(rl.Vector3{
				X: outward.X*stackStandoff + tanX*side,
				Z: outward.Z*stackStandoff + tanZ*side,
			}),
			Yaw:    yaw,
			HasYaw: true,
		}
	}
	return out
}

// nearestDoor returns the opening centre and outward normal of the door
// closest to `from`.
func (p *BuildingSlotPlanner) nearestDoor(children []ecs.Entity,
	from components.WorldPos) (components.WorldPos, rl.Vector3, bool) {

	fx, fz := worldXZ(from)
	best := float32(math.MaxFloat32)
	var bestPos components.WorldPos
	var bestOut rl.Vector3
	found := false
	for _, ch := range children {
		if !p.world.Alive(ch) || p.doorMap.Get(ch) == nil {
			continue
		}
		wall := p.wallMap.Get(ch)
		wp := p.posMap.Get(ch)
		if wall == nil || wp == nil {
			continue
		}
		var outward rl.Vector3
		if cd := p.coverDirMap.Get(ch); cd != nil {
			outward = cd.Dir
		}
		if outward.X == 0 && outward.Z == 0 {
			continue
		}
		t := wall.OpeningCenterT * wall.Length
		pos := wp.Add(rl.Vector3{
			X: float32(math.Sin(float64(wall.Yaw))) * t,
			Z: float32(math.Cos(float64(wall.Yaw))) * t,
		})
		x, z := worldXZ(pos)
		d := (x-fx)*(x-fx) + (z-fz)*(z-fz)
		if d < best {
			best, bestPos, bestOut, found = d, pos, outward, true
		}
	}
	return bestPos, bestOut, found
}

// collectFloors gathers the building's storeys with their room rects, sorted
// by level.
func (p *BuildingSlotPlanner) collectFloors(children []ecs.Entity) []slotFloor {
	var floors []slotFloor
	for _, ch := range children {
		if !p.world.Alive(ch) {
			continue
		}
		f := p.floorMap.Get(ch)
		if f == nil {
			continue
		}
		fp := p.posMap.Get(ch)
		if fp == nil {
			continue
		}
		sf := slotFloor{pos: *fp, level: f.Level}
		if lm := p.levelMemberMap.Get(ch); lm != nil &&
			lm.Level != (ecs.Entity{}) && p.world.Alive(lm.Level) {
			if lv := p.levelMap.Get(lm.Level); lv != nil {
				sf.rooms = lv.Rooms
				sf.roomsN = lv.RoomCount
			}
		}
		floors = append(floors, sf)
	}
	for i := 1; i < len(floors); i++ {
		for j := i; j > 0 && floors[j-1].level > floors[j].level; j-- {
			floors[j-1], floors[j] = floors[j], floors[j-1]
		}
	}
	return floors
}

// fillRoomSlots writes out[from:] as room-spread positions across `floors`.
func fillRoomSlots(out []BuildingSlot, from int, floors []slotFloor, spacing float32) {
	rest := len(out) - from
	if rest <= 0 || len(floors) == 0 {
		return
	}
	floorN := len(floors)
	perFloor := (rest + floorN - 1) / floorN
	for k := 0; k < rest; k++ {
		fi := k / perFloor
		if fi >= floorN {
			fi = floorN - 1
		}
		wi := k % perFloor
		fl := &floors[fi]
		var target components.WorldPos
		if rc := int(fl.roomsN); rc > 1 {
			room := fl.rooms[wi%rc]
			offX, offZ := FormationOffset(components.FormationLoose,
				uint8(wi/rc), spacing, rl.Vector3{X: 0, Y: 0, Z: 1})
			target = components.WorldPos{}.Add(rl.Vector3{
				X: room.CenterX() + offX,
				Y: fl.pos.Local.Y,
				Z: room.CenterZ() + offZ,
			})
		} else {
			offX, offZ := FormationOffset(components.FormationLoose,
				uint8(wi), spacing, rl.Vector3{X: 0, Y: 0, Z: 1})
			target = fl.pos.Add(rl.Vector3{X: offX, Y: 0, Z: offZ})
		}
		out[from+k] = BuildingSlot{Pos: target}
	}
}

// collectWindowSlots emits one fire slot per window opening: opening centre
// pulled slotWindowInset inside, yaw = outward normal. With a facing sector
// the list sorts by outward·facing descending (stable), so threat-side
// windows man first.
func (p *BuildingSlotPlanner) collectWindowSlots(
	children []ecs.Entity, hasFacing bool, facingYaw float32,
) []BuildingSlot {
	var out []BuildingSlot
	var score []float32
	fx := float32(math.Sin(float64(facingYaw)))
	fz := float32(math.Cos(float64(facingYaw)))
	for _, ch := range children {
		if !p.world.Alive(ch) {
			continue
		}
		if p.windowMap.Get(ch) == nil {
			continue
		}
		wall := p.wallMap.Get(ch)
		wp := p.posMap.Get(ch)
		if wall == nil || wp == nil {
			continue
		}
		var outward rl.Vector3
		if cd := p.coverDirMap.Get(ch); cd != nil {
			outward = cd.Dir
		}
		t := wall.OpeningCenterT * wall.Length
		sa := float32(math.Sin(float64(wall.Yaw)))
		ca := float32(math.Cos(float64(wall.Yaw)))
		pos := wp.Add(rl.Vector3{
			X: sa*t - outward.X*slotWindowInset,
			Z: ca*t - outward.Z*slotWindowInset,
		})
		out = append(out, BuildingSlot{
			Pos:    pos,
			Yaw:    float32(math.Atan2(float64(outward.X), float64(outward.Z))),
			HasYaw: true,
			Window: true,
		})
		score = append(score, outward.X*fx+outward.Z*fz)
	}
	if hasFacing {
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && score[j-1] < score[j]; j-- {
				score[j-1], score[j] = score[j], score[j-1]
				out[j-1], out[j] = out[j], out[j-1]
			}
		}
	}
	return out
}
