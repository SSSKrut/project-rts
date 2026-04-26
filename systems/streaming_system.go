package systems

import (
	"time"

	"rts-go/components"
	"rts-go/ecs"
)

// StreamingSystem updates the state of nodes based on the player's current node.
type StreamingSystem struct{}

func (StreamingSystem) Name() string { return "streaming" }
func (StreamingSystem) Phase() ecs.Phase { return ecs.PhaseLogic }
func (StreamingSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: ecs.LODDisabled,
		DormantEvery:  ecs.LODDisabled,
	}
}

func (StreamingSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{
		ecs.TypeOf[components.LODAnchor](),
		ecs.TypeOf[components.NodeEntity](),
		ecs.TypeOf[components.StreamingMap](),
	}
}

func (StreamingSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{
		ecs.TypeOf[components.StreamingMap](),
		ecs.TypeOf[ecs.LOD](),
	}
}

func (StreamingSystem) Update(ctx ecs.UpdateContext) {
	var anchorNode components.NodeID
	foundAnchor := false

	ecs.ForEach2[components.LODAnchor, components.NodeEntity](ctx.State, func(_ ecs.EntityID, _ *components.LODAnchor, nodeEnt *components.NodeEntity) {
		if !foundAnchor {
			anchorNode = nodeEnt.NodeID
			foundAnchor = true
		}
	})

	if !foundAnchor {
		return
	}

	var sMap components.StreamingMap
	var sMapID ecs.EntityID
	foundMap := false

	ecs.ForEach[components.StreamingMap](ctx.State, func(id ecs.EntityID, m *components.StreamingMap) {
		if !foundMap {
			sMap = *m
			sMapID = id
			foundMap = true
		}
	})

	if !foundMap {
		return
	}

	newStates := make(map[components.NodeID]components.NodeState)
	newStates[anchorNode] = components.NodeStateActive

	if nodeDef, ok := sMap.Nodes[anchorNode]; ok {
		for _, adj := range nodeDef.Connections {
			newStates[adj] = components.NodeStateLoaded
		}
	}

	ecs.Set(ctx.State, sMapID, components.StreamingMap{
		Nodes:  sMap.Nodes,
		States: newStates,
	})

	ecs.ForEach2[components.NodeEntity, ecs.LOD](ctx.State, func(id ecs.EntityID, nodeEnt *components.NodeEntity, lod *ecs.LOD) {
		ns := newStates[nodeEnt.NodeID]

		switch ns {
		case components.NodeStateActive:
			if lod.Level != ecs.LODActive {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODActive})
			}
		case components.NodeStateLoaded:
			if lod.Level != ecs.LODRelevant {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODRelevant})
			}
		default:
			if lod.Level != ecs.LODDormant {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODDormant})
			}
		}
	})
}
