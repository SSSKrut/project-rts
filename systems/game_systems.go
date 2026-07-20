package systems

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
	"rts-go/components"
	"rts-go/core"
)

type LODSystem struct {
	ActiveRadius   float32
	RelevantRadius float32
	Hysteresis     float32

	lodActiveMap    *ecs.Map[components.LODActive]
	lodRelevantMap  *ecs.Map[components.LODRelevant]
	lodDormantMap   *ecs.Map[components.LODDormant]
	posMap          *ecs.Map[components.WorldPos]
	alwaysActiveMap *ecs.Map[components.AlwaysActive]

	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
	posFilter    *ecs.Filter1[components.WorldPos]
}

func (system *LODSystem) InitUI(w *ecs.World) {
	system.lodActiveMap = ecs.NewMap[components.LODActive](w)
	system.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	system.lodDormantMap = ecs.NewMap[components.LODDormant](w)
	system.posMap = ecs.NewMap[components.WorldPos](w)
	system.alwaysActiveMap = ecs.NewMap[components.AlwaysActive](w)
	system.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	// Excluded archetypes have their own LOD owner (TerrainStreaming /
	// PropSpawn / BuildingSystem / SpatialBake), or run universally
	// without LOD markers (Unit). Re-including them would cause
	// LODSystem to thrash markers on entities with no readers.
	system.posFilter = ecs.NewFilter1[components.WorldPos](w).
		Without(
			ecs.C[components.TerrainChunk](),
			ecs.C[components.Prop](),
			ecs.C[components.BuildingMember](),
			ecs.C[components.Unit](),
			ecs.C[components.Vehicle](),
			ecs.C[components.CoverSlot](),
		)
}

func (LODSystem) Name() string { return "lod" }

func (LODSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (system LODSystem) Update(ctx core.UpdateContext) {
	var (
		anchorID  ecs.Entity
		anchorPos components.WorldPos
		found     bool
	)

	q := system.anchorFilter.Query()
	for q.Next() {
		if !found {
			_, pos := q.Get()
			anchorPos = *pos
			anchorID = q.Entity()
			found = true
		}
	}
	if !found {
		return
	}

	activeIn := system.ActiveRadius - system.Hysteresis
	activeOut := system.ActiveRadius + system.Hysteresis
	relevantIn := system.RelevantRadius - system.Hysteresis
	relevantOut := system.RelevantRadius + system.Hysteresis

	if activeIn < 0 {
		activeIn = 0
	}
	if relevantIn < activeOut {
		relevantIn = activeOut
	}

	activeIn2 := activeIn * activeIn
	activeOut2 := activeOut * activeOut
	relevantIn2 := relevantIn * relevantIn
	relevantOut2 := relevantOut * relevantOut

	// Ark forbids archetype mutation during iteration; buffer changes.
	type lodChange struct {
		id     ecs.Entity
		remove core.LODTier
		add    core.LODTier
	}
	var changes []lodChange

	q2 := system.posFilter.Query()
	for q2.Next() {
		id := q2.Entity()
		if id == anchorID {
			continue
		}

		if system.alwaysActiveMap.Has(id) {
			if !system.lodActiveMap.Has(id) {
				changes = append(changes, lodChange{id, -1, core.LODTierActive})
			}
			continue
		}

		pos := q2.Get()
		dist2 := components.DistanceSquared(*pos, anchorPos)

		var currentTier core.LODTier
		if system.lodActiveMap.Has(id) {
			currentTier = core.LODTierActive
		} else if system.lodRelevantMap.Has(id) {
			currentTier = core.LODTierRelevant
		} else if system.lodDormantMap.Has(id) {
			currentTier = core.LODTierDormant
		} else {
			if dist2 <= activeOut2 {
				currentTier = core.LODTierActive
			} else if dist2 <= relevantOut2 {
				currentTier = core.LODTierRelevant
			} else {
				currentTier = core.LODTierDormant
			}
			changes = append(changes, lodChange{id, -1, currentTier})
			continue
		}

		nextTier := currentTier
		switch currentTier {
		case core.LODTierActive:
			if dist2 > activeOut2 {
				nextTier = core.LODTierRelevant
			}
		case core.LODTierRelevant:
			if dist2 <= activeIn2 {
				nextTier = core.LODTierActive
			} else if dist2 > relevantOut2 {
				nextTier = core.LODTierDormant
			}
		case core.LODTierDormant:
			if dist2 <= relevantIn2 {
				nextTier = core.LODTierRelevant
			}
		}

		if nextTier != currentTier {
			changes = append(changes, lodChange{id, currentTier, nextTier})
		}
	}

	for _, ch := range changes {
		switch ch.remove {
		case core.LODTierActive:
			system.lodActiveMap.Remove(ch.id)
		case core.LODTierRelevant:
			system.lodRelevantMap.Remove(ch.id)
		case core.LODTierDormant:
			system.lodDormantMap.Remove(ch.id)
		}
		switch ch.add {
		case core.LODTierActive:
			system.lodActiveMap.Add(ch.id, &components.LODActive{})
		case core.LODTierRelevant:
			system.lodRelevantMap.Add(ch.id, &components.LODRelevant{})
		case core.LODTierDormant:
			system.lodDormantMap.Add(ch.id, &components.LODDormant{})
		}
	}
}

type MovementSystem struct {
	activeFilter   *ecs.Filter3[components.WorldPos, components.Velocity3D, components.LODActive]
	relevantFilter *ecs.Filter3[components.WorldPos, components.Velocity3D, components.LODRelevant]
	dormantFilter  *ecs.Filter3[components.WorldPos, components.Velocity3D, components.LODDormant]
}

func (system *MovementSystem) InitUI(w *ecs.World) {
	system.activeFilter = ecs.NewFilter3[components.WorldPos, components.Velocity3D, components.LODActive](w)
	system.relevantFilter = ecs.NewFilter3[components.WorldPos, components.Velocity3D, components.LODRelevant](w)
	system.dormantFilter = ecs.NewFilter3[components.WorldPos, components.Velocity3D, components.LODDormant](w)
}

func (MovementSystem) Name() string { return "movement" }

func (MovementSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  time.Second,
	}
}

func (system MovementSystem) Update(ctx core.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())

	switch ctx.Tier {
	case core.LODTierActive:
		q := system.activeFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			*pos = pos.Add(rl.Vector3{X: vel.X * dt, Y: vel.Y * dt, Z: vel.Z * dt})
		}
	case core.LODTierRelevant:
		q := system.relevantFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			*pos = pos.Add(rl.Vector3{X: vel.X * dt, Y: vel.Y * dt, Z: vel.Z * dt})
		}
	case core.LODTierDormant:
		q := system.dormantFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			*pos = pos.Add(rl.Vector3{X: vel.X * dt, Y: vel.Y * dt, Z: vel.Z * dt})
		}
	}
}
