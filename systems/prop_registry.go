package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// NewPropTypeRegistry builds the placeholder PropTypeRegistry. Returned by
// pointer so AddResource doesn't copy the 8 KB array.
func NewPropTypeRegistry() *components.PropTypeRegistry {
	r := &components.PropTypeRegistry{}

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
		Crushable:  true,
	}

	r.Metas[components.PropPine] = components.PropMeta{
		Primitive:  components.PrimitiveCone,
		Size:       rl.Vector3{X: 1.6, Y: 7.0},
		Color:      rl.Color{R: 30, G: 70, B: 35, A: 255},
		Cover:      0.5,
		HP:         400,
		BBoxRadius: 1.6,
		BlocksLOS:  true,
		BlocksMove: true,
		Crushable:  true,
	}

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
		Crushable:  true,
	}

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

	return r
}
