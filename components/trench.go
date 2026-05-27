package components

// Trench is a hand-authored defensive earthwork: WorldPos waypoints joined by
// straight segments, sharing one Width / Depth. Cosine-falloff cut, identical
// in shape to RiverPolyline - but distinguished as a separate type so render
// / nav / spawn systems can differentiate "earthwork" from "natural water".
type Trench struct {
	Points []WorldPos
	Width  float32
	Depth  float32
}

type TrenchNetwork struct {
	Lines []Trench
}

// TrenchProcessed marks a chunk whose trench cut has been applied. Filter
// excludes Modified - player edits aren't re-stamped. Local marker, not
// serialised: pristine respawn re-runs the cut deterministically.
type TrenchProcessed struct{}
