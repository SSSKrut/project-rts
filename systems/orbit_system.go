package systems

import (
	"math"

	"rts-go/components"
	"rts-go/core"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// OrbitSystem updates camera position from OrbitController using input.
type OrbitSystem struct {
	filter *ecs.Filter2[components.OrbitController, components.WorldPos]
	posMap *ecs.Map[components.WorldPos] // for target lookup
}

func (sys *OrbitSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter2[components.OrbitController, components.WorldPos](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
}

func (OrbitSystem) Name() string { return "orbit" }

func (OrbitSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{ActiveEvery: 0, RelevantEvery: core.LODDisabled, DormantEvery: core.LODDisabled}
}

func (sys OrbitSystem) Update(ctx core.UpdateContext) {
	// Only run in active tier
	if ctx.Tier != core.LODTierActive {
		return
	}

	// Input
	mouseDelta := rl.GetMouseDelta()
	wheel := rl.GetMouseWheelMove()
	rightDown := rl.IsMouseButtonDown(rl.MouseButtonRight)

	q := sys.filter.Query()
	var nilEnt ecs.Entity
	for q.Next() {
		orbit, pos := q.Get()

		// Read target world position
		var target components.WorldPos
		var hasTarget bool
		if orbit.Target != nilEnt {
			tptr := sys.posMap.Get(orbit.Target)
			if tptr != nil {
				target = *tptr
				hasTarget = true
			}
		}

		// Apply input
		if rightDown {
			orbit.Yaw -= mouseDelta.X * orbit.SensitivityYaw
			orbit.Pitch += mouseDelta.Y * orbit.SensitivityPitch
		}
		// Zoom
		if wheel != 0 {
			orbit.Radius -= wheel * orbit.SensitivityZoom
		}

		// Clamp
		if orbit.Pitch > math.Pi/2-0.01 {
			orbit.Pitch = math.Pi/2 - 0.01
		}
		if orbit.Pitch < -math.Pi/2+0.01 {
			orbit.Pitch = -math.Pi/2 + 0.01
		}
		if orbit.Radius < orbit.MinRadius {
			orbit.Radius = orbit.MinRadius
		}
		if orbit.Radius > orbit.MaxRadius {
			orbit.Radius = orbit.MaxRadius
		}

		// Spherical -> Cartesian world-space offset from target
		cp := float32(math.Cos(float64(orbit.Pitch)))
		sp := float32(math.Sin(float64(orbit.Pitch)))
		sy := float32(math.Sin(float64(orbit.Yaw)))
		cy := float32(math.Cos(float64(orbit.Yaw)))

		offset := rl.Vector3{
			X: orbit.Radius * cp * sy,
			Y: orbit.Radius * sp,
			Z: orbit.Radius * cp * cy,
		}

		// Camera position = target + offset (in WorldPos space, normalising chunk crossings).
		if hasTarget {
			*pos = target.Add(offset)
		} else {
			*pos = (components.WorldPos{}).Add(offset)
		}
	}
}
