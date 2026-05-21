package props

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func length2D(v rl.Vector3) float32 {
	return float32(math.Sqrt(float64(v.X*v.X + v.Z*v.Z)))
}

func atan2(x, z float32) float32 {
	return float32(math.Atan2(float64(x), float64(z)))
}

func cosSin(yaw float32) (float32, float32) {
	return float32(math.Cos(float64(yaw))), float32(math.Sin(float64(yaw)))
}
