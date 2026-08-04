package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// stepReflex drives the locomotion-owning reflexes. FaceThreat pivots the
// hull in place; SmokeAndReverse backs away while keeping the nose on the
// threat so the frontal armor and the gunner stay pointed at it; Flee runs
// for the retreat point through the ordinary steering stack (obstacles and
// terrain still apply — panic is not a licence to drive into a wall).
func (sys *VehicleDriverSystem) stepReflex(ent ecs.Entity, pos *components.WorldPos,
	mot *components.Motion, spec *components.VehicleSpec,
	follower *components.RoadFollower, ov *components.VehicleOverride, dt float32) {
	switch ov.Kind {
	case components.VehicleReflexFaceThreat:
		sys.brake(mot, dt)
		sys.steerYaw(mot, ov.ThreatYaw, spec, dt)
		sys.advance(pos, mot, dt)
	case components.VehicleReflexSmokeAndReverse:
		sys.steerYaw(mot, ov.ThreatYaw, spec, dt)
		sys.approachSpeed(mot, -spec.MaxSpeedReverse, dt)
		sys.advance(pos, mot, dt)
	case components.VehicleReflexFlee:
		diff := ov.Retreat.Sub(*pos)
		dist := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
		if dist < 1 {
			sys.brake(mot, dt)
			sys.advance(pos, mot, dt)
			return
		}
		cruise := spec.MaxSpeedOffroad * sys.slopeMul(pos, mot.Yaw)
		cruise = sys.crushPass(pos, spec, cruise)
		dx, dz := diff.X, diff.Z
		dx, dz, cruise = sys.terrainSteer(pos, mot, follower, spec, dx, dz, cruise)
		sys.drive(ent, follower, pos, mot, spec, dx, dz, dist, cruise, false, false, dt)
	}
}

// steerYaw rotates the hull toward targetYaw at the curvature-limited rate
// (translation speed contribution + the tracked/wheeled pivot term).
func (sys *VehicleDriverSystem) steerYaw(mot *components.Motion, targetYaw float32,
	spec *components.VehicleSpec, dt float32) {
	speedAbs := mot.Speed
	if speedAbs < 0 {
		speedAbs = -speedAbs
	}
	pivot := float32(0.15)
	if spec.Locomotion == components.LocomotionTracked {
		pivot = 0.6
	}
	maxYawDelta := (speedAbs/spec.TurnRadiusM + pivot) * dt
	steer := wrapAngle(targetYaw - mot.Yaw)
	if steer > maxYawDelta {
		steer = maxYawDelta
	} else if steer < -maxYawDelta {
		steer = -maxYawDelta
	}
	mot.Yaw = wrapAngle(mot.Yaw + steer)
}

// approachSpeed accelerates/decelerates Motion.Speed toward target (may be
// negative for reverse) using the shared accel/decel rates.
func (sys *VehicleDriverSystem) approachSpeed(mot *components.Motion, target, dt float32) {
	if mot.Speed < target {
		mot.Speed += vehAccel * dt
		if mot.Speed > target {
			mot.Speed = target
		}
	} else {
		mot.Speed -= vehDecel * dt
		if mot.Speed < target {
			mot.Speed = target
		}
	}
}
