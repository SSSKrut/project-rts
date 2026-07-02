package components

// MicroPathSize - waypoint capacity. A decimated A* path across a 60 m route
// peaks around 10-12 waypoints; 16 leaves headroom for cover-via /
// kneel-then-run inserts.
const MicroPathSize = 16

// MicroPath is the per-unit waypoint stream produced by MicroPathSystem from
// the unit's current ActionQueue MoveTo goal. UnitMovement steers toward
// Waypoints[Head] each tick; once it gets within microPathArrivalRadius the
// system advances Head. The buffer is rewritten in place when the goal
// shifts > microPathGoalShift or the unit hasn't made progress in
// microPathStuckTime seconds (anti-thrash).
//
// GoalSnap is the goal at the moment the path was planned. ReplanAt throttles
// replans so a jittery goal can't burn the path budget.
type MicroPath struct {
	Waypoints      [MicroPathSize]WorldPos
	Head           uint8    // index of next waypoint to reach
	Count          uint8    // valid waypoint count (entries [0..Count) live)
	Dirty          bool     // queued for replan
	GoalSnap       WorldPos // goal at the time of the current path's plan
	ReplanAt       float32  // earliest session-time the next replan may run
	LastProgressAt float32  // session-time of the last waypoint-distance improvement
	// BestDistSq: closest squared distance to Waypoints[Head] achieved since
	// LastProgressAt. Progress = getting CLOSER to the waypoint — raw
	// displacement lets a wall-pinned walker oscillate (±0.3 m along the
	// wall) hard enough to keep resetting the stuck timer forever.
	BestDistSq float32
	// GateMask: bit k set ⇒ Waypoints[k] is a transition endpoint (door /
	// stairs / wing junction). Gate waypoints advance only once the walker
	// has crossed the opening plane — popping one by radius alone from the
	// wrong side of the wall leaves the walker steering at the next
	// interior waypoint through solid wall beside the opening, sliding
	// along it forever. Gates at index 0 (post-replan; the pair went with
	// the trimmed leading waypoint) keep the plain radius: strictness
	// there clogs doorways under ORCA crowds, and the progress-based stuck
	// detector breaks any residual wrong-side pin via replan.
	GateMask uint16
}
