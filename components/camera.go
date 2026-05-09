package components

import "github.com/mlange-42/ark/ecs"

type Camera struct {
	Fovy        float32 // degrees
	Perspective bool
}

// ActiveCamera marks the singleton active camera entity.
type ActiveCamera struct{}

type OrbitController struct {
	Target ecs.Entity // ecs.NilEntity (0) = origin

	Yaw    float32 // radians
	Pitch  float32 // radians
	Radius float32

	MinRadius float32
	MaxRadius float32

	SensitivityYaw   float32
	SensitivityPitch float32
	SensitivityZoom  float32

	// Smoothing factor (0 = instant, >0 = lerp factor per second).
	Smooth float32
}
