package ecs

import "time"

// NodeID identifies a streaming node (chunk, room, etc.).
type NodeID int64

// StreamNode represents a streaming chunk in the world.
// Entities belong to a NodeID.
type StreamNode struct {
	ID          NodeID
	Connections []NodeID
}

func (StreamNode) Name() string { return "StreamNode" }

// NodeEntity links an entity to a specific streaming node.
type NodeEntity struct {
	NodeID NodeID
}

// NodeState represents the streaming state of a node.
type NodeState int

const (
	NodeStateUnloaded NodeState = iota
	NodeStateLoaded
	NodeStateActive
)

// StreamingMap holds the global graph of nodes and their states.
// In a real engine, this might not be an ECS component but a resource,
// but for simplicity we can store it as a unique component on a singleton entity,
// or pass it via context. Let's make it a resource component.
type StreamingMap struct {
	// Graph of nodes
	Nodes map[NodeID]*StreamNode
	// Current state of each node
	States map[NodeID]NodeState
}

// Create Map helper
func NewStreamingMap() StreamingMap {
	return StreamingMap{
		Nodes:  make(map[NodeID]*StreamNode),
		States: make(map[NodeID]NodeState),
	}
}

// StreamingSystem updates the state of nodes based on the player's current node.
type StreamingSystem struct{}

func (StreamingSystem) Name() string     { return "streaming" }
func (StreamingSystem) Phase() Phase     { return PhaseLogic }
func (StreamingSystem) LODPolicy() LODPolicy {
	// Not updated every frame, e.g. 2-4 times a second
	return LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: LODDisabled,
		DormantEvery:  LODDisabled,
	}
}

func (StreamingSystem) Reads() []ComponentType {
	return []ComponentType{
		TypeOf[LODAnchor](),
		TypeOf[NodeEntity](),
		TypeOf[StreamingMap](),
	}
}

func (StreamingSystem) Writes() []ComponentType {
	return []ComponentType{
		TypeOf[StreamingMap](),
		TypeOf[LOD](),
	}
}

func (StreamingSystem) Update(ctx UpdateContext) {
	// Find anchor's node
	var anchorNode NodeID
	foundAnchor := false

	// Normally there's one anchor, or multiple if multiplayer
	for _, id := range ctx.State.Query(TypeOf[LODAnchor](), TypeOf[NodeEntity]()) {
		nodeEnt, ok := Get[NodeEntity](ctx.State, id)
		if ok {
			anchorNode = nodeEnt.NodeID
			foundAnchor = true
			break
		}
	}

	if !foundAnchor {
		return
	}

	// Find the singleton StreamingMap
	var sMap StreamingMap
	var sMapID EntityID
	foundMap := false

	for _, id := range ctx.State.Query(TypeOf[StreamingMap]()) {
		sMap, _ = Get[StreamingMap](ctx.State, id)
		sMapID = id
		foundMap = true
		break
	}

	if !foundMap {
		return
	}

	// Update node states
	newStates := make(map[NodeID]NodeState)

	// active node
	newStates[anchorNode] = NodeStateActive

	// adjacent nodes are Loaded
	if nodeDef, ok := sMap.Nodes[anchorNode]; ok {
		for _, adj := range nodeDef.Connections {
			newStates[adj] = NodeStateLoaded
		}
	}

	// Any node not in newStates becomes Unloaded

	Set(ctx.State, sMapID, StreamingMap{
		Nodes:  sMap.Nodes,
		States: newStates,
	})

	// Now update LODs of entities based on their Node
	for _, id := range ctx.State.Query(TypeOf[NodeEntity]()) {
		nodeEnt, _ := Get[NodeEntity](ctx.State, id)
		ns := newStates[nodeEnt.NodeID]

		switch ns {
		case NodeStateActive:
			if LODLevelForEntity(ctx.State, id) != LODActive {
				Set(ctx.State, id, LOD{Level: LODActive})
			}
		case NodeStateLoaded:
			if LODLevelForEntity(ctx.State, id) != LODRelevant {
				Set(ctx.State, id, LOD{Level: LODRelevant})
			}
		default: // Unloaded
			if LODLevelForEntity(ctx.State, id) != LODDormant {
				Set(ctx.State, id, LOD{Level: LODDormant})
			}
		}
	}
}
