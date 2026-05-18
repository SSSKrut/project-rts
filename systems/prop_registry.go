package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// NewPropTypeRegistry builds the placeholder PropTypeRegistry. Cover/HP/
// BlocksLOS/BlocksMove are filled for forward-compatibility (Phase 6 NavGrid,
// Phase 11 combat) - placeholder visuals today, real gameplay later.
//
// Returned by pointer so AddResource holds it without copying the 8 KB array.
func NewPropTypeRegistry() *components.PropTypeRegistry {
	r := &components.PropTypeRegistry{}

	// Oak - chunky deciduous: brown trunk, broad green canopy.
	r.Metas[components.PropOak] = components.PropMeta{
		Primitive:  components.PrimitiveTree,
		Size:       rl.Vector3{X: 0.35, Y: 4.0, Z: 2.4},
		Color:      rl.Color{R: 50, G: 110, B: 40, A: 255},
		TrunkColor: rl.Color{R: 90, G: 60, B: 35, A: 255},
		Cover:      0.7,
		HP:         500,
		BBoxRadius: 2.4,
		BlocksLOS:  true,
		BlocksMove: true,
	}

	// Pine - narrow tall conifer; single dark cone (cheap visual, no trunk).
	r.Metas[components.PropPine] = components.PropMeta{
		Primitive:  components.PrimitiveCone,
		Size:       rl.Vector3{X: 1.6, Y: 7.0},
		Color:      rl.Color{R: 30, G: 70, B: 35, A: 255},
		Cover:      0.5,
		HP:         400,
		BBoxRadius: 1.6,
		BlocksLOS:  true,
		BlocksMove: true,
	}

	// Birch - slim trunk, lighter canopy.
	r.Metas[components.PropBirch] = components.PropMeta{
		Primitive:  components.PrimitiveTree,
		Size:       rl.Vector3{X: 0.22, Y: 5.5, Z: 1.6},
		Color:      rl.Color{R: 130, G: 170, B: 80, A: 255},
		TrunkColor: rl.Color{R: 220, G: 215, B: 200, A: 255},
		Cover:      0.45,
		HP:         300,
		BBoxRadius: 1.6,
		BlocksLOS:  true,
		BlocksMove: true,
	}

	// Bush - low ground sphere; partial cover, doesn't block move.
	r.Metas[components.PropBush] = components.PropMeta{
		Primitive:  components.PrimitiveSphere,
		Size:       rl.Vector3{X: 0.7},
		Color:      rl.Color{R: 60, G: 100, B: 45, A: 255},
		Cover:      0.3,
		HP:         50,
		BBoxRadius: 0.7,
		BlocksLOS:  false,
		BlocksMove: false,
	}

	// Rock - grey cube. Solid cover, blocks LOS and movement.
	r.Metas[components.PropRock] = components.PropMeta{
		Primitive:  components.PrimitiveCube,
		Size:       rl.Vector3{X: 1.4, Y: 1.0, Z: 1.4},
		Color:      rl.Color{R: 130, G: 125, B: 120, A: 255},
		Cover:      0.9,
		HP:         2000,
		BBoxRadius: 1.0,
		BlocksLOS:  true,
		BlocksMove: true,
	}

	// Water - placeholder blue plane along a river segment.
	r.Metas[components.PropWater] = components.PropMeta{
		Primitive:  components.PrimitivePlane,
		Size:       rl.Vector3{X: 4.0, Z: 4.0},
		Color:      rl.Color{R: 60, G: 110, B: 170, A: 230},
		Cover:      0,
		HP:         0,
		BBoxRadius: 2.0,
		BlocksLOS:  false,
		BlocksMove: true,
	}

	// Bridge - wooden plank spanning the river. Length matches the road spawn
	// step (4 m); width matches highway. Traversable overrides the underlying
	// water's BlocksMove for nav purposes.
	r.Metas[components.PropBridge] = components.PropMeta{
		Primitive:   components.PrimitiveCube,
		Size:        rl.Vector3{X: 4.0, Y: 0.4, Z: 4.0},
		Color:       rl.Color{R: 110, G: 80, B: 50, A: 255},
		Cover:       0.2,
		HP:          1500,
		BBoxRadius:  3.0,
		BlocksLOS:   false,
		BlocksMove:  false,
		Traversable: true,
	}

	// Road surface placeholders. Plane primitive: Size.X = full road width
	// (perpendicular to travel), Size.Z = step length along travel. Yaw is
	// applied per-spawn so the plane rotates into segment direction.
	r.Metas[components.PropRoadHighway] = components.PropMeta{
		Primitive:   components.PrimitivePlane,
		Size:        rl.Vector3{X: 4.0, Z: 4.0},
		Color:       rl.Color{R: 50, G: 50, B: 55, A: 255},
		BBoxRadius:  2.0,
		Traversable: true,
	}
	r.Metas[components.PropRoadLocal] = components.PropMeta{
		Primitive:   components.PrimitivePlane,
		Size:        rl.Vector3{X: 3.0, Z: 4.0},
		Color:       rl.Color{R: 110, G: 110, B: 115, A: 255},
		BBoxRadius:  1.5,
		Traversable: true,
	}
	r.Metas[components.PropRoadDirt] = components.PropMeta{
		Primitive:   components.PrimitivePlane,
		Size:        rl.Vector3{X: 2.5, Z: 4.0},
		Color:       rl.Color{R: 130, G: 95, B: 60, A: 255},
		BBoxRadius:  1.25,
		Traversable: true,
	}
	// Junction - square plate sized per-spawn via uniform Scale (= 1.5 ×
	// max-incident-edge-width). Single shared meta keeps the registry small.
	r.Metas[components.PropJunction] = components.PropMeta{
		Primitive:   components.PrimitivePlane,
		Size:        rl.Vector3{X: 1.0, Z: 1.0},
		Color:       rl.Color{R: 60, G: 60, B: 65, A: 255},
		BBoxRadius:  0.7,
		Traversable: true,
	}

	return r
}
