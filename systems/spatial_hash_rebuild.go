package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SpatialHashRebuildSystem snapshots every Unit's world XZ / velocity /
// radius into the core.SpatialHash resource. MUST run BEFORE unit_movement;
// this serial pass is the only live Motion/Collider read for neighbours.
type SpatialHashRebuildSystem struct {
	hash        ecs.Resource[core.SpatialHash]
	vehHash     ecs.Resource[core.VehicleSpatialHash]
	unitFilter  *ecs.Filter2[components.Unit, components.WorldPos]
	vehFilter   *ecs.Filter2[components.Vehicle, components.WorldPos]
	motionMap   *ecs.Map[components.Motion]
	colliderMap *ecs.Map[components.Collider]
	snapshot    []core.SpatialEntry
	vehSnapshot []core.SpatialEntry
}

const (
	defaultUnitRadius    float32 = 0.4
	defaultVehicleRadius float32 = 3.0
)

// NewSpatialHashRebuildSystem. Both hash resources MUST be added to the
// world via ecs.AddResource(world, hash) BEFORE InitUI.
func NewSpatialHashRebuildSystem() *SpatialHashRebuildSystem {
	return &SpatialHashRebuildSystem{
		snapshot:    make([]core.SpatialEntry, 0, 64),
		vehSnapshot: make([]core.SpatialEntry, 0, 16),
	}
}

func (sys *SpatialHashRebuildSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.vehFilter = ecs.NewFilter2[components.Vehicle, components.WorldPos](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.hash = ecs.NewResource[core.SpatialHash](w)
	sys.vehHash = ecs.NewResource[core.VehicleSpatialHash](w)
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
	sys.snapshot = collectSpatial(sys, sys.unitFilter.Query(), sys.snapshot[:0], defaultUnitRadius)
	if h := sys.hash.Get(); h != nil {
		h.Rebuild(sys.snapshot)
	}
	sys.vehSnapshot = collectSpatial(sys, sys.vehFilter.Query(), sys.vehSnapshot[:0], defaultVehicleRadius)
	if h := sys.vehHash.Get(); h != nil {
		h.Rebuild(sys.vehSnapshot)
	}
}

func collectSpatial[T any](sys *SpatialHashRebuildSystem, q ecs.Query2[T, components.WorldPos],
	out []core.SpatialEntry, fallbackRadius float32) []core.SpatialEntry {
	for q.Next() {
		_, pos := q.Get()
		ent := q.Entity()
		var velX, velZ float32
		if mot := sys.motionMap.Get(ent); mot != nil && mot.Speed > 0 {
			velX = float32(math.Sin(float64(mot.VelocityYaw))) * mot.Speed
			velZ = float32(math.Cos(float64(mot.VelocityYaw))) * mot.Speed
		}
		radius := fallbackRadius
		if col := sys.colliderMap.Get(ent); col != nil && col.Radius > 0 {
			radius = col.Radius
		}
		out = append(out, core.SpatialEntry{
			Ent:    ent,
			X:      float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			Z:      float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			Y:      pos.Local.Y,
			VelX:   velX,
			VelZ:   velZ,
			Radius: radius,
		})
	}
	return out
}
