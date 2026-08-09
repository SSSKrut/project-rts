package systems

import (
	"math"
	"math/rand"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func navPos(cx, cz int32, x, z float32) components.WorldPos {
	return components.WorldPos{
		Chunk: components.ChunkCoord{X: cx, Z: cz},
		Local: rl.Vector3{X: x, Z: z},
	}
}

var allPathStyles = []components.PathStyle{
	components.PathStyleDirect,
	components.PathStyleRoadPrefer,
	components.PathStyleRoadAvoid,
	components.PathStyleCoverSeek,
}

// every NavCell shape the bake can emit, plus a few it cannot — the bound has
// to hold for the whole component's value space, not just today's bake.
func navCellSpace() []components.NavCell {
	var out []components.NavCell
	for _, cost := range []uint8{navCostRoad, navCostOpen, navCostRough, navCostTrench, 255} {
		for _, flags := range []components.NavFlags{0, components.NavOnRoad, components.NavInBuilding, components.NavOnRoad | components.NavInTrench} {
			for _, cd := range []uint8{0, components.CoverSeekThreshold - 1, components.CoverSeekThreshold, components.CoverDistanceFar} {
				out = append(out, components.NavCell{Cost: cost, Flags: flags, CoverDistance: cd})
			}
		}
	}
	return out
}

func TestStyleCellCostAppliesTheDocumentedMultipliers(t *testing.T) {
	road := components.NavCell{Cost: navCostRoad, Flags: components.NavOnRoad, CoverDistance: components.CoverDistanceFar}
	open := components.NavCell{Cost: navCostOpen, CoverDistance: components.CoverDistanceFar}
	covered := components.NavCell{Cost: navCostOpen, CoverDistance: components.CoverSeekThreshold - 1}

	cases := []struct {
		name  string
		cell  components.NavCell
		style components.PathStyle
		want  float32
	}{
		{"direct road", road, components.PathStyleDirect, float32(navCostRoad)},
		{"prefer road", road, components.PathStyleRoadPrefer, float32(navCostRoad) * 0.5},
		{"prefer offroad", open, components.PathStyleRoadPrefer, float32(navCostOpen) * 1.5},
		{"avoid road", road, components.PathStyleRoadAvoid, float32(navCostRoad) * 2},
		{"avoid covered offroad", covered, components.PathStyleRoadAvoid, float32(navCostOpen) * 0.8},
		{"avoid bare offroad", open, components.PathStyleRoadAvoid, float32(navCostOpen)},
		{"coverseek covered", covered, components.PathStyleCoverSeek, float32(navCostOpen) * 0.7},
		{"coverseek bare", open, components.PathStyleCoverSeek, float32(navCostOpen)},
	}
	for _, c := range cases {
		if got := styleCellCost(c.cell, c.style); math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("%s: cost = %.3f, want %.3f", c.name, got, c.want)
		}
	}
}

func TestStyleCellCostStaysStrictlyPositive(t *testing.T) {
	for _, style := range allPathStyles {
		for _, cell := range navCellSpace() {
			if cell.Cost == 0 {
				continue
			}
			if got := styleCellCost(cell, style); got <= 0 {
				t.Fatalf("style %d cell %+v: cost %.3f is not positive", style, cell, got)
			}
		}
	}
}

func TestStyleMinMulIsTheActualMinimum(t *testing.T) {
	for _, style := range allPathStyles {
		var min float32 = math.MaxFloat32
		for _, cell := range navCellSpace() {
			if cell.Cost == 0 {
				continue
			}
			if mul := styleCellCost(cell, style) / float32(cell.Cost); mul < min {
				min = mul
			}
		}
		if got := styleMinMul(style); math.Abs(float64(got-min)) > 1e-4 {
			t.Errorf("style %d: styleMinMul = %.3f, observed minimum multiplier %.3f", style, got, min)
		}
	}
}

// A* is only correct while the heuristic never charges more per metre than the
// cheapest step that metre can be bought with. Overcharging turns the search
// greedy and it hands back a path that ignores the style it was given.
func TestNodeHeuristicNeverOutbidsACheapestStep(t *testing.T) {
	from := navPos(0, 0, 10, 10)
	oneMetre := navPos(0, 0, 11, 10)
	for _, style := range allPathStyles {
		h := nodeHeuristic(from, oneMetre, style)
		for _, cell := range navCellSpace() {
			if cell.Cost == 0 {
				continue
			}
			step := styleCellCost(cell, style)
			if step < h-1e-4 {
				t.Errorf("style %d: heuristic %.3f/m outbids a %.3f step over %+v",
					style, h, step, cell)
			}
		}
	}
}

func TestNodeHeuristicIsChebyshev(t *testing.T) {
	from := navPos(0, 0, 0, 0)
	// A diagonal cell is one Chebyshev metre away, same as a cardinal one.
	cardinal := nodeHeuristic(from, navPos(0, 0, 4, 0), components.PathStyleDirect)
	diagonal := nodeHeuristic(from, navPos(0, 0, 4, 4), components.PathStyleDirect)
	if math.Abs(float64(cardinal-diagonal)) > 1e-4 {
		t.Fatalf("cardinal %.3f != diagonal %.3f", cardinal, diagonal)
	}
	if want := 4 * float32(navCostRoad); math.Abs(float64(cardinal-want)) > 1e-4 {
		t.Fatalf("h = %.3f, want %.3f", cardinal, want)
	}
}

func TestNodeHeuristicIsZeroAtTheGoalAndSymmetric(t *testing.T) {
	p := navPos(3, -2, 12, 40)
	for _, style := range allPathStyles {
		if h := nodeHeuristic(p, p, style); h != 0 {
			t.Errorf("style %d: h at goal = %.3f, want 0", style, h)
		}
		q := navPos(-1, 5, 3, 3)
		if a, b := nodeHeuristic(p, q, style), nodeHeuristic(q, p, style); math.Abs(float64(a-b)) > 1e-3 {
			t.Errorf("style %d: asymmetric, %.3f vs %.3f", style, a, b)
		}
	}
}

func TestNodeHeuristicSpansChunks(t *testing.T) {
	h := nodeHeuristic(navPos(0, 0, 0, 0), navPos(2, 0, 0, 0), components.PathStyleDirect)
	want := 2 * components.ChunkSize * float32(navCostRoad)
	if math.Abs(float64(h-want)) > 1e-3 {
		t.Fatalf("h = %.3f, want %.3f", h, want)
	}
}

func TestNodeHeapPopsInAscendingOrder(t *testing.T) {
	h := nodeHeap{}
	rng := rand.New(rand.NewSource(7))
	const n = 200
	for i := 0; i < n; i++ {
		h.push(nodeHeapEntry{f: rng.Float32() * 100, node: components.NavNode{I: int16(i)}})
	}
	if h.len() != n {
		t.Fatalf("len = %d, want %d", h.len(), n)
	}
	prev := float32(-1)
	for i := 0; i < n; i++ {
		e := h.pop()
		if e.f < prev {
			t.Fatalf("pop %d: f = %.4f after %.4f", i, e.f, prev)
		}
		prev = e.f
	}
	if h.len() != 0 {
		t.Fatalf("heap not drained, len = %d", h.len())
	}
}

func TestNodeHeapCarriesItsPayload(t *testing.T) {
	h := nodeHeap{}
	want := components.NavNode{Kind: components.NodeSurface, I: 17, J: 42}
	h.push(nodeHeapEntry{f: 9, node: components.NavNode{I: 1}})
	h.push(nodeHeapEntry{f: 1, node: want})
	h.push(nodeHeapEntry{f: 5, node: components.NavNode{I: 2}})
	if got := h.pop(); got.node != want {
		t.Fatalf("popped %+v, want %+v", got.node, want)
	}
}

func TestNodeHeapHandlesDuplicateScores(t *testing.T) {
	h := nodeHeap{}
	for i := 0; i < 32; i++ {
		h.push(nodeHeapEntry{f: 3, node: components.NavNode{I: int16(i)}})
	}
	for i := 0; i < 32; i++ {
		if got := h.pop(); got.f != 3 {
			t.Fatalf("f = %.2f, want 3", got.f)
		}
	}
}

func TestWorldPosToCellIsGlobalAndFloors(t *testing.T) {
	cases := []struct {
		pos    components.WorldPos
		gi, gj int32
	}{
		{navPos(0, 0, 0.4, 0.9), 0, 0},
		{navPos(0, 0, 63.9, 63.1), 63, 63},
		{navPos(1, 0, 0.1, 0), 64, 0},
		{navPos(-1, -1, 63.5, 0.5), -1, -64},
		{navPos(2, -3, 10.75, 20.25), 138, -172},
	}
	for _, c := range cases {
		gi, gj := worldPosToCell(c.pos)
		if gi != c.gi || gj != c.gj {
			t.Errorf("%+v: cell = (%d, %d), want (%d, %d)", c.pos, gi, gj, c.gi, c.gj)
		}
	}
}

func TestWorldPosToCellRoundTripsThroughTheShift(t *testing.T) {
	for _, p := range []components.WorldPos{
		navPos(0, 0, 5.5, 5.5), navPos(-2, 3, 63.9, 0.1), navPos(7, -7, 32, 32),
	} {
		gi, gj := worldPosToCell(p)
		cc := components.ChunkCoord{X: gi >> navGridShift, Z: gj >> navGridShift}
		if cc != p.Chunk {
			t.Errorf("%+v: decoded chunk %+v", p, cc)
		}
		li, lj := int32(gi&navGridMask), int32(gj&navGridMask)
		if li != int32(p.Local.X) || lj != int32(p.Local.Z) {
			t.Errorf("%+v: decoded local (%d, %d)", p, li, lj)
		}
	}
}

func TestClampCellRangeCoversTheSpan(t *testing.T) {
	cases := []struct {
		a, b     float32
		min, max int
	}{
		{0.2, 0.8, 0, 1},
		{2, 5, 2, 5},
		{5, 2, 2, 5},
		{-3, 2.5, 0, 3},
		{-20, -5, 0, 0},
		{70, 90, components.NavGridSide, components.NavGridSide},
		{60, 200, 60, components.NavGridSide},
		{-10, 200, 0, components.NavGridSide},
	}
	for _, c := range cases {
		min, max := clampCellRange(c.a, c.b)
		if min != c.min || max != c.max {
			t.Errorf("[%.1f, %.1f]: got [%d, %d), want [%d, %d)", c.a, c.b, min, max, c.min, c.max)
		}
	}
}

func TestClampCellRangeNeverEscapesTheGrid(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 500; i++ {
		a := (rng.Float32() - 0.5) * 400
		b := (rng.Float32() - 0.5) * 400
		min, max := clampCellRange(a, b)
		if min < 0 || max < 0 || min > components.NavGridSide || max > components.NavGridSide {
			t.Fatalf("[%.2f, %.2f] -> [%d, %d)", a, b, min, max)
		}
		if min > max {
			t.Fatalf("[%.2f, %.2f] -> inverted [%d, %d)", a, b, min, max)
		}
	}
}

func TestAbsDelta(t *testing.T) {
	if got := absDelta(3, 7); got != 4 {
		t.Errorf("absDelta(3,7) = %.2f", got)
	}
	if got := absDelta(7, 3); got != 4 {
		t.Errorf("absDelta(7,3) = %.2f", got)
	}
	if got := absDelta(-2, -2); got != 0 {
		t.Errorf("absDelta(-2,-2) = %.2f", got)
	}
}

func TestMaxPairwiseAbs4FindsTheWidestSpread(t *testing.T) {
	cases := []struct {
		a, b, c, d float32
		want       float32
	}{
		{1, 1, 1, 1, 0},
		{0, 0, 0, 5, 5},
		{5, 0, 0, 0, 5},
		{0, 3, -3, 0, 6},
		{-1, -4, -2, -9, 8},
	}
	for _, c := range cases {
		if got := maxPairwiseAbs4(c.a, c.b, c.c, c.d); math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("maxPairwiseAbs4(%.1f,%.1f,%.1f,%.1f) = %.2f, want %.2f",
				c.a, c.b, c.c, c.d, got, c.want)
		}
	}
}

func TestMaxPairwiseAbs4EqualsMaxMinusMin(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 300; i++ {
		v := [4]float32{}
		lo, hi := float32(math.MaxFloat32), float32(-math.MaxFloat32)
		for j := range v {
			v[j] = (rng.Float32() - 0.5) * 40
			lo, hi = minF32(lo, v[j]), maxF32(hi, v[j])
		}
		want := hi - lo
		if got := maxPairwiseAbs4(v[0], v[1], v[2], v[3]); math.Abs(float64(got-want)) > 1e-3 {
			t.Fatalf("%v: got %.4f, want %.4f", v, got, want)
		}
	}
}

func TestMinMaxF32(t *testing.T) {
	if minF32(2, 3) != 2 || minF32(3, 2) != 2 {
		t.Error("minF32")
	}
	if maxF32(2, 3) != 3 || maxF32(3, 2) != 3 {
		t.Error("maxF32")
	}
}

func TestFloorI32MatchesMathFloor(t *testing.T) {
	cases := []float32{0, 0.5, -0.5, 1, -1, 63.999, -63.999, 100.25, -100.25, -0.0001}
	for _, v := range cases {
		want := int32(math.Floor(float64(v)))
		if got := floorI32(v); got != want {
			t.Errorf("floorI32(%v) = %d, want %d", v, got, want)
		}
	}
}

func TestGridNeighboursAreEightWay(t *testing.T) {
	var s NavService
	n := components.NavNode{Kind: components.NodeSurface, Chunk: components.ChunkCoord{X: 1, Z: 2}, I: 10, J: 20}
	nbs := s.gridNeighbours(n)
	if len(nbs) != 8 {
		t.Fatalf("got %d neighbours, want 8", len(nbs))
	}
	seen := map[components.NavNode]bool{}
	diag := 0
	for _, nb := range nbs {
		if nb.node == n {
			t.Fatal("a node is its own neighbour")
		}
		if seen[nb.node] {
			t.Fatalf("duplicate neighbour %+v", nb.node)
		}
		seen[nb.node] = true
		if nb.diag {
			diag++
		}
		if nb.node.Kind != components.NodeSurface {
			t.Fatalf("neighbour changed kind: %+v", nb.node)
		}
	}
	if diag != 4 {
		t.Fatalf("%d diagonals, want 4", diag)
	}
}

func TestGridNeighboursOverflowIntoTheNextChunk(t *testing.T) {
	var s NavService
	// Bottom-right corner cell of chunk (0, 0).
	n := components.NavNode{Kind: components.NodeSurface, I: components.NavGridSide - 1, J: components.NavGridSide - 1}
	var crossed int
	for _, nb := range s.gridNeighbours(n) {
		if nb.node.Chunk != (components.ChunkCoord{}) {
			crossed++
			if nb.node.I >= components.NavGridSide || nb.node.J >= components.NavGridSide {
				t.Fatalf("local index escaped the grid: %+v", nb.node)
			}
		}
	}
	// E, S, SE, NE and SW all step past an edge of the corner cell.
	if crossed != 5 {
		t.Fatalf("%d neighbours crossed the chunk edge, want 5", crossed)
	}
}

func TestGridNeighboursOverflowIntoNegativeChunks(t *testing.T) {
	var s NavService
	n := components.NavNode{Kind: components.NodeSurface} // cell (0, 0) of chunk (0, 0)
	for _, nb := range s.gridNeighbours(n) {
		if nb.node.I < 0 || nb.node.J < 0 {
			t.Fatalf("negative local index: %+v", nb.node)
		}
		if nb.node.Chunk.X < 0 && nb.node.I != components.NavGridSide-1 {
			t.Fatalf("west neighbour landed at I=%d, want %d", nb.node.I, components.NavGridSide-1)
		}
	}
}

func TestGridNeighboursStayOnTheSameLevel(t *testing.T) {
	var s NavService
	level := ecs.Entity{}
	n := components.NavNode{Kind: components.NodeLevel, Level: level, I: 3, J: 4}
	nbs := s.gridNeighbours(n)
	if len(nbs) != 8 {
		t.Fatalf("got %d neighbours, want 8", len(nbs))
	}
	for _, nb := range nbs {
		if nb.node.Kind != components.NodeLevel || nb.node.Level != level {
			t.Fatalf("level neighbour leaked to %+v", nb.node)
		}
	}
}

func TestArrivalRadiusForAlwaysPositive(t *testing.T) {
	for k := components.OrderKindCode(0); k < components.OrderKindCount; k++ {
		if r := arrivalRadiusFor(k); r <= 0 {
			t.Errorf("kind %d: arrival radius %.2f", k, r)
		}
	}
}
