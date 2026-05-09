package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AnchorEyeHeight: offset above the terrain surface for the anchor and any
// future ground-stuck pawn. Roughly average human eye level.
const AnchorEyeHeight float32 = 1.5

// GroundStickSystem clamps the anchor's Local.Y to the terrain surface every
// tick using the same GroundHeight() the procgen does, so the anchor sits
// exactly on the meshed surface.
//
// Filter is currently LODAnchor-only; widening to a generic GroundStick marker
// is a one-line change once units need it.
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
