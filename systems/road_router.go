package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// RoadRouter plans node routes over the RoadGraph (Phase 19 M2). Edge cost is
// travel time, so class speeds encode road preference on their own: a truck
// (road 16 / offroad 6) detours far for asphalt, a tank (17/11) cuts corners.
// Adjacency is built lazily once — the graph is static after
// PreprocessRoadGraph.
type RoadRouter struct {
	graphRes ecs.Resource[components.RoadGraph]
	built    bool
	adj      [][]roadAdj

	dist []float32
	prev []int32
	done []bool
}

type roadAdj struct {
	to   uint16
	edge int32
	len  float32
}

func NewRoadRouter(w *ecs.World) *RoadRouter {
	return &RoadRouter{graphRes: ecs.NewResource[components.RoadGraph](w)}
}

const roadDirtSpeedMul float32 = 0.8

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

// Graph returns the road graph, building adjacency on first call.
func (r *RoadRouter) Graph() *components.RoadGraph {
	g := r.graphRes.Get()
	if g == nil || len(g.Edges) == 0 || len(g.Nodes) == 0 {
		return nil
	}
	if !r.built {
		r.adj = make([][]roadAdj, len(g.Nodes))
		for i := range g.Edges {
			e := &g.Edges[i]
			ax, az := worldXZ(g.Nodes[e.From].Pos)
			bx, bz := worldXZ(g.Nodes[e.To].Pos)
			l := dist2D(ax, az, bx, bz)
			r.adj[e.From] = append(r.adj[e.From], roadAdj{to: e.To, edge: int32(i), len: l})
			r.adj[e.To] = append(r.adj[e.To], roadAdj{to: e.From, edge: int32(i), len: l})
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

// PlanRoute picks the fastest start→goal option: straight off-road vs
// off-road ramp to an entry node + road edges + off-road leg from the exit
// node ("on/off-ramp к ближайшему узлу" by time, not distance). Returns nil
// when driving straight wins, when no graph exists, or when the best "route"
// uses no road edge. Deterministic: linear-scan Dijkstra, ties break on the
// lower node index.
func (r *RoadRouter) PlanRoute(sx, sz, gx, gz float32, spec *components.VehicleSpec) []uint16 {
	g := r.Graph()
	if g == nil {
		return nil
	}
	off := spec.MaxSpeedOffroad
	if off <= 0 {
		return nil
	}
	n := len(g.Nodes)
	r.dist = resizeScratch(r.dist, n)
	r.prev = resizeScratch(r.prev, n)
	r.done = resizeScratch(r.done, n)
	for i := 0; i < n; i++ {
		nx, nz := worldXZ(g.Nodes[i].Pos)
		r.dist[i] = dist2D(sx, sz, nx, nz) / off
		r.prev[i] = -1
		r.done[i] = false
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
			}
		}
	}

	bestExit := -1
	bestTotal := dist2D(sx, sz, gx, gz) / off
	for i := 0; i < n; i++ {
		nx, nz := worldXZ(g.Nodes[i].Pos)
		total := r.dist[i] + dist2D(nx, nz, gx, gz)/off
		if total < bestTotal {
			bestTotal = total
			bestExit = i
		}
	}
	if bestExit < 0 || r.prev[bestExit] < 0 {
		return nil
	}
	var rev []uint16
	for at := int32(bestExit); at >= 0; at = r.prev[at] {
		if len(rev) >= 64 {
			return nil
		}
		rev = append(rev, uint16(at))
	}
	out := make([]uint16, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
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
