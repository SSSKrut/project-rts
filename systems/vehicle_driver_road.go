package systems

import (
	"rts-go/components"
)

const (
	// Node pop radii: the on-ramp entry needs a tight approach, edge nodes
	// pop early (base + half road width) so the arc rounds the corner.
	vehRampArrivalRadius float32 = 2.5
	vehNodePopBase       float32 = 3.0
	// Sticky-to-edge: lateral drift beyond halfWidth+this aims the hull back
	// at a point this far ahead along the edge instead of the far node.
	vehRejoinSlack float32 = 1.0
	vehRejoinAhead float32 = 6.0
)

func clearRoute(route *components.RoadRoute, follower *components.RoadFollower) {
	if route != nil {
		route.Count = 0
		route.Head = 0
		route.Planned = 0
	}
	if follower != nil {
		follower.Edge = -1
	}
}

func (sys *VehicleDriverSystem) planRoute(pos *components.WorldPos,
	target components.WorldPos, spec *components.VehicleSpec, route *components.RoadRoute) {
	route.Planned = 1
	route.Goal = target
	route.Count = 0
	route.Head = 0
	sx, sz := worldXZ(*pos)
	gx, gz := worldXZ(target)
	nodes := sys.router.PlanRoute(sx, sz, gx, gz, spec)
	if len(nodes) < 2 || len(nodes) > len(route.Nodes) {
		return
	}
	for i, nid := range nodes {
		route.Nodes[i] = nid
	}
	route.Count = uint8(len(nodes))
}

// stepRoute drives the current route leg: off-road to the entry node
// (Head == 0, reverse allowed), then edge to edge at road speed. Writes
// RoadFollower{Edge, T} while on an edge — GroundStick reads it for
// bridge-deck Y.
func (sys *VehicleDriverSystem) stepRoute(pos *components.WorldPos, mot *components.Motion,
	spec *components.VehicleSpec, route *components.RoadRoute,
	follower *components.RoadFollower, dt float32) {
	g := sys.router.Graph()
	node := int(route.Nodes[route.Head])
	if g == nil || node >= len(g.Nodes) {
		clearRoute(route, follower)
		return
	}
	px, pz := worldXZ(*pos)
	tx, tz := worldXZ(g.Nodes[node].Pos)
	aimX, aimZ := tx, tz

	cruise := spec.MaxSpeedOffroad
	popR := vehRampArrivalRadius
	onEdge := false
	if route.Head > 0 {
		prev := int(route.Nodes[route.Head-1])
		if prev < len(g.Nodes) {
			if ei := sys.router.EdgeBetween(uint16(prev), uint16(node)); ei >= 0 {
				e := &g.Edges[ei]
				cruise = roadSpeedForEdge(e.Kind, spec)
				popR = vehNodePopBase + e.Width*0.5
				onEdge = true
				ax, az := worldXZ(g.Nodes[prev].Pos)
				segLen := dist2D(ax, az, tx, tz)
				if segLen > 0 {
					t := ((px-ax)*(tx-ax) + (pz-az)*(tz-az)) / (segLen * segLen)
					if t < 0 {
						t = 0
					} else if t > 1 {
						t = 1
					}
					if follower != nil {
						follower.Edge = ei
						follower.T = t
					}
					cx := ax + t*(tx-ax)
					cz := az + t*(tz-az)
					if dist2D(px, pz, cx, cz) > e.Width*0.5+vehRejoinSlack {
						at := t + vehRejoinAhead/segLen
						if at > 1 {
							at = 1
						}
						aimX = ax + at*(tx-ax)
						aimZ = az + at*(tz-az)
					}
				}
			}
		}
	}
	if !onEdge && follower != nil {
		follower.Edge = -1
	}

	dist := dist2D(px, pz, tx, tz)
	if dist < popR {
		route.Head++
		if route.Head >= route.Count && follower != nil {
			follower.Edge = -1
		}
		return
	}
	cruise *= sys.slopeMul(pos, mot.Yaw)
	sys.drive(pos, mot, spec, aimX-px, aimZ-pz, dist, cruise, route.Head == 0, false, dt)
}
