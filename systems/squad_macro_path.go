package systems

import (
	"fmt"
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SquadMacroPathSystem builds and refreshes the MacroPath of each Squad.
// A* runs through NavService for the squad center (computed on the fly).
// Replan triggers:
//
//  1. ReplanAt = 0 (set by SquadService.OrderMoveTo) — immediate.
//  2. elapsed ≥ mp.ReplanAt — 1 s throttle.
//  3. Center drifted > SquadReplanCenterDrift from the next waypoint.
//
// Parallel per-squad: each FindPath call builds local A* state, read-only
// resources are immutable across the tick, and the MacroPath write touches
// only that squad's component (disjoint across workers).
type SquadMacroPathSystem struct {
	filter         *ecs.Filter5[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData, components.OrderQueueHead]
	posMap         *ecs.Map[components.WorldPos]
	orderKindMap   *ecs.Map[components.OrderKind]
	orderTargetMap *ecs.Map[components.OrderTarget]
	orderStateMap  *ecs.Map[components.OrderState]
	// MovementProfile.PathStyle (or order-level override) feeds FindPath.
	movementProfileMap       *ecs.Map[components.MovementProfile]
	orderMovementOverrideMap *ecs.Map[components.OrderParamMovementProfile]
	// Forward snap on a standstill order reads member speeds; the
	// waypoint-reach radius widens for a vehicle anchor (M7).
	motionMap  *ecs.Map[components.Motion]
	vehicleMap *ecs.Map[components.Vehicle]
	nav        *NavService
	// An all-vehicle squad plans its macro path over the RoadGraph (M7 tail):
	// the column follows the road the way a soloist does. The router keeps
	// scratch buffers, so those squads run serially — see Update.
	router *RoadRouter
	pool   *core.WorkerPool

	workBuf []macroPathWork
	vehBuf  []macroPathWork
}

// NewSquadMacroPathSystem. nil pool falls back to serial execution.
func NewSquadMacroPathSystem(nav *NavService, pool *core.WorkerPool) *SquadMacroPathSystem {
	return &SquadMacroPathSystem{
		nav:     nav,
		pool:    pool,
		workBuf: make([]macroPathWork, 0, 16),
	}
}

func (sys *SquadMacroPathSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter5[components.Squad, components.CommandRoster, components.MacroPath, components.FormationData, components.OrderQueueHead](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderTargetMap = ecs.NewMap[components.OrderTarget](w)
	sys.orderStateMap = ecs.NewMap[components.OrderState](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.orderMovementOverrideMap = ecs.NewMap[components.OrderParamMovementProfile](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.vehicleMap = ecs.NewMap[components.Vehicle](w)
	sys.router = NewRoadRouter(w)
}

func (SquadMacroPathSystem) Name() string { return "squad_macro_path" }

func (SquadMacroPathSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   1 * time.Second,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

const (
	// Throttle (seconds) between unforced A* calls for the same squad.
	SquadReplanInterval float32 = 1.0
	// Force a replan when center drifts further than this from the next
	// waypoint (bunch reorganised around an obstacle).
	SquadReplanCenterDrift float32 = 5.0
	// SquadArrivalCoeff × Spacing = "close enough, squad idle".
	SquadArrivalCoeff float32 = 1.5
	// Slightly larger than unit arrivalRadius so the macro path doesn't
	// outpace its members.
	SquadWaypointReached float32 = 2.0
)

type macroPathWork struct {
	roster    *components.CommandRoster
	mp        *components.MacroPath
	fd        *components.FormationData
	head      *components.OrderQueueHead
	pathStyle components.PathStyle
	// Set when every live member is a vehicle: the squad routes over roads
	// and is processed serially (the router is not concurrency-safe).
	vehSpec *components.VehicleSpec
}

func (sys *SquadMacroPathSystem) Update(ctx core.UpdateContext) {
	sys.workBuf = sys.workBuf[:0]
	sys.vehBuf = sys.vehBuf[:0]
	q := sys.filter.Query()
	for q.Next() {
		_, roster, mp, fd, head := q.Get()
		sq := q.Entity()
		// Resolve PathStyle from the squad's MovementProfile or the active
		// Order's override. Done serially here, once per squad.
		pathStyle := components.PathStyleDirect
		if profile := sys.movementProfileMap.Get(sq); profile != nil {
			pathStyle = profile.PathStyle
		}
		if head.First != (ecs.Entity{}) {
			if override := sys.orderMovementOverrideMap.Get(head.First); override != nil {
				pathStyle = override.Profile.PathStyle
			}
		}
		w := macroPathWork{
			roster: roster, mp: mp, fd: fd, head: head, pathStyle: pathStyle,
			vehSpec: sys.mechanizedSpec(ctx.World, roster),
		}
		if w.vehSpec != nil {
			sys.vehBuf = append(sys.vehBuf, w)
		} else {
			sys.workBuf = append(sys.workBuf, w)
		}
	}
	work := sys.workBuf

	world := ctx.World
	elapsed := float32(ctx.SimNow)
	sys.pool.ParallelFor(len(work), func(start, end int) {
		for i := start; i < end; i++ {
			sys.processSquad(world, work[i], elapsed)
		}
	})
	for i := range sys.vehBuf {
		sys.processSquad(world, sys.vehBuf[i], elapsed)
	}
}

// mechanizedSpec returns the leader's vehicle spec when EVERY live member is
// a vehicle, else nil. A mixed squad walks: routing the column over a highway
// its infantry cannot use would just stretch it.
func (sys *SquadMacroPathSystem) mechanizedSpec(world *ecs.World,
	roster *components.CommandRoster) *components.VehicleSpec {
	var lead *components.VehicleSpec
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		veh := sys.vehicleMap.Get(mem)
		if veh == nil {
			return nil
		}
		if lead == nil {
			lead = components.SpecForVehicle(veh.Kind)
		}
	}
	return lead
}

func (sys *SquadMacroPathSystem) processSquad(world *ecs.World, w macroPathWork, elapsed float32) {
	roster := w.roster
	mp := w.mp
	fd := w.fd
	head := w.head
	if roster.Count == 0 {
		return
	}

	center, ok := SquadAnchorPos(world, roster, sys.posMap)
	if !ok {
		return
	}

	// MacroPath is derived from the current Order. Idle squad → mp.HasGoal
	// stays false; FormationSystem sits this one out.
	var orderKind components.OrderKindCode
	var orderTargetPos components.WorldPos
	hasOrder := false
	if head.First != (ecs.Entity{}) && world.Alive(head.First) {
		if k := sys.orderKindMap.Get(head.First); k != nil {
			if t := sys.orderTargetMap.Get(head.First); t != nil {
				orderKind = k.Code
				orderTargetPos = t.Pos
				hasOrder = true
			}
		}
	}
	if !hasOrder {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}
	// Spec-driven hold-in-place: orders with DrivesMacroPath == false
	// (AttackTarget, SuppressFire) keep the squad stationary.
	if !components.SpecForOrderKind(orderKind).DrivesMacroPath {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}

	mp.Goal = orderTargetPos
	mp.HasGoal = true
	_ = orderKind

	// Pop head waypoints already crossed by the center. Skip while waiting
	// for stragglers so the FormationSystem gate stays consistent across
	// the slower 1 s SquadMacroPath cadence.
	if !mp.WaitingForStragglers {
		reach := SquadWaypointReach(world, roster, sys.vehicleMap)
		for mp.Head < mp.Count {
			d := center.Sub(mp.Waypoints[mp.Head])
			if d.X*d.X+d.Z*d.Z < reach*reach {
				mp.Head++
			} else {
				break
			}
		}
	}

	arrival := SquadArrivalCoeff * fd.Spacing
	if dSq := centerXZDistSq(center, mp.Goal); dSq < arrival*arrival {
		mp.HasGoal = false
		mp.Head = 0
		mp.Count = 0
		return
	}

	// Replan only when forced (new order), the path ran dry, or the anchor
	// drifted off it — a fresh path every second from a moving start flips
	// between near-equal A* routes and swings the march heading (#12).
	// Drift / exhaustion are rate-limited to the old 1 s interval.
	freshOrder := mp.ReplanAt == 0
	needReplan := false
	switch {
	case freshOrder:
		needReplan = true
	case elapsed < mp.LastPlanned+SquadReplanInterval:
	case mp.Head >= mp.Count:
		needReplan = true
	default:
		// Lateral deviation from the current leg — raw distance to the next
		// waypoint trips en route whenever decimated spacing exceeds it.
		var lat float32
		if mp.Head > 0 {
			lat = pointToSegXZ(center, mp.Waypoints[mp.Head-1], mp.Waypoints[mp.Head])
		} else {
			d := center.Sub(mp.Waypoints[0])
			lat = float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
		}
		if lat > SquadReplanCenterDrift {
			needReplan = true
		}
	}
	if !needReplan {
		return
	}

	if debugLog {
		fmt.Printf("[macro] replan squad kind=%d goal=(%.1f,%.1f) center=(%.1f,%.1f)\n",
			orderKind,
			mp.Goal.Local.X+float32(mp.Goal.Chunk.X)*components.ChunkSize,
			mp.Goal.Local.Z+float32(mp.Goal.Chunk.Z)*components.ChunkSize,
			center.Local.X+float32(center.Chunk.X)*components.ChunkSize,
			center.Local.Z+float32(center.Chunk.Z)*components.ChunkSize)
	}

	if w.vehSpec != nil && sys.planRoadMacro(center, mp, w.vehSpec) {
		sys.seedForward(world, w, center, freshOrder)
		mp.LastPlanned = elapsed
		mp.ReplanAt = elapsed + SquadReplanInterval
		return
	}

	path := sys.nav.FindPath(center, mp.Goal, NavOpts{
		Locomotion: components.LocomotionFoot,
		PathStyle:  w.pathStyle,
	})

	mp.Head = 0
	mp.Count = 0
	if len(path) == 0 {
		// No path — fall back to the raw goal so FormationSystem still drags
		// the squad in the right direction.
		mp.Waypoints[0] = mp.Goal
		mp.Count = 1
	} else {
		step := decimationStep(fd.Spacing)
		for i := step - 1; i < len(path); i += step {
			if int(mp.Count) >= components.SquadMacroPathSize {
				break
			}
			mp.Waypoints[mp.Count] = path[i]
			mp.Count++
		}
		// Always end on Goal so the squad doesn't park at the last decimated
		// waypoint instead of the target.
		if mp.Count == 0 || centerXZDistSq(mp.Waypoints[mp.Count-1], mp.Goal) > 1 {
			if int(mp.Count) < components.SquadMacroPathSize {
				mp.Waypoints[mp.Count] = mp.Goal
				mp.Count++
			} else {
				mp.Waypoints[components.SquadMacroPathSize-1] = mp.Goal
			}
		}
	}

	sys.seedForward(world, w, center, freshOrder)

	mp.LastPlanned = elapsed
	mp.ReplanAt = elapsed + SquadReplanInterval
}

// planRoadMacro lays the macro path along the RoadGraph itinerary: on-ramp
// point, node chain, off-ramp point, goal. Reports false when the road does
// not win on time — the caller falls back to the nav path.
func (sys *SquadMacroPathSystem) planRoadMacro(center components.WorldPos,
	mp *components.MacroPath, spec *components.VehicleSpec) bool {
	if sys.router == nil {
		return false
	}
	g := sys.router.Graph()
	if g == nil {
		return false
	}
	cx, cz := worldXZ(center)
	gx, gz := worldXZ(mp.Goal)
	plan, ok := sys.router.PlanRoute(cx, cz, gx, gz, spec)
	if !ok {
		return false
	}
	mp.Head = 0
	mp.Count = 0
	push := func(x, z float32) {
		if int(mp.Count) >= components.SquadMacroPathSize {
			return
		}
		mp.Waypoints[mp.Count] = components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
		mp.Count++
	}
	if plan.EntryEdge >= 0 && int(plan.EntryEdge) < len(g.Edges) {
		x, z := sys.router.EdgePoint(plan.EntryEdge, plan.EntryT)
		push(x, z)
	}
	for _, nid := range plan.Nodes {
		if int(nid) >= len(g.Nodes) {
			continue
		}
		x, z := worldXZ(g.Nodes[nid].Pos)
		push(x, z)
	}
	if plan.ExitEdge >= 0 && int(plan.ExitEdge) < len(g.Edges) {
		x, z := sys.router.EdgePoint(plan.ExitEdge, plan.ExitT)
		push(x, z)
	}
	// Always end on the goal, even if the ring filled up — the column must
	// leave the road for its target, not park at the last node.
	if mp.Count == 0 || centerXZDistSq(mp.Waypoints[mp.Count-1], mp.Goal) > 1 {
		if int(mp.Count) < components.SquadMacroPathSize {
			mp.Waypoints[mp.Count] = mp.Goal
			mp.Count++
		} else {
			mp.Waypoints[components.SquadMacroPathSize-1] = mp.Goal
		}
	}
	return mp.Count > 0
}

// seedForward snaps the formation heading when it is still unset (fresh
// squad) or on a fresh order issued from standstill: snapping before anyone
// moves is free for infantry and saves a vehicle the 20 m U-turn loop it
// takes chasing a slot that sweeps 90° during the slew (M7 owner repro).
// Mid-march the slew-limited FormationSystem stays the only writer (#12).
func (sys *SquadMacroPathSystem) seedForward(world *ecs.World, w macroPathWork,
	center components.WorldPos, freshOrder bool) {
	fd, mp := w.fd, w.mp
	seed := fd.Forward.X == 0 && fd.Forward.Z == 0
	if !seed && freshOrder && squadStationary(world, w.roster, sys.motionMap) {
		seed = true
	}
	if !seed {
		return
	}
	target := mp.Goal
	if mp.Count > 0 {
		target = mp.Waypoints[0]
	}
	diff := target.Sub(center)
	mag := float32(math.Sqrt(float64(diff.X*diff.X + diff.Z*diff.Z)))
	if mag > 0.01 {
		fd.Forward = rl.Vector3{X: diff.X / mag, Y: 0, Z: diff.Z / mag}
	}
}

// SquadWaypointReach is the waypoint-advance radius for a squad: the
// infantry 2 m ring, widened when the anchor is a vehicle — the M6 arrival
// scaling parks a hull rampPopRadius (up to 6 m) short of the point, and a
// reach tighter than that livelocks the march in the dead ring between the
// two radii (M7: convoy froze mid-field, every queue empty).
func SquadWaypointReach(world *ecs.World, roster *components.CommandRoster,
	vehicleMap *ecs.Map[components.Vehicle]) float32 {
	r := SquadWaypointReached
	if roster.Count > 0 {
		if lead := roster.Members[0]; lead != (ecs.Entity{}) && world.Alive(lead) {
			if veh := vehicleMap.Get(lead); veh != nil {
				spec := components.SpecForVehicle(veh.Kind)
				if vr := rampPopRadius(vehArrivalRadius, spec) + 0.5; vr > r {
					r = vr
				}
			}
		}
	}
	return r
}

// squadStationary reports every live member as (near) parked — the gate for
// the Forward snap on a fresh order.
func squadStationary(world *ecs.World, roster *components.CommandRoster,
	motionMap *ecs.Map[components.Motion]) bool {
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		if m := motionMap.Get(mem); m != nil && (m.Speed > 0.5 || m.Speed < -0.5) {
			return false
		}
	}
	return true
}

// SquadAnchorPos — the formation reference point: the commander (slot 0)
// when alive, else the roster centroid. Slots and waypoint progress key on
// the leader; stragglers regain their slots at raised Pace instead of the
// whole squad waiting (owner feedback 2026-07-19).
func SquadAnchorPos(world *ecs.World, roster *components.CommandRoster, posMap *ecs.Map[components.WorldPos]) (components.WorldPos, bool) {
	if roster.Count > 0 {
		if lead := roster.Members[0]; lead != (ecs.Entity{}) && world.Alive(lead) {
			if p := posMap.Get(lead); p != nil {
				return *p, true
			}
		}
	}
	return SquadCenter(world, roster, posMap)
}

// SquadCenter returns the XZ-averaged WorldPos of every live roster member.
// Y is averaged too — falls out naturally as the squad ascends stairs / bunkers.
// `world` is required for the per-member Alive check (Ark's Map.Get panics on
// a dead entity).
func SquadCenter(world *ecs.World, roster *components.CommandRoster, posMap *ecs.Map[components.WorldPos]) (components.WorldPos, bool) {
	if roster.Count == 0 {
		return components.WorldPos{}, false
	}
	// First valid member becomes the reference chunk so accumulated Local
	// values stay bounded; offsets fold through WorldPos.Sub.
	var ref components.WorldPos
	found := false
	var sumX, sumY, sumZ float32
	var n float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		p := posMap.Get(mem)
		if p == nil {
			continue
		}
		if !found {
			ref = *p
			found = true
		}
		d := p.Sub(ref)
		sumX += d.X
		sumY += d.Y
		sumZ += d.Z
		n++
	}
	if !found {
		return components.WorldPos{}, false
	}
	inv := 1 / n
	out := ref.Add(rl.Vector3{X: sumX * inv, Y: sumY * inv, Z: sumZ * inv})
	return out, true
}

// centerXZDistSq — squared XZ distance between two WorldPos. Used in tight
// loops where DistanceSquared would also fold Y.
func centerXZDistSq(a, b components.WorldPos) float32 {
	d := a.Sub(b)
	return d.X*d.X + d.Z*d.Z
}

// pointToSegXZ — XZ distance from p to segment [a, b].
func pointToSegXZ(p, a, b components.WorldPos) float32 {
	ab := b.Sub(a)
	ap := p.Sub(a)
	lenSq := ab.X*ab.X + ab.Z*ab.Z
	t := float32(0)
	if lenSq > 1e-6 {
		t = (ap.X*ab.X + ap.Z*ab.Z) / lenSq
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	dx := ap.X - ab.X*t
	dz := ap.Z - ab.Z*t
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

// SquadSpread returns (maxDistance, caughtUp, total): largest XZ distance
// from `center` to any live member, count within caughtThreshold, total live.
func SquadSpread(
	world *ecs.World,
	roster *components.CommandRoster,
	center components.WorldPos,
	posMap *ecs.Map[components.WorldPos],
	caughtThreshold float32,
) (float32, uint8, uint8) {
	var maxSq float32
	var caught, total uint8
	thresholdSq := caughtThreshold * caughtThreshold
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !world.Alive(mem) {
			continue
		}
		p := posMap.Get(mem)
		if p == nil {
			continue
		}
		total++
		dSq := centerXZDistSq(*p, center)
		if dSq > maxSq {
			maxSq = dSq
		}
		if dSq <= thresholdSq {
			caught++
		}
	}
	return float32(math.Sqrt(float64(maxSq))), caught, total
}

// decimationStep — how many fine-grained NavService waypoints to skip
// between macro waypoints. Spacing 2 → step 4, Spacing 4 → step 6 etc.
func decimationStep(spacing float32) int {
	step := int(spacing) + 2
	if step < 4 {
		step = 4
	}
	if step > 8 {
		step = 8
	}
	return step
}
