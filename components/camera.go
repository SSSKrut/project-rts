package components

import "github.com/mlange-42/ark/ecs"

// Camera stores projection settings for an entity that acts as a camera.
type Camera struct {
	// Field of view in degrees
	Fovy float32
	// Use perspective if true, otherwise orthographic (not implemented here)
	Perspective bool
}

// ActiveCamera marks the currently active camera entity (singleton marker).
type ActiveCamera struct{}

// OrbitController holds parameters for an orbital camera controller.
type OrbitController struct {
	// Target entity to orbit around. Use ecs.NilEntity (0) for origin.
	Target ecs.Entity

	// Spherical parameters
	Yaw    float32 // radians
	Pitch  float32 // radians
	Radius float32

	// Limits
	MinRadius float32
	MaxRadius float32

	// Input sensitivities
	SensitivityYaw   float32
	SensitivityPitch float32
	SensitivityZoom  float32

	// Smoothing factor (0 = instant, >0 = lerp factor per second)
	Smooth float32
}
