package systems

import (
	"math"
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// CirclePatrolSystem drives any entity with CirclePatrol+ActionQueue around
// its Center. Recomputes target every Active tick and refreshes the action
// queue head so the unit smoothly follows the moving waypoint. Phase 18.5
// test utility — used by wildlife and hostile-dummy spawns.
type CirclePatrolSystem struct {
	filter *ecs.Filter3[components.CirclePatrol, components.WorldPos, components.ActionQueue]
}

func NewCirclePatrolSystem() *CirclePatrolSystem { return &CirclePatrolSystem{} }

func (sys *CirclePatrolSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter3[components.CirclePatrol, components.WorldPos, components.ActionQueue](w)
}

func (CirclePatrolSystem) Name() string { return "circle_patrol" }

func (CirclePatrolSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: 1 * time.Second,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *CirclePatrolSystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)
	q := sys.filter.Query()
	for q.Next() {
		patrol, _, queue := q.Get()
		if patrol.RadiusM <= 0 || patrol.Speed <= 0 {
			continue
		}
		angVel := patrol.Speed / patrol.RadiusM
		angle := patrol.Phase + now*angVel
		target := patrol.Center
		target.Local.X += patrol.RadiusM * float32(math.Cos(float64(angle)))
		target.Local.Z += patrol.RadiusM * float32(math.Sin(float64(angle)))
		ClearActions(queue)
		PushAction(queue, components.Action{Kind: components.ActionMoveTo, Target: target})
	}
	q.Close()
}
