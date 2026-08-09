package systems

import (
	"math"
	"math/rand"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func TestPointToSegment2DProjectsOntoTheSpan(t *testing.T) {
	cases := []struct {
		name           string
		px, pz         float32
		ax, az, bx, bz float32
		want           float32
	}{
		{"perpendicular", 5, 3, 0, 0, 10, 0, 3},
		{"on the segment", 5, 0, 0, 0, 10, 0, 0},
		{"past the start", -4, 0, 0, 0, 10, 0, 4},
		{"past the end", 14, 0, 0, 0, 10, 0, 4},
		{"past the end diagonally", 13, 4, 0, 0, 10, 0, 5},
		{"degenerate segment", 3, 4, 0, 0, 0, 0, 5},
		{"diagonal segment", 0, 10, 0, 0, 10, 10, float32(math.Sqrt2 * 5)},
	}
	for _, c := range cases {
		got := pointToSegment2D(c.px, c.pz, c.ax, c.az, c.bx, c.bz)
		if math.Abs(float64(got-c.want)) > 1e-3 {
			t.Errorf("%s: %.4f, want %.4f", c.name, got, c.want)
		}
	}
}

func TestPointToSegment2DIsEndpointSymmetric(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 300; i++ {
		px, pz := rng.Float32()*40-20, rng.Float32()*40-20
		ax, az := rng.Float32()*40-20, rng.Float32()*40-20
		bx, bz := rng.Float32()*40-20, rng.Float32()*40-20
		fwd := pointToSegment2D(px, pz, ax, az, bx, bz)
		rev := pointToSegment2D(px, pz, bx, bz, ax, az)
		if math.Abs(float64(fwd-rev)) > 1e-3 {
			t.Fatalf("swap changed the distance: %.4f vs %.4f", fwd, rev)
		}
	}
}

func TestNearestOnSegmentMatchesTheDistanceHelper(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 300; i++ {
		px, pz := rng.Float32()*40-20, rng.Float32()*40-20
		ax, az := rng.Float32()*40-20, rng.Float32()*40-20
		bx, bz := rng.Float32()*40-20, rng.Float32()*40-20
		cx, cz := nearestOnSegment(px, pz, ax, az, bx, bz)
		want := pointToSegment2D(px, pz, ax, az, bx, bz)
		got := float32(math.Hypot(float64(px-cx), float64(pz-cz)))
		if math.Abs(float64(got-want)) > 1e-3 {
			t.Fatalf("nearest point is %.4f away, distance helper says %.4f", got, want)
		}
	}
}

func TestNearestOnSegmentClampsToEndpoints(t *testing.T) {
	if x, z := nearestOnSegment(-5, 0, 0, 0, 10, 0); x != 0 || z != 0 {
		t.Errorf("before the start: (%.2f, %.2f), want (0, 0)", x, z)
	}
	if x, z := nearestOnSegment(50, 0, 0, 0, 10, 0); x != 10 || z != 0 {
		t.Errorf("past the end: (%.2f, %.2f), want (10, 0)", x, z)
	}
	if x, z := nearestOnSegment(3, 4, 7, 7, 7, 7); x != 7 || z != 7 {
		t.Errorf("degenerate: (%.2f, %.2f), want (7, 7)", x, z)
	}
}

func TestPointToSegXZIgnoresHeight(t *testing.T) {
	p := components.WorldPos{Local: rl.Vector3{X: 5, Y: 100, Z: 3}}
	a := components.WorldPos{Local: rl.Vector3{X: 0, Y: 0, Z: 0}}
	b := components.WorldPos{Local: rl.Vector3{X: 10, Y: -50, Z: 0}}
	if got := pointToSegXZ(p, a, b); math.Abs(float64(got-3)) > 1e-3 {
		t.Fatalf("distance %.4f, want 3", got)
	}
}

func TestPointToSegXZSpansChunks(t *testing.T) {
	p := navPos(1, 0, 0, 5)
	a := navPos(0, 0, 0, 0)
	b := navPos(2, 0, 0, 0)
	if got := pointToSegXZ(p, a, b); math.Abs(float64(got-5)) > 1e-3 {
		t.Fatalf("distance %.4f, want 5", got)
	}
}

func TestPointToSegXZDegenerateSegment(t *testing.T) {
	p := navPos(0, 0, 3, 4)
	a := navPos(0, 0, 0, 0)
	if got := pointToSegXZ(p, a, a); math.Abs(float64(got-5)) > 1e-3 {
		t.Fatalf("distance %.4f, want 5", got)
	}
}

func TestCenterXZDistSqDropsY(t *testing.T) {
	a := components.WorldPos{Local: rl.Vector3{X: 0, Y: 0, Z: 0}}
	b := components.WorldPos{Local: rl.Vector3{X: 3, Y: 99, Z: 4}}
	if got := centerXZDistSq(a, b); math.Abs(float64(got-25)) > 1e-3 {
		t.Fatalf("dist^2 = %.4f, want 25", got)
	}
}

func TestDecimationStepStaysInBand(t *testing.T) {
	cases := []struct {
		spacing float32
		want    int
	}{
		{0, 4}, {1, 4}, {2, 4}, {3, 5}, {4, 6}, {6, 8}, {12, 8}, {100, 8},
	}
	for _, c := range cases {
		if got := decimationStep(c.spacing); got != c.want {
			t.Errorf("spacing %.1f: step %d, want %d", c.spacing, got, c.want)
		}
	}
}

func TestDecimationStepIsMonotonic(t *testing.T) {
	prev := decimationStep(0)
	for s := float32(0.5); s <= 20; s += 0.5 {
		got := decimationStep(s)
		if got < prev {
			t.Fatalf("spacing %.1f: step dropped %d -> %d", s, prev, got)
		}
		prev = got
	}
}

func TestSplashFalloffMulIsLinearAndQuadraticAtTheEnds(t *testing.T) {
	for _, tt := range []float32{0, 0.25, 0.5, 0.75, 1} {
		if got := splashFalloffMul(tt, 1); math.Abs(float64(got-tt)) > 1e-4 {
			t.Errorf("falloff 1 at t=%.2f: %.4f, want %.4f", tt, got, tt)
		}
		if got, want := splashFalloffMul(tt, 2), tt*tt; math.Abs(float64(got-want)) > 1e-4 {
			t.Errorf("falloff 2 at t=%.2f: %.4f, want %.4f", tt, got, want)
		}
	}
}

func TestSplashFalloffMulIsContinuousAcrossTheBranches(t *testing.T) {
	const eps = 1e-3
	for _, tt := range []float32{0.2, 0.5, 0.9} {
		if a, b := splashFalloffMul(tt, 1), splashFalloffMul(tt, 1+eps); math.Abs(float64(a-b)) > 1e-2 {
			t.Errorf("t=%.2f: jump at falloff 1, %.4f vs %.4f", tt, a, b)
		}
		if a, b := splashFalloffMul(tt, 2-eps), splashFalloffMul(tt, 2); math.Abs(float64(a-b)) > 1e-2 {
			t.Errorf("t=%.2f: jump at falloff 2, %.4f vs %.4f", tt, a, b)
		}
	}
}

func TestSplashFalloffMulStaysInUnitRangeAndFallsOff(t *testing.T) {
	for _, falloff := range []float32{0.5, 1, 1.5, 2, 4} {
		prev := float32(-1)
		for tt := float32(0); tt <= 1.0001; tt += 0.05 {
			got := splashFalloffMul(tt, falloff)
			if got < -1e-4 || got > 1+1e-4 {
				t.Fatalf("falloff %.1f t=%.2f: %.4f outside [0, 1]", falloff, tt, got)
			}
			if got < prev-1e-4 {
				t.Fatalf("falloff %.1f: not monotonic at t=%.2f (%.4f after %.4f)", falloff, tt, got, prev)
			}
			prev = got
		}
	}
	// Higher falloff must never damage more at the same distance.
	for _, tt := range []float32{0.2, 0.5, 0.8} {
		if splashFalloffMul(tt, 2) > splashFalloffMul(tt, 1)+1e-4 {
			t.Errorf("t=%.2f: quadratic falloff pays more than linear", tt)
		}
	}
}

func TestClamp32(t *testing.T) {
	if got := clamp32(5, 0, 1); got != 1 {
		t.Errorf("high clamp: %.2f", got)
	}
	if got := clamp32(-5, 0, 1); got != 0 {
		t.Errorf("low clamp: %.2f", got)
	}
	if got := clamp32(0.4, 0, 1); got != 0.4 {
		t.Errorf("passthrough: %.2f", got)
	}
}

func TestStanceConcealmentMulOrdersByExposure(t *testing.T) {
	stand := stanceConcealmentMul(components.StanceStand)
	crouch := stanceConcealmentMul(components.StanceCrouch)
	prone := stanceConcealmentMul(components.StanceProne)
	if !(prone < crouch && crouch < stand) {
		t.Fatalf("prone %.2f, crouch %.2f, stand %.2f — expected strictly increasing", prone, crouch, stand)
	}
	if stand != 1 {
		t.Fatalf("standing must be full visibility, got %.2f", stand)
	}
}

func TestMaskProtectionCoversTheMaskedBearings(t *testing.T) {
	const north = 1 << 0 // coverDirs[0] = (0, -1)
	if got := maskProtection(north, 0, -1); math.Abs(float64(got-1)) > 1e-3 {
		t.Errorf("straight into the masked sector: %.3f, want 1", got)
	}
	if got := maskProtection(north, 0, 1); got != 0 {
		t.Errorf("opposite bearing: %.3f, want 0", got)
	}
	if got := maskProtection(0, 0, -1); got != 0 {
		t.Errorf("empty mask: %.3f, want 0", got)
	}
	if got := maskProtection(north, 0, 0); got != 0 {
		t.Errorf("degenerate bearing: %.3f, want 0", got)
	}
}

func TestMaskProtectionInterpolatesBetweenSectors(t *testing.T) {
	const northAndNE = 1<<0 | 1<<1
	// NNE lies between two masked sectors: full protection.
	if got := maskProtection(northAndNE, 0.383, -0.924); got < 0.95 {
		t.Errorf("NNE between two masked sectors: %.3f, want ~1", got)
	}
	// Due east brackets E/SE — neither masked, so the ridge does nothing.
	if got := maskProtection(northAndNE, 1, 0); got != 0 {
		t.Errorf("east of a N+NE mask: %.3f, want 0", got)
	}
	// Halfway between NE and E only the NE half of the vote is covered.
	if got := maskProtection(northAndNE, 0.924, -0.383); math.Abs(float64(got-0.5)) > 1e-2 {
		t.Errorf("ENE of a N+NE mask: %.3f, want 0.5", got)
	}
}

func TestMaskProtectionMatchesEachCompassSector(t *testing.T) {
	for d := 0; d < 8; d++ {
		mask := uint8(1) << uint(d)
		tx, tz := coverDirs[d][0], coverDirs[d][1]
		if got := maskProtection(mask, tx, tz); math.Abs(float64(got-1)) > 1e-3 {
			t.Errorf("sector %d dead-on: %.4f, want 1", d, got)
		}
		// Two sectors away is 90 degrees off: outside both bracketing sectors.
		opp := coverDirs[(d+2)%8]
		if got := maskProtection(mask, opp[0], opp[1]); got != 0 {
			t.Errorf("sector %d at 90 degrees: %.4f, want 0", d, got)
		}
	}
}

func TestMaskProtectionIsLinearAcrossASector(t *testing.T) {
	const mask = 1 << 4 // S (+Z)
	// Sweep from S toward SW; protection must fall monotonically to zero.
	prev := float32(2)
	for f := float32(0); f <= 1.0001; f += 0.1 {
		a := float64(math.Pi/2) + float64(f)*math.Pi/4
		got := maskProtection(mask, float32(math.Cos(a)), float32(math.Sin(a)))
		if got > prev+1e-3 {
			t.Fatalf("f=%.1f: protection rose %.3f -> %.3f", f, prev, got)
		}
		if math.Abs(float64(got-(1-f))) > 1e-2 {
			t.Fatalf("f=%.1f: protection %.3f, want %.3f", f, got, 1-f)
		}
		prev = got
	}
}

func TestMaskProtectionStaysInUnitRange(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for i := 0; i < 400; i++ {
		mask := uint8(rng.Intn(256))
		tx, tz := rng.Float32()*2-1, rng.Float32()*2-1
		got := maskProtection(mask, tx, tz)
		if got < -1e-4 || got > 1+1e-4 {
			t.Fatalf("mask %08b bearing (%.2f, %.2f): %.4f", mask, tx, tz, got)
		}
	}
	if got := maskProtection(0xFF, 1, 0); math.Abs(float64(got-1)) > 1e-3 {
		t.Fatalf("all-round mask: %.4f, want 1", got)
	}
}

func TestMaskProtectionIsScaleInvariant(t *testing.T) {
	const mask = 1<<2 | 1<<3
	base := maskProtection(mask, 3, 4)
	for _, s := range []float32{0.1, 2, 50} {
		if got := maskProtection(mask, 3*s, 4*s); math.Abs(float64(got-base)) > 1e-3 {
			t.Fatalf("scale %.1f changed protection: %.4f vs %.4f", s, got, base)
		}
	}
}

func riverAt(width float32, pts ...[2]float32) *components.Rivers {
	pl := components.RiverPolyline{Width: width}
	for _, p := range pts {
		pl.Points = append(pl.Points, components.Normalize(components.WorldPos{
			Local: rl.Vector3{X: p[0], Z: p[1]},
		}))
	}
	return &components.Rivers{Polylines: []components.RiverPolyline{pl}}
}

func TestIsInRiverStripUsesHalfWidth(t *testing.T) {
	rivers := riverAt(20, [2]float32{0, 0}, [2]float32{100, 0})
	if !isInRiverStrip(50, 9.9, rivers) {
		t.Error("just inside the strip read as outside")
	}
	if isInRiverStrip(50, 10.1, rivers) {
		t.Error("just outside the strip read as inside")
	}
	if isInRiverStrip(-20, 0, rivers) {
		t.Error("before the polyline start read as inside")
	}
}

func TestIsInRiverStripHandlesEmptyRivers(t *testing.T) {
	if isInRiverStrip(0, 0, &components.Rivers{}) {
		t.Error("no polylines must read as dry")
	}
	if isInRiverStrip(0, 0, riverAt(10, [2]float32{5, 5})) {
		t.Error("a single-point polyline has no segments")
	}
}

func roadPair(ax, az, bx, bz float32, width float32) *components.RoadGraph {
	mk := func(x, z float32) components.RoadNode {
		return components.RoadNode{Pos: components.Normalize(components.WorldPos{Local: rl.Vector3{X: x, Z: z}})}
	}
	return &components.RoadGraph{
		Nodes: []components.RoadNode{mk(ax, az), mk(bx, bz)},
		Edges: []components.RoadEdge{{From: 0, To: 1, Kind: components.RoadLocal, Width: width}},
	}
}

func TestPreprocessRoadGraphTagsTheCrossing(t *testing.T) {
	g := roadPair(0, 50, 200, 50, 6)
	rivers := riverAt(20, [2]float32{100, 0}, [2]float32{100, 200})
	PreprocessRoadGraph(g, rivers)

	if len(g.Edges) != 3 {
		t.Fatalf("expected approach / span / approach, got %d edges", len(g.Edges))
	}
	var bridges int
	for _, e := range g.Edges {
		if e.Kind == components.RoadBridge {
			bridges++
		}
		if e.Width != 6 {
			t.Errorf("sub-edge lost its width: %.1f", e.Width)
		}
	}
	if bridges != 1 {
		t.Fatalf("bridges = %d, want exactly 1", bridges)
	}
	if g.Edges[1].Kind != components.RoadBridge {
		t.Errorf("the middle sub-edge should be the span, got kind %v", g.Edges[1].Kind)
	}
}

func TestPreprocessRoadGraphChainsTheSubEdges(t *testing.T) {
	g := roadPair(0, 50, 200, 50, 6)
	from, to := g.Edges[0].From, g.Edges[0].To
	PreprocessRoadGraph(g, riverAt(20, [2]float32{100, 0}, [2]float32{100, 200}))

	if g.Edges[0].From != from {
		t.Errorf("chain does not start at the original node")
	}
	if g.Edges[len(g.Edges)-1].To != to {
		t.Errorf("chain does not end at the original node")
	}
	for i := 1; i < len(g.Edges); i++ {
		if g.Edges[i].From != g.Edges[i-1].To {
			t.Fatalf("edge %d starts at %d, previous ended at %d", i, g.Edges[i].From, g.Edges[i-1].To)
		}
	}
}

func TestPreprocessRoadGraphLeavesDryRoadsAlone(t *testing.T) {
	g := roadPair(0, 50, 200, 50, 6)
	before := len(g.Edges)
	PreprocessRoadGraph(g, riverAt(20, [2]float32{100, 500}, [2]float32{100, 700}))
	if len(g.Edges) != before {
		t.Fatalf("a road nowhere near the river was split into %d edges", len(g.Edges))
	}
	if g.Edges[0].Kind == components.RoadBridge {
		t.Error("a dry road was tagged as a bridge")
	}
}

func TestPreprocessRoadGraphIsIdempotentOnDryGraphs(t *testing.T) {
	g := roadPair(0, 50, 200, 50, 6)
	rivers := riverAt(20, [2]float32{100, 500}, [2]float32{100, 700})
	PreprocessRoadGraph(g, rivers)
	first := len(g.Edges)
	PreprocessRoadGraph(g, rivers)
	if len(g.Edges) != first {
		t.Fatalf("second pass changed the edge count: %d -> %d", first, len(g.Edges))
	}
}

func TestPreprocessRoadGraphHandlesNilInputs(t *testing.T) {
	PreprocessRoadGraph(nil, &components.Rivers{})
	g := roadPair(0, 0, 10, 0, 6)
	PreprocessRoadGraph(g, nil)
	if len(g.Edges) != 1 {
		t.Fatalf("nil rivers must be a no-op, got %d edges", len(g.Edges))
	}
}

func TestWorldXZUnfoldsTheChunk(t *testing.T) {
	x, z := worldXZ(navPos(2, -1, 10, 20))
	if x != 2*components.ChunkSize+10 || z != -components.ChunkSize+20 {
		t.Fatalf("worldXZ = (%.2f, %.2f)", x, z)
	}
}

func TestTooCloseToBuildingUsesPerimeterDistance(t *testing.T) {
	foot := []components.AABB2D{{MinX: 0, MinZ: 0, MaxX: 10, MaxZ: 10}}
	if !tooCloseToBuilding(foot, 5, 5) {
		t.Error("inside the footprint must be too close")
	}
	if !tooCloseToBuilding(foot, 10.1, 5) {
		t.Error("just outside the wall must still be too close")
	}
	if tooCloseToBuilding(foot, 500, 500) {
		t.Error("far away must be clear")
	}
	if tooCloseToBuilding(nil, 0, 0) {
		t.Error("no footprints must be clear")
	}
}

func TestTooCloseToRoadWidensWithTheStrip(t *testing.T) {
	narrow := roadPair(0, 0, 100, 0, 2)
	wide := roadPair(0, 0, 100, 0, 40)
	if tooCloseToRoad(narrow, 50, 15) {
		t.Error("15 m off a 2 m road must be clear")
	}
	if !tooCloseToRoad(wide, 50, 15) {
		t.Error("15 m off a 40 m road must be blocked")
	}
	if tooCloseToRoad(&components.RoadGraph{}, 0, 0) {
		t.Error("an empty graph must be clear")
	}
}

func TestTooCloseToTrenchUsesHalfWidth(t *testing.T) {
	line := []components.Trench{{
		Width: 10,
		Points: []components.WorldPos{
			components.Normalize(components.WorldPos{Local: rl.Vector3{X: 0, Z: 0}}),
			components.Normalize(components.WorldPos{Local: rl.Vector3{X: 100, Z: 0}}),
		},
	}}
	if !tooCloseToTrench(line, 50, 4) {
		t.Error("inside the trench must be too close")
	}
	if tooCloseToTrench(line, 50, 40) {
		t.Error("40 m away must be clear")
	}
	if tooCloseToTrench(nil, 0, 0) {
		t.Error("no trenches must be clear")
	}
}

// entitySeq hands out distinct entities; the FIFO keys on Target, so zero
// values would all collide.
func entitySeq(n int) []ecs.Entity {
	w := ecs.NewWorld()
	out := make([]ecs.Entity, n)
	for i := range out {
		out[i] = w.NewEntity()
	}
	return out
}

func awarenessSlotOf(aw *components.Awareness, target ecs.Entity) int {
	for i := range aw.LastSeen {
		if aw.LastSeen[i].Time != 0 && aw.LastSeen[i].Target == target {
			return i
		}
	}
	return -1
}

func awarenessUsed(aw *components.Awareness) int {
	n := 0
	for _, e := range aw.LastSeen {
		if e.Time != 0 {
			n++
		}
	}
	return n
}

func TestRecordSightingUpsertsInPlace(t *testing.T) {
	var aw components.Awareness
	target := entitySeq(1)[0]
	recordSighting(&aw, target, navPos(0, 0, 1, 1), 1, components.AwareDirect)
	slot := awarenessSlotOf(&aw, target)
	if slot < 0 {
		t.Fatal("first sighting was not recorded")
	}
	recordSighting(&aw, target, navPos(0, 0, 9, 9), 2, 0)

	if used := awarenessUsed(&aw); used != 1 {
		t.Fatalf("same target occupies %d slots, want 1", used)
	}
	if got := awarenessSlotOf(&aw, target); got != slot {
		t.Fatalf("entry moved slot %d -> %d", slot, got)
	}
	if aw.LastSeen[slot].Time != 2 || aw.LastSeen[slot].Pos.Local.X != 9 {
		t.Fatalf("entry not refreshed: %+v", aw.LastSeen[slot])
	}
}

func TestRecordSightingKeepsDirectWithinTheSameStamp(t *testing.T) {
	var aw components.Awareness
	target := entitySeq(1)[0]
	recordSighting(&aw, target, navPos(0, 0, 1, 1), 5, components.AwareDirect)
	recordSighting(&aw, target, navPos(0, 0, 1, 1), 5, 0)
	slot := awarenessSlotOf(&aw, target)
	if aw.LastSeen[slot].Flags&components.AwareDirect == 0 {
		t.Fatal("a shared report at the same stamp downgraded a direct sighting")
	}
}

func TestRecordSightingDowngradesOnALaterStamp(t *testing.T) {
	var aw components.Awareness
	target := entitySeq(1)[0]
	recordSighting(&aw, target, navPos(0, 0, 1, 1), 5, components.AwareDirect)
	recordSighting(&aw, target, navPos(0, 0, 1, 1), 6, 0)
	slot := awarenessSlotOf(&aw, target)
	if aw.LastSeen[slot].Flags&components.AwareDirect != 0 {
		t.Fatal("a later shared-only report must not keep the direct flag")
	}
}

func TestRecordSightingFillsEverySlotBeforeEvicting(t *testing.T) {
	var aw components.Awareness
	targets := entitySeq(components.AwarenessSlots)
	for i, e := range targets {
		recordSighting(&aw, e, navPos(0, 0, float32(i), 0), float32(i+1), 0)
	}
	if used := awarenessUsed(&aw); used != components.AwarenessSlots {
		t.Fatalf("%d slots used, want %d", used, components.AwarenessSlots)
	}
	for _, e := range targets {
		if awarenessSlotOf(&aw, e) < 0 {
			t.Fatalf("target %v was dropped while the FIFO still had room", e)
		}
	}
}

func TestRecordSightingEvictsTheOldest(t *testing.T) {
	var aw components.Awareness
	targets := entitySeq(components.AwarenessSlots + 1)
	for i := 0; i < components.AwarenessSlots; i++ {
		recordSighting(&aw, targets[i], navPos(0, 0, float32(i), 0), float32(i+1), 0)
	}
	fresh := targets[components.AwarenessSlots]
	recordSighting(&aw, fresh, navPos(0, 0, 99, 0), 100, 0)

	if awarenessSlotOf(&aw, fresh) < 0 {
		t.Fatal("the new sighting was not recorded")
	}
	if awarenessSlotOf(&aw, targets[0]) >= 0 {
		t.Fatal("the oldest entry survived the eviction")
	}
	for i := 1; i < components.AwarenessSlots; i++ {
		if awarenessSlotOf(&aw, targets[i]) < 0 {
			t.Fatalf("entry %d was evicted instead of the oldest", i)
		}
	}
}
