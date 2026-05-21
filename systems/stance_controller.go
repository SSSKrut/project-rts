package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// stanceAnimLock - minimum gap (s) between autonomous stance changes. Keeps
// the controller from flapping Prone <-> Stand when Threat.Total hovers near
// a band edge.
const stanceAnimLock float32 = 0.5

// stanceProneSpeedCap - desired speed above this disallows Prone (the prone
// MaxSpeed in StanceSpec is 1.5 m/s; trying to move faster while prone
// would just clamp speed). Matches the design budget in plan C.2.
const stanceProneSpeedCap float32 = 1.5

// StanceControllerSystem is Phase 17 M17.C autonomy. Each tick maps the
// unit's Threat.State into a target stance band and writes Stance.Code (with
// an animation-lock gate) when:
//
//   - BehaviorRules.AllowAutoStance is true (player can lock it off per
//     doctrine - sniper / ATGunner who stays prone regardless of threat).
//   - StanceOverride is absent / expired (player Z/X/C wins).
//   - sys.elapsed >= Stance.LockUntil (anim lock against per-tick flap).
//
// Movement gate: when the unit's current Motion.Speed is above
// stanceProneSpeedCap, target is clamped to Crouch even if Threat.State
// asks for Prone (prone immobilises - design plan C.2).
//
// Runs serial; mutations are scalar field writes on Stance, no archetype
// changes.
type StanceControllerSystem struct {
	filter         *ecs.Filter4[components.Unit, components.Stance, components.Threat, components.Motion]
	memberMap      *ecs.Map[components.SquadMember]
	behaviorMap    *ecs.Map[components.BehaviorRules]
	overrideMap    *ecs.Map[components.StanceOverride]
	movementMap    *ecs.Map[components.MovementProfile]
	orderMoveOverr *ecs.Map[components.OrderParamMovementProfile]
	orderQueueMap  *ecs.Map[components.OrderQueueHead]
	elapsed        float32
}

func NewStanceControllerSystem() *StanceControllerSystem {
	return &StanceControllerSystem{}
}

func (sys *StanceControllerSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.Stance, components.Threat, components.Motion](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.behaviorMap = ecs.NewMap[components.BehaviorRules](w)
	sys.overrideMap = ecs.NewMap[components.StanceOverride](w)
	sys.movementMap = ecs.NewMap[components.MovementProfile](w)
	sys.orderMoveOverr = ecs.NewMap[components.OrderParamMovementProfile](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
}

func (StanceControllerSystem) Name() string { return "stance_controller" }

func (StanceControllerSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *StanceControllerSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed

	q := sys.filter.Query()
	for q.Next() {
		ent := q.Entity()
		_, stance, threat, mot := q.Get()

		// Gate: player override takes priority.
		if ov := sys.overrideMap.Get(ent); ov != nil && ov.Until > now {
			continue
		}
		// Gate: autonomy disabled per doctrine (sniper / ATGunner).
		if !sys.allowAutoStance(ent) {
			continue
		}
		// Gate: animation lock - hold the last change for at least
		// stanceAnimLock seconds before flipping again.
		if now < stance.LockUntil {
			continue
		}

		target := sys.targetStance(ent, threat.State)

		// Movement gate: prone is incompatible with > 1.5 m/s. If the unit
		// is actually moving that fast, snap up to Crouch.
		if target == components.StanceProne && mot.Speed > stanceProneSpeedCap {
			target = components.StanceCrouch
		}

		if stance.Code != target {
			stance.Code = target
			stance.LockUntil = now + stanceAnimLock
		}
	}
}

// allowAutoStance reads the unit's squad BehaviorRules. Default true for
// soloists / units without a squad - they fall through to the autonomous
// path. Squad doctrine (Sniper / ATGunner) can flip it off.
func (sys *StanceControllerSystem) allowAutoStance(unit ecs.Entity) bool {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return true
	}
	br := sys.behaviorMap.Get(mem.Squad)
	if br == nil {
		return true
	}
	return br.AllowAutoStance
}

// targetStance maps Threat.State to the desired stance band. Safe/Vigilant
// falls through to the squad's standing MovementProfile.Stance so a Patrol
// doctrine staying in Crouch still gets respected when the threat clears.
func (sys *StanceControllerSystem) targetStance(unit ecs.Entity, state components.ThreatState) components.StanceCode {
	switch state {
	case components.ThreatThreatened:
		return components.StanceProne
	case components.ThreatAlerted:
		return components.StanceCrouch
	default:
		return sys.squadStandingStance(unit)
	}
}

// squadStandingStance resolves the unit's idle stance: per-order override
// (Phase 13 OrderParamMovementProfile.Stance) > squad standing
// MovementProfile.Stance > StanceStand fallback. Matches the same precedence
// UnitMovementSystem.resolveProfile follows.
func (sys *StanceControllerSystem) squadStandingStance(unit ecs.Entity) components.StanceCode {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return components.StanceStand
	}
	if head := sys.orderQueueMap.Get(mem.Squad); head != nil && head.First != (ecs.Entity{}) {
		if ov := sys.orderMoveOverr.Get(head.First); ov != nil {
			return ov.Profile.Stance
		}
	}
	if mp := sys.movementMap.Get(mem.Squad); mp != nil {
		return mp.Stance
	}
	return components.StanceStand
}
