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
// bridge-deck Y.
type RoadFollower struct {
	Edge int32
	T    float32
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
