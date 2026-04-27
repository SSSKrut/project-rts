package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"
	"rts-go/components"
	"rts-go/core"
)

// LODSystem assigns LOD marker components based on distance to the anchor.
type LODSystem struct {
	ActiveRadius   float32
	RelevantRadius float32
	Hysteresis     float32

	// Pre-built Maps for component manipulation
	lodActiveMap    *ecs.Map[components.LODActive]
	lodRelevantMap  *ecs.Map[components.LODRelevant]
	lodDormantMap   *ecs.Map[components.LODDormant]
	posMap          *ecs.Map[components.Position3D]
	alwaysActiveMap *ecs.Map[components.AlwaysActive]

	// Pre-built Filters for queries
	anchorFilter *ecs.Filter2[components.LODAnchor, components.Position3D]
	posFilter    *ecs.Filter1[components.Position3D]
}

func (system *LODSystem) InitUI(w *ecs.World) {
	system.lodActiveMap = ecs.NewMap[components.LODActive](w)
	system.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	system.lodDormantMap = ecs.NewMap[components.LODDormant](w)
	system.posMap = ecs.NewMap[components.Position3D](w)
	system.alwaysActiveMap = ecs.NewMap[components.AlwaysActive](w)
	system.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.Position3D](w)
	system.posFilter = ecs.NewFilter1[components.Position3D](w)
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
		anchorID ecs.Entity
		anchor   components.Position3D
		found    bool
	)

	// Find the LOD anchor using stored filter
	q := system.anchorFilter.Query()
	for q.Next() {
		if !found {
			_, pos := q.Get()
			anchor = *pos
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

	// Collect entities that need LOD changes — can't modify archetypes during iteration
	type lodChange struct {
		id     ecs.Entity
		remove core.LODTier
		add    core.LODTier
	}
	var changes []lodChange

	// Iterate all entities with Position (skip anchor)
	q2 := system.posFilter.Query()
	for q2.Next() {
		id := q2.Entity()
		if id == anchorID {
			continue
		}

		// Check for AlwaysActive — force Active
		if system.alwaysActiveMap.Has(id) {
			if !system.lodActiveMap.Has(id) {
				changes = append(changes, lodChange{id, -1, core.LODTierActive})
			}
			continue
		}

		pos := q2.Get()

		dx := pos.X - anchor.X
		dy := pos.Y - anchor.Y
		dz := pos.Z - anchor.Z
		dist2 := dx*dx + dy*dy + dz*dz

		// Determine current LOD tier from marker components
		var currentTier core.LODTier
		if system.lodActiveMap.Has(id) {
			currentTier = core.LODTierActive
		} else if system.lodRelevantMap.Has(id) {
			currentTier = core.LODTierRelevant
		} else if system.lodDormantMap.Has(id) {
			currentTier = core.LODTierDormant
		} else {
			// Entity has no LOD marker — assign based on distance
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

		// Compute next LOD tier with hysteresis
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

	// Apply LOD changes — archetype modifications must happen outside iteration
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

// MovementSystem updates positions based on velocity.
type MovementSystem struct {
	// Pre-built Filters per LOD tier
	activeFilter   *ecs.Filter3[components.Position3D, components.Velocity3D, components.LODActive]
	relevantFilter *ecs.Filter3[components.Position3D, components.Velocity3D, components.LODRelevant]
	dormantFilter  *ecs.Filter3[components.Position3D, components.Velocity3D, components.LODDormant]
}

func (system *MovementSystem) InitUI(w *ecs.World) {
	system.activeFilter = ecs.NewFilter3[components.Position3D, components.Velocity3D, components.LODActive](w)
	system.relevantFilter = ecs.NewFilter3[components.Position3D, components.Velocity3D, components.LODRelevant](w)
	system.dormantFilter = ecs.NewFilter3[components.Position3D, components.Velocity3D, components.LODDormant](w)
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
	bounds := float32(30.0)

	switch ctx.Tier {
	case core.LODTierActive:
		q := system.activeFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			pos.X += vel.X * dt
			pos.Y += vel.Y * dt
			pos.Z += vel.Z * dt
			wrapBounds(pos, bounds)
		}
	case core.LODTierRelevant:
		q := system.relevantFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			pos.X += vel.X * dt
			pos.Y += vel.Y * dt
			pos.Z += vel.Z * dt
			wrapBounds(pos, bounds)
		}
	case core.LODTierDormant:
		q := system.dormantFilter.Query()
		for q.Next() {
			pos, vel, _ := q.Get()
			pos.X += vel.X * dt
			pos.Y += vel.Y * dt
			pos.Z += vel.Z * dt
			wrapBounds(pos, bounds)
		}
	}
}

func wrapBounds(pos *components.Position3D, bounds float32) {
	if pos.X < -bounds {
		pos.X = bounds
	}
	if pos.X > bounds {
		pos.X = -bounds
	}
	if pos.Z < -bounds {
		pos.Z = bounds
	}
	if pos.Z > bounds {
		pos.Z = -bounds
	}
}
