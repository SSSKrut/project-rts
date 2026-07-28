package ribbons

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// roadStyle is the per-kind look and earthworks budget. Lift is how far the
// finished carriageway sits above the smoothed ground; MaxFill / MaxCut cap
// how far the profile may stray from it, which is what keeps embankments
// and cuttings from turning into terraces.
type roadStyle struct {
	Lift    float32
	MaxFill float32
	MaxCut  float32
	Crown   float32
	CornerR float32
	Color   [4]uint8
}

var roadStyles = [4]roadStyle{
	components.RoadHighway:   {Lift: 0.30, MaxFill: 2.0, MaxCut: 1.4, Crown: 0.09, CornerR: 10, Color: [4]uint8{52, 52, 58, 255}},
	components.RoadLocal:     {Lift: 0.22, MaxFill: 1.6, MaxCut: 1.2, Crown: 0.07, CornerR: 8, Color: [4]uint8{104, 104, 110, 255}},
	components.RoadDirtTrack: {Lift: 0.12, MaxFill: 0.8, MaxCut: 0.6, Crown: 0.05, CornerR: 6, Color: [4]uint8{128, 96, 62, 255}},
	components.RoadBridge:    {Lift: BridgeLift, MaxFill: 8.0, MaxCut: 0.2, Crown: 0.05, CornerR: 4, Color: [4]uint8{96, 74, 50, 255}},
}

// BridgeLift is the deck clearance above the pre-cut river bank.
const BridgeLift float32 = 1.5

const (
	roadStep      float32 = 1.5
	approachGrade float32 = 0.12
	embankSlope   float32 = 1.6
	minShoulder   float32 = 0.5
	maxShoulder   float32 = 6.0
	footSink      float32 = 0.35
	deckThickness float32 = 0.45
	railHeight    float32 = 0.55
	railInset     float32 = 0.10
	pierSpacing   float32 = 9.0
	pierHalf      float32 = 0.7
	pierFoot      float32 = 2.5
	profileIters          = 48
	polishIters           = 6
	profileLambda float32 = 0.5
)

var (
	earthColor  = [4]uint8{92, 76, 54, 255}
	bridgeSide  = [4]uint8{88, 66, 44, 255}
	railColor   = [4]uint8{74, 70, 64, 255}
	pierColor   = [4]uint8{104, 102, 98, 255}
	shoulderCol = [4]uint8{116, 108, 88, 255}
)

// Network is everything derived from the road graph: geometry to upload and
// the deck cross-sections the sim queries for height.
type Network struct {
	Meshes []Mesh
	Deck   []components.RoadDeckSample
}

// BuildRoads turns the road graph into ribbon geometry. Edges are grouped
// into chains through degree-2 nodes so the vertical profile — and therefore
// bridge approaches — stay continuous across edge boundaries. Junction node
// heights are shared by every chain that touches them.
func BuildRoads(g *components.RoadGraph, h Sampler) Network {
	var net Network
	if g == nil || len(g.Edges) == 0 || h == nil {
		return net
	}
	adj := make([][]int32, len(g.Nodes))
	for i := range g.Edges {
		e := &g.Edges[i]
		adj[e.From] = append(adj[e.From], int32(i))
		adj[e.To] = append(adj[e.To], int32(i))
	}
	nodeY := make([]float32, len(g.Nodes))
	for i := range g.Nodes {
		x, z := nodeXZ(g, uint16(i))
		lift := float32(0)
		for _, ei := range adj[i] {
			if l := roadStyles[g.Edges[ei].Kind].Lift; l > lift {
				lift = l
			}
		}
		nodeY[i] = h(x, z) + lift
	}
	for _, ch := range roadChains(g, adj) {
		net.appendChain(g, ch, nodeY, h)
	}
	for i := range net.Meshes {
		net.Meshes[i].finishCull()
	}
	return net
}

type chain struct {
	nodes []uint16
	edges []int32
}

// roadChains walks maximal paths whose interior nodes have exactly two
// incident edges. Junctions end a chain; leftover rings are picked up last.
func roadChains(g *components.RoadGraph, adj [][]int32) []chain {
	used := make([]bool, len(g.Edges))
	walk := func(start uint16, first int32) chain {
		ch := chain{nodes: []uint16{start}}
		node, ei := start, first
		for {
			used[ei] = true
			e := &g.Edges[ei]
			next := e.To
			if next == node {
				next = e.From
			}
			ch.edges = append(ch.edges, ei)
			ch.nodes = append(ch.nodes, next)
			if next == node || len(adj[next]) != 2 {
				break
			}
			cont := int32(-1)
			for _, c := range adj[next] {
				if c != ei && !used[c] {
					cont = c
				}
			}
			if cont < 0 {
				break
			}
			node, ei = next, cont
		}
		return ch
	}

	var out []chain
	for n := range g.Nodes {
		if len(adj[n]) == 2 {
			continue
		}
		for _, ei := range adj[n] {
			if !used[ei] {
				out = append(out, walk(uint16(n), ei))
			}
		}
	}
	for ei := range g.Edges {
		if !used[ei] {
			out = append(out, walk(g.Edges[ei].From, int32(ei)))
		}
	}
	return out
}

// xsec is one finished cross-section: carriageway rails plus the points
// where the embankment (or the bridge fascia) ends.
type xsec struct {
	ctr, edgeL, edgeR, footL, footR rl.Vector3
	latX, latZ                      float32
	bridge                          bool
	col                             [4]uint8
}

func (net *Network) appendChain(g *components.RoadGraph, ch chain, nodeY []float32, h Sampler) {
	if len(ch.edges) == 0 {
		return
	}
	ctrl := make([]pathPt, len(ch.nodes))
	for i, n := range ch.nodes {
		x, z := nodeXZ(g, n)
		ei := ch.edges[len(ch.edges)-1]
		if i < len(ch.edges) {
			ei = ch.edges[i]
		}
		ctrl[i] = pathPt{X: x, Z: z, Edge: ei}
	}

	// A corner is rounded on the terms of the two roads that form it, and may
	// stray at most a quarter of the narrower one's width off their centre
	// lines — half the nav half-width, so the drawn line never leaves the strip.
	radius := make([]float32, len(ctrl))
	maxDev := make([]float32, len(ctrl))
	for i := range ctrl {
		in, out := ctrl[maxI(i-1, 0)].Edge, ctrl[i].Edge
		radius[i] = minF(roadStyles[g.Edges[in].Kind].CornerR, roadStyles[g.Edges[out].Kind].CornerR)
		maxDev[i] = minF(g.Edges[in].Width, g.Edges[out].Width) * 0.25
	}
	pts := resample(fillet(ctrl, radius, maxDev), roadStep)
	n := len(pts)
	if n < 2 {
		return
	}

	ground := make([]float32, n)
	y := make([]float32, n)
	for i, p := range pts {
		st := &roadStyles[g.Edges[p.Edge].Kind]
		ground[i] = h(p.X, p.Z)
		y[i] = ground[i] + st.Lift
	}
	y[0] = nodeY[ch.nodes[0]]
	y[n-1] = nodeY[ch.nodes[len(ch.nodes)-1]]
	// Each bridge span is levelled and pinned before smoothing runs, so the
	// passes build approach ramps up to a deck that stays put instead of
	// being averaged down and snapped back.
	fixed := levelBridgeSpans(g, pts, ground, y)
	allow := approachAllowance(pts, ground, y, fixed)
	// ground+allow is the constant-grade ramp down from the deck; seeding it
	// gives smoothing a line to round off instead of a cliff to diffuse.
	for i := range y {
		if !fixed[i] && allow[i] > 0 {
			y[i] = maxF(y[i], ground[i]+allow[i])
		}
	}

	clampDev := func() {
		for i := 1; i < n-1; i++ {
			if fixed[i] {
				continue
			}
			st := &roadStyles[g.Edges[pts[i].Edge].Kind]
			y[i] = clampF(y[i], ground[i]-st.MaxCut, ground[i]+st.MaxFill+allow[i])
		}
	}
	smooth(y, fixed, profileIters, profileLambda)
	clampDev()
	smooth(y, fixed, polishIters, profileLambda)
	clampDev()

	secs := make([]xsec, n)
	for i := range pts {
		e := &g.Edges[pts[i].Edge]
		st := &roadStyles[e.Kind]
		latX, latZ := lateral(pts, i)
		halfW := e.Width * 0.5
		deck := y[i] - st.Crown
		p := pts[i]
		s := xsec{
			ctr:    rl.Vector3{X: p.X, Y: y[i], Z: p.Z},
			edgeL:  rl.Vector3{X: p.X - latX*halfW, Y: deck, Z: p.Z - latZ*halfW},
			edgeR:  rl.Vector3{X: p.X + latX*halfW, Y: deck, Z: p.Z + latZ*halfW},
			latX:   latX,
			latZ:   latZ,
			bridge: e.Kind == components.RoadBridge,
			col:    st.Color,
		}
		shoulder := halfW
		if s.bridge {
			s.footL = rl.Vector3{X: s.edgeL.X, Y: deck - deckThickness, Z: s.edgeL.Z}
			s.footR = rl.Vector3{X: s.edgeR.X, Y: deck - deckThickness, Z: s.edgeR.Z}
		} else {
			s.footL = embankFoot(h, p.X, p.Z, -latX, -latZ, halfW, deck)
			s.footR = embankFoot(h, p.X, p.Z, latX, latZ, halfW, deck)
			shoulder = maxF(
				hypot(s.footL.X-p.X, s.footL.Z-p.Z),
				hypot(s.footR.X-p.X, s.footR.Z-p.Z))
		}
		secs[i] = s

		net.Deck = append(net.Deck, components.RoadDeckSample{
			X: p.X, Z: p.Z, Y: y[i], HalfW: halfW, ShoulderW: shoulder,
		})
	}

	mesh := net.newMesh(pts[0].X, pts[0].Z)
	sinceP := float32(0)
	for i := 0; i+1 < n; i++ {
		if !net.Meshes[mesh].roomFor(80) {
			mesh = net.newMesh(pts[i].X, pts[i].Z)
		}
		m := &net.Meshes[mesh]
		a, b := &secs[i], &secs[i+1]
		m.quad(a.edgeL, a.ctr, b.edgeL, b.ctr, a.col, a.col, up)
		m.quad(a.ctr, a.edgeR, b.ctr, b.edgeR, a.col, a.col, up)
		if a.bridge && b.bridge {
			emitBridgeStep(m, a, b)
			sinceP += hypot(b.ctr.X-a.ctr.X, b.ctr.Z-a.ctr.Z)
			if sinceP >= pierSpacing {
				sinceP = 0
				m.box(a.ctr.X, a.ctr.Z, h(a.ctr.X, a.ctr.Z)-pierFoot,
					a.footL.Y, pierHalf, pierColor)
			}
			continue
		}
		sinceP = 0
		m.quad(a.footL, a.edgeL, b.footL, b.edgeL, earthColor, shoulderCol, up)
		m.quad(a.edgeR, a.footR, b.edgeR, b.footR, shoulderCol, earthColor, up)
	}
}

// levelBridgeSpans flattens every contiguous bridge run to one height that
// clears its highest bank, and reports which samples are now pinned.
func levelBridgeSpans(g *components.RoadGraph, pts []pathPt, ground, y []float32) []bool {
	fixed := make([]bool, len(pts))
	isBridge := func(i int) bool {
		return g.Edges[pts[i].Edge].Kind == components.RoadBridge
	}
	for i := 0; i < len(pts); {
		if !isBridge(i) {
			i++
			continue
		}
		j := i
		deck := float32(0)
		for ; j < len(pts) && isBridge(j); j++ {
			deck = maxF(deck, ground[j]+BridgeLift)
		}
		for k := i; k < j; k++ {
			y[k] = deck
			fixed[k] = true
		}
		i = j
	}
	return fixed
}

// approachAllowance widens the fill budget on the way to a bridge deck: an
// approach embankment is allowed to be as tall as the deck demands, shedding
// approachGrade metres of that permission per metre walked away. Without it
// the ordinary MaxFill clamp would flatten the ramp and leave a step at the
// abutment.
func approachAllowance(pts []pathPt, ground, y []float32, fixed []bool) []float32 {
	n := len(pts)
	allow := make([]float32, n)
	dist := make([]float32, n)
	deck := make([]float32, n)
	const far float32 = 1e9

	sweep := func(from, to, dir int) {
		for i := from; i != to; i += dir {
			j := i - dir
			if fixed[i] {
				dist[i], deck[i] = 0, y[i]
				continue
			}
			if dist[j] >= far {
				continue
			}
			d := dist[j] + hypot(pts[i].X-pts[j].X, pts[i].Z-pts[j].Z)
			if d < dist[i] {
				dist[i], deck[i] = d, deck[j]
			}
		}
	}
	for i := range dist {
		dist[i] = far
	}
	if fixed[0] {
		dist[0], deck[0] = 0, y[0]
	}
	sweep(1, n, 1)
	if fixed[n-1] {
		dist[n-1], deck[n-1] = 0, y[n-1]
	}
	sweep(n-2, -1, -1)

	for i := range allow {
		if dist[i] >= far {
			continue
		}
		allow[i] = maxF(0, deck[i]-ground[i]-dist[i]*approachGrade)
	}
	return allow
}

// emitBridgeStep adds the deck fascias, underside and railings for one step.
func emitBridgeStep(m *Mesh, a, b *xsec) {
	outL := rl.Vector3{X: -a.latX, Z: -a.latZ}
	outR := rl.Vector3{X: a.latX, Z: a.latZ}
	m.quad(a.edgeL, a.footL, b.edgeL, b.footL, bridgeSide, bridgeSide, outL)
	m.quad(a.edgeR, a.footR, b.edgeR, b.footR, bridgeSide, bridgeSide, outR)
	m.quad(a.footL, a.footR, b.footL, b.footR, bridgeSide, bridgeSide, rl.Vector3{Y: -1})

	rail := func(edgeA, edgeB rl.Vector3, outward rl.Vector3) {
		inA := offsetXZ(edgeA, -outward.X*railInset, -outward.Z*railInset)
		inB := offsetXZ(edgeB, -outward.X*railInset, -outward.Z*railInset)
		topA := lift(edgeA, railHeight)
		topB := lift(edgeB, railHeight)
		topInA := lift(inA, railHeight)
		topInB := lift(inB, railHeight)
		m.quad(topA, edgeA, topB, edgeB, railColor, railColor, outward)
		m.quad(topInA, inA, topInB, inB, railColor, railColor,
			rl.Vector3{X: -outward.X, Z: -outward.Z})
		m.quad(topA, topInA, topB, topInB, railColor, railColor, up)
	}
	rail(a.edgeL, b.edgeL, outL)
	rail(a.edgeR, b.edgeR, outR)
}

// embankFoot walks out from the carriageway edge until the fill is closed,
// then sinks the vertex so the skirt never floats over the terrain.
func embankFoot(h Sampler, cx, cz, dirX, dirZ, halfW, deck float32) rl.Vector3 {
	edgeG := h(cx+dirX*halfW, cz+dirZ*halfW)
	run := clampF((deck-edgeG)*embankSlope, minShoulder, maxShoulder)
	off := halfW + run
	fx, fz := cx+dirX*off, cz+dirZ*off
	return rl.Vector3{X: fx, Y: minF(h(fx, fz), deck) - footSink, Z: fz}
}

func (net *Network) newMesh(x, z float32) int {
	ax := float32(math.Floor(float64(x/components.ChunkSize))) * components.ChunkSize
	az := float32(math.Floor(float64(z/components.ChunkSize))) * components.ChunkSize
	net.Meshes = append(net.Meshes, Mesh{AnchorX: ax, AnchorZ: az})
	return len(net.Meshes) - 1
}

// lateral returns the unit vector perpendicular to the centre line at i.
func lateral(pts []pathPt, i int) (float32, float32) {
	a, b := i-1, i+1
	if a < 0 {
		a = i
	}
	if b >= len(pts) {
		b = i
	}
	tx, tz := pts[b].X-pts[a].X, pts[b].Z-pts[a].Z
	l := hypot(tx, tz)
	if l == 0 {
		return 1, 0
	}
	return -tz / l, tx / l
}

func nodeXZ(g *components.RoadGraph, n uint16) (float32, float32) {
	p := g.Nodes[n].Pos
	return float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
		float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
}

func offsetXZ(v rl.Vector3, dx, dz float32) rl.Vector3 {
	return rl.Vector3{X: v.X + dx, Y: v.Y, Z: v.Z + dz}
}

func lift(v rl.Vector3, dy float32) rl.Vector3 {
	return rl.Vector3{X: v.X, Y: v.Y + dy, Z: v.Z}
}
