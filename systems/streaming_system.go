package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"
	"rts-go/components"
	"rts-go/core"
)

// StreamingSystem updates the state of nodes based on the player's current node.
type StreamingSystem struct {
	// Pre-built Filters and Maps
	anchorNodeFilter   *ecs.Filter2[components.LODAnchor, components.NodeEntity]
	streamingMapRes    ecs.Resource[components.StreamingMap]
	nodeEntityFilter   *ecs.Filter2[components.NodeEntity, components.LODActive]
	nodeRelevantFilter *ecs.Filter2[components.NodeEntity, components.LODRelevant]
	nodeDormantFilter  *ecs.Filter2[components.NodeEntity, components.LODDormant]
	lodActiveMap       *ecs.Map[components.LODActive]
	lodRelevantMap     *ecs.Map[components.LODRelevant]
	lodDormantMap      *ecs.Map[components.LODDormant]
	nodeEntityMap      *ecs.Map[components.NodeEntity]
	streamingMapMap    *ecs.Map[components.StreamingMap]
}

func (sys *StreamingSystem) InitUI(w *ecs.World) {
	sys.anchorNodeFilter = ecs.NewFilter2[components.LODAnchor, components.NodeEntity](w)
	sys.streamingMapRes = ecs.NewResource[components.StreamingMap](w)
	sys.nodeEntityFilter = ecs.NewFilter2[components.NodeEntity, components.LODActive](w)
	sys.nodeRelevantFilter = ecs.NewFilter2[components.NodeEntity, components.LODRelevant](w)
	sys.nodeDormantFilter = ecs.NewFilter2[components.NodeEntity, components.LODDormant](w)
	sys.lodActiveMap = ecs.NewMap[components.LODActive](w)
	sys.lodRelevantMap = ecs.NewMap[components.LODRelevant](w)
	sys.lodDormantMap = ecs.NewMap[components.LODDormant](w)
	sys.nodeEntityMap = ecs.NewMap[components.NodeEntity](w)
	sys.streamingMapMap = ecs.NewMap[components.StreamingMap](w)
}

func (StreamingSystem) Name() string { return "streaming" }

func (StreamingSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys StreamingSystem) Update(ctx core.UpdateContext) {
	var anchorNode components.NodeID
	foundAnchor := false

	q := sys.anchorNodeFilter.Query()
	for q.Next() {
		if !foundAnchor {
			_, nodeEnt := q.Get()
			anchorNode = nodeEnt.NodeID
			foundAnchor = true
		}
	}

	if !foundAnchor {
		return
	}

	// Use Resource for singleton StreamingMap — no ForEach search needed
	sMapPtr := sys.streamingMapRes.Get()
	if sMapPtr == nil {
		return
	}
	sMap := *sMapPtr

	newStates := make(map[components.NodeID]components.NodeState)
	newStates[anchorNode] = components.NodeStateActive

	if nodeDef, ok := sMap.Nodes[anchorNode]; ok {
		for _, adj := range nodeDef.Connections {
			newStates[adj] = components.NodeStateLoaded
		}
	}

	// Update the StreamingMap resource
	sys.streamingMapRes.Add(&components.StreamingMap{
		Nodes:  sMap.Nodes,
		States: newStates,
	})

	// Collect LOD changes for node entities — can't modify archetypes during iteration
	type lodChange struct {
		id     ecs.Entity
		remove core.LODTier
		add    core.LODTier
	}
	var changes []lodChange

	// Iterate Active node entities
	qActive := sys.nodeEntityFilter.Query()
	for qActive.Next() {
		nodeEnt, _ := qActive.Get()
		ns := newStates[nodeEnt.NodeID]
		if ns != components.NodeStateActive {
			changes = append(changes, lodChange{qActive.Entity(), core.LODTierActive, tierForState(ns)})
		}
	}

	// Iterate Relevant node entities
	qRelevant := sys.nodeRelevantFilter.Query()
	for qRelevant.Next() {
		nodeEnt, _ := qRelevant.Get()
		ns := newStates[nodeEnt.NodeID]
		if ns != components.NodeStateLoaded {
			changes = append(changes, lodChange{qRelevant.Entity(), core.LODTierRelevant, tierForState(ns)})
		}
	}

	// Iterate Dormant node entities
	qDormant := sys.nodeDormantFilter.Query()
	for qDormant.Next() {
		nodeEnt, _ := qDormant.Get()
		ns := newStates[nodeEnt.NodeID]
		if ns != components.NodeStateUnloaded {
			changes = append(changes, lodChange{qDormant.Entity(), core.LODTierDormant, tierForState(ns)})
		}
	}

	// Apply LOD changes outside iteration
	for _, ch := range changes {
		switch ch.remove {
		case core.LODTierActive:
			sys.lodActiveMap.Remove(ch.id)
		case core.LODTierRelevant:
			sys.lodRelevantMap.Remove(ch.id)
		case core.LODTierDormant:
			sys.lodDormantMap.Remove(ch.id)
		}
		switch ch.add {
		case core.LODTierActive:
			sys.lodActiveMap.Add(ch.id, &components.LODActive{})
		case core.LODTierRelevant:
			sys.lodRelevantMap.Add(ch.id, &components.LODRelevant{})
		case core.LODTierDormant:
			sys.lodDormantMap.Add(ch.id, &components.LODDormant{})
		}
	}
}

func tierForState(ns components.NodeState) core.LODTier {
	switch ns {
	case components.NodeStateActive:
		return core.LODTierActive
	case components.NodeStateLoaded:
		return core.LODTierRelevant
	default:
		return core.LODTierDormant
	}
}
