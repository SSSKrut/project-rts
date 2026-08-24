package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AirDriverSystem flies every airframe along its ActionQueue. Three things
// make it a different animal from the ground driver rather than a variant:
//
//   - It owns Y outright. GroundStick gates on OnGround and an aircraft has
//     none, so nothing else writes the vertical axis (P10).
//   - Speed and altitude are DIALS, not order parameters. A waypoint says
//     where; Aircraft.SpeedSet and AltSet say how, and they survive every
//     arrival, replan and reflex (P1).
//   - It sub-steps. Not for the arrival ring — that is metres wide and no
//     rotary step gets near it — but because terrain following re-samples the
//     ground once per step, so a single long jump flies a straight chord over
//     a ridge the airframe was supposed to hug. The count comes from speed,
//     never from a variable dt: that would break replay determinism and
//     save/load continuity alike (P8).
//
// It also does NOT plan. There is no A*, no road graph, no avoidance: at
// altitude the straight line is the route, and the only thing an airframe can
// hit is ground it was told to fly at.
type AirDriverSystem struct {
	filter      *ecs.Filter3[components.Aircraft, components.WorldPos, components.Motion]
	queueMap    *ecs.Map[components.ActionQueue]
	overrideMap *ecs.Map[components.AircraftOverride]
	sampler     *HeightSampler
}

const (
	// Arrival ring. Wide because a helicopter cruising at 55 m/s covers most
	// of it in a tick, and a ring it cannot resolve is a ring it orbits.
	airArrivalRadius float32 = 18.0
	// Sub-step budget: split the tick so no integration step exceeds this
	// many metres. Derived from speed (state), never from wall time, so the
	// count is identical in every run of the same scene. Half a metre puts
	// the split inside the rotary speed range instead of leaving it dead code
	// until fixed wing arrives.
	airMaxStepM float32 = 0.5
	airMaxSteps         = 8
	// Yaw error past which the airframe gives up cruise and slows into the
	// turn, and the floor it slows to. A helicopter can turn on the spot but
	// looks (and flies) wrong doing it at cruise.
	airTurnSlowErr   float32 = 1.0
	airTurnSlowSpeed float32 = 12.0
	// Below this ground clearance the driver refuses to descend further, so a
	// bad AltSet cannot fly an airframe into a hillside. Not a crash model —
	// M0 has no damage; it is a floor that keeps the scene honest.
	airMinClearance float32 = 6.0
	// Egress despawn ring around Aircraft.Exit.
	airExitRadius float32 = 40.0
	// Deck height the break maneuver dives to — terrain masking without
	// touching the player's altitude dial.
	airEvadeAGL float32 = 12.0
)

func (sys *AirDriverSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter3[components.Aircraft, components.WorldPos, components.Motion](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.overrideMap = ecs.NewMap[components.AircraftOverride](w)
	sys.sampler = NewHeightSampler(w)
}

func (AirDriverSystem) Name() string { return "air_driver" }

func (AirDriverSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *AirDriverSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	dt := float32(ctx.Delta.Seconds())
	if dt <= 0 {
		return
	}
	now := float32(ctx.SimNow)
	q := sys.filter.Query()
	for q.Next() {
		ac, pos, mot := q.Get()
		ent := q.Entity()
		aq := sys.queueMap.Get(ent)
		if aq == nil {
			continue
		}
		sys.step(ent, ac, pos, mot, aq, dt, now)
	}
}

// step advances one airframe. The tick is split into whole sub-steps chosen
// from the current speed; every sub-step re-reads its own aim, which is what
// keeps a fast pass from cutting the corner of its arrival ring.
func (sys *AirDriverSystem) step(ent ecs.Entity, ac *components.Aircraft, pos *components.WorldPos,
	mot *components.Motion, aq *components.ActionQueue, dt, now float32) {
	spec := components.SpecForAircraft(ac.Kind)

	if ac.Fuel > 0 {
		ac.Fuel -= dt
		if ac.Fuel <= 0 {
			ac.Fuel = 0
			sys.beginEgress(ac, aq)
		}
	}

	n := sys.subSteps(mot.Speed, dt)
	sub := dt / float32(n)

	// A live Break / Flare owns locomotion (P7 mirror of the ground driver).
	// Abort does not: it already reduced to the ordinary egress leg.
	if ov := sys.overrideMap.Get(ent); ov != nil &&
		ov.Kind != components.AirReflexNone &&
		ov.Kind != components.AirReflexAbort && now < ov.Until {
		for i := 0; i < n; i++ {
			sys.stepEvade(spec, pos, mot, ov, sub)
		}
		return
	}

	for i := 0; i < n; i++ {
		sys.substep(ac, spec, pos, mot, aq, sub)
	}
}

// stepEvade: hard turn away from the threat bearing, full throttle, down
// toward the deck. The altitude DIAL is untouched — the reflex borrows the
// airframe, it does not reconfigure it.
func (sys *AirDriverSystem) stepEvade(spec *components.AircraftSpec, pos *components.WorldPos,
	mot *components.Motion, ov *components.AircraftOverride, dt float32) {
	away := wrapAngle(ov.ThreatYaw + float32(math.Pi))
	yawErr := wrapAngle(away - mot.Yaw)
	speedAbs := mot.Speed
	if speedAbs < 0 {
		speedAbs = -speedAbs
	}
	pivot := spec.PivotDps * float32(math.Pi) / 180
	maxYawDelta := (speedAbs/spec.TurnRadiusM + pivot) * dt
	steer := yawErr
	if steer > maxYawDelta {
		steer = maxYawDelta
	} else if steer < -maxYawDelta {
		steer = -maxYawDelta
	}
	mot.Yaw = wrapAngle(mot.Yaw + steer)

	if mot.Speed < spec.MaxSpeed {
		mot.Speed += spec.AccelMs2 * dt
		if mot.Speed > spec.MaxSpeed {
			mot.Speed = spec.MaxSpeed
		}
	}
	sys.advanceXZ(pos, mot, dt)

	wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	ground := sys.sampler.Sample(wx, wz)
	want := ground + airEvadeAGL
	if floor := ground + airMinClearance; want < floor {
		want = floor
	}
	delta := want - pos.Local.Y
	max := spec.ClimbRateMs * dt
	if delta > max {
		delta = max
	} else if delta < -max {
		delta = -max
	}
	pos.Local.Y += delta
}

// subSteps splits the tick so no single integration exceeds airMaxStepM.
func (sys *AirDriverSystem) subSteps(speed, dt float32) int {
	if speed < 0 {
		speed = -speed
	}
	n := int(speed*dt/airMaxStepM) + 1
	if n > airMaxSteps {
		n = airMaxSteps
	}
	return n
}

func (sys *AirDriverSystem) substep(ac *components.Aircraft, spec *components.AircraftSpec,
	pos *components.WorldPos, mot *components.Motion, aq *components.ActionQueue, dt float32) {
	target, hasTarget := sys.aim(ac, aq)
	if !hasTarget {
		// No waypoint: a rotary airframe holds station. The dials still run —
		// altitude is held, so a hover over a rising slope climbs with it.
		sys.brake(mot, spec, dt)
		sys.advanceXZ(pos, mot, dt)
		sys.holdAltitude(ac, spec, pos, dt)
		return
	}

	dx, dz, dist := horizontalTo(pos, target)
	if dist <= airArrivalRadius {
		sys.arrive(ac, aq)
		sys.brake(mot, spec, dt)
		sys.advanceXZ(pos, mot, dt)
		sys.holdAltitude(ac, spec, pos, dt)
		return
	}

	desiredYaw := float32(math.Atan2(float64(dx), float64(dz)))
	yawErr := wrapAngle(desiredYaw - mot.Yaw)
	absErr := yawErr
	if absErr < 0 {
		absErr = -absErr
	}

	cruise := ac.SpeedSet
	if cruise > spec.MaxSpeed {
		cruise = spec.MaxSpeed
	}
	if cruise < 0 {
		cruise = 0
	}
	if absErr > airTurnSlowErr && cruise > airTurnSlowSpeed {
		cruise = airTurnSlowSpeed
	}
	// The braking envelope applies to the LAST leg only. An intermediate
	// waypoint is a turning point, not a stop: braking to a crawl at each one
	// turns a four-leg route into four separate flights, and at 4.5 m/s² an
	// 80 m/s airframe starts braking 700 m out — i.e. always. Egress has no
	// envelope either; the airframe leaves the map at speed and despawns.
	if aq.Count <= 1 && !ac.Egressing {
		if vMax := float32(math.Sqrt(float64(2 * spec.AccelMs2 * (dist - airArrivalRadius)))); cruise > vMax {
			cruise = vMax
		}
	}

	if mot.Speed < cruise {
		mot.Speed += spec.AccelMs2 * dt
		if mot.Speed > cruise {
			mot.Speed = cruise
		}
	} else {
		mot.Speed -= spec.AccelMs2 * dt
		if mot.Speed < cruise {
			mot.Speed = cruise
		}
	}

	// Yaw authority is the sum of the cruise turn rate and the hover pivot:
	// a radius says nothing at zero airspeed, and a gunship swinging its nose
	// while stationary is a real manoeuvre, not an artefact.
	speedAbs := mot.Speed
	if speedAbs < 0 {
		speedAbs = -speedAbs
	}
	pivot := spec.PivotDps * float32(math.Pi) / 180
	maxYawDelta := (speedAbs/spec.TurnRadiusM + pivot) * dt
	steer := yawErr
	if steer > maxYawDelta {
		steer = maxYawDelta
	} else if steer < -maxYawDelta {
		steer = -maxYawDelta
	}
	mot.Yaw = wrapAngle(mot.Yaw + steer)

	sys.advanceXZ(pos, mot, dt)
	sys.holdAltitude(ac, spec, pos, dt)
}

// aim returns the point the airframe is flying at: the head MoveTo, or the
// exit while egressing. Non-MoveTo actions are dropped — an aircraft has no
// stance to take and stopping is what an empty queue already means.
func (sys *AirDriverSystem) aim(ac *components.Aircraft,
	aq *components.ActionQueue) (components.WorldPos, bool) {
	for aq.Count > 0 {
		a := aq.Actions[aq.Head]
		if a.Kind == components.ActionMoveTo {
			return a.Target, true
		}
		popAction(aq)
	}
	if ac.Egressing {
		return ac.Exit, true
	}
	return components.WorldPos{}, false
}

func (sys *AirDriverSystem) arrive(ac *components.Aircraft, aq *components.ActionQueue) {
	if aq.Count > 0 {
		popAction(aq)
		return
	}
	// Arrived at the exit with nothing queued: AirTrafficSystem sees the
	// airframe inside the exit ring next tick and clears it off the map.
	_ = ac
}

// beginEgress sends the airframe home. Fuel exhaustion is the only trigger in
// M0; a task ending or the player calling it off join here later, which is
// why the queue is cleared rather than appended to — an egress that politely
// waits behind three waypoints is not an egress.
func (sys *AirDriverSystem) beginEgress(ac *components.Aircraft, aq *components.ActionQueue) {
	if ac.Egressing {
		return
	}
	ac.Egressing = true
	ClearActions(aq)
}

func (sys *AirDriverSystem) brake(mot *components.Motion, spec *components.AircraftSpec, dt float32) {
	if mot.Speed > 0 {
		mot.Speed -= spec.AccelMs2 * dt
		if mot.Speed < 0 {
			mot.Speed = 0
		}
	}
}

func (sys *AirDriverSystem) advanceXZ(pos *components.WorldPos, mot *components.Motion, dt float32) {
	if mot.Speed == 0 {
		return
	}
	sinY := float32(math.Sin(float64(mot.Yaw)))
	cosY := float32(math.Cos(float64(mot.Yaw)))
	*pos = pos.Add(rl.Vector3{X: sinY * mot.Speed * dt, Z: cosY * mot.Speed * dt})
	mot.VelocityYaw = mot.Yaw
}

// holdAltitude drives Y toward the dial at the spec climb rate. AGL resolves
// against the live heightmap every sub-step, which is what makes nap-of-the-
// earth flight follow the ground instead of pretending to.
func (sys *AirDriverSystem) holdAltitude(ac *components.Aircraft, spec *components.AircraftSpec,
	pos *components.WorldPos, dt float32) {
	wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	ground := sys.sampler.Sample(wx, wz)

	want := ac.AltSet
	if ac.AltRef == components.AltAGL {
		want = ground + ac.AltSet
	}
	if floor := ground + airMinClearance; want < floor {
		want = floor
	}
	if ceil := ground + spec.CeilingM; want > ceil {
		want = ceil
	}

	delta := want - pos.Local.Y
	max := spec.ClimbRateMs * dt
	if delta > max {
		delta = max
	} else if delta < -max {
		delta = -max
	}
	pos.Local.Y += delta
}

// horizontalTo is the XZ leg to a target; altitude is a separate axis and
// must not leak into the steering distance, or a high transit reads as
// "far away" and never arrives.
func horizontalTo(pos *components.WorldPos, target components.WorldPos) (dx, dz, dist float32) {
	d := target.Sub(*pos)
	dx, dz = d.X, d.Z
	dist = float32(math.Sqrt(float64(dx*dx + dz*dz)))
	return dx, dz, dist
}

// AircraftAGL is the height above ground of an airframe — the input to the
// altitude band, shared by the detect pass and the UI.
func AircraftAGL(sampler *HeightSampler, pos components.WorldPos) float32 {
	wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	return pos.Local.Y - sampler.Sample(wx, wz)
}
