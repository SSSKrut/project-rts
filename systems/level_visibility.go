package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// LevelVisibilitySystem flips LevelVisibility.Discovered to true and refreshes
// LastSeenAt for every Level whose AABB currently contains a friendly's XZ
// (with a small Y proximity gate). Phase 16.C.2 uses the LODAnchor as the
// proxy "friendly" so fog can be tested without unit AI; once units enter
// buildings (Phase 16.B.2 ClearBuilding / Garrison) they'll be the real
// signal.
type LevelVisibilitySystem struct {
	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
	unitFilter   *ecs.Filter2[components.Unit, components.WorldPos]
	levelFilter  *ecs.Filter2[components.Level, components.LevelVisibility]
	visMap       *ecs.Map[components.LevelVisibility]
	clock        float32
}

func NewLevelVisibilitySystem() *LevelVisibilitySystem {
	return &LevelVisibilitySystem{}
}

func (sys *LevelVisibilitySystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.levelFilter = ecs.NewFilter2[components.Level, components.LevelVisibility](w)
	sys.visMap = ecs.NewMap[components.LevelVisibility](w)
}

func (LevelVisibilitySystem) Name() string { return "level_visibility" }

func (LevelVisibilitySystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *LevelVisibilitySystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	sys.clock += float32(ctx.Delta.Seconds())

	// Collect friendly XYZ positions (anchor + units). Buildings rarely
	// span more than a few chunks; the level loop below is small.
	type pt struct{ x, y, z float32 }
	var pts []pt
	qa := sys.anchorFilter.Query()
	for qa.Next() {
		_, pos := qa.Get()
		pts = append(pts, pt{
			x: pos.Local.X + float32(pos.Chunk.X)*components.ChunkSize,
			y: pos.Local.Y,
			z: pos.Local.Z + float32(pos.Chunk.Z)*components.ChunkSize,
		})
	}
	qu := sys.unitFilter.Query()
	for qu.Next() {
		_, pos := qu.Get()
		pts = append(pts, pt{
			x: pos.Local.X + float32(pos.Chunk.X)*components.ChunkSize,
			y: pos.Local.Y,
			z: pos.Local.Z + float32(pos.Chunk.Z)*components.ChunkSize,
		})
	}
	if len(pts) == 0 {
		return
	}

	const yPad float32 = 0.6
	qL := sys.levelFilter.Query()
	for qL.Next() {
		lvl, vis := qL.Get()
		seen := false
		for _, p := range pts {
			if !lvl.AABB.ContainsXZ(p.x, p.z) {
				continue
			}
			if p.y < lvl.AABB.MinY-yPad || p.y > lvl.AABB.MaxY+yPad {
				continue
			}
			seen = true
			break
		}
		if seen {
			vis.Discovered = true
			vis.LastSeenAt = sys.clock
		}
	}
}

// Clock exposes the system's session-time accumulator so readers (renderer)
// can compare against LevelVisibility.LastSeenAt without a separate clock.
func (sys *LevelVisibilitySystem) Clock() float32 {
	return sys.clock
}
