package components

// PropType indexes PropTypeRegistry.Metas.
type PropType uint16

const (
	PropNone PropType = iota
	PropOak
	PropPine
	PropBirch
	PropBush
	PropRock
	PropTypeMax
)

// Prop is the runtime component on every static-world-object entity. All
// gameplay attributes live in PropTypeRegistry.Metas[Type] so per-entity data
// stays small and placeholder mesh -> loaded model is a registry-only swap.
type Prop struct {
	Type  PropType
	Yaw   float32 // radians, [0, 2pi)
	Scale float32 // uniform; 1.0 = registry default size
}

// PropsDirty marks a chunk that hasn't had its procedural props spawned yet.
type PropsDirty struct{}

// RiverProcessed marks a chunk whose river-cut has been applied. Local to
// the chunk, not serialized: after eviction the chunk
// reincarnates pristine and RiverSystem reapplies the cut deterministically.
// RiverSystem's filter excludes Modified so player edits aren't re-stamped.
type RiverProcessed struct{}
