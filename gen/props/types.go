// Package props is the sequence / composite / single generator output for
// world-decoration props (vegetation, fences, lamp posts, sandbags, debris).
// Generators in this package and its siblings (gen/fences, gen/lamps,
// gen/vegetation) all return []PropPlacement so a single spawner can apply
// the result regardless of the rule that produced it.
//
// Buildings stay in gen/buildings because their spec (Walls / Floors /
// Stairs / Levels / Transitions) is structurally richer than a flat
// placement list - they keep BuildingPlan as their output shape.
package props

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// PropPlacement is the canonical generator output for a single decoration
// prop. The caller spawns one entity per placement in the order returned
// (some generators rely on ordering for chain visuals like fence posts).
//
// Pos is world-space (chunk + local). The spawner is responsible for
// converting to the host chunk's local frame before attaching WorldPos.
// Scale is a multiplicative factor on the registry's default size; 1.0 = use
// the registry meta verbatim.
type PropPlacement struct {
	Type  components.PropType
	Pos   components.WorldPos
	Yaw   float32
	Scale float32
}

// AlongPolyline emits one PropPlacement per `spacing` metres along a chain
// of waypoints. Each placement faces along the local segment tangent. Used
// by fence / lamp / sandbag-line generators - the same primitive walks any
// polyline (road centre, building perimeter, hand-drawn path).
//
// Returns nil for < 2 waypoints or non-positive spacing. The trailing partial
// segment is dropped (caller adds the endpoint manually if needed).
func AlongPolyline(points []components.WorldPos, spacing float32, kind components.PropType, scale float32) []PropPlacement {
	if len(points) < 2 || spacing <= 0 {
		return nil
	}
	if scale <= 0 {
		scale = 1
	}
	var out []PropPlacement
	residual := float32(0)
	for i := 0; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		diff := b.Sub(a)
		segLen := length2D(diff)
		if segLen < 1e-3 {
			continue
		}
		yaw := atan2(diff.X, diff.Z)
		t := residual
		for t < segLen {
			f := t / segLen
			pos := a.Add(rl.Vector3{X: diff.X * f, Y: 0, Z: diff.Z * f})
			out = append(out, PropPlacement{
				Type:  kind,
				Pos:   components.Normalize(pos),
				Yaw:   yaw,
				Scale: scale,
			})
			t += spacing
		}
		residual = t - segLen
	}
	return out
}

// Composite copies a fixed offset list into PropPlacements anchored at
// `centre`. Used for single "structures" made of multiple props (lamp =
// pole + bulb + base; sandbag stack = three cubes in a triangle).
type CompositeOffset struct {
	Type  components.PropType
	DX    float32 // metres east of centre
	DZ    float32 // metres north of centre
	Yaw   float32 // additional yaw on top of centre yaw
	Scale float32
}

// Composite returns one PropPlacement per `parts` entry, rotated/anchored
// around `centre`. `centreYaw` rotates the whole arrangement; each part's
// own Yaw stacks on top.
func Composite(centre components.WorldPos, centreYaw float32, parts []CompositeOffset) []PropPlacement {
	if len(parts) == 0 {
		return nil
	}
	cs, sn := cosSin(centreYaw)
	out := make([]PropPlacement, 0, len(parts))
	for _, p := range parts {
		// Rotate local offset by centreYaw.
		dx := p.DX*cs + p.DZ*sn
		dz := -p.DX*sn + p.DZ*cs
		scale := p.Scale
		if scale <= 0 {
			scale = 1
		}
		out = append(out, PropPlacement{
			Type:  p.Type,
			Pos:   components.Normalize(centre.Add(rl.Vector3{X: dx, Y: 0, Z: dz})),
			Yaw:   centreYaw + p.Yaw,
			Scale: scale,
		})
	}
	return out
}
