package components

import "math"

// RoadDeckSample is one cross-section of the built road surface: centre XZ,
// finished deck height, carriageway half-width and the half-width at the
// foot of the embankment. Between the two the deck's authority fades, which
// is what stops a walker popping up a step as it crosses the shoulder.
type RoadDeckSample struct {
	X, Z, Y, HalfW, ShoulderW float32
}

// RoadSurface indexes the generated road decks so the sim can ask "how high
// is the roadway here". Derived at boot from RoadGraph + terrain and never
// serialised. Subsumes the old bridge-only deck lookup: a bridge is just a
// stretch whose profile is held above the river.
type RoadSurface struct {
	Samples []RoadDeckSample

	cells map[deckCell][]int32
	reach int32
}

type deckCell struct{ X, Z int32 }

const deckCellSize float32 = 8

// Build re-indexes the surface. Sample order is the query's tie-break, so it
// must stay deterministic across runs.
func (rs *RoadSurface) Build(samples []RoadDeckSample) {
	rs.Samples = samples
	rs.cells = make(map[deckCell][]int32, len(samples))
	maxReach := float32(0)
	for i := range samples {
		s := &samples[i]
		if s.ShoulderW > maxReach {
			maxReach = s.ShoulderW
		}
		c := deckCellOf(s.X, s.Z)
		rs.cells[c] = append(rs.cells[c], int32(i))
	}
	rs.reach = int32(math.Ceil(float64(maxReach / deckCellSize)))
}

// SurfaceY raises groundY onto the roadway wherever the road runs above it.
// Full deck height inside the carriageway, cosine-blended back to the ground
// across the embankment — the same shape the drawn shoulder has, so walking
// on and off a fill is continuous.
func (rs *RoadSurface) SurfaceY(groundY, wx, wz float32) float32 {
	if len(rs.Samples) == 0 {
		return groundY
	}
	c := deckCellOf(wx, wz)
	best := float32(math.Inf(1))
	var deck, weight float32
	for dz := -rs.reach; dz <= rs.reach; dz++ {
		for dx := -rs.reach; dx <= rs.reach; dx++ {
			for _, i := range rs.cells[deckCell{X: c.X + dx, Z: c.Z + dz}] {
				s := &rs.Samples[i]
				ddx, ddz := wx-s.X, wz-s.Z
				d2 := ddx*ddx + ddz*ddz
				if d2 > s.ShoulderW*s.ShoulderW || d2 >= best {
					continue
				}
				best, deck, weight = d2, s.Y, s.blend(float32(math.Sqrt(float64(d2))))
			}
		}
	}
	if weight <= 0 || deck <= groundY {
		return groundY
	}
	return groundY*(1-weight) + deck*weight
}

func (s *RoadDeckSample) blend(d float32) float32 {
	if d <= s.HalfW {
		return 1
	}
	span := s.ShoulderW - s.HalfW
	if span <= 0 {
		return 0
	}
	return 0.5 * (1 + float32(math.Cos(math.Pi*float64((d-s.HalfW)/span))))
}

func deckCellOf(x, z float32) deckCell {
	return deckCell{
		X: int32(math.Floor(float64(x / deckCellSize))),
		Z: int32(math.Floor(float64(z / deckCellSize))),
	}
}
