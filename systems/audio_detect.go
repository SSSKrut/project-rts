package systems

import "rts-go/components"

// Sensor range cap shared with the LOS wall window (3×3 chunks = one chunk
// radius, 64 m). Also clamps the audio emission bubble.
const visionMaxRange float32 = 64.0

// Audio detection: emission radius = base * pace * posture, clamped at
// visionMaxRange. Walls don't attenuate audio in the current skeleton.
// Consumed by ContactSystem.audioEmissionRadius (contact_detect.go).
const (
	audioBaseRadius   float32 = 25.0
	audioPostureQuiet float32 = 0.4
)

var audioPaceMul = [...]float32{
	components.PaceWalk:   1.0,
	components.PaceRun:    1.5,
	components.PaceSprint: 2.0,
}
