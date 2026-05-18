package components

type Velocity3D struct {
	X float32
	Y float32
	Z float32
}

type AudioSource struct {
	SoundID     string
	IsPlaying   bool
	IsLooping   bool
	MaxDistance float32
	Volume      float32
}

// LOD marker components - adding/removing them changes archetype, enabling
// cache-friendly per-tier iteration.
type LODActive struct{}
type LODRelevant struct{}
type LODDormant struct{}

// TerrainChunk: LOD owned by TerrainStreamingSystem; generic LODSystem must
// skip it via Without.
type TerrainChunk struct{}

type LODAnchor struct{}

// AlwaysActive pins an entity in the active LOD bucket.
type AlwaysActive struct{}

type NodeID int64

type StreamNode struct {
	ID          NodeID
	Connections []NodeID
}

type NodeEntity struct {
	NodeID NodeID
}

type NodeState int

const (
	NodeStateUnloaded NodeState = iota
	NodeStateLoaded
	NodeStateActive
)

type StreamingMap struct {
	Nodes  map[NodeID]*StreamNode
	States map[NodeID]NodeState
}

func NewStreamingMap() StreamingMap {
	return StreamingMap{
		Nodes:  make(map[NodeID]*StreamNode),
		States: make(map[NodeID]NodeState),
	}
}
