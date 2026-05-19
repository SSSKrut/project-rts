// Package building_gen produces *components.BuildingPlan deterministically
// from a (template, seed, params) triple. The output shape is identical to
// what the Phase 16.A .glb loader emits, so downstream systems consume both
// interchangeably.
package building_gen

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// Builder assembles a BuildingPlan step by step. Helper methods return
// indices into the relevant slice (used for cross-references like
// LevelTransitionSpec.ViaWall).
//
// All Local-coord inputs are chunk-local of the building's host chunk
// (Pos.Chunk). The builder performs no validation - run Validate(plan)
// after Plan() to catch mistakes.
type Builder struct {
	plan components.BuildingPlan
}

// NewBuilder seeds a plan with placement metadata.
func NewBuilder(pos components.WorldPos, kind components.BuildingKind, yaw float32, seed uint64) *Builder {
	return &Builder{
		plan: components.BuildingPlan{
			Pos:  pos,
			Kind: kind,
			Yaw:  yaw,
			Seed: seed,
		},
	}
}

func (b *Builder) SetSize(sizeX, sizeZ float32) {
	b.plan.Size = rl.Vector2{X: sizeX, Y: sizeZ}
}

func (b *Builder) SetStories(n uint8) {
	b.plan.Stories = n
}

// AddLevel appends a level volume and returns its index in BuildingPlan.Levels.
func (b *Builder) AddLevel(name string, aabb components.AABB3D, displayOrder uint8) uint8 {
	idx := uint8(len(b.plan.Levels))
	b.plan.Levels = append(b.plan.Levels, components.LevelSpec{
		Name:         name,
		AABB:         aabb,
		DisplayOrder: displayOrder,
	})
	return idx
}

// AddWall appends a wall segment and returns its index in BuildingPlan.Walls.
// `from` and `to` are chunk-local endpoints; length and yaw are derived.
// `outward` is the OUTWARD-pointing XZ unit vector (caller knows which side
// is outside / cover-providing).
//
// Yaw convention matches WallSegment.Yaw / Stairs.Yaw: after a Rotatef(yaw,
// +Y), local +Z points along the wall direction (from -> to). So yaw=0 for
// a wall running along world +Z, yaw=pi/2 for +X, etc.
func (b *Builder) AddWall(from, to rl.Vector3, height, thickness float32, outward rl.Vector3, levelRefs ...uint8) int {
	dx := to.X - from.X
	dz := to.Z - from.Z
	length := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	yaw := float32(math.Atan2(float64(dx), float64(dz)))
	idx := len(b.plan.Walls)
	b.plan.Walls = append(b.plan.Walls, components.WallSpec{
		Local: from,
		Segment: components.WallSegment{
			Length:    length,
			Yaw:       yaw,
			Height:    height,
			Thickness: thickness,
		},
		OutwardNormal: outward,
		LevelRefs:     append([]uint8(nil), levelRefs...),
	})
	return idx
}

// SetOpening writes a door or window onto an existing wall. At most one
// opening per wall - multi-opening walls must be split by the caller.
// `centerT` is [0,1] along the wall length.
func (b *Builder) SetOpening(wallIdx int, kind components.OpeningKind, centerT, width, bottom, height float32) {
	w := &b.plan.Walls[wallIdx]
	w.Segment.OpeningKind = kind
	w.Segment.OpeningCenterT = centerT
	w.Segment.OpeningWidth = width
	w.Segment.OpeningBottom = bottom
	w.Segment.OpeningHeight = height
}

// AddFloor appends a horizontal floor plate and returns its index.
func (b *Builder) AddFloor(center rl.Vector3, sizeX, sizeZ float32, level, levelRef uint8) int {
	idx := len(b.plan.Floors)
	b.plan.Floors = append(b.plan.Floors, components.FloorSpec{
		Local:    center,
		Floor:    components.Floor{Level: level, SizeX: sizeX, SizeZ: sizeZ},
		LevelRef: levelRef,
	})
	return idx
}

// AddStraightStair appends a stair with a default 3-waypoint chain
// (bottom -> middle -> top) anchored at endpoints to the two given levels.
// Use AddCustomStair if you need switchback / spiral geometry. Yaw
// convention matches Stairs.Yaw: yaw=0 means the stair extends along +Z.
func (b *Builder) AddStraightStair(local rl.Vector3, yaw, length, width, rise float32, fromFloor, toFloor uint8, fromLevelRef, toLevelRef uint8) int {
	sin := float32(math.Sin(float64(yaw)))
	cos := float32(math.Cos(float64(yaw)))
	end := rl.Vector3{
		X: local.X + length*sin,
		Y: local.Y + rise,
		Z: local.Z + length*cos,
	}
	mid := rl.Vector3{
		X: 0.5 * (local.X + end.X),
		Y: 0.5 * (local.Y + end.Y),
		Z: 0.5 * (local.Z + end.Z),
	}
	return b.addStair(components.StairSpec{
		Local: local,
		Stairs: components.Stairs{
			FromFloor: fromFloor,
			ToFloor:   toFloor,
			Yaw:       yaw,
			Length:    length,
			Width:     width,
			Rise:      rise,
		},
		Waypoints: []rl.Vector3{local, mid, end},
		Anchors: []components.StairAnchor{
			{WpIndex: 0, LevelRef: fromLevelRef},
			{WpIndex: 2, LevelRef: toLevelRef},
		},
	})
}

// AddCustomStair appends a stair with an explicit waypoint chain and level
// anchors. Used by templates that need switchback / horizontal-passage
// topology.
func (b *Builder) AddCustomStair(local rl.Vector3, stairs components.Stairs, waypoints []rl.Vector3, anchors []components.StairAnchor) int {
	return b.addStair(components.StairSpec{
		Local:     local,
		Stairs:    stairs,
		Waypoints: append([]rl.Vector3(nil), waypoints...),
		Anchors:   append([]components.StairAnchor(nil), anchors...),
	})
}

func (b *Builder) addStair(s components.StairSpec) int {
	idx := len(b.plan.Stairs)
	b.plan.Stairs = append(b.plan.Stairs, s)
	return idx
}

// AddFurniture appends a furniture entry (sandbags / table / crate / ...).
func (b *Builder) AddFurniture(local rl.Vector3, kind components.PropType, yaw float32, levelRef uint8) int {
	idx := len(b.plan.Furniture)
	b.plan.Furniture = append(b.plan.Furniture, components.FurnitureSpec{
		Local:    local,
		Kind:     kind,
		Yaw:      yaw,
		LevelRef: levelRef,
	})
	return idx
}

// AddMarker appends a point-of-interest annotation.
func (b *Builder) AddMarker(local rl.Vector3, kind components.MarkerKind, levelRef uint8) int {
	idx := len(b.plan.Markers)
	b.plan.Markers = append(b.plan.Markers, components.MarkerSpec{
		Local:    local,
		Kind:     kind,
		LevelRef: levelRef,
	})
	return idx
}

// AddTransition appends a level<->level connection. `viaWall` is the index
// returned by AddWall (the wall carrying the door); pass -1 for stair /
// passage transitions (anchors carry the binding instead).
func (b *Builder) AddTransition(levelA, levelB uint8, viaWall int) {
	b.plan.LevelTransitions = append(b.plan.LevelTransitions, components.LevelTransitionSpec{
		LevelA:  levelA,
		LevelB:  levelB,
		ViaWall: int16(viaWall),
	})
}

// WallCount returns the current number of walls (handy when SetOpening
// targets the most recently added one without bookkeeping).
func (b *Builder) WallCount() int { return len(b.plan.Walls) }

// Plan returns a pointer to the assembled BuildingPlan. The Builder may
// be discarded after this call - the returned plan owns its slices.
func (b *Builder) Plan() *components.BuildingPlan {
	p := b.plan
	return &p
}
