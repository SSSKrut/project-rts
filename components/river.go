package components

// RiverPolyline is a hand-authored river: WorldPos waypoints joined by
// straight segments, sharing one Width / Depth.
//
// Width is the half-strip distance from centre line (cut spans 2*Width); Depth
// is peak cut depth at the centre line, blended to zero at the edges via
// cosine half-falloff.
type RiverPolyline struct {
	Points []WorldPos
	Width  float32
	Depth  float32
}

// Rivers is the singleton resource holding every static river polyline.
type Rivers struct {
	Polylines []RiverPolyline
}
