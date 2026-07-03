package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SpatialHashRebuildSystem snapshots every Unit's world XZ + velocity +
// collider radius into the core.SpatialHash resource at the start of each
// tick. MUST run BEFORE unit_movement (separation / ORCA read the hash).
// This serial pass is the ONLY place that reads live Motion/Collider for
// neighbour purposes — parallel readers consume the frozen SpatialEntry
// (WS-B M1). WeaponSystem tolerates 1-tick staleness — query radii are
// 1.5 m+ vs. ~8 cm/tick top speed.
type SpatialHashRebuildSystem struct {
	hash        ecs.Resource[core.SpatialHash]
	unitFilter  *ecs.Filter2[components.Unit, components.WorldPos]
	motionMap   *ecs.Map[components.Motion]
	colliderMap *ecs.Map[components.Collider]
	snapshot    []core.SpatialEntry
}

// defaultUnitRadius backs SpatialEntry.Radius when a unit has no Collider —
// matches the historical ORCA fallback.
const defaultUnitRadius float32 = 0.4

// NewSpatialHashRebuildSystem. The hash resource MUST be added to the world
// via ecs.AddResource(world, hash) BEFORE InitUI.
func NewSpatialHashRebuildSystem() *SpatialHashRebuildSystem {
	return &SpatialHashRebuildSystem{
		snapshot: make([]core.SpatialEntry, 0, 64),
	}
}

func (sys *SpatialHashRebuildSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
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
		ent := q.Entity()
		var velX, velZ float32
		if mot := sys.motionMap.Get(ent); mot != nil && mot.Speed > 0 {
			velX = float32(math.Sin(float64(mot.VelocityYaw))) * mot.Speed
			velZ = float32(math.Cos(float64(mot.VelocityYaw))) * mot.Speed
		}
		radius := defaultUnitRadius
		if col := sys.colliderMap.Get(ent); col != nil && col.Radius > 0 {
			radius = col.Radius
		}
		sys.snapshot = append(sys.snapshot, core.SpatialEntry{
			Ent:    ent,
			X:      float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			Z:      float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			VelX:   velX,
			VelZ:   velZ,
			Radius: radius,
		})
	}
	if h := sys.hash.Get(); h != nil {
		h.Rebuild(sys.snapshot)
	}
}
