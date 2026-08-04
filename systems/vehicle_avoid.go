package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// vehicle_avoid.go — Phase 19 M6 obstacle layer. Two passes per driver tick:
// avoid() bends the aim vector around building footprints and other hulls
// BEFORE drive() steers, resolveOverlaps() separates any residual
// interpenetration AFTER advance(). Vehicles stay out of ORCA (P5 —
// nonholonomic); infantry yields on its own side via the vehicle hash.

const (
	// Extra clearance beyond the sum of collider radii / footprint edge.
	avoidMargin float32 = 0.6
	// Look-ahead bounds for the forward probe (metres).
	avoidMinLook float32 = 6.0
	avoidMaxLook float32 = 30.0
	// Stand-off kept behind a slower same-direction hull (column headway).
	avoidHeadwayGap float32 = 3.0
	// A neighbour slower than this counts as parked (steer around it, no
	// priority game).
	avoidStoppedSpeed float32 = 0.5
	// Tangent over-rotation past the silhouette edge.
	avoidTangentPad float32 = 0.12
	// Hard-resolve position-correction speed cap (m/s).
	resolvePushCap float32 = 3.0
)

// vehObstacle is one building footprint in world XZ, snapshotted per tick.
type vehObstacle struct {
	minX, minZ, maxX, maxZ float32
}

// vehPropObstacle is one BlocksMove prop (circle) in world XZ. Crushable
// props (trees) are obstacles for Wheeled only — a Tracked hull drives
// through at ×0.4 and the prop despawns (P8-a).
type vehPropObstacle struct {
	x, z, r float32
	crush   bool
	ent     ecs.Entity
	chunk   components.ChunkCoord
}

func (o *vehObstacle) inflated(r float32) vehObstacle {
	return vehObstacle{o.minX - r, o.minZ - r, o.maxX + r, o.maxZ + r}
}

func (o *vehObstacle) contains(x, z float32) bool {
	return x >= o.minX && x <= o.maxX && z >= o.minZ && z <= o.maxZ
}

// segmentHits reports whether p→q crosses the box (slab test).
func (o *vehObstacle) segmentHits(px, pz, qx, qz float32) bool {
	tMin, tMax := float32(0), float32(1)
	for i := 0; i < 2; i++ {
		var p, d, lo, hi float32
		if i == 0 {
			p, d, lo, hi = px, qx-px, o.minX, o.maxX
		} else {
			p, d, lo, hi = pz, qz-pz, o.minZ, o.maxZ
		}
		if d > -1e-6 && d < 1e-6 {
			if p < lo || p > hi {
				return false
			}
			continue
		}
		t0 := (lo - p) / d
		t1 := (hi - p) / d
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		if t0 > tMin {
			tMin = t0
		}
		if t1 < tMax {
			tMax = t1
		}
		if tMin > tMax {
			return false
		}
	}
	return true
}

func (sys *VehicleDriverSystem) snapshotObstacles() {
	sys.obstacles = sys.obstacles[:0]
	q := sys.buildingFilter.Query()
	for q.Next() {
		b, _ := q.Get()
		fp := b.Footprint
		if fp.MaxX <= fp.MinX || fp.MaxZ <= fp.MinZ {
			continue
		}
		sys.obstacles = append(sys.obstacles, vehObstacle{fp.MinX, fp.MinZ, fp.MaxX, fp.MaxZ})
	}
	sys.propObs = sys.propObs[:0]
	registry := sys.registryRes.Get()
	if registry == nil {
		return
	}
	qp := sys.propFilter.Query()
	for qp.Next() {
		prop, pos := qp.Get()
		meta := registry.Metas[prop.Type]
		if !meta.BlocksMove {
			continue
		}
		wx, wz := worldXZ(*pos)
		sys.propObs = append(sys.propObs, vehPropObstacle{
			x:     wx,
			z:     wz,
			r:     meta.BBoxRadius * prop.Scale,
			crush: meta.Crushable,
			ent:   qp.Entity(),
			chunk: pos.Chunk,
		})
	}
}

// crushesProps — Tracked hulls flatten crushable props (P8-a).
func crushesProps(spec *components.VehicleSpec) bool {
	return spec.Locomotion == components.LocomotionTracked
}

// avoidProps redirects the aim past the tangent of the nearest blocking prop
// circle. Crushable props are transparent to a crushing hull.
func (sys *VehicleDriverSystem) avoidProps(px, pz, dx, dz, aimLen, look, selfR float32,
	crusher bool) (float32, float32) {
	if len(sys.propObs) == 0 {
		return dx, dz
	}
	aimX, aimZ := dx/aimLen, dz/aimLen
	bestFwd := float32(math.MaxFloat32)
	var best *vehPropObstacle
	for i := range sys.propObs {
		o := &sys.propObs[i]
		if o.crush && crusher {
			continue
		}
		ex, ez := o.x-px, o.z-pz
		fwd := ex*aimX + ez*aimZ
		if fwd <= 0 || fwd > look+selfR+o.r {
			continue
		}
		lat := ex*aimZ - ez*aimX
		if lat < 0 {
			lat = -lat
		}
		if lat >= selfR+o.r+avoidMargin {
			continue
		}
		if fwd < bestFwd {
			bestFwd = fwd
			best = o
		}
	}
	if best == nil {
		return dx, dz
	}
	ex, ez := best.x-px, best.z-pz
	d := float32(math.Sqrt(float64(ex*ex + ez*ez)))
	if d < 1e-4 {
		return dx, dz
	}
	combR := selfR + best.r + avoidMargin
	sinT := combR / d
	if sinT > 1 {
		sinT = 1
	}
	offset := float32(math.Asin(float64(sinT))) + avoidTangentPad
	bearing := float32(math.Atan2(float64(ex), float64(ez)))
	side := ex*aimZ - ez*aimX
	var newBearing float32
	if side > 0 {
		newBearing = bearing - offset
	} else {
		newBearing = bearing + offset
	}
	return float32(math.Sin(float64(newBearing))) * aimLen,
		float32(math.Cos(float64(newBearing))) * aimLen
}

// avoid bends the aim (dx, dz) around the nearest blocking obstacle and
// returns a possibly reduced cruise speed. Buildings first (they are big and
// static), then hulls against the bent aim: same-direction slower hull →
// match speed (column), moving cross/oncoming → lower entity ID keeps
// course, parked → steer around like a wall.
func (sys *VehicleDriverSystem) avoid(ent ecs.Entity, pos *components.WorldPos,
	mot *components.Motion, spec *components.VehicleSpec,
	dx, dz, dist, cruise float32) (float32, float32, float32) {
	aimLen := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if aimLen < 1e-4 {
		return dx, dz, cruise
	}
	px, pz := worldXZ(*pos)
	speed := mot.Speed
	if speed < 0 {
		speed = -speed
	}
	look := speed*speed/(2*vehDecel) + spec.BoxLen*0.5
	if look < avoidMinLook {
		look = avoidMinLook
	} else if look > avoidMaxLook {
		look = avoidMaxLook
	}
	if look > dist {
		look = dist
	}

	dx, dz = sys.avoidBuildings(px, pz, dx, dz, aimLen, look, spec.ColliderR)

	aimLen = float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if aimLen < 1e-4 {
		return dx, dz, cruise
	}
	dx, dz = sys.avoidProps(px, pz, dx, dz, aimLen, look, spec.ColliderR, crushesProps(spec))

	aimLen = float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if aimLen < 1e-4 {
		return dx, dz, cruise
	}
	aimX, aimZ := dx/aimLen, dz/aimLen

	h := sys.vehHash.Get()
	if h == nil {
		return dx, dz, cruise
	}
	// Nearest blocking hull wins; the rest resolve on later ticks.
	selfR := spec.ColliderR
	bestFwd := float32(math.MaxFloat32)
	var best core.SpatialEntry
	found := false
	h.ForEachEntryInRadius(px, pz, look+selfR+8, func(e *core.SpatialEntry, _ float32) {
		if e.Ent == ent {
			return
		}
		ex, ez := e.X-px, e.Z-pz
		fwd := ex*aimX + ez*aimZ
		if fwd <= 0 || fwd > look+selfR+e.Radius {
			return
		}
		lat := ex*aimZ - ez*aimX
		if lat < 0 {
			lat = -lat
		}
		if lat >= selfR+e.Radius+avoidMargin {
			return
		}
		if fwd < bestFwd {
			bestFwd = fwd
			best = *e
			found = true
		}
	})
	if !found {
		return dx, dz, cruise
	}

	ex, ez := best.X-px, best.Z-pz
	eSpeed := float32(math.Sqrt(float64(best.VelX*best.VelX + best.VelZ*best.VelZ)))
	gap := bestFwd - (selfR + best.Radius)

	if eSpeed > avoidStoppedSpeed {
		eAlong := best.VelX*aimX + best.VelZ*aimZ
		if eAlong > 0.3*eSpeed {
			// Same direction, slower: hold the lane, match its speed inside
			// the headway envelope — columns fall out of this for free.
			brakeDist := speed * speed / (2 * vehDecel)
			if gap < brakeDist+avoidHeadwayGap {
				if eAlong < cruise {
					cruise = eAlong
				}
				if gap < avoidHeadwayGap {
					cruise = 0
				}
			}
			return dx, dz, cruise
		}
		// Moving cross / oncoming traffic: lower ID keeps course.
		if ent.ID() < best.Ent.ID() {
			return dx, dz, cruise
		}
		if cruise > vehTurnSlowSpeed {
			cruise = vehTurnSlowSpeed
		}
	}

	// Parked hull (or we are the yielding side): steer past the tangent.
	d := float32(math.Sqrt(float64(ex*ex + ez*ez)))
	if d < 1e-4 {
		return dx, dz, cruise
	}
	combR := selfR + best.Radius + avoidMargin
	sinT := combR / d
	if sinT > 1 {
		sinT = 1
	}
	offset := float32(math.Asin(float64(sinT))) + avoidTangentPad
	bearing := float32(math.Atan2(float64(ex), float64(ez)))
	side := ex*aimZ - ez*aimX // >0: obstacle right of aim → detour left
	var newBearing float32
	if side > 0 {
		newBearing = bearing - offset
	} else {
		newBearing = bearing + offset
	}
	dx = float32(math.Sin(float64(newBearing))) * aimLen
	dz = float32(math.Cos(float64(newBearing))) * aimLen
	return dx, dz, cruise
}

// avoidBuildings redirects the aim to a visible corner of the first
// footprint blocking the probe segment. Corner-aiming along successive ticks
// degenerates into wall-following, which is exactly the wanted behaviour.
func (sys *VehicleDriverSystem) avoidBuildings(px, pz, dx, dz, aimLen, look, selfR float32) (float32, float32) {
	if len(sys.obstacles) == 0 {
		return dx, dz
	}
	scale := look / aimLen
	qx, qz := px+dx*scale, pz+dz*scale
	infl := selfR + avoidMargin

	blockIdx := -1
	blockDistSq := float32(math.MaxFloat32)
	for i := range sys.obstacles {
		o := sys.obstacles[i].inflated(infl)
		if !o.segmentHits(px, pz, qx, qz) {
			continue
		}
		cx := (o.minX + o.maxX) * 0.5
		cz := (o.minZ + o.maxZ) * 0.5
		dSq := (cx-px)*(cx-px) + (cz-pz)*(cz-pz)
		if dSq < blockDistSq {
			blockDistSq = dSq
			blockIdx = i
		}
	}
	if blockIdx < 0 {
		return dx, dz
	}
	raw := sys.obstacles[blockIdx]
	o := raw.inflated(infl)
	pad := infl * 0.5
	corners := [4][2]float32{
		{o.minX - pad, o.minZ - pad},
		{o.maxX + pad, o.minZ - pad},
		{o.maxX + pad, o.maxZ + pad},
		{o.minX - pad, o.maxZ + pad},
	}
	aimX, aimZ := dx/aimLen, dz/aimLen
	bestScore := float32(math.MaxFloat32)
	bestX, bestZ := dx, dz
	for _, c := range corners {
		// Reachability is judged against the RAW footprint: the resolve band
		// often parks the hull centre inside the INFLATED box, and testing
		// against that would reject all four corners and leave the aim
		// pointed straight into the wall (deadlock at the face midpoint).
		if raw.segmentHits(px, pz, c[0], c[1]) {
			continue // corner across the building, not reachable directly
		}
		vx, vz := c[0]-px, c[1]-pz
		vLen := float32(math.Sqrt(float64(vx*vx + vz*vz)))
		if vLen < 1e-4 {
			continue
		}
		// Deviation from the wanted direction, with a mild detour-length tax.
		dev := 1 - (vx*aimX+vz*aimZ)/vLen
		score := dev + 0.01*vLen
		if score < bestScore {
			bestScore = score
			bestX, bestZ = vx/vLen*aimLen, vz/vLen*aimLen
		}
	}
	return bestX, bestZ
}

// goalCrowded reports a stopped hull already parked over the goal point —
// the spot is taken, so stopping short is arrival, not failure (a shared
// group target otherwise becomes a merry-go-round around the first
// arrival).
func (sys *VehicleDriverSystem) goalCrowded(self ecs.Entity, gx, gz, within float32) bool {
	h := sys.vehHash.Get()
	if h == nil {
		return false
	}
	crowded := false
	h.ForEachEntryInRadius(gx, gz, within, func(e *core.SpatialEntry, _ float32) {
		if e.Ent == self {
			return
		}
		if e.VelX*e.VelX+e.VelZ*e.VelZ < avoidStoppedSpeed*avoidStoppedSpeed {
			crowded = true
		}
	})
	return crowded
}

// resolveOverlaps is the hard guarantee: whatever the steering missed, no
// hull ends a tick inside another hull or a building footprint. Pushes are
// XZ-only (GroundStick re-clamps Y) and speed-capped so a bad spawn
// separates over a few ticks instead of teleporting.
func (sys *VehicleDriverSystem) resolveOverlaps(ent ecs.Entity, pos *components.WorldPos,
	spec *components.VehicleSpec, dt float32) {
	px, pz := worldXZ(*pos)
	var pushX, pushZ float32

	if h := sys.vehHash.Get(); h != nil {
		selfR := spec.ColliderR
		h.ForEachEntryInRadius(px, pz, selfR+8, func(e *core.SpatialEntry, dSq float32) {
			if e.Ent == ent {
				return
			}
			minD := selfR + e.Radius
			if dSq >= minD*minD {
				return
			}
			d := float32(math.Sqrt(float64(dSq)))
			var nx, nz float32
			if d > 1e-4 {
				nx, nz = (px-e.X)/d, (pz-e.Z)/d
			} else {
				// Dead-centre overlap: deterministic split by ID order.
				if ent.ID() < e.Ent.ID() {
					nx = 1
				} else {
					nx = -1
				}
			}
			// Both sides run this against the same pre-tick snapshot, so each
			// takes half the penetration.
			push := (minD - d) * 0.5
			pushX += nx * push
			pushZ += nz * push
		})
	}

	// Hull-vs-hull pushes are speed-capped: both sides resolve against the
	// same pre-tick snapshot and a bad spawn should separate over ticks, not
	// teleport.
	if pLen := float32(math.Sqrt(float64(pushX*pushX + pushZ*pushZ))); pLen > resolvePushCap*dt {
		pushX = pushX / pLen * resolvePushCap * dt
		pushZ = pushZ / pLen * resolvePushCap * dt
	}

	// Static prop circles: same full-ejection rule as building faces. A
	// crushing hull ignores crushable props here — it drives through and the
	// crush pass despawns them.
	crusher := crushesProps(spec)
	propInfl := spec.ColliderR * 0.7
	for i := range sys.propObs {
		o := &sys.propObs[i]
		if o.crush && crusher {
			continue
		}
		minD := o.r + propInfl
		dxp := px - o.x
		dzp := pz - o.z
		dSq := dxp*dxp + dzp*dzp
		if dSq >= minD*minD {
			continue
		}
		d := float32(math.Sqrt(float64(dSq)))
		if d > 1e-4 {
			pushX += dxp / d * (minD - d)
			pushZ += dzp / d * (minD - d)
		} else {
			pushX += minD
		}
	}

	// Building faces are static walls: eject the full penetration at once —
	// per-tick intrusion is bounded by speed·dt, so an uncapped push cannot
	// teleport, while a capped one loses the shoving match against a 6 m/s
	// drive and lets the hull grind through the band.
	infl := spec.ColliderR * 0.7 // hull centre must stay this far off a wall
	for i := range sys.obstacles {
		o := sys.obstacles[i].inflated(infl)
		if !o.contains(px, pz) {
			continue
		}
		// Push out along the cheapest axis.
		left := px - o.minX
		right := o.maxX - px
		down := pz - o.minZ
		up := o.maxZ - pz
		best := left
		exitX, exitZ := -left, float32(0)
		if right < best {
			best, exitX, exitZ = right, right, 0
		}
		if down < best {
			best, exitX, exitZ = down, 0, -down
		}
		if up < best {
			best, exitX, exitZ = up, 0, up
		}
		pushX += exitX
		pushZ += exitZ
	}

	if pushX == 0 && pushZ == 0 {
		return
	}
	*pos = pos.Add(rl.Vector3{X: pushX, Z: pushZ})
}
