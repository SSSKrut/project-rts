package components

// RoadKind selects gameplay attributes (width, surface) and visual placeholder
// of a road edge. RoadBridge is auto-assigned by PreprocessRoadGraph on the
// sub-edges that lie inside a river polyline strip - users do not author it
// directly.
type RoadKind uint8

const (
	RoadHighway RoadKind = iota
	RoadLocal
	RoadDirtTrack
	RoadBridge
)

// RoadNode is a graph vertex: a junction, an inflection waypoint, or an
// intersection point auto-inserted by PreprocessRoadGraph at a river crossing.
type RoadNode struct {
	Pos WorldPos
}

// RoadEdge connects two RoadNodes by index. Width is the full strip width in
// metres (RoadFlatten halves it for cosine-falloff).
type RoadEdge struct {
	From, To uint16
	Kind     RoadKind
	Width    float32
}

// RoadGraph is the singleton resource holding every road segment in the world.
// Mutated only at startup (PreprocessRoadGraph) and on rare runtime events
// (bridge destruction).
type RoadGraph struct {
	Nodes []RoadNode
	Edges []RoadEdge
}

// RoadProcessed marks a chunk whose RoadCarve pass has run. Filter excludes
// Modified - player edits aren't re-cut. Local marker, not serialised:
// pristine respawn re-runs the carve deterministically.
type RoadProcessed struct{}
