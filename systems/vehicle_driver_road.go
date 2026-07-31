package systems

import (
	"fmt"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

const (
	vehRampArrivalRadius float32 = 2.5
	vehNodePopBase       float32 = 3.0
	// Sticky-to-edge: lateral drift beyond halfWidth+this aims the hull back
	// at a point this far ahead along the edge instead of the far node.
	vehRejoinSlack float32 = 1.0
	vehRejoinAhead float32 = 6.0
)

// rampPopRadius scales the waypoint pop with the hull's turning circle: a
// truck (R=8) physically cannot hit a 2.5 m point off-arc and orbits it
// forever. Popping intermediate ramp/node points early is free — the next
// leg re-aims.
func rampPopRadius(base float32, spec *components.VehicleSpec) float32 {
	if r := 0.75 * spec.TurnRadiusM; r > base {
		return r
	}
	return base
}

func clearRoute(route *components.RoadRoute, follower *components.RoadFollower) {
	if route != nil {
		route.Count = 0
		route.Head = 0
		route.Planned = 0
		route.Phase = 0
		route.EntryEdge = -1
		route.ExitEdge = -1
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
	route.Phase = 3
	route.EntryEdge = -1
	route.ExitEdge = -1
	sx, sz := worldXZ(*pos)
	gx, gz := worldXZ(target)
	plan, ok := sys.router.PlanRoute(sx, sz, gx, gz, spec)
	if !ok || len(plan.Nodes) > len(route.Nodes) {
		return
	}
	for i, nid := range plan.Nodes {
		route.Nodes[i] = nid
	}
	route.Count = uint8(len(plan.Nodes))
	route.EntryEdge = plan.EntryEdge
	route.EntryT = plan.EntryT
	route.ExitEdge = plan.ExitEdge
	route.ExitT = plan.ExitT
	route.Phase = 0
	if debugLog {
		fmt.Printf("[route] start=(%.1f,%.1f) goal=(%.1f,%.1f) nodes=%v entry=(%d,%.2f) exit=(%d,%.2f)\n",
			sx, sz, gx, gz, plan.Nodes, plan.EntryEdge, plan.EntryT, plan.ExitEdge, plan.ExitT)
	}
}

// stepRoute advances the itinerary phases: off-road to the entry ramp →
// node chain edge by edge → along ExitEdge to the exit ramp. Writes
// RoadFollower{Edge, T} whenever the hull rides a known edge — GroundStick
// reads it for bridge-deck Y. Phase 3 hands control back to the direct leg.
func (sys *VehicleDriverSystem) stepRoute(ent ecs.Entity, pos *components.WorldPos,
	mot *components.Motion, spec *components.VehicleSpec, route *components.RoadRoute,
	follower *components.RoadFollower, dt float32) {
	g := sys.router.Graph()
	if g == nil {
		route.Phase = 3
		if follower != nil {
			follower.Edge = -1
		}
		return
	}
	px, pz := worldXZ(*pos)

	if route.Phase == 0 {
		if route.EntryEdge < 0 || int(route.EntryEdge) >= len(g.Edges) {
			route.Phase = 1
		} else {
			ex, ez := sys.router.EdgePoint(route.EntryEdge, route.EntryT)
			d := dist2D(px, pz, ex, ez)
			if d < rampPopRadius(vehRampArrivalRadius, spec) {
				route.Phase = 1
			} else {
				if follower != nil {
					follower.Edge = -1
				}
				cruise := spec.MaxSpeedOffroad * sys.slopeMul(pos, mot.Yaw)
				sys.drive(ent, follower, pos, mot, spec, ex-px, ez-pz, d, cruise, true, false, dt)
				return
			}
		}
	}

	for route.Phase == 1 {
		if route.Head >= route.Count {
			route.Phase = 2
			break
		}
		node := int(route.Nodes[route.Head])
		if node >= len(g.Nodes) {
			route.Phase = 3
			if follower != nil {
				follower.Edge = -1
			}
			return
		}
		tx, tz := worldXZ(g.Nodes[node].Pos)
		ei := int32(-1)
		if route.Head > 0 {
			prev := int(route.Nodes[route.Head-1])
			if prev < len(g.Nodes) {
				ei = sys.router.EdgeBetween(uint16(prev), uint16(node))
			}
		} else if route.EntryEdge >= 0 {
			ei = route.EntryEdge
		}
		aimX, aimZ := tx, tz
		popR := rampPopRadius(vehRampArrivalRadius, spec)
		cruise := spec.MaxSpeedOffroad
		if ei >= 0 && int(ei) < len(g.Edges) {
			e := &g.Edges[ei]
			cruise = roadSpeedForEdge(e.Kind, spec)
			popR = rampPopRadius(vehNodePopBase+e.Width*0.5, spec)
			t, lat := sys.router.projOnEdge(int(ei), px, pz)
			if follower != nil {
				follower.Edge = ei
				follower.T = t
			}
			if elen := sys.router.EdgeLen(ei); lat > e.Width*0.5+vehRejoinSlack && elen > 0 {
				// Rejoin aims ahead ALONG THE TRAVEL DIRECTION: toward From
				// means decreasing T. A +T-blind rejoin point runs away up
				// the edge and drags the hull to the wrong end.
				dir := float32(1)
				if int(e.From) == node {
					dir = -1
				}
				at := t + dir*vehRejoinAhead/elen
				if at > 1 {
					at = 1
				} else if at < 0 {
					at = 0
				}
				aimX, aimZ = sys.router.EdgePoint(ei, at)
			}
		} else if follower != nil {
			follower.Edge = -1
		}
		d := dist2D(px, pz, tx, tz)
		if d < popR {
			route.Head++
			continue
		}
		cruise *= sys.slopeMul(pos, mot.Yaw)
		allowRev := route.Head == 0 && route.EntryEdge < 0
		sys.drive(ent, follower, pos, mot, spec, aimX-px, aimZ-pz, d, cruise, allowRev, false, dt)
		return
	}

	if route.Phase == 2 {
		if route.ExitEdge < 0 || int(route.ExitEdge) >= len(g.Edges) {
			route.Phase = 3
			if follower != nil {
				follower.Edge = -1
			}
			return
		}
		e := &g.Edges[route.ExitEdge]
		ex, ez := sys.router.EdgePoint(route.ExitEdge, route.ExitT)
		d := dist2D(px, pz, ex, ez)
		if d < rampPopRadius(vehRampArrivalRadius, spec) {
			route.Phase = 3
			if follower != nil {
				follower.Edge = -1
			}
			return
		}
		t, _ := sys.router.projOnEdge(int(route.ExitEdge), px, pz)
		if follower != nil {
			follower.Edge = route.ExitEdge
			follower.T = t
		}
		cruise := roadSpeedForEdge(e.Kind, spec) * sys.slopeMul(pos, mot.Yaw)
		sys.drive(ent, follower, pos, mot, spec, ex-px, ez-pz, d, cruise, false, false, dt)
	}
}
