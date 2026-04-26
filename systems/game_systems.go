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

	// Find the LOD anchor
	ecs.ForEach2[components.LODAnchor, components.Position3D](ctx.State, func(id ecs.EntityID, _ *components.LODAnchor, pos *components.Position3D) {
		if !found {
			anchor = *pos
			anchorID = id
			found = true
		}
	})
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

	ecs.ForEach2[components.Position3D, ecs.LOD](ctx.State, func(id ecs.EntityID, pos *components.Position3D, lod *ecs.LOD) {
		if id == anchorID {
			return
		}

		// Check for AlwaysActive
		if ecs.Has[components.AlwaysActive](ctx.State, id) {
			if lod.Level != ecs.LODActive {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODActive})
			}
			return
		}

		dx := pos.X - anchor.X
		dy := pos.Y - anchor.Y
		dz := pos.Z - anchor.Z
		dist2 := dx*dx + dy*dy + dz*dz

		currentLOD := lod.Level
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
	})
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

	// Process entities at the current LOD level
	ecs.ForEach3LOD[components.Position3D, components.Velocity3D, ecs.LOD](ctx.State, ctx.LOD, func(id ecs.EntityID, pos *components.Position3D, vel *components.Velocity3D, _ *ecs.LOD) {
		pos.X += vel.X * dt
		pos.Y += vel.Y * dt
		pos.Z += vel.Z * dt

		// Simple wrap around grid bounds
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

		ecs.Set(ctx.State, id, *pos)
	})
}
