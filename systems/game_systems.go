package systems

import (
	"time"

	"rts-go/components"
	"rts-go/ecs"
)

// LODSystem assigns LOD levels based on distance to the anchor.
type LODSystem struct {
	ActiveRadius   float32
	RelevantRadius float32
	Hysteresis     float32
}

func (LODSystem) Name() string { return "lod" }

func (LODSystem) Phase() ecs.Phase { return ecs.PhaseLogic }

func (LODSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: ecs.LODDisabled,
		DormantEvery:  ecs.LODDisabled,
	}
}

func (LODSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{
		ecs.TypeOf[components.Position3D](),
		ecs.TypeOf[ecs.LOD](),
		ecs.TypeOf[components.LODAnchor](),
		ecs.TypeOf[components.AlwaysActive](),
	}
}

func (LODSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[ecs.LOD]()}
}

func (system LODSystem) Update(ctx ecs.UpdateContext) {
	var (
		anchorID ecs.EntityID
		anchor   components.Position3D
		found    bool
	)
	for _, id := range ctx.State.Query(ecs.TypeOf[components.LODAnchor](), ecs.TypeOf[components.Position3D]()) {
		position, ok := ecs.Get[components.Position3D](ctx.State, id)
		if !ok {
			continue
		}
		anchor = position
		anchorID = id
		found = true
		break
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

	for _, id := range ctx.State.Query(ecs.TypeOf[components.Position3D]()) {
		if id == anchorID {
			continue
		}
		if _, ok := ctx.State.GetComponent(id, ecs.TypeOf[components.AlwaysActive]()); ok {
			if ecs.LODLevelForEntity(ctx.State, id) != ecs.LODActive {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODActive})
			}
			continue
		}

		position, ok := ecs.Get[components.Position3D](ctx.State, id)
		if !ok {
			continue
		}
		dx := position.X - anchor.X
		dy := position.Y - anchor.Y
		dz := position.Z - anchor.Z
		dist2 := dx*dx + dy*dy + dz*dz

		currentLOD := ecs.LODLevelForEntity(ctx.State, id)
		nextLOD := currentLOD
		switch currentLOD {
		case ecs.LODActive:
			if dist2 > activeOut2 {
				nextLOD = ecs.LODRelevant
			}
		case ecs.LODRelevant:
			if dist2 <= activeIn2 {
				nextLOD = ecs.LODActive
			} else if dist2 > relevantOut2 {
				nextLOD = ecs.LODDormant
			}
		case ecs.LODDormant:
			if dist2 <= relevantIn2 {
				nextLOD = ecs.LODRelevant
			}
		}

		if nextLOD != currentLOD {
			ecs.Set(ctx.State, id, ecs.LOD{Level: nextLOD})
		}
	}
}

// MovementSystem updates positions based on velocity.
type MovementSystem struct{}

func (MovementSystem) Name() string { return "movement" }

func (MovementSystem) Phase() ecs.Phase { return ecs.PhaseLogic }

func (MovementSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  time.Second,
	}
}

func (MovementSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[components.Position3D](), ecs.TypeOf[components.Velocity3D]()}
}

func (MovementSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[components.Position3D]()}
}

func (MovementSystem) Update(ctx ecs.UpdateContext) {
	dt := float32(ctx.Delta.Seconds())
	bounds := float32(30.0)
	for _, id := range ecs.QueryLOD(ctx.State, ctx.LOD, ecs.TypeOf[components.Position3D](), ecs.TypeOf[components.Velocity3D]()) {
		position, ok := ecs.Get[components.Position3D](ctx.State, id)
		if !ok {
			continue
		}
		velocity, ok := ecs.Get[components.Velocity3D](ctx.State, id)
		if !ok {
			continue
		}

		position.X += velocity.X * dt
		position.Y += velocity.Y * dt
		position.Z += velocity.Z * dt

		// Simple wrap around grid bounds
		if position.X < -bounds {
			position.X = bounds
		}
		if position.X > bounds {
			position.X = -bounds
		}
		if position.Z < -bounds {
			position.Z = bounds
		}
		if position.Z > bounds {
			position.Z = -bounds
		}

		ecs.Set(ctx.State, id, position)
	}
}
