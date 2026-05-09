package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// preprocessSamples controls how finely we sample each edge to detect "in
// river" transitions. At 200 samples per edge a 100 m road resolves to 0.5 m
// step — well below the smallest river width — so transition points land in
// the right sub-edge with no aliasing.
const preprocessSamples = 200

// PreprocessRoadGraph splits every edge that crosses a river polyline strip
// at the entry/exit boundaries and tags the inside-the-strip sub-edges as
// RoadBridge. After this, no system needs to do river-vs-road geometry — the
// graph is the truth.
//
// Determinism: same input graph + same rivers ⇒ identical output. Safe to
// call once at startup before any system reads the graph.
func PreprocessRoadGraph(g *components.RoadGraph, rivers *components.Rivers) {
	if g == nil || rivers == nil || len(g.Edges) == 0 || len(rivers.Polylines) == 0 {
		return
	}

	newEdges := make([]components.RoadEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		ax, az := worldXZ(g.Nodes[e.From].Pos)
		bx, bz := worldXZ(g.Nodes[e.To].Pos)

		// Sample-based "in river strip" boolean trace; transition t-values
		// become split nodes.
		var splitTs []float32
		prevIn := isInRiverStrip(ax, az, rivers)
		for s := 1; s <= preprocessSamples; s++ {
			t := float32(s) / float32(preprocessSamples)
			x := ax + t*(bx-ax)
			z := az + t*(bz-az)
			cur := isInRiverStrip(x, z, rivers)
			if cur != prevIn {
				// Linear midpoint between samples is good enough at this
				// resolution; a binary refinement would cut error by another
				// log(N) but is unnecessary for placeholder bridges.
				splitTs = append(splitTs, t-0.5/float32(preprocessSamples))
			}
			prevIn = cur
		}

		prevIdx := e.From
		prevT := float32(0)
		for _, t := range splitTs {
			if t <= 0.001 || t >= 0.999 {
				continue
			}
			x := ax + t*(bx-ax)
			z := az + t*(bz-az)
			wp := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
			wp.Local.Y = GroundHeight(x, z)
			newIdx := uint16(len(g.Nodes))
			g.Nodes = append(g.Nodes, components.RoadNode{Pos: wp})

			newEdges = append(newEdges, makeSubEdge(g, e, prevIdx, newIdx, prevT, t, ax, az, bx, bz, rivers))
			prevIdx = newIdx
			prevT = t
		}
		newEdges = append(newEdges, makeSubEdge(g, e, prevIdx, e.To, prevT, 1, ax, az, bx, bz, rivers))
	}
	g.Edges = newEdges
}

func makeSubEdge(g *components.RoadGraph, base components.RoadEdge,
	from, to uint16, tStart, tEnd, ax, az, bx, bz float32,
	rivers *components.Rivers) components.RoadEdge {
	midT := 0.5 * (tStart + tEnd)
	midX := ax + midT*(bx-ax)
	midZ := az + midT*(bz-az)
	kind := base.Kind
	if isInRiverStrip(midX, midZ, rivers) {
		kind = components.RoadBridge
	}
	return components.RoadEdge{From: from, To: to, Kind: kind, Width: base.Width}
}

// isInRiverStrip returns true if (wx, wz) lies within Width/2 of any river
// polyline segment — i.e. inside the cosine-cut river bed.
func isInRiverStrip(wx, wz float32, rivers *components.Rivers) bool {
	for pi := range rivers.Polylines {
		pl := &rivers.Polylines[pi]
		halfW := pl.Width * 0.5
		for i := 0; i+1 < len(pl.Points); i++ {
			ax, az := worldXZ(pl.Points[i])
			bx, bz := worldXZ(pl.Points[i+1])
			if pointToSegment2D(wx, wz, ax, az, bx, bz) < halfW {
				return true
			}
		}
	}
	return false
}

func worldXZ(p components.WorldPos) (float32, float32) {
	return float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
		float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
}
