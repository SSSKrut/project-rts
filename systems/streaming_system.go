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

	for _, id := range ctx.State.Query(ecs.TypeOf[components.LODAnchor](), ecs.TypeOf[components.NodeEntity]()) {
		nodeEnt, ok := ecs.Get[components.NodeEntity](ctx.State, id)
		if ok {
			anchorNode = nodeEnt.NodeID
			foundAnchor = true
			break
		}
	}

	if !foundAnchor {
		return
	}

	var sMap components.StreamingMap
	var sMapID ecs.EntityID
	foundMap := false

	for _, id := range ctx.State.Query(ecs.TypeOf[components.StreamingMap]()) {
		sMap, _ = ecs.Get[components.StreamingMap](ctx.State, id)
		sMapID = id
		foundMap = true
		break
	}

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

	for _, id := range ctx.State.Query(ecs.TypeOf[components.NodeEntity]()) {
		nodeEnt, _ := ecs.Get[components.NodeEntity](ctx.State, id)
		ns := newStates[nodeEnt.NodeID]

		switch ns {
		case components.NodeStateActive:
			if ecs.LODLevelForEntity(ctx.State, id) != ecs.LODActive {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODActive})
			}
		case components.NodeStateLoaded:
			if ecs.LODLevelForEntity(ctx.State, id) != ecs.LODRelevant {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODRelevant})
			}
		default:
			if ecs.LODLevelForEntity(ctx.State, id) != ecs.LODDormant {
				ecs.Set(ctx.State, id, ecs.LOD{Level: ecs.LODDormant})
			}
		}
	}
}
