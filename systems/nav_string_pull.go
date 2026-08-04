package systems

import (
	"math"

	"rts-go/components"
)

const (
	// stringPullMaxSegment caps one candidate chord's DDA length.
	stringPullMaxSegment float32 = 96
	// stringPullSideOffset — clearance half-width of the chord test: the
	// centre line plus two parallels at ± the unit collider radius. A chord
	// that grazes a blocked corner inside that band is rejected, so pulled
	// paths keep body clearance instead of shaving corners.
	stringPullSideOffset float32 = 0.4
)

// StringPull drops intermediate waypoints whose straight chord from the
// previous kept point crosses no wall / blocked cell (LOS string-pulling,
// P7-c). Gates and their mouth waypoints are always kept — they thread wall
// openings and need the precise approach. Only surface stretches pull;
// interior (Level) waypoints pass through untouched. The first chord is
// anchored on the walker's live position, so a fresh plan never walks back
// to the path's start cell.
func (s *NavService) StringPull(start components.WorldPos, wps []components.WorldPos, gates []bool, opts NavOpts) ([]components.WorldPos, []bool) {
	n := len(wps)
	if n < 2 {
		return wps, gates
	}
	idx := s.indexRes.Get()
	if idx == nil {
		return wps, gates
	}
	floors := s.snapshotLevels()

	must := make([]bool, n)
	must[n-1] = true
	for i := 0; i < n && i < len(gates); i++ {
		if gates[i] {
			must[i] = true
			if i > 0 {
				must[i-1] = true
			}
		}
	}

	outW := make([]components.WorldPos, 0, n)
	outG := make([]bool, 0, n)
	anchor := start
	i := 0
	for i < n {
		m := i
		for m < n-1 && !must[m] {
			m++
		}
		// Longest clear jump first; halve the reach on failure so a cluttered
		// stretch costs O(log) chord tests, not O(n). k == i keeps the next
		// waypoint unconditionally (today's unsmoothed behaviour).
		k := m
		for k > i && !s.surfaceSegClear(anchor, wps[k], idx, floors, opts) {
			k = i + (k-i)/2
		}
		outW = append(outW, wps[k])
		g := false
		if k < len(gates) {
			g = gates[k]
		}
		outG = append(outG, g)
		anchor = wps[k]
		i = k + 1
	}
	return outW, outG
}

// SegmentClear reports whether the straight XZ chord a→b runs entirely over
// walkable surface cells. False for interior (Level) endpoints — callers
// there fall back to a replan.
func (s *NavService) SegmentClear(a, b components.WorldPos) bool {
	idx := s.indexRes.Get()
	if idx == nil {
		return false
	}
	return s.surfaceSegClear(a, b, idx, s.snapshotLevels(), NavOpts{})
}

// surfaceSegClear reports whether the straight XZ chord a→b runs entirely
// over walkable surface cells. False when either endpoint resolves into a
// building Level — interior stretches never pull.
func (s *NavService) surfaceSegClear(a, b components.WorldPos, idx *TerrainChunkIndex, floors []levelRec, opts NavOpts) bool {
	an, okA := s.resolveNode(a, idx, floors)
	if !okA || an.Kind != components.NodeSurface {
		return false
	}
	bn, okB := s.resolveNode(b, idx, floors)
	if !okB || bn.Kind != components.NodeSurface {
		return false
	}
	ax := float32(a.Chunk.X)*components.ChunkSize + a.Local.X
	az := float32(a.Chunk.Z)*components.ChunkSize + a.Local.Z
	bx := float32(b.Chunk.X)*components.ChunkSize + b.Local.X
	bz := float32(b.Chunk.Z)*components.ChunkSize + b.Local.Z
	dx := bx - ax
	dz := bz - az
	lenSq := dx*dx + dz*dz
	if lenSq > stringPullMaxSegment*stringPullMaxSegment {
		return false
	}
	if lenSq < 1e-6 {
		return true
	}
	inv := 1 / float32(math.Sqrt(float64(lenSq)))
	px := -dz * inv * stringPullSideOffset
	pz := dx * inv * stringPullSideOffset
	av := avoidZone{}
	if opts.AvoidR > 0 {
		av = avoidZone{
			x: float32(opts.Avoid.Chunk.X)*components.ChunkSize + opts.Avoid.Local.X,
			z: float32(opts.Avoid.Chunk.Z)*components.ChunkSize + opts.Avoid.Local.Z,
			r: opts.AvoidR,
		}
	}
	return s.ddaClear(ax, az, bx, bz, idx, floors, av) &&
		s.ddaClear(ax+px, az+pz, bx+px, bz+pz, idx, floors, av) &&
		s.ddaClear(ax-px, az-pz, bx-px, bz-pz, idx, floors, av)
}

// avoidZone — the detour hint projected into the chord test: a chord that
// crosses it is rejected so string-pulling can't cut back through the spot
// the replanned path just detoured around. r == 0 disarms.
type avoidZone struct {
	x, z, r float32
}

func (a avoidZone) blocks(gi, gj int32) bool {
	if a.r <= 0 {
		return false
	}
	dx := float32(gi) + 0.5 - a.x
	dz := float32(gj) + 0.5 - a.z
	return dx*dx+dz*dz < a.r*a.r
}

// ddaClear walks every surface cell the segment crosses (Amanatides-Woo);
// blocked or unloaded cell = false. An exact corner crossing checks both
// corner-adjacent cells so a diagonal can't slip between two blocked cells.
func (s *NavService) ddaClear(ax, az, bx, bz float32, idx *TerrainChunkIndex, floors []levelRec, av avoidZone) bool {
	gi := int32(math.Floor(float64(ax)))
	gj := int32(math.Floor(float64(az)))
	ei := int32(math.Floor(float64(bx)))
	ej := int32(math.Floor(float64(bz)))
	if !s.surfaceCellWalkable(gi, gj, idx, floors) || av.blocks(gi, gj) {
		return false
	}
	dx := bx - ax
	dz := bz - az
	stepI, stepJ := int32(1), int32(1)
	if dx < 0 {
		stepI = -1
	}
	if dz < 0 {
		stepJ = -1
	}
	inf := float32(math.Inf(1))
	tMaxX, tDeltaX := inf, inf
	if dx != 0 {
		next := float32(gi)
		if stepI > 0 {
			next = float32(gi + 1)
		}
		tMaxX = (next - ax) / dx
		tDeltaX = float32(stepI) / dx
	}
	tMaxZ, tDeltaZ := inf, inf
	if dz != 0 {
		next := float32(gj)
		if stepJ > 0 {
			next = float32(gj + 1)
		}
		tMaxZ = (next - az) / dz
		tDeltaZ = float32(stepJ) / dz
	}
	for iter := 0; iter < 512; iter++ {
		if gi == ei && gj == ej {
			return true
		}
		switch {
		case tMaxX < tMaxZ:
			gi += stepI
			tMaxX += tDeltaX
		case tMaxZ < tMaxX:
			gj += stepJ
			tMaxZ += tDeltaZ
		default:
			if !s.surfaceCellWalkable(gi+stepI, gj, idx, floors) ||
				!s.surfaceCellWalkable(gi, gj+stepJ, idx, floors) {
				return false
			}
			gi += stepI
			gj += stepJ
			tMaxX += tDeltaX
			tMaxZ += tDeltaZ
		}
		if !s.surfaceCellWalkable(gi, gj, idx, floors) || av.blocks(gi, gj) {
			return false
		}
	}
	return false
}

func (s *NavService) surfaceCellWalkable(gi, gj int32, idx *TerrainChunkIndex, floors []levelRec) bool {
	n := components.NavNode{
		Kind:  components.NodeSurface,
		Chunk: components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift},
		I:     int16(gi & navGridMask),
		J:     int16(gj & navGridMask),
	}
	cell, ok := s.cellAt(n, idx, floors)
	return ok && cell.Cost > 0
}
