package components

// VehicleKind indexes VehicleSpecs. Classes differ by spec row + weapon
// set only; behaviour code is shared.
type VehicleKind uint8

const (
	VehicleTruck VehicleKind = iota
	VehicleBTR
	VehicleBMP
	VehicleTank
	VehicleATCarrier
	VehicleCar
	// VehicleAAGun is a towed emplacement: every speed in its spec is zero
	// and the driver simply never moves it. Emplacement-as-vehicle buys the
	// factory, the gunner pass, the turret and the panels for free.
	VehicleAAGun
	// Command post on wheels: no gun, one job — carry the net forward.
	VehicleCommand
	VehicleKindCount
)

// Vehicle marks a ground-vehicle entity. Hull yaw lives in Motion.Yaw.
type Vehicle struct {
	Kind VehicleKind
}

// Turret is present only on turreted vehicles (Spec.TurretSlewDps > 0).
// Yaw is relative to the hull.
type Turret struct {
	Yaw float32
}

// RoadFollower is the road-graph locomotion state. Edge indexes
// RoadGraph.Edges, -1 while off-road; T is the hull projection param along
// the edge. Always present on vehicles; GroundStick reads it for
// bridge-deck Y. RevHold accumulates seconds the reverse-gear enter
// condition has been continuously true — a transient aim flip behind the
// hull (replan, ramp pop, formation slot swing) must persist before the
// driver commits to backing up (Phase 19 M6).
type RoadFollower struct {
	Edge    int32
	T       float32
	RevHold float32
	// DetourSide (MB1, P8-c seed): committed side of a terrain detour
	// (+1/−1, 0 = free). Without commitment the goal pull re-centres the
	// bearing scan every tick and the hull ping-pongs in front of the wall.
	// Reset when the goal-ward bearing is passable again.
	DetourSide int8
	// AvoidSide (MB2, P8-c): committed side of a building corner detour.
	// Held ACROSS blocker changes — two adjacent footprints otherwise trade
	// the "nearest blocker" role and the corner pick flickers between their
	// mouths. Reset when the probe to the goal is clear.
	AvoidSide int8
	// Progress watchdog (MB2, P8-e), soloist MoveTo only; both timers unarmed
	// at Since = 0. Wedge timer: Stuck* anchor the last position that counted
	// as displacement — a wedged hull jitters in place. Orbit timer:
	// Best* track the best distance-to-goal seen — a hull lapping a sealed
	// cluster moves plenty without ever getting closer. Either verdict ends
	// the action honestly instead of forever.
	StuckX     float32
	StuckZ     float32
	StuckSince float32
	BestDist   float32
	BestSince  float32
	StuckTries uint8
}

// RoadRoute is the planned road itinerary for the head MoveTo action.
// Planned != 0 means the routing decision was made for Goal; a head action
// whose target differs triggers a replan. Entry/Exit are mid-edge ramp
// points (projection param T), -1 when the route starts/ends at a node.
// Phase: 0 = off-road to entry point, 1 = node chain, 2 = along ExitEdge to
// exit point, 3 = exhausted (drive straight to Goal).
type RoadRoute struct {
	Nodes     [32]uint16
	Count     uint8
	Head      uint8
	Planned   uint8
	Phase     uint8
	EntryEdge int32
	EntryT    float32
	ExitEdge  int32
	ExitT     float32
	Goal      WorldPos
}

// SmokeField is a short-lived concealment volume spawned by the
// SmokeAndReverse reflex. ContactSystem applies concealmentMul
// to targets inside Radius.
type SmokeField struct {
	Radius    float32
	ExpiresAt float64
}

// SmokeFieldTTL is shared with the render side: spawn time is reconstructed
// as ExpiresAt - TTL for the ease-in, so the component stays two fields.
const SmokeFieldTTL float32 = 8.0
