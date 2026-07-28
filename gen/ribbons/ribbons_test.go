package ribbons

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// bumpy is deterministic relief with both long swells and short chop, so the
// profile has something to smooth away.
func bumpy(x, z float32) float32 {
	return 3*float32(math.Sin(float64(x)/17)) +
		2*float32(math.Cos(float64(z)/11)) +
		0.6*float32(math.Sin(float64(x)/3.1))
}

func wp(x, z float32) components.WorldPos {
	return components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
}

func graph(nodes [][2]float32, edges []components.RoadEdge) *components.RoadGraph {
	g := &components.RoadGraph{Edges: edges}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, components.RoadNode{Pos: wp(n[0], n[1])})
	}
	return g
}

func straightHighway() *components.RoadGraph {
	return graph([][2]float32{{0, 0}, {60, 0}, {120, 20}},
		[]components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4},
		})
}

// The whole point of P1/P2: the deck tracks the ground closely enough that
// the earthworks stay inside the class budget instead of terracing.
func TestProfileStaysInsideEarthworkBudget(t *testing.T) {
	net := BuildRoads(straightHighway(), bumpy)
	if len(net.Deck) < 50 {
		t.Fatalf("deck samples = %d, want a dense run", len(net.Deck))
	}
	st := roadStyles[components.RoadHighway]
	for i, s := range net.Deck {
		g := bumpy(s.X, s.Z)
		if s.Y > g+st.MaxFill+0.01 {
			t.Fatalf("sample %d fills %.2f m, budget %.2f", i, s.Y-g, st.MaxFill)
		}
		if s.Y < g-st.MaxCut-0.01 {
			t.Fatalf("sample %d cuts %.2f m, budget %.2f", i, g-s.Y, st.MaxCut)
		}
	}
}

// Smoothing is what turns raw relief into a drivable line; without it the
// deck would inherit the terrain's short chop.
func TestProfileIsSmootherThanTheGround(t *testing.T) {
	net := BuildRoads(straightHighway(), bumpy)
	var deckJerk, groundJerk float64
	for i := 1; i+1 < len(net.Deck); i++ {
		a, b, c := net.Deck[i-1], net.Deck[i], net.Deck[i+1]
		deckJerk += math.Abs(float64(a.Y - 2*b.Y + c.Y))
		ga, gb, gc := bumpy(a.X, a.Z), bumpy(b.X, b.Z), bumpy(c.X, c.Z)
		groundJerk += math.Abs(float64(ga - 2*gb + gc))
	}
	if deckJerk > groundJerk*0.35 {
		t.Errorf("deck curvature %.3f vs ground %.3f — profile is not smoothed",
			deckJerk, groundJerk)
	}
}

// Every chain meeting at a junction is pinned to one shared node height, so
// there is no step where two roads meet.
func TestJunctionHeightIsShared(t *testing.T) {
	g := graph([][2]float32{{0, 0}, {50, 0}, {100, 0}, {50, 50}},
		[]components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 3, Kind: components.RoadLocal, Width: 3},
		})
	net := BuildRoads(g, bumpy)

	var atNode []float32
	for _, s := range net.Deck {
		if hypot(s.X-50, s.Z-0) < 0.01 {
			atNode = append(atNode, s.Y)
		}
	}
	if len(atNode) != 3 {
		t.Fatalf("chains ending at the junction = %d, want 3", len(atNode))
	}
	for _, y := range atNode[1:] {
		if absF(y-atNode[0]) > 1e-4 {
			t.Errorf("junction heights disagree: %v", atNode)
		}
	}
}

// Corners are rounded, but the rounding may not leave the straight-edge
// corridor that NavGrid and the router still rasterise (P4).
func TestCornerStaysInsideNavCorridor(t *testing.T) {
	g := graph([][2]float32{{0, 0}, {50, 0}, {50, 50}},
		[]components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 4},
		})
	net := BuildRoads(g, bumpy)

	halfW := float32(2.0)
	rounded := false
	for _, s := range net.Deck {
		d := minF(
			pointToSeg(s.X, s.Z, 0, 0, 50, 0),
			pointToSeg(s.X, s.Z, 50, 0, 50, 50))
		if d > halfW {
			t.Fatalf("centre line strays %.2f m off both edges at (%.1f, %.1f)", d, s.X, s.Z)
		}
		if d > 0.05 {
			rounded = true
		}
	}
	if !rounded {
		t.Error("corner was not rounded at all")
	}
}

// A bridge deck must clear its banks by the full lift even after the profile
// smoothing that produces the approach ramps.
func TestBridgeDeckClearsItsBanks(t *testing.T) {
	g := graph([][2]float32{{0, 0}, {40, 0}, {70, 0}, {110, 0}},
		[]components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 2, Kind: components.RoadBridge, Width: 4},
			{From: 2, To: 3, Kind: components.RoadHighway, Width: 4},
		})
	net := BuildRoads(g, bumpy)

	span := 0
	for _, s := range net.Deck {
		if s.X < 41 || s.X > 69 {
			continue
		}
		span++
		if got := s.Y - bumpy(s.X, s.Z); got < BridgeLift-0.01 {
			t.Fatalf("deck clearance %.2f m at x=%.1f, want >= %.2f", got, s.X, BridgeLift)
		}
	}
	if span == 0 {
		t.Fatal("no samples landed on the bridge span")
	}
}

// The approach is a ramp, not a step: the metre before the bridge is already
// most of the way up.
func TestBridgeApproachIsRamped(t *testing.T) {
	g := graph([][2]float32{{0, 0}, {40, 0}, {70, 0}, {110, 0}},
		[]components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 4},
			{From: 1, To: 2, Kind: components.RoadBridge, Width: 4},
			{From: 2, To: 3, Kind: components.RoadHighway, Width: 4},
		})
	net := BuildRoads(g, bumpy)
	for i := 1; i < len(net.Deck); i++ {
		if d := absF(net.Deck[i].Y - net.Deck[i-1].Y); d > 0.5 {
			t.Fatalf("%.2f m step between consecutive samples at x=%.1f",
				d, net.Deck[i].X)
		}
	}
}

// RoadSurface is what carries units and vehicles over an embankment; it must
// answer on the carriageway and stay quiet off it.
func TestDeckQueryCoversTheCarriageway(t *testing.T) {
	net := BuildRoads(straightHighway(), bumpy)
	var rs components.RoadSurface
	rs.Build(net.Deck)

	mid := net.Deck[len(net.Deck)/2]
	const low float32 = -100
	if got := rs.SurfaceY(low, mid.X, mid.Z); absF(got-mid.Y) > 0.01 {
		t.Errorf("carriageway centre = %.3f, deck = %.3f", got, mid.Y)
	}
	if got := rs.SurfaceY(low, mid.X, mid.Z+20); got != low {
		t.Errorf("20 m off the road = %.3f, want the untouched ground", got)
	}
	// The shoulder hands authority back gradually — a hard edge is what
	// popped walkers up a step as they crossed it.
	edge := rs.SurfaceY(low, mid.X, mid.Z+mid.HalfW+0.01)
	foot := rs.SurfaceY(low, mid.X, mid.Z+mid.ShoulderW-0.01)
	if !(edge > foot && foot > low) {
		t.Errorf("shoulder blend is not monotone: edge=%.3f foot=%.3f", edge, foot)
	}
}

// The surface must sit inside its own bed everywhere: never above the bank
// (it would spill across the field) and never below the cut floor (the
// stretch would vanish under the terrain — what a strict downstream-only
// rule did on relief that climbs again).
func TestWaterSurfaceStaysInItsBed(t *testing.T) {
	rollingValley := func(x, z float32) float32 {
		return 20 - z*0.05 + 1.6*float32(math.Sin(float64(z)/13))
	}
	rivers := &components.Rivers{Polylines: []components.RiverPolyline{{
		Points: []components.WorldPos{wp(0, 0), wp(4, 60), wp(-2, 120)},
		Width:  6, Depth: 1.4,
	}}}
	meshes := BuildWater(rivers, rollingValley)
	if len(meshes) == 0 {
		t.Fatal("no water geometry")
	}
	for _, m := range meshes {
		for i := 0; i+2 < len(m.Verts); i += 3 {
			x := m.Verts[i] + m.AnchorX
			y := m.Verts[i+1]
			z := m.Verts[i+2] + m.AnchorZ
			sink := rollingValley(x, z) - y
			if sink < 0.05 || sink > 1.4 {
				t.Fatalf("surface sits %.2f m below a %.1f m bed at (%.1f, %.1f)",
					sink, 1.4, x, z)
			}
		}
	}
}

// Downstream bias: over a run that only descends, the surface must descend
// with it rather than pool into steps.
func TestWaterFollowsADescendingRun(t *testing.T) {
	descend := func(_, z float32) float32 { return 20 - z*0.05 }
	rivers := &components.Rivers{Polylines: []components.RiverPolyline{{
		Points: []components.WorldPos{wp(0, 0), wp(0, 120)},
		Width:  6, Depth: 1.4,
	}}}
	meshes := BuildWater(rivers, descend)
	prev := float32(math.Inf(1))
	for _, m := range meshes {
		for i := 1; i < len(m.Verts); i += 3 {
			if m.Verts[i] > prev+1e-3 {
				t.Fatalf("water climbs on a descending run: %.3f after %.3f",
					m.Verts[i], prev)
			}
			prev = m.Verts[i]
		}
	}
}

func pointToSeg(px, pz, ax, az, bx, bz float32) float32 {
	dx, dz := bx-ax, bz-az
	l2 := dx*dx + dz*dz
	if l2 == 0 {
		return hypot(px-ax, pz-az)
	}
	t := clampF(((px-ax)*dx+(pz-az)*dz)/l2, 0, 1)
	return hypot(px-(ax+t*dx), pz-(az+t*dz))
}
