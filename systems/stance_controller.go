package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Minimum gap (s) between autonomous stance changes — keeps the controller
// from flapping Prone↔Stand when Threat.Total hovers near a band edge.
const stanceAnimLock float32 = 0.5

// Desired speed above this disallows Prone (prone MaxSpeed = 1.5 m/s).
const stanceProneSpeedCap float32 = 1.5

// StanceControllerSystem maps Threat.State to a target stance band and
// writes Stance.Code under animation lock. Gates:
//   - BehaviorRules.AllowAutoStance (off for sniper / ATGunner).
//   - No active StanceOverride (player Z/X/C wins).
//   - sys.elapsed ≥ Stance.LockUntil.
//
// Movement gate: Motion.Speed > stanceProneSpeedCap clamps target to Crouch.
type StanceControllerSystem struct {
	filter         *ecs.Filter4[components.Unit, components.Stance, components.Threat, components.Motion]
	memberMap      *ecs.Map[components.SquadMember]
	behaviorMap    *ecs.Map[components.BehaviorRules]
	overrideMap    *ecs.Map[components.StanceOverride]
	movementMap    *ecs.Map[components.MovementProfile]
	orderMoveOverr *ecs.Map[components.OrderParamMovementProfile]
	orderQueueMap *ecs.Map[components.OrderQueueHead]
	// ModeSuppressed forces Prone regardless of band / lock / doctrine.
	blackboardMap *ecs.Map[components.LocalBlackboard]
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
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
}

func (StanceControllerSystem) Name() string { return "stance_controller" }

func (StanceControllerSystem) LODPolicy() core.LODPolicy {
	// Active-only: filter is not tier-scoped (see ContactSystem note).
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *StanceControllerSystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)

	q := sys.filter.Query()
	for q.Next() {
		ent := q.Entity()
		_, stance, threat, mot := q.Get()

		if ov := sys.overrideMap.Get(ent); ov != nil && ov.Until > now {
			continue
		}
		if !sys.allowAutoStance(ent) {
			continue
		}
		// ModeSuppressed bypasses the animation lock (reactive collapse to
		// Prone) but still respects the moving-too-fast gate below.
		suppressedNow := false
		if b := sys.blackboardMap.Get(ent); b != nil && b.CurrentMode == components.ModeSuppressed {
			suppressedNow = true
		}

		if !suppressedNow && now < stance.LockUntil {
			continue
		}

		target := sys.targetStance(ent, threat.State)
		if suppressedNow {
			target = components.StanceProne
		}

		if target == components.StanceProne && mot.Speed > stanceProneSpeedCap {
			target = components.StanceCrouch
		}

		if stance.Code != target {
			stance.Code = target
			stance.LockUntil = now + stanceAnimLock
		}
	}
}

// allowAutoStance returns the squad's BehaviorRules.AllowAutoStance, true
// by default for soloists.
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

// targetStance maps Threat.State to the desired stance. Safe/Vigilant falls
// through to the squad's standing MovementProfile.Stance.
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

// squadStandingStance resolves the idle stance: per-order override > squad
// MovementProfile.Stance > StanceStand. Same precedence as
// UnitMovementSystem.resolveProfile.
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
