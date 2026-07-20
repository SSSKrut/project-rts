package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// Vehicle route preview: committed itineraries draw for every
// selected soloist vehicle in its squad colour; a translucent hypothetical
// line from the first selected vehicle follows the cursor so the player sees
// road-vs-straight BEFORE ordering. Lines hug terrain via 8 m subdivision.
const (
	routeLineStep float32 = 8
	routeLineLift float32 = 0.35
)

type routePreviewCtx struct {
	world      *ecs.World
	router     *systems.RoadRouter
	vehicleMap *ecs.Map[components.Vehicle]
	routeMap   *ecs.Map[components.RoadRoute]
	posMap     *ecs.Map[components.WorldPos]
	memberMap  *ecs.Map[components.SquadMember]
	squadColor func(ent ecs.Entity) rl.Color
}

func wpWorldXZ(p components.WorldPos) (float32, float32) {
	return float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
		float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z
}

func drawVehicleRoutes(ctx *routePreviewCtx, selected []ecs.Entity,
	hoverOK bool, hoverTarget components.WorldPos) {
	if ctx == nil {
		return
	}
	g := ctx.router.Graph()
	hoverDrawn := false
	for _, e := range selected {
		if e == (ecs.Entity{}) || !ctx.world.Alive(e) {
			continue
		}
		veh := ctx.vehicleMap.Get(e)
		if veh == nil {
			continue
		}
		if sm := ctx.memberMap.Get(e); sm != nil && sm.Squad != (ecs.Entity{}) {
			continue
		}
		pos := ctx.posMap.Get(e)
		if pos == nil {
			continue
		}
		px, pz := wpWorldXZ(*pos)

		if rt := ctx.routeMap.Get(e); rt != nil && rt.Planned == 1 {
			col := rl.Color{R: 90, G: 200, B: 255, A: 200}
			if ctx.squadColor != nil {
				c := ctx.squadColor(e)
				col = rl.Color{R: c.R, G: c.G, B: c.B, A: 210}
			}
			pts := committedRoutePoints(ctx.router, g, rt, px, pz)
			drawRoutePolyline(pts, col)
			if n := len(pts); n > 0 {
				drawRouteGoalMarker(pts[n-1], col)
			}
		}

		if hoverOK && !hoverDrawn {
			hoverDrawn = true
			gx, gz := wpWorldXZ(hoverTarget)
			pts := [][2]float32{{px, pz}}
			if plan, ok := ctx.router.PlanRoute(px, pz, gx, gz,
				components.SpecForVehicle(veh.Kind)); ok && g != nil {
				if plan.EntryEdge >= 0 {
					x, z := ctx.router.EdgePoint(plan.EntryEdge, plan.EntryT)
					pts = append(pts, [2]float32{x, z})
				}
				for _, nid := range plan.Nodes {
					if int(nid) < len(g.Nodes) {
						x, z := wpWorldXZ(g.Nodes[nid].Pos)
						pts = append(pts, [2]float32{x, z})
					}
				}
				if plan.ExitEdge >= 0 {
					x, z := ctx.router.EdgePoint(plan.ExitEdge, plan.ExitT)
					pts = append(pts, [2]float32{x, z})
				}
			}
			pts = append(pts, [2]float32{gx, gz})
			drawRoutePolyline(pts, rl.Color{R: 230, G: 230, B: 240, A: 110})
		}
	}
}

func committedRoutePoints(router *systems.RoadRouter, g *components.RoadGraph,
	rt *components.RoadRoute, px, pz float32) [][2]float32 {
	pts := [][2]float32{{px, pz}}
	if g != nil {
		if rt.Phase == 0 && rt.EntryEdge >= 0 && int(rt.EntryEdge) < len(g.Edges) {
			x, z := router.EdgePoint(rt.EntryEdge, rt.EntryT)
			pts = append(pts, [2]float32{x, z})
		}
		if rt.Phase <= 1 {
			for i := rt.Head; i < rt.Count; i++ {
				if int(rt.Nodes[i]) < len(g.Nodes) {
					x, z := wpWorldXZ(g.Nodes[rt.Nodes[i]].Pos)
					pts = append(pts, [2]float32{x, z})
				}
			}
		}
		if rt.Phase <= 2 && rt.ExitEdge >= 0 && int(rt.ExitEdge) < len(g.Edges) {
			x, z := router.EdgePoint(rt.ExitEdge, rt.ExitT)
			pts = append(pts, [2]float32{x, z})
		}
	}
	gx, gz := wpWorldXZ(rt.Goal)
	return append(pts, [2]float32{gx, gz})
}

func routeRenderPoint(wx, wz float32) rl.Vector3 {
	origin := systems.CurrentOriginChunk
	return rl.Vector3{
		X: wx - float32(origin.X)*components.ChunkSize,
		Y: systems.GroundHeight(wx, wz) + routeLineLift,
		Z: wz - float32(origin.Z)*components.ChunkSize,
	}
}

func drawRoutePolyline(pts [][2]float32, col rl.Color) {
	for i := 0; i+1 < len(pts); i++ {
		ax, az := pts[i][0], pts[i][1]
		bx, bz := pts[i+1][0], pts[i+1][1]
		segLen := dist2Dxz(ax, az, bx, bz)
		steps := int(segLen/routeLineStep) + 1
		prev := routeRenderPoint(ax, az)
		for s := 1; s <= steps; s++ {
			t := float32(s) / float32(steps)
			cur := routeRenderPoint(ax+t*(bx-ax), az+t*(bz-az))
			rl.DrawLine3D(prev, cur, col)
			prev = cur
		}
	}
}

func drawRouteGoalMarker(p [2]float32, col rl.Color) {
	c := routeRenderPoint(p[0], p[1])
	rl.DrawCircle3D(c, 1.2, rl.Vector3{X: 1}, 90, col)
}

func dist2Dxz(ax, az, bx, bz float32) float32 {
	dx := bx - ax
	dz := bz - az
	return rl.Vector2Length(rl.Vector2{X: dx, Y: dz})
}
