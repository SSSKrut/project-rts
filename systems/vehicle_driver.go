package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// VehicleDriverSystem — kinematic arc-steering
// along the vehicle's ActionQueue: forward speed rides the hull yaw, yaw
// rate is curvature-limited (|Speed| / TurnRadius, plus a pivot term for
// tracked hulls). A target far behind flips into reverse (Speed < 0) until
// the nose can come around — simplified three-point turn. Gear lives in the
// sign of Motion.Speed, so there is no private cross-tick state to restore
// on load. M2: the head MoveTo is routed over the RoadGraph when the road
// wins on time (RoadRoute); on edges the hull cruises at road speed and
// writes RoadFollower for bridge-deck Y (vehicle_driver_road.go).
type VehicleDriverSystem struct {
	filter      *ecs.Filter3[components.Vehicle, components.WorldPos, components.Motion]
	queueMap    *ecs.Map[components.ActionQueue]
	routeMap    *ecs.Map[components.RoadRoute]
	followerMap *ecs.Map[components.RoadFollower]
	overrideMap *ecs.Map[components.VehicleOverride]
	sampler     *HeightSampler
	router      *RoadRouter
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
	sys.routeMap = ecs.NewMap[components.RoadRoute](w)
	sys.followerMap = ecs.NewMap[components.RoadFollower](w)
	sys.overrideMap = ecs.NewMap[components.VehicleOverride](w)
	sys.sampler = NewHeightSampler(w)
	sys.router = NewRoadRouter(w)
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
		ent := q.Entity()
		aq := sys.queueMap.Get(ent)
		if aq == nil {
			continue
		}
		sys.step(veh, pos, mot, aq, sys.routeMap.Get(ent),
			sys.followerMap.Get(ent), sys.overrideMap.Get(ent), dt)
	}
}

func (sys *VehicleDriverSystem) step(veh *components.Vehicle, pos *components.WorldPos,
	mot *components.Motion, aq *components.ActionQueue,
	route *components.RoadRoute, follower *components.RoadFollower,
	ov *components.VehicleOverride, dt float32) {
	spec := components.SpecForVehicle(veh.Kind)

	// Reflexes that own locomotion (FaceThreat / SmokeAndReverse) run before
	// the order queue. Flee drives through the queue, so it falls through.
	if ov != nil && ov.Kind != components.VehicleReflexNone &&
		ov.Kind != components.VehicleReflexFlee {
		clearRoute(route, follower)
		sys.stepReflex(pos, mot, spec, ov, dt)
		return
	}

	if aq.Count == 0 {
		clearRoute(route, follower)
		sys.brake(mot, dt)
		sys.advance(pos, mot, dt)
		return
	}
	action := aq.Actions[aq.Head]
	switch action.Kind {
	case components.ActionMoveTo:
	case components.ActionStop:
		mot.Speed = 0
		clearRoute(route, follower)
		popAction(aq)
		return
	default:
		popAction(aq)
		return
	}

	if route != nil && (route.Planned == 0 || route.Goal != action.Target) {
		sys.planRoute(pos, action.Target, spec, route)
	}
	if route != nil && route.Planned == 1 && route.Phase < 3 {
		sys.stepRoute(pos, mot, spec, route, follower, dt)
		return
	}
	if follower != nil {
		follower.Edge = -1
	}

	diff := action.Target.Sub(*pos)
	distSq := diff.X*diff.X + diff.Z*diff.Z
	if distSq < vehArrivalRadius*vehArrivalRadius {
		clearRoute(route, follower)
		popAction(aq)
		return
	}
	dist := float32(math.Sqrt(float64(distSq)))
	cruise := spec.MaxSpeedOffroad * sys.slopeMul(pos, mot.Yaw)
	sys.drive(pos, mot, spec, diff.X, diff.Z, dist, cruise, true, true, dt)
}

// drive steers toward the point at (dx, dz) relative to the hull and
// advances. envelope enables the brake-to-stop arrival envelope; allowReverse
// enables the reverse-gear hysteresis (enter when the target sits far behind
// and close — no room for a forward arc; exit once the nose is near enough or
// the target has drifted away).
func (sys *VehicleDriverSystem) drive(pos *components.WorldPos, mot *components.Motion,
	spec *components.VehicleSpec, dx, dz, dist, cruise float32,
	allowReverse, envelope bool, dt float32) {
	desiredYaw := float32(math.Atan2(float64(dx), float64(dz)))
	yawErr := wrapAngle(desiredYaw - mot.Yaw)
	absErr := yawErr
	if absErr < 0 {
		absErr = -absErr
	}

	reversing := false
	if allowReverse {
		reversing = mot.Speed < -0.01
		if !reversing && absErr > 2.1 && dist < 4*spec.TurnRadiusM {
			reversing = true
		} else if reversing && (absErr < 1.2 || dist > 6*spec.TurnRadiusM) {
			reversing = false
		}
	}

	var target float32
	if reversing {
		target = -spec.MaxSpeedReverse
	} else {
		target = cruise
		if absErr > 0.9 && target > vehTurnSlowSpeed {
			target = vehTurnSlowSpeed
		}
		if envelope {
			if vMax := float32(math.Sqrt(float64(2 * vehDecel * dist))); target > vMax {
				target = vMax
			}
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
