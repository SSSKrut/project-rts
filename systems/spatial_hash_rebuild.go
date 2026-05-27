package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SpatialHashRebuildSystem snapshots every Unit's world XZ into the
// core.SpatialHash resource at the start of each tick. MUST run BEFORE
// unit_movement (separation steering reads the hash). WeaponSystem /
// VisionSystem tolerate 1-tick staleness — query radii are 1.5 m+ vs.
// ~8 cm/tick top speed.
type SpatialHashRebuildSystem struct {
	hash       ecs.Resource[core.SpatialHash]
	unitFilter *ecs.Filter2[components.Unit, components.WorldPos]
	snapshot   []core.SpatialEntry
}

// NewSpatialHashRebuildSystem. The hash resource MUST be added to the world
// via ecs.AddResource(world, hash) BEFORE InitUI.
func NewSpatialHashRebuildSystem() *SpatialHashRebuildSystem {
	return &SpatialHashRebuildSystem{
		snapshot: make([]core.SpatialEntry, 0, 64),
	}
}

func (sys *SpatialHashRebuildSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.hash = ecs.NewResource[core.SpatialHash](w)
}

func (SpatialHashRebuildSystem) Name() string { return "spatial_hash_rebuild" }

func (SpatialHashRebuildSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *SpatialHashRebuildSystem) Update(_ core.UpdateContext) {
	sys.snapshot = sys.snapshot[:0]
	q := sys.unitFilter.Query()
	for q.Next() {
		_, pos := q.Get()
		sys.snapshot = append(sys.snapshot, core.SpatialEntry{
			Ent: q.Entity(),
			X:   float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			Z:   float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
		})
	}
	if h := sys.hash.Get(); h != nil {
		h.Rebuild(sys.snapshot)
	}
}
