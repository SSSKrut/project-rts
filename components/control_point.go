package components

// ControlPoint is the object the whole lite game is played over: ground you
// hold by standing on it, which then carries your radio net (MODEL.md 2).
//
// Capture is PRESENCE, not a button — the only input is how many live bodies
// of each side are inside the radius. That is what makes a point a place on the
// map rather than a menu, and it is why aircraft are excluded: a helicopter
// hovering over a square does not hold it.
type ControlPoint struct {
	Radius float32
	// Owner is the faction holding it; FactionNone = nobody yet.
	Owner uint8
	// Challenger is whoever Progress is running in favour of. Meaningless
	// while Progress is zero.
	Challenger uint8
	Progress   float32 // 0..1 toward Challenger taking it
	Contested  bool    // both sides inside: progress frozen, relay silent
	// RelayRangeM > 0 makes the point a node of its owner's net. This is the
	// join with block A and the reason losing a point hurts somewhere else.
	RelayRangeM float32
}

// FactionNone marks a point nobody holds. Deliberately not FactionNeutral:
// neutral is a side that exists and can own things, "nobody" is the absence of
// one, and a point starts as the second.
const FactionNone uint8 = 255

// Capture tuning. A lone man takes a neutral point in ten seconds and three or
// more take it in a bit over three: enough that a squad detaching one man to
// hold ground is a real decision, not enough that a point flips as somebody
// runs past.
const (
	CaptureRatePerSec   float32 = 0.10
	CaptureMaxDiff      float32 = 3
	ControlPointRadiusM float32 = 25
)

// CaptureRate is the progress per second a presence difference buys. Zero when
// nobody is pushing.
func CaptureRate(diff int) float32 {
	if diff <= 0 {
		return 0
	}
	d := float32(diff)
	if d > CaptureMaxDiff {
		d = CaptureMaxDiff
	}
	return CaptureRatePerSec * d
}
