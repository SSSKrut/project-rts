package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AnchorEyeHeight is the offset above the terrain surface that the anchor
// (and any future ground-stuck pawn) sits at. Roughly average human eye
// level — also keeps the camera target a touch above the ground.
const AnchorEyeHeight float32 = 1.5

// GroundStickSystem clamps the anchor's WorldPos.Local.Y to the terrain
// surface every tick. It uses the same GroundHeight() function as procgen,
// so the anchor sits exactly on the meshed surface, never below or above
// (Р9).
//
// In Phase 1 we ground-stick only the LODAnchor. A general GroundStick
// marker arrives in a later phase when units need it; widening the filter
// at that point is a one-line change.
type GroundStickSystem struct {
	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
}

func (sys *GroundStickSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
}

func (GroundStickSystem) Name() string { return "ground_stick" }

func (GroundStickSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys GroundStickSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	q := sys.anchorFilter.Query()
	for q.Next() {
		_, pos := q.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		pos.Local.Y = GroundHeight(wx, wz) + AnchorEyeHeight
	}
}
