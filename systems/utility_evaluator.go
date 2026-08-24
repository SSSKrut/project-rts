package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// UtilityEvaluatorSystem is the per-unit Utility-AI evaluator. Each unit is
// re-scored once per ~0.5 s (bucket-distributed across UtilityBucketCount
// frames). Hysteresis guards the switch: newScore must beat currentScore by
// ModeSwitchScoreDelta AND the unit must have spent ≥ MinModeDurationSec in
// the current mode (bypassed by UtilityEmergencyDelta for sudden transitions).
//
// Writes LocalBlackboard.CurrentMode / LastModeSwitch / Reason. Other AI
// executors read CurrentMode for mode-specific behavior. Adding a new mode
// = bump ModeCount + spec row + scoring function — no switch on ActionMode
// here.
type UtilityEvaluatorSystem struct {
	filter         *ecs.Filter2[components.Unit, components.LocalBlackboard]
	blackboardMap  *ecs.Map[components.LocalBlackboard]
	threatMap      *ecs.Map[components.Threat]
	awarenessMap   *ecs.Map[components.Awareness]
	equipmentMap   *ecs.Map[components.Equipment]
	weaponMap      *ecs.Map[components.Weapon]
	motionMap      *ecs.Map[components.Motion]
	posMap         *ecs.Map[components.WorldPos]
	squadMemberMap *ecs.Map[components.SquadMember]
	orderQueueMap  *ecs.Map[components.OrderQueueHead]
	factionMap     *ecs.Map[components.Faction]
	aircraftMap    *ecs.Map[components.Aircraft]

	clock float32
}

func NewUtilityEvaluatorSystem() *UtilityEvaluatorSystem {
	return &UtilityEvaluatorSystem{}
}

func (sys *UtilityEvaluatorSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter2[components.Unit, components.LocalBlackboard](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.awarenessMap = ecs.NewMap[components.Awareness](w)
	sys.equipmentMap = ecs.NewMap[components.Equipment](w)
	sys.weaponMap = ecs.NewMap[components.Weapon](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.aircraftMap = ecs.NewMap[components.Aircraft](w)
}

func (UtilityEvaluatorSystem) Name() string { return "utility_evaluator" }

func (UtilityEvaluatorSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *UtilityEvaluatorSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	sys.clock = float32(ctx.SimNow)
	q := sys.filter.Query()
	for q.Next() {
		_, blackboard := q.Get()
		ent := q.Entity()
		// Bucket gate: only evaluate on the matching frame index; spreads
		// cost evenly across UtilityBucketCount frames.
		if (uint32(ent.ID())+ctx.FrameIndex)%components.UtilityBucketCount != 0 {
			continue
		}
		uctx := sys.buildContext(ent, blackboard)
		sys.evaluateAndApply(blackboard, &uctx)
	}
}

// evaluateAndApply scores every enabled mode, picks the winner, and writes
// CurrentMode/LastModeSwitch/Reason if the hysteresis gate passes.
func (sys *UtilityEvaluatorSystem) evaluateAndApply(b *components.LocalBlackboard, uctx *UtilityContext) {
	currentScore := scoreForMode(b.CurrentMode, uctx)
	bestMode := b.CurrentMode
	bestScore := currentScore
	for _, spec := range components.UtilitySpecs {
		if !spec.Enabled {
			continue
		}
		s := scoreForMode(spec.Mode, uctx)
		if s > bestScore {
			bestScore = s
			bestMode = spec.Mode
		}
	}

	if bestMode == b.CurrentMode {
		applyReason(b, bestMode, uctx)
		return
	}

	if bestScore < currentScore+components.ModeSwitchScoreDelta {
		applyReason(b, b.CurrentMode, uctx)
		return
	}

	// Dwell-time gate; bypassed for emergency-class transitions.
	dwell := sys.clock - b.LastModeSwitch
	emergency := bestScore >= currentScore+components.UtilityEmergencyDelta
	if !emergency && dwell < components.MinModeDurationSec {
		applyReason(b, b.CurrentMode, uctx)
		return
	}

	b.CurrentMode = bestMode
	b.LastModeSwitch = sys.clock
	applyReason(b, bestMode, uctx)
}
