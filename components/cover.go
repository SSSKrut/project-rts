package components

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// CoverHostKind tags where a cover slot grew from. Tactical AI in Phase 10
// reads this to pick stance / approach behaviour (a prop slot lets you crouch
// behind a tree; a wall corner lets you peek around a building edge).
type CoverHostKind uint8

const (
	CoverHostProp CoverHostKind = iota
	CoverHostWindow
	CoverHostWallCorner
)

// StanceMask is a bit set of the stances a slot supports — e.g. a low bush
// only allows prone/crouch; a tall wall corner allows all three.
type StanceMask uint8

const (
	StanceMaskProne StanceMask = 1 << iota
	StanceMaskCrouch
	StanceMaskStand
)

// CoverSlot is the per-slot data on its own entity. WorldPos lives separately
// (slot is a real spatial entity, addressable by AI radius queries). Host is
// the prop / wall / corner-anchor that emitted the slot — Phase 11 destruction
// uses CoverSlotIndex.ByHost[host] to wipe slots when the host dies.
//
// OriginDir is a unit vector pointing OUTWARD from cover, i.e. the direction a
// shooter should look. Quality is a static [0..255] pre-bake estimate; Phase
// 10 reranks dynamically per threat vector.
type CoverSlot struct {
	Host      ecs.Entity
	HostKind  CoverHostKind
	OriginDir rl.Vector3
	Quality   uint8
	Stance    StanceMask
}
