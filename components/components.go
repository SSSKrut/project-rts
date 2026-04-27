package components

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

// LOD marker components — archetype-level filtering.
// Adding/removing these triggers archetype change for cache-friendly iteration.
type LODActive struct{}
type LODRelevant struct{}
type LODDormant struct{}

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
