package components

// VehicleKind indexes VehicleSpecs. Classes differ by spec row + weapon
// set only; behaviour code is shared (PHASE-19 P2).
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

// RoadFollower is the road-graph locomotion state (Phase 19 M2). Edge is an
// index into RoadGraph.Edges, -1 while off-road; T is the hull projection
// param along the edge. Always present on vehicles (factory-stamped);
// GroundStick reads it for bridge-deck Y.
type RoadFollower struct {
	Edge int32
	T    float32
}

// RoadRoute is the planned node sequence for the head MoveTo action
// (Phase 19 M2). Planned != 0 means the routing decision was made for Goal
// (Count == 0 → drive straight off-road); a head action whose target differs
// from Goal triggers a replan.
type RoadRoute struct {
	Nodes   [32]uint16
	Count   uint8
	Head    uint8
	Planned uint8
	Goal    WorldPos
}

// SmokeField is a short-lived concealment volume spawned by the
// SmokeAndReverse reflex (Phase 19 M4). ContactSystem applies concealmentMul
// to targets inside Radius.
type SmokeField struct {
	Radius    float32
	ExpiresAt float64
}
