package components

import rl "github.com/gen2brain/raylib-go/raylib"

// MicroPathSize - waypoint capacity. A decimated A* path across a 60 m route
// peaks around 10-12 waypoints; 16 leaves headroom for the smaller
// cover-via / kneel-then-run inserts a later track will splice in.
const MicroPathSize = 16

// MicroPath is the per-unit waypoint stream produced by MicroPathSystem from
// the unit's current ActionQueue MoveTo goal. UnitMovement steers toward
// Waypoints[Head] each tick; once it gets within microPathArrivalRadius the
// system advances Head. The buffer is rewritten in place when the goal
// shifts > microPathGoalShift or the unit hasn't made progress in
// microPathStuckTime seconds (anti-thrash).
//
// GoalSnap is the goal at the moment the path was planned; comparing it to
// the live ActionQueue head target gives a cheap "did the player move the
// pin" check. ReplanAt throttles replans to one per microPathReplanCooldown
// seconds per unit so a jittery goal can't burn the path budget.
type MicroPath struct {
	Waypoints      [MicroPathSize]WorldPos
	Head           uint8    // index of next waypoint to reach
	Count          uint8    // valid waypoint count (entries [0..Count) live)
	Dirty          bool     // queued for replan
	GoalSnap       WorldPos // goal at the time of the current path's plan
	ReplanAt       float32  // earliest session-time the next replan may run
	LastProgressAt float32  // session-time of the last position step > stuckDist
	LastPos        rl.Vector3
}
