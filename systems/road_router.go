package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// RoadRouter plans road itineraries over the RoadGraph (Phase 19 M2 +
// edge-ramps ISSUES #16). Edge cost is travel time; off-road legs are costed
// at a locomotion-dependent PLANNING speed below the spec value (terrain
// roughness — and the P5 hard road preference for wheels), so class data
// still decides: a truck detours far for asphalt, a tank cuts corners.
// On/off-ramps are the PROJECTIONS of start/goal onto edges (virtual entry /
// exit points), not just graph nodes — driving parallel to a road 5 m away
// enters it immediately. Bridges are enterable only via their end nodes.
// Adjacency is built lazily once — the graph is static after
// PreprocessRoadGraph.
type RoadRouter struct {
	graphRes ecs.Resource[components.RoadGraph]
	built    bool
	adj      [][]roadAdj

	dist    []float32
	prev    []int32
	done    []bool
	entryE  []int32
	entryT  []float32
	edgeGeo []edgeGeom
}

type roadAdj struct {
	to   uint16
	edge int32
	len  float32
}

type edgeGeom struct {
	ax, az, bx, bz float32
	len            float32
}

// RoadPlan is PlanRoute's result. Nodes may be empty (same-edge itinerary:
// entry point → exit point along one edge). EntryEdge/ExitEdge -1 = the
// corresponding ramp is a node, not a mid-edge point.
type RoadPlan struct {
	Nodes     []uint16
	EntryEdge int32
	EntryT    float32
	ExitEdge  int32
	ExitT     float32
}

func NewRoadRouter(w *ecs.World) *RoadRouter {
	return &RoadRouter{graphRes: ecs.NewResource[components.RoadGraph](w)}
}

const roadDirtSpeedMul float32 = 0.8

// Planning-only off-road speed multipliers (P5 hard preference): wheels
// crawl off-road in practice (obstacles, ruts), tracks cut corners readily.
// Actual drive speed stays at the spec value.
const (
	planOffroadMulWheeled float32 = 0.6
	planOffroadMulTracked float32 = 0.9
)

func roadSpeedForEdge(kind components.RoadKind, spec *components.VehicleSpec) float32 {
	sp := spec.MaxSpeedRoad
	if kind == components.RoadDirtTrack {
		sp *= roadDirtSpeedMul
	}
	if sp < spec.MaxSpeedOffroad {
		sp = spec.MaxSpeedOffroad
	}
	return sp
}

func planOffroadSpeed(spec *components.VehicleSpec) float32 {
	mul := planOffroadMulTracked
	if spec.Locomotion == components.LocomotionWheeled {
		mul = planOffroadMulWheeled
	}
	return spec.MaxSpeedOffroad * mul
}

// Graph returns the road graph, building adjacency + edge geometry on first
// call.
func (r *RoadRouter) Graph() *components.RoadGraph {
	g := r.graphRes.Get()
	if g == nil || len(g.Edges) == 0 || len(g.Nodes) == 0 {
		return nil
	}
	if !r.built {
		r.adj = make([][]roadAdj, len(g.Nodes))
		r.edgeGeo = make([]edgeGeom, len(g.Edges))
		for i := range g.Edges {
			e := &g.Edges[i]
			ax, az := worldXZ(g.Nodes[e.From].Pos)
			bx, bz := worldXZ(g.Nodes[e.To].Pos)
			l := dist2D(ax, az, bx, bz)
			r.adj[e.From] = append(r.adj[e.From], roadAdj{to: e.To, edge: int32(i), len: l})
			r.adj[e.To] = append(r.adj[e.To], roadAdj{to: e.From, edge: int32(i), len: l})
			r.edgeGeo[i] = edgeGeom{ax: ax, az: az, bx: bx, bz: bz, len: l}
		}
		r.built = true
	}
	return g
}

// EdgeBetween returns the edge index connecting nodes a and b, -1 if none.
func (r *RoadRouter) EdgeBetween(a, b uint16) int32 {
	if int(a) >= len(r.adj) {
		return -1
	}
	for _, ad := range r.adj[a] {
		if ad.to == b {
			return ad.edge
		}
	}
	return -1
}

// EdgePoint returns the world XZ of param t along edge ei.
func (r *RoadRouter) EdgePoint(ei int32, t float32) (float32, float32) {
	geo := &r.edgeGeo[ei]
	return geo.ax + t*(geo.bx-geo.ax), geo.az + t*(geo.bz-geo.az)
}

// EdgeLen returns edge ei's length in metres.
func (r *RoadRouter) EdgeLen(ei int32) float32 {
	return r.edgeGeo[ei].len
}

// projOnEdge projects (px,pz) onto edge ei; returns param t (clamped) and
// perpendicular distance.
func (r *RoadRouter) projOnEdge(ei int, px, pz float32) (float32, float32) {
	geo := &r.edgeGeo[ei]
	if geo.len <= 0 {
		return 0, dist2D(px, pz, geo.ax, geo.az)
	}
	t := ((px-geo.ax)*(geo.bx-geo.ax) + (pz-geo.az)*(geo.bz-geo.az)) / (geo.len * geo.len)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := geo.ax + t*(geo.bx-geo.ax)
	cz := geo.az + t*(geo.bz-geo.az)
	return t, dist2D(px, pz, cx, cz)
}

// PlanRoute picks the fastest start→goal itinerary: straight off-road vs
// ramp (node or mid-edge projection) + road travel + off-ramp. Returns
// (plan, true) only when the road wins. Deterministic: linear-scan Dijkstra,
// ties break on the lower index.
func (r *RoadRouter) PlanRoute(sx, sz, gx, gz float32, spec *components.VehicleSpec) (RoadPlan, bool) {
	g := r.Graph()
	if g == nil {
		return RoadPlan{}, false
	}
	off := planOffroadSpeed(spec)
	if off <= 0 {
		return RoadPlan{}, false
	}
	n := len(g.Nodes)
	direct := dist2D(sx, sz, gx, gz) / off

	// Same-edge shortcut: both projections on one non-bridge edge.
	bestSame := direct
	sameEdge := int32(-1)
	var sameTs, sameTg float32
	for i := range g.Edges {
		if g.Edges[i].Kind == components.RoadBridge || r.edgeGeo[i].len <= 0 {
			continue
		}
		ts, ds := r.projOnEdge(i, sx, sz)
		tg, dg := r.projOnEdge(i, gx, gz)
		dt := ts - tg
		if dt < 0 {
			dt = -dt
		}
		total := ds/off + dt*r.edgeGeo[i].len/roadSpeedForEdge(g.Edges[i].Kind, spec) + dg/off
		if total < bestSame {
			bestSame = total
			sameEdge = int32(i)
			sameTs, sameTg = ts, tg
		}
	}

	// Dijkstra init: per node, cheapest of direct off-road vs mid-edge entry
	// ramp + along-edge to that node.
	r.dist = resizeScratch(r.dist, n)
	r.prev = resizeScratch(r.prev, n)
	r.done = resizeScratch(r.done, n)
	r.entryE = resizeScratch(r.entryE, n)
	r.entryT = resizeScratch(r.entryT, n)
	for i := 0; i < n; i++ {
		nx, nz := worldXZ(g.Nodes[i].Pos)
		r.dist[i] = dist2D(sx, sz, nx, nz) / off
		r.prev[i] = -1
		r.done[i] = false
		r.entryE[i] = -1
		r.entryT[i] = 0
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind == components.RoadBridge || r.edgeGeo[i].len <= 0 {
			continue
		}
		ts, ds := r.projOnEdge(i, sx, sz)
		spd := roadSpeedForEdge(e.Kind, spec)
		if c := ds/off + ts*r.edgeGeo[i].len/spd; c < r.dist[e.From] {
			r.dist[e.From] = c
			r.entryE[e.From] = int32(i)
			r.entryT[e.From] = ts
		}
		if c := ds/off + (1-ts)*r.edgeGeo[i].len/spd; c < r.dist[e.To] {
			r.dist[e.To] = c
			r.entryE[e.To] = int32(i)
			r.entryT[e.To] = ts
		}
	}
	for {
		best := -1
		bestD := float32(math.MaxFloat32)
		for i := 0; i < n; i++ {
			if !r.done[i] && r.dist[i] < bestD {
				bestD = r.dist[i]
				best = i
			}
		}
		if best < 0 {
			break
		}
		r.done[best] = true
		for _, ad := range r.adj[best] {
			t := bestD + ad.len/roadSpeedForEdge(g.Edges[ad.edge].Kind, spec)
			if t < r.dist[ad.to] {
				r.dist[ad.to] = t
				r.prev[ad.to] = int32(best)
				// Entry ramp is inherited through prev-chain reconstruction.
			}
		}
	}

	// Exit options: off-road from a node, or along an edge to the goal's
	// projection then off-road.
	bestTotal := direct
	exitNode := -1
	exitEdge := int32(-1)
	exitT := float32(0)
	for i := 0; i < n; i++ {
		nx, nz := worldXZ(g.Nodes[i].Pos)
		if total := r.dist[i] + dist2D(nx, nz, gx, gz)/off; total < bestTotal {
			bestTotal = total
			exitNode = i
			exitEdge = -1
		}
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind == components.RoadBridge || r.edgeGeo[i].len <= 0 {
			continue
		}
		tg, dg := r.projOnEdge(i, gx, gz)
		spd := roadSpeedForEdge(e.Kind, spec)
		if total := r.dist[e.From] + tg*r.edgeGeo[i].len/spd + dg/off; total < bestTotal {
			bestTotal = total
			exitNode = int(e.From)
			exitEdge = int32(i)
			exitT = tg
		}
		if total := r.dist[e.To] + (1-tg)*r.edgeGeo[i].len/spd + dg/off; total < bestTotal {
			bestTotal = total
			exitNode = int(e.To)
			exitEdge = int32(i)
			exitT = tg
		}
	}

	if sameEdge >= 0 && bestSame <= bestTotal {
		return RoadPlan{
			Nodes:     nil,
			EntryEdge: sameEdge, EntryT: sameTs,
			ExitEdge: sameEdge, ExitT: sameTg,
		}, true
	}
	if exitNode < 0 {
		return RoadPlan{}, false
	}
	var rev []uint16
	for at := int32(exitNode); at >= 0; at = r.prev[at] {
		if len(rev) >= 64 {
			return RoadPlan{}, false
		}
		rev = append(rev, uint16(at))
	}
	nodes := make([]uint16, len(rev))
	for i := range rev {
		nodes[i] = rev[len(rev)-1-i]
	}
	return RoadPlan{
		Nodes:     nodes,
		EntryEdge: r.entryE[nodes[0]], EntryT: r.entryT[nodes[0]],
		ExitEdge: exitEdge, ExitT: exitT,
	}, true
}

func dist2D(ax, az, bx, bz float32) float32 {
	dx := bx - ax
	dz := bz - az
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

func resizeScratch[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
