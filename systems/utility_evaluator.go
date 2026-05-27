package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// UtilityEvaluatorSystem — Phase 17.8 M17.8.2.
//
// Per-unit Utility-AI evaluator. Each unit is re-scored once per ~0.5s
// (bucket-distributed across UtilityBucketCount frames so total cost is
// amortised). For every enabled ActionMode the system computes a score
// from the unit's perception state (Threat, Awareness, cover assignment,
// ammo, formation goal, recent fire) and picks the highest-scoring mode.
//
// Hysteresis guards the switch:
//   - newScore must beat currentScore by ModeSwitchScoreDelta, AND
//   - the unit must have spent at least MinModeDurationSec in its current
//     mode (bypassed by UtilityEmergencyDelta for emergency transitions
//     like sudden Suppressed).
//
// Writes: LocalBlackboard.CurrentMode, LastModeSwitch, Reason (via
// SetReason). Other AI executor systems (WeaponSystem, StanceController,
// MicroPath goal selection) read CurrentMode for mode-specific behavior —
// wired in M17.8.3.
//
// Score functions live in utility_evaluator_scoring.go. Adding a new mode
// = bump ModeCount + spec row + scoring function + (optional) executor
// wiring. No switch on ActionMode here — it loops over UtilitySpecs.
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

	clock float32 // session-time; set via SetClock once per frame from main.go
}

// NewUtilityEvaluatorSystem allocates the system stub. InitUI populates
// the handles after the World exists.
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
}

func (UtilityEvaluatorSystem) Name() string { return "utility_evaluator" }

// LODPolicy: Active per-tick (with internal bucket gate), Relevant once
// per 500 ms (covers dormant-bucket fallback), Dormant disabled (off-screen
// units don't need mode re-evaluation until they come back into focus).
func (UtilityEvaluatorSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// SetClock updates the session clock used for hysteresis (LastModeSwitch
// comparisons). main.go calls this once per frame before app.Tick — same
// pattern as SquadService.SetClock.
func (sys *UtilityEvaluatorSystem) SetClock(t float32) {
	sys.clock = t
}

// Update runs the bucket-gated evaluation for one tick.
func (sys *UtilityEvaluatorSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	q := sys.filter.Query()
	for q.Next() {
		_, blackboard := q.Get()
		ent := q.Entity()
		// Bucket gate: hash entity into one of UtilityBucketCount slots and
		// evaluate only on the matching frame index. Spreads cost evenly.
		if (uint32(ent.ID())+ctx.FrameIndex)%components.UtilityBucketCount != 0 {
			continue
		}
		uctx := sys.buildContext(ent, blackboard)
		sys.evaluateAndApply(blackboard, &uctx)
	}
}

// evaluateAndApply scores every enabled mode against the context, picks
// the winner, and writes CurrentMode + LastModeSwitch + Reason if the
// hysteresis gate passes. No-op when the winner matches the current mode
// (still updates Reason in case the dominant signal changed).
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

	// Hysteresis: same mode = just refresh reason and exit.
	if bestMode == b.CurrentMode {
		applyReason(b, bestMode, uctx)
		return
	}

	// Score-delta gate.
	if bestScore < currentScore+components.ModeSwitchScoreDelta {
		applyReason(b, b.CurrentMode, uctx)
		return
	}

	// Dwell-time gate — bypassed for emergency-class transitions.
	dwell := sys.clock - b.LastModeSwitch
	emergency := bestScore >= currentScore+components.UtilityEmergencyDelta
	if !emergency && dwell < components.MinModeDurationSec {
		applyReason(b, b.CurrentMode, uctx)
		return
	}

	// Commit the switch.
	b.CurrentMode = bestMode
	b.LastModeSwitch = sys.clock
	applyReason(b, bestMode, uctx)
}
