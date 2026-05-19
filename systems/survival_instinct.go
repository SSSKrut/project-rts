package systems

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SurvivalInstinctSystem pulls suppressed units toward cover. Reads Suppression
// (written by WeaponSystem) + the live CoverSlot scene. Writes TacticalOverride
// markers and overrides the unit's ActionQueue with a MoveTo against the
// chosen cover slot.
//
// Two passes per tick:
//
//  1. ScatterProtocol - aggregates per-squad suppression into a 3 s rolling
//     window on SquadState. A spike (delta > scrambleDeltaTrigger) flips the
//     squad into Scrambling and aggregates a squad-wide ThreatDir for members
//     whose own Suppression.ThreatDir is still zero. Recovery is delta below
//     scrambleRecoveryDelta sustained for scrambleRecoveryDuration.
//
//  2. Per-unit acquire / clear - the unit acquires cover when its individual
//     Suppression crosses BehaviorRules.SuppressionThreshold, or when the
//     squad is Scrambling (effective threshold drops to 0). Clear conditions
//     remain in M15.A.0: Until expired, suppression below clear threshold for
//     siClearLowDuration straight, or an explicit player order arrives.
//
// FormationSystem skips members carrying TacticalOverride so this system's
// ActionQueue writes survive past the next formation tick. UnitMovement is
// override-blind; it just executes the queued action.
type SurvivalInstinctSystem struct {
	unitFilter     *ecs.Filter3[components.Unit, components.WorldPos, components.Suppression]
	slotFilter     *ecs.Filter2[components.WorldPos, components.CoverSlot]
	squadFilter    *ecs.Filter2[components.Squad, components.CommandRoster]
	queueMap       *ecs.Map[components.ActionQueue]
	overrideMap    *ecs.Map[components.TacticalOverride]
	memberMap      *ecs.Map[components.SquadMember]
	behaviorMap    *ecs.Map[components.BehaviorRules]
	suppressionMap *ecs.Map[components.Suppression]
	squadStateMap  *ecs.Map[components.SquadState]
	posMap         *ecs.Map[components.WorldPos]
	eventLogRes    ecs.Resource[components.EventLog]

	world *ecs.World

	slots      []siCoverSlot
	acquires   []siAcquireOp
	clears     []ecs.Entity
	stateAdds  []siStateAdd
	squadInfo  map[ecs.Entity]siSquadInfo

	elapsed float32
}

// siCoverSlot is the snapshot of one live CoverSlot entity used for utility
// evaluation. World-space XZ + outward direction + quality + the slot entity
// itself. Filled once per tick before the unit walk.
type siCoverSlot struct {
	ent      ecs.Entity
	worldX   float32
	worldZ   float32
	worldPos components.WorldPos
	originX  float32
	originZ  float32
	quality  float32
}

// siAcquireOp is one "give this unit cover" decision queued for the post-pass.
// We can't mutate archetypes (Add[TacticalOverride]) inside the unit filter
// walk so we batch and apply after.
type siAcquireOp struct {
	unit ecs.Entity
	slot ecs.Entity
	pos  components.WorldPos
}

// siStateAdd is a "this squad needs SquadState attached" entry. Same archetype
// constraint as siAcquireOp - batched and applied after the query.
type siStateAdd struct {
	squad ecs.Entity
	init  components.SquadState
}

// siSquadInfo is the per-squad snapshot the unit pass reads to evaluate
// effective threshold + threat direction during scramble. Built by the
// ScatterProtocol pass; read-only for the rest of Update.
type siSquadInfo struct {
	code      components.SquadStateCode
	threatDir rl.Vector3
}

// siSuppressionDefault - fallback threshold when no BehaviorRules visible on
// the unit's squad (soloists, legacy spawns).
const siSuppressionDefault float32 = 0.45

// siClearLowDuration - sustained sub-clearThreshold time needed before the
// marker is removed. Hysteresis against firing-burst gaps.
const siClearLowDuration float32 = 3.0

// siSafetyUntil - safety timeout the marker carries; force-clear if still set
// after this many seconds. Prevents the unit from being permanently stuck on
// a bad cover assignment.
const siSafetyUntil float32 = 30.0

// siCoverSearchRadius - max distance (m) from the unit to a candidate slot.
// One chunk-ish; anything farther isn't a useful cover decision.
const siCoverSearchRadius float32 = 30.0

// scrambleDeltaTrigger - rise in squad-average Suppression over the rolling
// window that flips the squad into SquadStateScrambling. 0.4 means "the
// average member just gained nearly half-suppression in 3 s" - typical of an
// MG burst landing in the middle of formation.
const scrambleDeltaTrigger float32 = 0.4

// scrambleRecoveryDelta / scrambleRecoveryDuration - if the rolling delta
// drops below scrambleRecoveryDelta and stays there for scrambleRecoveryDuration
// seconds, the squad returns to Engaged. Individual members may still hold
// their own M15.A.0 overrides while suppressed.
const scrambleRecoveryDelta float32 = 0.1
const scrambleRecoveryDuration float32 = 15.0

// scrambleSafetyDuration - force-clear the Scrambling code after this many
// seconds even if delta hasn't recovered. Prevents pathological state stickiness
// (e.g. constant fire but the unit pass already covers everyone).
const scrambleSafetyDuration float32 = 60.0

// NewSurvivalInstinctSystem constructs the empty system. Wire handles via
// InitUI after the World exists.
func NewSurvivalInstinctSystem() *SurvivalInstinctSystem {
	return &SurvivalInstinctSystem{
		slots:     make([]siCoverSlot, 0, 64),
		acquires:  make([]siAcquireOp, 0, 16),
		clears:    make([]ecs.Entity, 0, 16),
		stateAdds: make([]siStateAdd, 0, 4),
		squadInfo: make(map[ecs.Entity]siSquadInfo, 8),
	}
}

func (sys *SurvivalInstinctSystem) InitUI(w *ecs.World) {
	sys.world = w
	sys.unitFilter = ecs.NewFilter3[components.Unit, components.WorldPos, components.Suppression](w)
	sys.slotFilter = ecs.NewFilter2[components.WorldPos, components.CoverSlot](w)
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.overrideMap = ecs.NewMap[components.TacticalOverride](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.behaviorMap = ecs.NewMap[components.BehaviorRules](w)
	sys.suppressionMap = ecs.NewMap[components.Suppression](w)
	sys.squadStateMap = ecs.NewMap[components.SquadState](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.eventLogRes = ecs.NewResource[components.EventLog](w)
}

func (SurvivalInstinctSystem) Name() string { return "survival_instinct" }

func (SurvivalInstinctSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *SurvivalInstinctSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed

	// Pass 1 - snapshot live cover slots. World-space XZ for cheap distance
	// compares; cover OriginDir copied verbatim.
	sys.slots = sys.slots[:0]
	qS := sys.slotFilter.Query()
	for qS.Next() {
		pos, slot := qS.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		sys.slots = append(sys.slots, siCoverSlot{
			ent:      qS.Entity(),
			worldX:   wx,
			worldZ:   wz,
			worldPos: *pos,
			originX:  slot.OriginDir.X,
			originZ:  slot.OriginDir.Z,
			quality:  float32(slot.Quality) * (1.0 / 255.0),
		})
	}

	// Pass 1.5 - ScatterProtocol. Per squad, sample roster suppression, push
	// into the rolling history, decide Idle/Engaged/Scrambling. The squadInfo
	// map is read by Pass 2 to apply effective-threshold + fallback-threatDir
	// when scrambling.
	sys.runScatterProtocol(now)

	// Build claimed-slot set from existing overrides so the acquire path can
	// apply occupancyPenalty. One read per claimed unit.
	claimed := map[ecs.Entity]ecs.Entity{} // slot -> owning unit
	qOv := sys.unitFilter.Query()
	for qOv.Next() {
		ent := qOv.Entity()
		if ov := sys.overrideMap.Get(ent); ov != nil && ov.AssignedSlot != (ecs.Entity{}) {
			claimed[ov.AssignedSlot] = ent
		}
	}

	sys.acquires = sys.acquires[:0]
	sys.clears = sys.clears[:0]

	// Pass 2 - per-unit decision. We can mutate scalar fields on the
	// TacticalOverride pointer in-place; archetype changes (Add / Remove) are
	// queued for the post-pass.
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, supp := q.Get()
		threshold := sys.thresholdFor(ent)
		clearThreshold := threshold * 0.6
		scrambling, squadThreat := sys.squadScrambleContext(ent)
		// When the squad is Scrambling every member acquires cover, even ones
		// whose own Suppression is still cold. Fallback threatDir comes from
		// the squad-level aggregate computed in Pass 1.5.
		effectiveThreshold := threshold
		if scrambling {
			effectiveThreshold = 0
		}
		threatDir := supp.ThreatDir
		if threatDir.X == 0 && threatDir.Z == 0 && scrambling {
			threatDir = squadThreat
		}

		existing := sys.overrideMap.Get(ent)
		if existing == nil {
			if supp.Level <= effectiveThreshold && !scrambling {
				continue
			}
			slot, slotPos, found := sys.pickCover(ent, pos, threatDir, claimed)
			if !found {
				continue
			}
			claimed[slot] = ent
			sys.acquires = append(sys.acquires, siAcquireOp{unit: ent, slot: slot, pos: slotPos})
			continue
		}

		// Override held - decide whether to keep or clear.
		if now > existing.Until {
			sys.clears = append(sys.clears, ent)
			continue
		}
		if supp.Level < clearThreshold && !scrambling {
			if existing.LowSuppSince == 0 {
				existing.LowSuppSince = now
			} else if now-existing.LowSuppSince >= siClearLowDuration {
				sys.clears = append(sys.clears, ent)
			}
		} else {
			existing.LowSuppSince = 0
		}
	}

	// Pass 3 - apply ECS mutations. Order: clears first (so a cleared unit
	// can be re-acquired on the same tick if it's still suppressed; rare but
	// possible if Until expired while threat is fresh), then acquires.
	for _, e := range sys.clears {
		if sys.overrideMap.Has(e) {
			sys.overrideMap.Remove(e)
		}
	}
	for _, op := range sys.acquires {
		if !sys.world.Alive(op.unit) {
			continue
		}
		aq := sys.queueMap.Get(op.unit)
		if aq == nil {
			continue
		}
		ClearActions(aq)
		PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: op.pos})
		if sys.overrideMap.Has(op.unit) {
			sys.overrideMap.Remove(op.unit)
		}
		sys.overrideMap.Add(op.unit, &components.TacticalOverride{
			Reason:       components.TacticalOverrideUnderFire,
			Until:        now + siSafetyUntil,
			LowSuppSince: 0,
			AssignedSlot: op.slot,
		})
	}
}

// pushSuppressionEvent records a SuppressionStart event when a squad first
// flips to Scrambling. Squad center is best-effort - we use the first live
// member's WorldPos as a representative anchor since SquadCenter would
// require another roster walk in the hot path.
func (sys *SurvivalInstinctSystem) pushSuppressionEvent(squad ecs.Entity, now float32) {
	log := sys.eventLogRes.Get()
	if log == nil {
		return
	}
	roster := sys.rosterMember(squad)
	var pos components.WorldPos
	if roster != (ecs.Entity{}) {
		if p := sys.posMap.Get(roster); p != nil {
			pos = *p
		}
	}
	log.Push(components.EventEntry{
		Kind:  components.EventSuppressionStart,
		At:    now,
		Pos:   pos,
		Squad: squad,
		Text:  "Squad suppressed",
	})
}

// rosterMember returns the first live member of `squad` (commander preferred).
// Used as a positional anchor for events that need a representative location.
func (sys *SurvivalInstinctSystem) rosterMember(squad ecs.Entity) ecs.Entity {
	if !sys.world.Alive(squad) {
		return ecs.Entity{}
	}
	// Walk via SquadFilter is not handy here; cheap to re-resolve via
	// squadStateMap path is no help (it doesn't carry roster). Use the
	// roster map directly through reflection-free path: we already touched
	// roster in the squadFilter pass and stored squadInfo, but not the first
	// member. Just iterate the small squadFilter again.
	q := sys.squadFilter.Query()
	for q.Next() {
		if q.Entity() != squad {
			continue
		}
		_, roster := q.Get()
		for i := uint8(0); i < roster.Count; i++ {
			mem := roster.Members[i]
			if mem != (ecs.Entity{}) && sys.world.Alive(mem) {
				return mem
			}
		}
		break
	}
	return ecs.Entity{}
}

// thresholdFor returns the squad's BehaviorRules.SuppressionThreshold, or the
// system default when the unit is a soloist or the squad lacks the rule.
func (sys *SurvivalInstinctSystem) thresholdFor(unit ecs.Entity) float32 {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return siSuppressionDefault
	}
	if br := sys.behaviorMap.Get(mem.Squad); br != nil && br.SuppressionThreshold > 0 {
		return br.SuppressionThreshold
	}
	return siSuppressionDefault
}

// squadScrambleContext returns (scrambling, threatDir) for the unit's squad.
// Soloists get (false, zero). When the squad is Scrambling, threatDir is the
// squad-aggregate computed in runScatterProtocol.
func (sys *SurvivalInstinctSystem) squadScrambleContext(unit ecs.Entity) (bool, rl.Vector3) {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return false, rl.Vector3{}
	}
	info, ok := sys.squadInfo[mem.Squad]
	if !ok {
		return false, rl.Vector3{}
	}
	return info.code == components.SquadStateScrambling, info.threatDir
}

// runScatterProtocol walks every squad, lazy-adds SquadState when missing,
// pushes the latest squad-average Suppression into the history ring, and runs
// the Idle / Engaged / Scrambling transition logic. squadInfo is repopulated
// from scratch each tick so Pass 2 reads a consistent view.
func (sys *SurvivalInstinctSystem) runScatterProtocol(now float32) {
	clear(sys.squadInfo)
	sys.stateAdds = sys.stateAdds[:0]

	q := sys.squadFilter.Query()
	for q.Next() {
		squad := q.Entity()
		_, roster := q.Get()
		if roster.Count == 0 {
			continue
		}

		avgSupp, threatDir := sys.aggregateSquadSuppression(roster)

		state := sys.squadStateMap.Get(squad)
		if state == nil {
			// Lazy add - the squad was spawned before this system existed.
			init := components.SquadState{Code: components.SquadStateIdle}
			sys.stateAdds = append(sys.stateAdds, siStateAdd{squad: squad, init: init})
			sys.squadInfo[squad] = siSquadInfo{code: components.SquadStateIdle, threatDir: threatDir}
			continue
		}

		// Push current avg into history ring.
		state.SuppHistory[state.HistHead] = avgSupp
		state.HistHead = (state.HistHead + 1) % components.SquadSuppressionWindow
		if state.HistCount < components.SquadSuppressionWindow {
			state.HistCount++
		}

		delta := sys.windowDelta(state)

		// State transitions.
		switch state.Code {
		case components.SquadStateIdle:
			if avgSupp > 0 {
				state.Code = components.SquadStateEngaged
			}
		case components.SquadStateEngaged:
			if delta >= scrambleDeltaTrigger {
				state.Code = components.SquadStateScrambling
				state.ScramblingSince = now
				state.LowDeltaSince = 0
				sys.pushSuppressionEvent(squad, now)
			} else if avgSupp <= 0 {
				state.Code = components.SquadStateIdle
			}
		case components.SquadStateScrambling:
			if now-state.ScramblingSince >= scrambleSafetyDuration {
				state.Code = components.SquadStateEngaged
				state.LowDeltaSince = 0
			} else if delta < scrambleRecoveryDelta {
				if state.LowDeltaSince == 0 {
					state.LowDeltaSince = now
				} else if now-state.LowDeltaSince >= scrambleRecoveryDuration {
					state.Code = components.SquadStateEngaged
					state.LowDeltaSince = 0
				}
			} else {
				state.LowDeltaSince = 0
			}
		}

		sys.squadInfo[squad] = siSquadInfo{code: state.Code, threatDir: threatDir}
	}

	for _, add := range sys.stateAdds {
		if !sys.world.Alive(add.squad) {
			continue
		}
		if !sys.squadStateMap.Has(add.squad) {
			cpy := add.init
			sys.squadStateMap.Add(add.squad, &cpy)
		}
	}
}

// aggregateSquadSuppression walks the roster, returns the mean Suppression.Level
// across live members + the unit-length suppression-weighted ThreatDir (XZ).
// Members without a Suppression component count as zero contribution.
func (sys *SurvivalInstinctSystem) aggregateSquadSuppression(roster *components.CommandRoster) (float32, rl.Vector3) {
	var sum, weight float32
	var tx, tz float32
	var live float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		live++
		supp := sys.suppressionMap.Get(mem)
		if supp == nil {
			continue
		}
		sum += supp.Level
		tx += supp.ThreatDir.X * supp.Level
		tz += supp.ThreatDir.Z * supp.Level
		weight += supp.Level
	}
	if live == 0 {
		return 0, rl.Vector3{}
	}
	avg := sum / live
	dir := rl.Vector3{}
	if weight > 0 {
		l := float32(math.Sqrt(float64(tx*tx + tz*tz)))
		if l > 0 {
			dir = rl.Vector3{X: tx / l, Z: tz / l}
		}
	}
	return avg, dir
}

// windowDelta returns history[newest] - history[oldest]. Both indices are
// computed from HistHead with wrap-around. While the buffer is filling
// (HistCount < window) the "oldest" sample is index 0.
func (sys *SurvivalInstinctSystem) windowDelta(state *components.SquadState) float32 {
	if state.HistCount < 2 {
		return 0
	}
	newestIdx := (state.HistHead + components.SquadSuppressionWindow - 1) % components.SquadSuppressionWindow
	var oldestIdx uint8
	if state.HistCount < components.SquadSuppressionWindow {
		oldestIdx = 0
	} else {
		oldestIdx = state.HistHead
	}
	return state.SuppHistory[newestIdx] - state.SuppHistory[oldestIdx]
}

// pickCover returns the best cover slot for a unit at `pos` threatened from
// direction `threatDir`. Score formula (PHASE-15.md A-P2):
//
//   score = max(0, dot(slot.OriginDir, threatDir))
//         * 1/(1 + dist/10m)
//         * (1 - occupancyPenalty)
//         * slot.Quality
//
// occupancyPenalty is 0.5 when another unit has the slot in its current
// override. The slot's OriginDir points outward from cover - aligning with
// threatDir means the unit will face the threat with cover behind it.
//
// Returns (slot, worldPos, true) on success or (_, _, false) when no slot
// is in range or scores positively.
func (sys *SurvivalInstinctSystem) pickCover(
	unit ecs.Entity, pos *components.WorldPos, threatDir rl.Vector3,
	claimed map[ecs.Entity]ecs.Entity,
) (ecs.Entity, components.WorldPos, bool) {
	if threatDir.X == 0 && threatDir.Z == 0 {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	tx, tz := threatDir.X, threatDir.Z
	if l := float32(math.Sqrt(float64(tx*tx + tz*tz))); l > 0 {
		tx /= l
		tz /= l
	}
	unitX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	unitZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z

	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	var bestScore float32

	for i := range sys.slots {
		s := &sys.slots[i]
		dx := s.worldX - unitX
		dz := s.worldZ - unitZ
		distSq := dx*dx + dz*dz
		if distSq > siCoverSearchRadius*siCoverSearchRadius {
			continue
		}
		facing := s.originX*tx + s.originZ*tz
		if facing <= 0 {
			continue
		}
		dist := float32(math.Sqrt(float64(distSq)))
		falloff := 1.0 / (1.0 + dist/10.0)
		occ := float32(0)
		if owner, ok := claimed[s.ent]; ok && owner != unit {
			occ = 0.5
		}
		score := facing * falloff * (1 - occ) * s.quality
		if score > bestScore {
			bestScore = score
			bestEnt = s.ent
			bestPos = s.worldPos
		}
	}
	if bestScore <= 0 {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	return bestEnt, bestPos, true
}
