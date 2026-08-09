package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// tickPath runs one MicroPath lifecycle pass with the replan budget spent, so
// the pure bookkeeping (waypoint pops, stuck handling, dirty flags) is
// exercised without touching NavService.
func tickPath(pos *components.WorldPos, mp *components.MicroPath,
	aq *components.ActionQueue, now float32) {
	sys := &MicroPathSystem{}
	budget := 0
	sys.tickUnit(pos, mp, aq, now, &budget)
}

// pathQueue is an ActionQueue holding a single MoveTo at (x, z).
func pathQueue(x, z float32) *components.ActionQueue {
	q := &components.ActionQueue{}
	PushAction(q, moveTo(x, z))
	return q
}

func freshPath(mp *components.MicroPath) {
	mp.BestDistSq = float32(math.MaxFloat32)
}

func TestMicroPathPopsReachedWaypoint(t *testing.T) {
	mp := &components.MicroPath{Count: 2}
	mp.Waypoints[0] = wpAt(10, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	freshPath(mp)
	pos := wpAt(10.3, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 1)
	if mp.Head != 1 {
		t.Errorf("Head = %d, want 1 (inside the 0.6 m arrival radius)", mp.Head)
	}
}

func TestMicroPathKeepsDistantWaypoint(t *testing.T) {
	mp := &components.MicroPath{Count: 2}
	mp.Waypoints[0] = wpAt(10, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	freshPath(mp)
	pos := wpAt(8, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 1)
	if mp.Head != 0 {
		t.Errorf("Head = %d, want 0 — 2 m out is not arrival", mp.Head)
	}
}

// Stacked storeys overlap in XZ: an upper-floor waypoint must not pop from
// underneath, or the climber is stranded a floor below.
func TestMicroPathYBandBlocksPopFromAnotherStorey(t *testing.T) {
	mp := &components.MicroPath{Count: 2}
	mp.Waypoints[0] = components.WorldPos{Local: rl.Vector3{X: 10, Y: 3.5, Z: 0}}
	mp.Waypoints[1] = wpAt(20, 0)
	freshPath(mp)
	pos := components.WorldPos{Local: rl.Vector3{X: 10.2, Y: 0, Z: 0}}
	tickPath(&pos, mp, pathQueue(20, 0), 1)
	if mp.Head != 0 {
		t.Errorf("Head = %d, want 0 — the waypoint is a storey up", mp.Head)
	}
}

// A gate endpoint pops only once the walker has crossed the opening plane;
// radius alone reaches through the wall from the wrong side.
func TestMicroPathGateRequiresCrossingThePlane(t *testing.T) {
	newPath := func() *components.MicroPath {
		mp := &components.MicroPath{Count: 2, GateMask: 1 << 1}
		mp.Waypoints[0] = wpAt(0, 0)
		mp.Waypoints[1] = wpAt(1, 0)
		mp.Head = 1
		freshPath(mp)
		return mp
	}
	// Short of the midpoint but inside the arrival radius: must NOT pop.
	before := newPath()
	pos := wpAt(0.45, 0.2)
	tickPath(&pos, before, pathQueue(20, 0), 1)
	if before.Head != 1 {
		t.Errorf("Head = %d, want 1 — still on the near side of the opening", before.Head)
	}
	// Past the midpoint: pops.
	after := newPath()
	pos = wpAt(0.6, 0.2)
	tickPath(&pos, after, pathQueue(20, 0), 1)
	if after.Head != 2 {
		t.Errorf("Head = %d, want 2 — the opening plane was crossed", after.Head)
	}
}

// The waypoint BEFORE a gate is the opening's mouth: it needs the tight
// radius, or a walker beside the jamb switches to the through-wall target.
func TestMicroPathMouthWaypointNeedsTightRadius(t *testing.T) {
	newPath := func() *components.MicroPath {
		mp := &components.MicroPath{Count: 3, GateMask: 1 << 1}
		mp.Waypoints[0] = wpAt(10, 0)
		mp.Waypoints[1] = wpAt(11, 0)
		mp.Waypoints[2] = wpAt(12, 0)
		freshPath(mp)
		return mp
	}
	loose := newPath()
	pos := wpAt(10.5, 0) // inside 0.6, outside gateMouthRadius 0.3
	tickPath(&pos, loose, pathQueue(20, 0), 1)
	if loose.Head != 0 {
		t.Errorf("Head = %d, want 0 — mouth alignment is not done at 0.5 m", loose.Head)
	}
	tight := newPath()
	pos = wpAt(10.2, 0)
	tickPath(&pos, tight, pathQueue(20, 0), 1)
	if tight.Head != 1 {
		t.Errorf("Head = %d, want 1 — aligned with the mouth", tight.Head)
	}
}

func TestMicroPathClearsWhenQueueEmpties(t *testing.T) {
	mp := &components.MicroPath{Count: 3, Head: 1, Dirty: true, GateMask: 5}
	mp.Waypoints[0] = wpAt(50, 0)
	mp.GoalSnap = wpAt(9, 9)
	freshPath(mp)
	pos := wpAt(0, 0)
	tickPath(&pos, mp, &components.ActionQueue{}, 1)
	if mp.Count != 0 || mp.Head != 0 || mp.Dirty || mp.GateMask != 0 {
		t.Errorf("path not cleared: %+v", struct {
			Count, Head uint8
			Dirty       bool
			GateMask    uint16
		}{mp.Count, mp.Head, mp.Dirty, mp.GateMask})
	}
	if mp.GoalSnap != (components.WorldPos{}) {
		t.Error("GoalSnap survived the clear")
	}
}

func TestMicroPathClearsForNonMoveHead(t *testing.T) {
	mp := &components.MicroPath{Count: 2}
	mp.Waypoints[0] = wpAt(50, 0)
	freshPath(mp)
	q := &components.ActionQueue{}
	PushAction(q, components.Action{Kind: components.ActionStance})
	pos := wpAt(0, 0)
	tickPath(&pos, mp, q, 1)
	if mp.Count != 0 {
		t.Errorf("Count = %d, want the path dropped for a non-MoveTo head", mp.Count)
	}
}

// A body parked on the waypoint is invisible to nav: replanning returns the
// identical path forever, so a close stall pops instead.
func TestMicroPathStuckNearWaypointPops(t *testing.T) {
	mp := &components.MicroPath{Count: 2, BestDistSq: 1.0, LastProgressAt: 0}
	mp.Waypoints[0] = wpAt(1, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	pos := wpAt(0, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 2) // 2 s without progress
	if mp.Head != 1 {
		t.Errorf("Head = %d, want 1 (blocked-pop past the occupied waypoint)", mp.Head)
	}
	if mp.Dirty {
		t.Error("blocked-pop should not also request a replan")
	}
}

// Stalled far from the waypoint is a genuine routing problem: replan, and
// leave a detour hint nav can see.
func TestMicroPathStuckFarRequestsReplanWithAvoidHint(t *testing.T) {
	mp := &components.MicroPath{Count: 2, BestDistSq: 9.0, LastProgressAt: 0}
	mp.Waypoints[0] = wpAt(3, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	pos := wpAt(0, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 2)
	if !mp.Dirty {
		t.Error("a far stall must request a replan")
	}
	if mp.Head != 0 {
		t.Errorf("Head = %d, want 0 — a far stall is not a blocked-pop", mp.Head)
	}
	if mp.AvoidUntil <= 2 {
		t.Errorf("AvoidUntil = %f, want a live TTL past now=2", mp.AvoidUntil)
	}
	// The hint sits one body-length toward the stalled waypoint.
	if !nearlyEqual(mp.AvoidPos.Local.X, microPathAvoidLead) {
		t.Errorf("AvoidPos.X = %f, want %f", mp.AvoidPos.Local.X, microPathAvoidLead)
	}
}

// Gates keep their precise approach: a stall there must replan, never pop.
func TestMicroPathStuckOnGateDoesNotPop(t *testing.T) {
	mp := &components.MicroPath{Count: 2, GateMask: 1, BestDistSq: 1.0, LastProgressAt: 0}
	mp.Waypoints[0] = wpAt(1, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	pos := wpAt(0, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 2)
	if mp.Head != 0 {
		t.Errorf("Head = %d, want 0 — a gate is never blocked-popped", mp.Head)
	}
	if !mp.Dirty {
		t.Error("a stalled gate approach must replan instead")
	}
}

func TestMicroPathProgressResetsStuckTimer(t *testing.T) {
	mp := &components.MicroPath{Count: 2, BestDistSq: 100, LastProgressAt: 0}
	mp.Waypoints[0] = wpAt(3, 0)
	mp.Waypoints[1] = wpAt(20, 0)
	pos := wpAt(0, 0)
	tickPath(&pos, mp, pathQueue(20, 0), 2)
	if mp.Dirty {
		t.Error("closing distance is progress; the stuck timer must not fire")
	}
	if !nearlyEqual(mp.LastProgressAt, 2) {
		t.Errorf("LastProgressAt = %f, want the current time 2", mp.LastProgressAt)
	}
	if !nearlyEqual(mp.BestDistSq, 9) {
		t.Errorf("BestDistSq = %f, want the improved 9", mp.BestDistSq)
	}
}

// An exhausted path with the goal still out of reach must ask for more, or
// the unit walks blind at action.Target forever.
func TestMicroPathExhaustedPathReplansTowardDistantGoal(t *testing.T) {
	mp := &components.MicroPath{Count: 1, Head: 1}
	mp.Waypoints[0] = wpAt(5, 0)
	mp.GoalSnap = wpAt(50, 0)
	freshPath(mp)
	pos := wpAt(5, 0)
	tickPath(&pos, mp, pathQueue(50, 0), 1)
	if !mp.Dirty {
		t.Error("exhausted path far from the goal must set Dirty")
	}
}

func TestMicroPathExhaustedPathAtGoalStaysQuiet(t *testing.T) {
	mp := &components.MicroPath{Count: 1, Head: 1}
	mp.Waypoints[0] = wpAt(50, 0)
	mp.GoalSnap = wpAt(50, 0)
	freshPath(mp)
	pos := wpAt(50.5, 0) // inside the 1 m threshold
	tickPath(&pos, mp, pathQueue(50, 0), 1)
	if mp.Dirty {
		t.Error("standing on the goal must not churn replans")
	}
}

func TestClearMicroPathResetsFields(t *testing.T) {
	mp := &components.MicroPath{
		Count: 5, Head: 3, Dirty: true, GateMask: 0xFF,
		GoalSnap: wpAt(1, 2),
	}
	clearMicroPath(mp)
	if mp.Count != 0 || mp.Head != 0 || mp.Dirty || mp.GateMask != 0 {
		t.Errorf("clearMicroPath left state behind: %+v", *mp)
	}
	if mp.GoalSnap != (components.WorldPos{}) {
		t.Error("GoalSnap not reset")
	}
}
