package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// LevelVisibilitySystem flips LevelVisibility.Discovered to true and refreshes
// LastSeenAt for every Level whose AABB contains a friendly's XZ (with a
// small Y proximity gate).
type LevelVisibilitySystem struct {
	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
	unitFilter   *ecs.Filter2[components.Unit, components.WorldPos]
	levelFilter  *ecs.Filter2[components.Level, components.LevelVisibility]
	visMap       *ecs.Map[components.LevelVisibility]
	factionMap   *ecs.Map[components.Faction]
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
	sys.factionMap = ecs.NewMap[components.Faction](w)
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
		// Only player-faction units reveal interiors (missing Faction =
		// player by the zero-value convention). An enemy patrol walking
		// through a building must not defog it for the player.
		if f := sys.factionMap.Get(qu.Entity()); f != nil && f.ID != components.FactionPlayer {
			continue
		}
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

// Clock exposes the session-time accumulator for readers comparing against
// LevelVisibility.LastSeenAt.
func (sys *LevelVisibilitySystem) Clock() float32 {
	return sys.clock
}
