package components

import "rts-go/ecs"

// Position3D stores a 3D position.
type Position3D struct {
	X float32
	Y float32
	Z float32
}

// Velocity3D stores a 3D velocity.
type Velocity3D struct {
	X float32
	Y float32
	Z float32
}

// AudioSource represents a continuous or spatial sound attached to an entity.
type AudioSource struct {
	SoundID     string
	IsPlaying   bool
	IsLooping   bool
	MaxDistance float32
	Volume      float32 // 0.0 to 1.0
}

// LODAnchor marks an entity as the focus point for LOD decisions.
type LODAnchor struct{}

// AlwaysActive pins an entity in the active LOD bucket.
type AlwaysActive struct{}

// NodeID identifies a streaming node (chunk, room, etc.).
type NodeID int64

// StreamNode represents a streaming chunk in the world.
type StreamNode struct {
	ID          NodeID
	Connections []NodeID
}

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
type StreamingMap struct {
	Nodes  map[NodeID]*StreamNode
	States map[NodeID]NodeState
}

// NewStreamingMap creates a new streaming map.
func NewStreamingMap() StreamingMap {
	return StreamingMap{
		Nodes:  make(map[NodeID]*StreamNode),
		States: make(map[NodeID]NodeState),
	}
}

// Ensure types implement ecs.Component (no-op, just documentation)
var (
	_ ecs.Component = Position3D{}
	_ ecs.Component = Velocity3D{}
	_ ecs.Component = AudioSource{}
	_ ecs.Component = LODAnchor{}
	_ ecs.Component = AlwaysActive{}
	_ ecs.Component = NodeEntity{}
	_ ecs.Component = StreamingMap{}
)
