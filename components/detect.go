package components

// FactionCount sizes per-faction arrays (IDs 0..3 in faction.go).
const FactionCount = 4

// Detectability is the per-unit exposure meter, one slot per observing
// faction. Fills while an observer holds clear LOS (rate grows with signal
// magnitude), decays when unobserved; a sighting fires when a slot reaches 1.
type Detectability struct {
	Meter [FactionCount]float32
}
