package systems

import "rts-go/components"

// stepReflex drives the two locomotion-owning reflexes. FaceThreat pivots the
// hull in place; SmokeAndReverse backs away while keeping the nose on the
// threat so the frontal armor and the gunner stay pointed at it.
func (sys *VehicleDriverSystem) stepReflex(pos *components.WorldPos, mot *components.Motion,
	spec *components.VehicleSpec, ov *components.VehicleOverride, dt float32) {
	switch ov.Kind {
	case components.VehicleReflexFaceThreat:
		sys.brake(mot, dt)
		sys.steerYaw(mot, ov.ThreatYaw, spec, dt)
		sys.advance(pos, mot, dt)
	case components.VehicleReflexSmokeAndReverse:
		sys.steerYaw(mot, ov.ThreatYaw, spec, dt)
		sys.approachSpeed(mot, -spec.MaxSpeedReverse, dt)
		sys.advance(pos, mot, dt)
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
