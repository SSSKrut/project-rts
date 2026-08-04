package systems

import (
	"fmt"
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
	filter         *ecs.Filter3[components.Vehicle, components.WorldPos, components.Motion]
	queueMap       *ecs.Map[components.ActionQueue]
	routeMap       *ecs.Map[components.RoadRoute]
	followerMap    *ecs.Map[components.RoadFollower]
	overrideMap    *ecs.Map[components.VehicleOverride]
	buildingFilter *ecs.Filter2[components.Building, components.WorldPos]
	vehHash        ecs.Resource[core.VehicleSpatialHash]
	sampler        *HeightSampler
	router         *RoadRouter
	obstacles      []vehObstacle
	// P8-a props: BlocksMove circles snapshotted per tick; crushed trees are
	// despawned after the query (deferred archetype change).
	propFilter   *ecs.Filter2[components.Prop, components.WorldPos]
	registryRes  ecs.Resource[components.PropTypeRegistry]
	propIndexRes ecs.Resource[PropChunkIndex]
	chunkIndex   ecs.Resource[TerrainChunkIndex]
	navBakedMap  *ecs.Map[components.NavBaked]
	riversRes    ecs.Resource[components.Rivers]
	eventLogRes  ecs.Resource[components.EventLog]
	propObs      []vehPropObstacle
	crushed      []vehPropObstacle
	// Convoy pacing (M7): squad members read roster mates to derive the
	// column speed cap.
	worldRef       *ecs.World
	posMap         *ecs.Map[components.WorldPos]
	squadMemberMap *ecs.Map[components.SquadMember]
	rosterMap      *ecs.Map[components.CommandRoster]
	formationMap   *ecs.Map[components.FormationData]
	vehicleMap     *ecs.Map[components.Vehicle]
}

const (
	vehArrivalRadius float32 = 2.0
	vehAccel         float32 = 3.0
	vehDecel         float32 = 5.0
	vehTurnSlowSpeed float32 = 3.0
	// Uphill grade beyond which speed scales down; slopeMul floors at 0.25.
	vehSlopeGain float32 = 1.2
	// The reverse-gear enter condition must hold this long before the driver
	// commits — transient aim flips (replan, ramp pop, formation slot swing)
	// must not trigger a three-point turn (Phase 19 M6 owner bug).
	vehReverseHold float32 = 0.4
	// Crawl multiplier while flattening a crushable prop (P8-a).
	vehCrushSlowMul float32 = 0.4
	// Terrain refusal (P8-b): per-metre grade at the nose probe that blocks a
	// bearing; matches the nav bake's impassable threshold.
	vehSlopeBlock float32 = 0.6
	// Extra keep-out beyond a river's half-width for the water probe.
	vehWaterMargin float32 = 1.0
	// Progress watchdog (P8-e). Wedge: no vehStuckEps displacement for
	// vehStuckWindow → replan, second window → Failed (a wedged hull jitters
	// ~0.3 m; a legitimate detour moves ≥ 3 m/s). Orbit: distance-to-goal not
	// improved by vehStuckDistEps for vehOrbitWindow → Failed directly — the
	// steering has been retrying that whole time, and the longest legitimate
	// stall in the suite (lateral ridge flank, ~8 s) stays under the window.
	vehStuckWindow  float32 = 3.0
	vehStuckEps     float32 = 1.5
	vehOrbitWindow  float32 = 10.0
	vehStuckDistEps float32 = 0.5
)

func (sys *VehicleDriverSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter3[components.Vehicle, components.WorldPos, components.Motion](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.routeMap = ecs.NewMap[components.RoadRoute](w)
	sys.followerMap = ecs.NewMap[components.RoadFollower](w)
	sys.overrideMap = ecs.NewMap[components.VehicleOverride](w)
	sys.buildingFilter = ecs.NewFilter2[components.Building, components.WorldPos](w)
	sys.propFilter = ecs.NewFilter2[components.Prop, components.WorldPos](w)
	sys.registryRes = ecs.NewResource[components.PropTypeRegistry](w)
	sys.propIndexRes = ecs.NewResource[PropChunkIndex](w)
	sys.chunkIndex = ecs.NewResource[TerrainChunkIndex](w)
	sys.navBakedMap = ecs.NewMap[components.NavBaked](w)
	sys.riversRes = ecs.NewResource[components.Rivers](w)
	sys.eventLogRes = ecs.NewResource[components.EventLog](w)
	sys.vehHash = ecs.NewResource[core.VehicleSpatialHash](w)
	sys.sampler = NewHeightSampler(w)
	sys.router = NewRoadRouter(w)
	sys.worldRef = w
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.rosterMap = ecs.NewMap[components.CommandRoster](w)
	sys.formationMap = ecs.NewMap[components.FormationData](w)
	sys.vehicleMap = ecs.NewMap[components.Vehicle](w)
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
	sys.snapshotObstacles()
	q := sys.filter.Query()
	for q.Next() {
		veh, pos, mot := q.Get()
		ent := q.Entity()
		if aq := sys.queueMap.Get(ent); aq != nil {
			sys.step(ent, veh, pos, mot, aq, sys.routeMap.Get(ent),
				sys.followerMap.Get(ent), sys.overrideMap.Get(ent),
				float32(ctx.SimNow), dt)
		}
		sys.resolveOverlaps(ent, pos, components.SpecForVehicle(veh.Kind), dt)
	}
	sys.applyCrushes()
}

// applyCrushes despawns trees flattened this tick (after the query — Ark
// forbids archetype changes inside), drops them from the PropChunkIndex so
// chunk evict never touches a dead entity, and re-bakes the chunk's NavGrid
// (the tree's blocked circle is gone; CoverBaked stays — the cover pass is
// one-shot per chunk and a re-run would duplicate slot entities).
func (sys *VehicleDriverSystem) applyCrushes() {
	if len(sys.crushed) == 0 {
		return
	}
	propIdx := sys.propIndexRes.Get()
	chunkIdx := sys.chunkIndex.Get()
	for i := range sys.crushed {
		c := &sys.crushed[i]
		if !sys.worldRef.Alive(c.ent) {
			continue
		}
		if propIdx != nil {
			list := propIdx.Loaded[c.chunk]
			for j, e := range list {
				if e == c.ent {
					propIdx.Loaded[c.chunk] = append(list[:j], list[j+1:]...)
					break
				}
			}
		}
		sys.worldRef.RemoveEntity(c.ent)
		if chunkIdx != nil {
			if chunkEnt, ok := chunkIdx.Loaded[c.chunk]; ok && sys.navBakedMap.Has(chunkEnt) {
				sys.navBakedMap.Remove(chunkEnt)
			}
		}
	}
	sys.crushed = sys.crushed[:0]
}

// crushPass — a Tracked hull overlapping a crushable prop flattens it and
// crawls (×0.4) through the debris this tick.
func (sys *VehicleDriverSystem) crushPass(pos *components.WorldPos,
	spec *components.VehicleSpec, cruise float32) float32 {
	if !crushesProps(spec) || len(sys.propObs) == 0 {
		return cruise
	}
	px, pz := worldXZ(*pos)
	slowed := false
	for i := range sys.propObs {
		o := &sys.propObs[i]
		if !o.crush {
			continue
		}
		reach := o.r + spec.ColliderR*0.5
		dx, dz := px-o.x, pz-o.z
		if dx*dx+dz*dz >= reach*reach {
			continue
		}
		slowed = true
		sys.crushed = append(sys.crushed, *o)
	}
	if slowed {
		cruise *= vehCrushSlowMul
	}
	return cruise
}

func (sys *VehicleDriverSystem) step(ent ecs.Entity, veh *components.Vehicle,
	pos *components.WorldPos, mot *components.Motion, aq *components.ActionQueue,
	route *components.RoadRoute, follower *components.RoadFollower,
	ov *components.VehicleOverride, now, dt float32) {
	spec := components.SpecForVehicle(veh.Kind)

	// Reflexes that own locomotion (FaceThreat / SmokeAndReverse) run before
	// the order queue. Flee drives through the queue, so it falls through.
	if ov != nil && ov.Kind != components.VehicleReflexNone &&
		ov.Kind != components.VehicleReflexFlee {
		clearRoute(route, follower)
		resetWatchdog(follower)
		sys.stepReflex(pos, mot, spec, ov, dt)
		return
	}

	if aq.Count == 0 {
		clearRoute(route, follower)
		resetWatchdog(follower)
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

	// Squad members drive STRAIGHT to their formation slots — the slot is a
	// short moving hop, and per-member road routing tears the column apart
	// (a truck's wheeled planning bias detours it onto a highway while a
	// tracked mate cuts straight; owner repro 2026-07-31). Squad-level road
	// use stays with the squad macro path.
	inSquad := false
	if sm := sys.squadMemberMap.Get(ent); sm != nil && sm.Squad != (ecs.Entity{}) {
		inSquad = true
	}
	if inSquad {
		if route != nil && route.Planned == 1 {
			clearRoute(route, follower)
		}
	} else {
		if route != nil && (route.Planned == 0 || route.Goal != action.Target) {
			sys.planRoute(pos, action.Target, spec, route)
		}
		if route != nil && route.Planned == 1 && route.Phase < 3 {
			sys.stepRoute(ent, pos, mot, spec, route, follower, dt)
			return
		}
	}
	if follower != nil {
		follower.Edge = -1
	}

	diff := action.Target.Sub(*pos)
	distSq := diff.X*diff.X + diff.Z*diff.Z
	// Arrival scales with the turning circle (a truck cannot pin a 2 m
	// point); a stopped hull already parked on a shared goal also counts as
	// arrival — stopping short beats orbiting the occupied spot.
	arrive := rampPopRadius(vehArrivalRadius, spec)
	arrived := distSq < arrive*arrive
	if !arrived && distSq < (arrive+2*spec.ColliderR)*(arrive+2*spec.ColliderR) {
		gx, gz := worldXZ(action.Target)
		arrived = sys.goalCrowded(ent, gx, gz, arrive+spec.ColliderR)
	}
	if arrived {
		clearRoute(route, follower)
		resetWatchdog(follower)
		popAction(aq)
		return
	}
	dist := float32(math.Sqrt(float64(distSq)))
	if !inSquad && sys.progressWatchdog(pos, aq, route, follower, dist, now) {
		sys.brake(mot, dt)
		sys.advance(pos, mot, dt)
		return
	}
	cruise := spec.MaxSpeedOffroad * sys.slopeMul(pos, mot.Yaw)
	cruise = sys.crushPass(pos, spec, cruise)
	dx, dz := diff.X, diff.Z
	dx, dz, cruise = sys.terrainSteer(pos, mot, follower, spec, dx, dz, cruise)
	sys.drive(ent, follower, pos, mot, spec, dx, dz, dist, cruise, true, true, dt)
}

func resetWatchdog(follower *components.RoadFollower) {
	if follower != nil {
		follower.StuckSince = 0
		follower.BestSince = 0
		follower.StuckTries = 0
	}
}

// progressWatchdog (P8-e): honest refusal for soloists. The wedge timer
// rearms on displacement and replans once before the verdict; the orbit
// timer rearms on distance-to-goal improvement and goes straight to the
// verdict — lapping a sealed cluster IS the steering retrying. The verdict
// clears the action and files the reason instead of executing forever.
func (sys *VehicleDriverSystem) progressWatchdog(pos *components.WorldPos,
	aq *components.ActionQueue, route *components.RoadRoute,
	follower *components.RoadFollower, dist, now float32) bool {
	if follower == nil {
		return false
	}
	px, pz := worldXZ(*pos)
	ax, az := px-follower.StuckX, pz-follower.StuckZ
	if follower.StuckSince == 0 || ax*ax+az*az >= vehStuckEps*vehStuckEps {
		follower.StuckX, follower.StuckZ = px, pz
		follower.StuckSince = now
		follower.StuckTries = 0
	}
	if follower.BestSince == 0 || dist < follower.BestDist-vehStuckDistEps {
		follower.BestDist = dist
		follower.BestSince = now
	}
	verdict := now-follower.BestSince >= vehOrbitWindow
	if !verdict && now-follower.StuckSince >= vehStuckWindow {
		follower.StuckSince = now
		if follower.StuckTries == 0 {
			follower.StuckTries = 1
			clearRoute(route, follower)
			follower.DetourSide = 0
			follower.AvoidSide = 0
			return false
		}
		verdict = true
	}
	if !verdict {
		return false
	}
	ClearActions(aq)
	clearRoute(route, follower)
	resetWatchdog(follower)
	if debugLog {
		fmt.Printf("[stuck] FAILED no path at (%.1f,%.1f) dist=%.1f\n", px, pz, dist)
	}
	if log := sys.eventLogRes.Get(); log != nil {
		log.Push(components.EventEntry{
			Kind: components.EventOrderFailed,
			At:   now,
			Pos:  *pos,
			Text: "Vehicle stuck: no path",
		})
	}
	return true
}

// terrainSteer (P8-b): refuse a bearing whose ray probes hit impassable
// grade (≥ vehSlopeBlock across a 2 m baseline) or water. The probe horizon
// covers the braking distance; any refusal caps cruise at turn-slow speed so
// the hull never carries momentum into the band. Alternate bearings scan the
// FULL circle (±15°..±180°) — a forward hemisphere fully walled must resolve
// into a retreat bearing, not a permanent park on the skirt.
func (sys *VehicleDriverSystem) terrainSteer(pos *components.WorldPos,
	mot *components.Motion, follower *components.RoadFollower,
	spec *components.VehicleSpec, dx, dz, cruise float32) (float32, float32, float32) {
	aimLen := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if aimLen < 1e-4 {
		return dx, dz, cruise
	}
	px, pz := worldXZ(*pos)
	speed := mot.Speed
	if speed < 0 {
		speed = -speed
	}
	far := spec.BoxLen*0.5 + speed*speed/(2*vehDecel) + 4
	if far < spec.BoxLen*0.5+7 {
		far = spec.BoxLen*0.5 + 7
	}
	base := float32(math.Atan2(float64(dx), float64(dz)))
	if sys.bearingPassable(px, pz, base, spec, far) {
		if follower != nil {
			follower.DetourSide = 0
		}
		return dx, dz, cruise
	}
	if debugLog {
		fmt.Printf("[terr] blocked at (%.1f,%.1f) base=%.2f\n", px, pz, base)
	}
	if cruise > vehTurnSlowSpeed {
		cruise = vehTurnSlowSpeed
	}
	// Committed-side scan (P8-c seed): the goal pull re-centres `base` every
	// tick, and an uncommitted two-sided scan ping-pongs the hull in front of
	// the wall forever. Hold the chosen side until the goal-ward bearing
	// clears; flip only when the whole committed semicircle is walled.
	sides := [2]float32{1, -1}
	if follower != nil && follower.DetourSide != 0 {
		sides = [2]float32{float32(follower.DetourSide), -float32(follower.DetourSide)}
	}
	for _, s := range sides {
		for k := 1; k <= 12; k++ {
			b := base + s*float32(k)*(15*math.Pi/180)
			if sys.bearingPassable(px, pz, b, spec, far) {
				if follower != nil {
					follower.DetourSide = int8(s)
				}
				return float32(math.Sin(float64(b))) * aimLen,
					float32(math.Cos(float64(b))) * aimLen, cruise
			}
		}
	}
	return dx, dz, 0
}

// bearingPassable probes the WHOLE ray, not one point: 3 nose-width tracks ×
// distance steps out to the refusal horizon. A single fixed-distance probe
// accepts bearings whose clear spot lies past (or before) the steep band and
// the hull climbs anyway.
func (sys *VehicleDriverSystem) bearingPassable(px, pz, bearing float32,
	spec *components.VehicleSpec, far float32) bool {
	dirX := float32(math.Sin(float64(bearing)))
	dirZ := float32(math.Cos(float64(bearing)))
	rx, rz := dirZ, -dirX
	half := spec.BoxWid * 0.5
	near := spec.BoxLen * 0.5
	for dist := near; dist <= far; dist += 2.0 {
		cx := px + dirX*dist
		cz := pz + dirZ*dist
		for _, o := range [3]float32{-half, 0, half} {
			x := cx + rx*o
			z := cz + rz*o
			// Full gradient magnitude, not the along-ray component: an
			// oblique ray projects a 2.2 slope down to "passable" and the
			// hull legally traverses the cliff face sideways.
			gx := (sys.sampler.Sample(x+1, z) - sys.sampler.Sample(x-1, z)) * 0.5
			gz := (sys.sampler.Sample(x, z+1) - sys.sampler.Sample(x, z-1)) * 0.5
			if gx*gx+gz*gz >= vehSlopeBlock*vehSlopeBlock {
				return false
			}
			if sys.nearWater(x, z) {
				return false
			}
		}
	}
	return true
}

func (sys *VehicleDriverSystem) nearWater(x, z float32) bool {
	rivers := sys.riversRes.Get()
	if rivers == nil {
		return false
	}
	for pi := range rivers.Polylines {
		pl := &rivers.Polylines[pi]
		keep := pl.Width*0.5 + vehWaterMargin
		for si := 0; si+1 < len(pl.Points); si++ {
			ax, az := worldXZ(pl.Points[si])
			bx, bz := worldXZ(pl.Points[si+1])
			if pointToSegment2D(x, z, ax, az, bx, bz) <= keep {
				return true
			}
		}
	}
	return false
}

// drive steers toward the point at (dx, dz) relative to the hull and
// advances. envelope enables the brake-to-stop arrival envelope; allowReverse
// enables the reverse-gear hysteresis (enter when the target sits far behind
// and close — no room for a forward arc — and has STAYED there vehReverseHold
// seconds; exit once the nose is near enough or the target has drifted away).
func (sys *VehicleDriverSystem) drive(ent ecs.Entity, follower *components.RoadFollower,
	pos *components.WorldPos, mot *components.Motion,
	spec *components.VehicleSpec, dx, dz, dist, cruise float32,
	allowReverse, envelope bool, dt float32) {
	if pace := sys.squadPaceCap(ent, pos); pace > 0 && cruise > pace {
		cruise = pace
	}
	if mot.Speed >= -0.01 {
		dx, dz, cruise = sys.avoid(ent, pos, mot, spec, follower, dx, dz, dist, cruise)
	}
	desiredYaw := float32(math.Atan2(float64(dx), float64(dz)))
	yawErr := wrapAngle(desiredYaw - mot.Yaw)
	absErr := yawErr
	if absErr < 0 {
		absErr = -absErr
	}

	reversing := false
	if allowReverse {
		reversing = mot.Speed < -0.01
		enter := absErr > 2.1 && dist < 4*spec.TurnRadiusM
		if !reversing && enter {
			if follower == nil {
				reversing = true
			} else if follower.RevHold += dt; follower.RevHold >= vehReverseHold {
				reversing = true
			}
		} else if reversing && (absErr < 1.2 || dist > 6*spec.TurnRadiusM) {
			reversing = false
		}
		if follower != nil && (!enter || reversing) {
			follower.RevHold = 0
		}
	} else if follower != nil {
		follower.RevHold = 0
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
