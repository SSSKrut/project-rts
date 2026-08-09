package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func roadNode(x, z float32) components.RoadNode {
	return components.RoadNode{Pos: components.Normalize(components.WorldPos{Local: rl.Vector3{X: x, Z: z}})}
}

// routerOn wires a RoadRouter over a hand-built graph — no systems, no scene.
func routerOn(t *testing.T, g *components.RoadGraph) *RoadRouter {
	t.Helper()
	w := ecs.NewWorld()
	ecs.AddResource(w, g)
	return NewRoadRouter(w)
}

// A straight highway along +X at z = 0, four nodes 100 m apart.
func straightHighway() *components.RoadGraph {
	return &components.RoadGraph{
		Nodes: []components.RoadNode{roadNode(0, 0), roadNode(100, 0), roadNode(200, 0), roadNode(300, 0)},
		Edges: []components.RoadEdge{
			{From: 0, To: 1, Kind: components.RoadHighway, Width: 8},
			{From: 1, To: 2, Kind: components.RoadHighway, Width: 8},
			{From: 2, To: 3, Kind: components.RoadHighway, Width: 8},
		},
	}
}

func truckSpec() *components.VehicleSpec { return components.SpecForVehicle(components.VehicleTruck) }
func tankSpec() *components.VehicleSpec  { return components.SpecForVehicle(components.VehicleTank) }

func TestRoadSpeedForEdgeDiscountsDirt(t *testing.T) {
	spec := truckSpec()
	highway := roadSpeedForEdge(components.RoadHighway, spec)
	dirt := roadSpeedForEdge(components.RoadDirtTrack, spec)
	if highway != spec.MaxSpeedRoad {
		t.Errorf("highway speed %.2f, want the road spec %.2f", highway, spec.MaxSpeedRoad)
	}
	if dirt >= highway {
		t.Errorf("dirt %.2f is not slower than highway %.2f", dirt, highway)
	}
}

func TestRoadSpeedForEdgeNeverBelowOffroad(t *testing.T) {
	// A pathological spec whose dirt discount would dip under its off-road
	// speed: a road must never be the slower choice.
	spec := components.VehicleSpec{MaxSpeedRoad: 10, MaxSpeedOffroad: 9}
	if got := roadSpeedForEdge(components.RoadDirtTrack, &spec); got < spec.MaxSpeedOffroad {
		t.Fatalf("dirt speed %.2f fell below off-road %.2f", got, spec.MaxSpeedOffroad)
	}
}

func TestPlanOffroadSpeedPenalisesWheels(t *testing.T) {
	wheeled := components.VehicleSpec{MaxSpeedOffroad: 10, Locomotion: components.LocomotionWheeled}
	tracked := components.VehicleSpec{MaxSpeedOffroad: 10, Locomotion: components.LocomotionTracked}
	w, tr := planOffroadSpeed(&wheeled), planOffroadSpeed(&tracked)
	if w >= tr {
		t.Fatalf("wheeled planning speed %.2f is not below tracked %.2f", w, tr)
	}
	if tr > tracked.MaxSpeedOffroad {
		t.Fatalf("planning speed %.2f exceeds the spec %.2f", tr, tracked.MaxSpeedOffroad)
	}
}

func TestGraphBuildsSymmetricAdjacency(t *testing.T) {
	r := routerOn(t, straightHighway())
	if r.Graph() == nil {
		t.Fatal("graph not built")
	}
	if got := r.EdgeBetween(0, 1); got != 0 {
		t.Errorf("EdgeBetween(0,1) = %d, want 0", got)
	}
	if got := r.EdgeBetween(1, 0); got != 0 {
		t.Errorf("adjacency is not symmetric: EdgeBetween(1,0) = %d", got)
	}
	if got := r.EdgeBetween(0, 3); got != -1 {
		t.Errorf("unconnected nodes returned edge %d", got)
	}
	if got := r.EdgeBetween(99, 0); got != -1 {
		t.Errorf("out-of-range node returned edge %d", got)
	}
}

func TestGraphRejectsEmptyGraphs(t *testing.T) {
	if r := routerOn(t, &components.RoadGraph{}); r.Graph() != nil {
		t.Error("an empty graph must not build")
	}
	nodesOnly := &components.RoadGraph{Nodes: []components.RoadNode{roadNode(0, 0)}}
	if r := routerOn(t, nodesOnly); r.Graph() != nil {
		t.Error("a graph with no edges must not build")
	}
}

func TestEdgeGeometryAccessors(t *testing.T) {
	r := routerOn(t, straightHighway())
	r.Graph()
	if got := r.EdgeLen(0); math.Abs(float64(got-100)) > 1e-3 {
		t.Errorf("EdgeLen = %.3f, want 100", got)
	}
	x, z := r.EdgePoint(0, 0.25)
	if math.Abs(float64(x-25)) > 1e-3 || math.Abs(float64(z)) > 1e-3 {
		t.Errorf("EdgePoint(0.25) = (%.3f, %.3f), want (25, 0)", x, z)
	}
	dx, dz := r.EdgeDir(0)
	if math.Abs(float64(dx-1)) > 1e-3 || math.Abs(float64(dz)) > 1e-3 {
		t.Errorf("EdgeDir = (%.3f, %.3f), want (1, 0)", dx, dz)
	}
}

func TestEdgeDirOfADegenerateEdgeIsZero(t *testing.T) {
	g := &components.RoadGraph{
		Nodes: []components.RoadNode{roadNode(5, 5), roadNode(5, 5)},
		Edges: []components.RoadEdge{{From: 0, To: 1, Kind: components.RoadLocal, Width: 4}},
	}
	r := routerOn(t, g)
	r.Graph()
	if dx, dz := r.EdgeDir(0); dx != 0 || dz != 0 {
		t.Fatalf("EdgeDir = (%.3f, %.3f), want (0, 0)", dx, dz)
	}
}

func TestNearestEdgeProjectsOntoTheStrip(t *testing.T) {
	r := routerOn(t, straightHighway())
	ei, tt, d := r.NearestEdge(150, 20)
	if ei != 1 {
		t.Fatalf("nearest edge = %d, want 1", ei)
	}
	if math.Abs(float64(tt-0.5)) > 1e-3 {
		t.Errorf("t = %.3f, want 0.5", tt)
	}
	if math.Abs(float64(d-20)) > 1e-3 {
		t.Errorf("distance = %.3f, want 20", d)
	}
}

func TestNearestEdgeClampsBeyondTheEnds(t *testing.T) {
	r := routerOn(t, straightHighway())
	ei, tt, d := r.NearestEdge(-50, 0)
	if ei != 0 || tt != 0 {
		t.Fatalf("before the start: edge %d t %.3f, want edge 0 t 0", ei, tt)
	}
	if math.Abs(float64(d-50)) > 1e-3 {
		t.Errorf("distance = %.3f, want 50", d)
	}
	_, tt, _ = r.NearestEdge(400, 0)
	if tt != 1 {
		t.Errorf("past the end: t = %.3f, want 1", tt)
	}
}

func TestNearestEdgeOnAnEmptyGraph(t *testing.T) {
	r := routerOn(t, &components.RoadGraph{})
	if ei, _, _ := r.NearestEdge(0, 0); ei != -1 {
		t.Fatalf("edge = %d, want -1", ei)
	}
}

func TestPlanRouteTakesTheRoadWhenItIsFaster(t *testing.T) {
	r := routerOn(t, straightHighway())
	spec := truckSpec()
	// Start and goal both sit beside the highway, 300 m apart along it.
	plan, ok := r.PlanRoute(5, 6, 295, 6, spec)
	if !ok {
		t.Fatal("the router refused a road that runs straight to the goal")
	}
	if plan.EntryEdge < 0 && len(plan.Nodes) == 0 {
		t.Fatalf("plan has neither ramp nor nodes: %+v", plan)
	}
}

func TestPlanRouteRefusesADetour(t *testing.T) {
	r := routerOn(t, straightHighway())
	// Goal is perpendicular to the road and close: driving to the highway and
	// back cannot beat 20 m of open ground.
	if _, ok := r.PlanRoute(0, 200, 0, 220, truckSpec()); ok {
		t.Fatal("the router routed a 20 m hop onto a highway 200 m away")
	}
}

func TestPlanRouteEntersMidEdgeNotOnlyAtNodes(t *testing.T) {
	r := routerOn(t, straightHighway())
	// Beside the middle of edge 1, far from either of its end nodes.
	plan, ok := r.PlanRoute(150, 4, 295, 4, truckSpec())
	if !ok {
		t.Fatal("no plan")
	}
	if plan.EntryEdge < 0 {
		t.Fatalf("entered at a node instead of the projection beside it: %+v", plan)
	}
	if plan.EntryT <= 0 || plan.EntryT >= 1 {
		t.Errorf("entry t = %.3f, expected a mid-edge projection", plan.EntryT)
	}
}

func TestPlanRouteSameEdgeShortcutHasNoNodes(t *testing.T) {
	r := routerOn(t, straightHighway())
	plan, ok := r.PlanRoute(110, 3, 190, 3, truckSpec())
	if !ok {
		t.Fatal("no plan along a single edge")
	}
	if len(plan.Nodes) != 0 {
		t.Fatalf("same-edge itinerary should carry no node chain, got %v", plan.Nodes)
	}
	if plan.EntryEdge != plan.ExitEdge {
		t.Fatalf("entry edge %d, exit edge %d", plan.EntryEdge, plan.ExitEdge)
	}
}

func TestPlanRouteChainIsConnected(t *testing.T) {
	r := routerOn(t, straightHighway())
	plan, ok := r.PlanRoute(5, 6, 295, 6, truckSpec())
	if !ok {
		t.Fatal("no plan")
	}
	for i := 1; i < len(plan.Nodes); i++ {
		if r.EdgeBetween(plan.Nodes[i-1], plan.Nodes[i]) < 0 {
			t.Fatalf("nodes %d and %d are not adjacent", plan.Nodes[i-1], plan.Nodes[i])
		}
	}
}

func TestPlanRouteWillNotRampOntoABridge(t *testing.T) {
	g := straightHighway()
	g.Edges[1].Kind = components.RoadBridge
	r := routerOn(t, g)
	// Beside the middle of the span: a bridge may only be joined at its ends.
	plan, ok := r.PlanRoute(150, 4, 295, 4, truckSpec())
	if ok && plan.EntryEdge == 1 {
		t.Fatalf("ramped onto a bridge mid-span: %+v", plan)
	}
	if ok && plan.ExitEdge == 1 {
		t.Fatalf("left a bridge mid-span: %+v", plan)
	}
}

func TestPlanRouteIsDeterministic(t *testing.T) {
	r := routerOn(t, straightHighway())
	spec := truckSpec()
	first, ok1 := r.PlanRoute(5, 6, 295, 6, spec)
	second, ok2 := r.PlanRoute(5, 6, 295, 6, spec)
	if ok1 != ok2 || first.EntryEdge != second.EntryEdge || first.ExitEdge != second.ExitEdge ||
		len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("two identical calls disagreed: %+v vs %+v", first, second)
	}
	for i := range first.Nodes {
		if first.Nodes[i] != second.Nodes[i] {
			t.Fatalf("node chain differs at %d", i)
		}
	}
}

// Tracks plan off-road at a smaller penalty, so a road has to be worth more
// to a tank than to a truck.
func TestPlanRouteRoadPreferenceIsClassDependent(t *testing.T) {
	truckOff := planOffroadSpeed(truckSpec())
	tankOff := planOffroadSpeed(tankSpec())
	if truckSpec().Locomotion == tankSpec().Locomotion {
		t.Skip("truck and tank share a locomotion in this spec table")
	}
	if truckOff/truckSpec().MaxSpeedOffroad >= tankOff/tankSpec().MaxSpeedOffroad {
		t.Fatalf("wheels are not penalised harder: truck %.2f, tank %.2f",
			truckOff/truckSpec().MaxSpeedOffroad, tankOff/tankSpec().MaxSpeedOffroad)
	}
}

func TestResizeScratchKeepsCapacityAndLength(t *testing.T) {
	var s []float32
	s = resizeScratch(s, 4)
	if len(s) != 4 {
		t.Fatalf("len = %d, want 4", len(s))
	}
	for i := range s {
		s[i] = float32(i)
	}
	grown := resizeScratch(s, 8)
	if len(grown) != 8 {
		t.Fatalf("grown len = %d, want 8", len(grown))
	}
	shrunk := resizeScratch(grown, 2)
	if len(shrunk) != 2 {
		t.Fatalf("shrunk len = %d, want 2", len(shrunk))
	}
	if cap(shrunk) < 8 {
		t.Errorf("shrinking reallocated: cap %d", cap(shrunk))
	}
}

func TestDist2D(t *testing.T) {
	if got := dist2D(0, 0, 3, 4); math.Abs(float64(got-5)) > 1e-4 {
		t.Errorf("dist2D = %.4f, want 5", got)
	}
	if got := dist2D(-1, -1, -1, -1); got != 0 {
		t.Errorf("dist2D of a point to itself = %.4f", got)
	}
}
