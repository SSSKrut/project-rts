package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// VehicleDriverSystem — Phase 19 M1 Driver layer. Kinematic arc-steering
// along the vehicle's ActionQueue: forward speed rides the hull yaw, yaw
// rate is curvature-limited (|Speed| / TurnRadius, plus a pivot term for
// tracked hulls). A target far behind flips into reverse (Speed < 0) until
// the nose can come around — simplified three-point turn. Gear lives in the
// sign of Motion.Speed, so there is no private cross-tick state to restore
// on load. Off-road only; road preference and bridge decks are M2
// (RoadFollower).
type VehicleDriverSystem struct {
	filter   *ecs.Filter3[components.Vehicle, components.WorldPos, components.Motion]
	queueMap *ecs.Map[components.ActionQueue]
	sampler  *HeightSampler
}

const (
	vehArrivalRadius float32 = 2.0
	vehAccel         float32 = 3.0
	vehDecel         float32 = 5.0
	vehTurnSlowSpeed float32 = 3.0
	// Uphill grade beyond which speed scales down; slopeMul floors at 0.25.
	vehSlopeGain float32 = 1.2
)

func (sys *VehicleDriverSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter3[components.Vehicle, components.WorldPos, components.Motion](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.sampler = NewHeightSampler(w)
}

func (VehicleDriverSystem) Name() string { return "vehicle_driver" }

func (VehicleDriverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *VehicleDriverSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	q := sys.filter.Query()
	for q.Next() {
		veh, pos, mot := q.Get()
		aq := sys.queueMap.Get(q.Entity())
		if aq == nil {
			continue
		}
		sys.step(veh, pos, mot, aq, dt)
	}
}

func (sys *VehicleDriverSystem) step(veh *components.Vehicle, pos *components.WorldPos,
	mot *components.Motion, aq *components.ActionQueue, dt float32) {
	spec := components.SpecForVehicle(veh.Kind)

	if aq.Count == 0 {
		sys.brake(mot, dt)
		sys.advance(pos, mot, dt)
		return
	}
	action := aq.Actions[aq.Head]
	switch action.Kind {
	case components.ActionMoveTo:
	case components.ActionStop:
		mot.Speed = 0
		popAction(aq)
		return
	default:
		popAction(aq)
		return
	}

	diff := action.Target.Sub(*pos)
	distSq := diff.X*diff.X + diff.Z*diff.Z
	if distSq < vehArrivalRadius*vehArrivalRadius {
		popAction(aq)
		return
	}
	dist := float32(math.Sqrt(float64(distSq)))
	desiredYaw := float32(math.Atan2(float64(diff.X), float64(diff.Z)))
	yawErr := wrapAngle(desiredYaw - mot.Yaw)
	absErr := yawErr
	if absErr < 0 {
		absErr = -absErr
	}

	// Gear: hysteresis rides the sign of Speed. Enter reverse when the
	// target sits far behind and close (no room for a forward arc); exit
	// once the nose is near enough or the target has drifted away.
	reversing := mot.Speed < -0.01
	if !reversing && absErr > 2.1 && dist < 4*spec.TurnRadiusM {
		reversing = true
	} else if reversing && (absErr < 1.2 || dist > 6*spec.TurnRadiusM) {
		reversing = false
	}

	var target float32
	if reversing {
		target = -spec.MaxSpeedReverse
	} else {
		target = spec.MaxSpeedOffroad * sys.slopeMul(pos, mot.Yaw)
		if absErr > 0.9 && target > vehTurnSlowSpeed {
			target = vehTurnSlowSpeed
		}
		// Arrival envelope: never faster than a full-brake stop allows.
		if vMax := float32(math.Sqrt(float64(2 * vehDecel * dist))); target > vMax {
			target = vMax
		}
	}
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

	speedAbs := mot.Speed
	if speedAbs < 0 {
		speedAbs = -speedAbs
	}
	pivot := float32(0.15)
	if spec.Locomotion == components.LocomotionTracked {
		pivot = 0.6
	}
	maxYawDelta := (speedAbs/spec.TurnRadiusM + pivot) * dt
	steer := yawErr
	if steer > maxYawDelta {
		steer = maxYawDelta
	} else if steer < -maxYawDelta {
		steer = -maxYawDelta
	}
	mot.Yaw += steer
	mot.Yaw = wrapAngle(mot.Yaw)

	sys.advance(pos, mot, dt)
}

func (sys *VehicleDriverSystem) brake(mot *components.Motion, dt float32) {
	switch {
	case mot.Speed > 0:
		mot.Speed -= vehDecel * dt
		if mot.Speed < 0 {
			mot.Speed = 0
		}
	case mot.Speed < 0:
		mot.Speed += vehDecel * dt
		if mot.Speed > 0 {
			mot.Speed = 0
		}
	}
}

func (sys *VehicleDriverSystem) advance(pos *components.WorldPos, mot *components.Motion, dt float32) {
	if mot.Speed == 0 {
		return
	}
	sinY := float32(math.Sin(float64(mot.Yaw)))
	cosY := float32(math.Cos(float64(mot.Yaw)))
	*pos = pos.Add(rl.Vector3{X: sinY * mot.Speed * dt, Z: cosY * mot.Speed * dt})
	mot.VelocityYaw = mot.Yaw
}

// slopeMul samples the grade 3 m ahead of the hull; uphill scales speed
// down toward a 0.25 floor, downhill and flat pass through.
func (sys *VehicleDriverSystem) slopeMul(pos *components.WorldPos, yaw float32) float32 {
	wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	sinY := float32(math.Sin(float64(yaw)))
	cosY := float32(math.Cos(float64(yaw)))
	const ahead = 3.0
	h0 := sys.sampler.Sample(wx, wz)
	h1 := sys.sampler.Sample(wx+sinY*ahead, wz+cosY*ahead)
	slope := (h1 - h0) / ahead
	if slope <= 0 {
		return 1
	}
	mul := 1 - slope*vehSlopeGain
	if mul < 0.25 {
		mul = 0.25
	}
	return mul
}
