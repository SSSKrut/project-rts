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
	unitFilter    *ecs.Filter3[components.Unit, components.WorldPos, components.Threat]
	slotFilter    *ecs.Filter2[components.WorldPos, components.CoverSlot]
	squadFilter   *ecs.Filter2[components.Squad, components.CommandRoster]
	queueMap      *ecs.Map[components.ActionQueue]
	overrideMap   *ecs.Map[components.TacticalOverride]
	memberMap     *ecs.Map[components.SquadMember]
	behaviorMap   *ecs.Map[components.BehaviorRules]
	threatMap     *ecs.Map[components.Threat]
	squadStateMap *ecs.Map[components.SquadState]
	rosterMap     *ecs.Map[components.CommandRoster]
	microPathMap  *ecs.Map[components.MicroPath]
	posMap        *ecs.Map[components.WorldPos]
	eventLogRes   ecs.Resource[components.EventLog]

	world *ecs.World

	slots     []siCoverSlot
	acquires  []siAcquireOp
	clears    []ecs.Entity
	stateAdds []siStateAdd
	squadInfo map[ecs.Entity]siSquadInfo

	// Phase 17 M17.B.3: persistent capacity tracking. slot entity -> number of
	// units currently holding it via TacticalOverride.AssignedSlot. Incremented
	// in the acquire pass, decremented when the override clears or the unit
	// dies / changes slot. pickCover uses it to penalise full slots without
	// resorting to a hard reject (a saturated slot can still win as a
	// last-resort when nothing else is in range).
	occupancyClaim map[ecs.Entity]uint8

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
		slots:          make([]siCoverSlot, 0, 64),
		acquires:       make([]siAcquireOp, 0, 16),
		clears:         make([]ecs.Entity, 0, 16),
		stateAdds:      make([]siStateAdd, 0, 4),
		squadInfo:      make(map[ecs.Entity]siSquadInfo, 8),
		occupancyClaim: make(map[ecs.Entity]uint8, 32),
	}
}

func (sys *SurvivalInstinctSystem) InitUI(w *ecs.World) {
	sys.world = w
	sys.unitFilter = ecs.NewFilter3[components.Unit, components.WorldPos, components.Threat](w)
	sys.slotFilter = ecs.NewFilter2[components.WorldPos, components.CoverSlot](w)
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.overrideMap = ecs.NewMap[components.TacticalOverride](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.behaviorMap = ecs.NewMap[components.BehaviorRules](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.squadStateMap = ecs.NewMap[components.SquadState](w)
	sys.rosterMap = ecs.NewMap[components.CommandRoster](w)
	sys.microPathMap = ecs.NewMap[components.MicroPath](w)
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
	//
	// Phase 17 M17.0.3: trigger / clear gates compare against Threat.Total
	// (aggregate of Suppression + ShotsFired + Endangered + Injury) instead of
	// the Suppression channel alone. BehaviorRules.SuppressionThreshold keeps
	// its float knob - it now means "total threat above this triggers cover".
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, threat := q.Get()
		threshold := sys.thresholdFor(ent)
		clearThreshold := threshold * 0.6
		scrambling, squadThreat := sys.squadScrambleContext(ent)
		// When the squad is Scrambling every member acquires cover, even ones
		// whose own Threat is still cold. Fallback threatDir comes from the
		// squad-level aggregate computed in Pass 1.5.
		effectiveThreshold := threshold
		if scrambling {
			effectiveThreshold = 0
		}
		threatDir := threat.ThreatDir
		if threatDir.X == 0 && threatDir.Z == 0 && scrambling {
			threatDir = squadThreat
		}

		existing := sys.overrideMap.Get(ent)
		if existing == nil {
			if threat.Total <= effectiveThreshold && !scrambling {
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
		if threat.Total < clearThreshold && !scrambling {
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
		if ov := sys.overrideMap.Get(e); ov != nil && ov.AssignedSlot != (ecs.Entity{}) {
			if sys.occupancyClaim[ov.AssignedSlot] > 0 {
				sys.occupancyClaim[ov.AssignedSlot]--
			}
		}
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
		// Phase 17 M17.B.5: retarget in place + MicroPath.Dirty instead of
		// ClearActions+PushAction. UnitMovement / MicroPathSystem then route
		// the unit toward the cover slot via A* (the cover may be on the far
		// side of a doorway or building corner; straight-line steering would
		// jam against a wall).
		if aq.Count > 0 && aq.Actions[aq.Head].Kind == components.ActionMoveTo {
			aq.Actions[aq.Head].Target = op.pos
		} else {
			ClearActions(aq)
			PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: op.pos})
		}
		if mp := sys.microPathMap.Get(op.unit); mp != nil {
			mp.Dirty = true
		}
		// If the unit already held a different slot, release the old claim.
		if existing := sys.overrideMap.Get(op.unit); existing != nil &&
			existing.AssignedSlot != (ecs.Entity{}) && existing.AssignedSlot != op.slot {
			if sys.occupancyClaim[existing.AssignedSlot] > 0 {
				sys.occupancyClaim[existing.AssignedSlot]--
			}
		}
		if sys.overrideMap.Has(op.unit) {
			sys.overrideMap.Remove(op.unit)
		}
		sys.overrideMap.Add(op.unit, &components.TacticalOverride{
			Reason:       components.TacticalOverrideUnderFire,
			Until:        now + siSafetyUntil,
			LowSuppSince: 0,
			AssignedSlot: op.slot,
		})
		sys.occupancyClaim[op.slot]++
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
//
// Called from pushSuppressionEvent which itself runs inside runScatterProtocol's
// squadFilter query - so this resolves CommandRoster via Map.Get (O(1), no lock)
// rather than re-opening a nested query (which previously left a lock dangling
// when the early return / break short-circuited Ark's auto-close, eventually
// panicking the next archetype mutation with "cannot modify a locked world").
func (sys *SurvivalInstinctSystem) rosterMember(squad ecs.Entity) ecs.Entity {
	if !sys.world.Alive(squad) {
		return ecs.Entity{}
	}
	roster := sys.rosterMap.Get(squad)
	if roster == nil {
		return ecs.Entity{}
	}
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem != (ecs.Entity{}) && sys.world.Alive(mem) {
			return mem
		}
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
// pushes the latest squad-average Threat.Total into the history ring, and
// runs the Idle / Engaged / Scrambling transition logic. squadInfo is
// repopulated from scratch each tick so Pass 2 reads a consistent view.
//
// Phase 17 M17.0.3: history tracks Total instead of raw Suppression, so a
// far-fire-only spike (ShotsFired channel) can also flip a squad into
// Scrambling once that producer ships.
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

		avgSupp, threatDir := sys.aggregateSquadThreat(roster)

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

// aggregateSquadThreat walks the roster, returns the mean Threat.Total
// across live members + the unit-length Total-weighted ThreatDir (XZ).
// Members without a Threat component count as zero contribution.
func (sys *SurvivalInstinctSystem) aggregateSquadThreat(roster *components.CommandRoster) (float32, rl.Vector3) {
	var sum, weight float32
	var tx, tz float32
	var live float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		live++
		threat := sys.threatMap.Get(mem)
		if threat == nil {
			continue
		}
		sum += threat.Total
		tx += threat.ThreatDir.X * threat.Total
		tz += threat.ThreatDir.Z * threat.Total
		weight += threat.Total
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
// direction `threatDir`. Phase 17 M17.B.1/M17.B.2/M17.B.3 score formula:
//
//   score = quality*100 + facing*20 - dist*5 - anglePenalty*30 - capacityPenalty*50
//
// facing = dot(slot.OriginDir, threatDir). In our convention OriginDir points
// outward from the cover material toward the unit side (slot lives just
// outside the cover), and threatDir = unit←from-threat also points toward the
// unit side, so a "between threat and unit" slot has facing ~ +1. Slots
// behind the unit relative to the threat get facing << 0; M17.B.1 rejects
// anything with facing < -0.3 (tolerance keeps side-cover viable but kills
// "cover behind my back" picks).
//
// anglePenalty = 1 - dot(approach_forward, dir_to_slot). Approach forward is
// the "away from threat" direction (-threatDir) - the unit should not be
// running backwards toward a slot, so slots behind the unit get penalised.
//
// capacityPenalty grows with the persistent occupancyClaim count: 1 if any
// other unit already holds the slot via TacticalOverride.AssignedSlot, 20 if
// the slot is at the soft cap. Within-tick reservations from `claimed`
// double-count for the same effect.
//
// Returns (slot, worldPos, true) on success or (_, _, false) when no slot
// passes the validation gate.
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
	// Approach forward: away from threat (the unit is bolting from danger,
	// so "ahead" for utility purposes is the survive-away direction).
	fx, fz := -tx, -tz

	unitX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	unitZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z

	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	const negInf = float32(-1e9)
	bestScore := negInf

	for i := range sys.slots {
		s := &sys.slots[i]
		dx := s.worldX - unitX
		dz := s.worldZ - unitZ
		distSq := dx*dx + dz*dz
		if distSq > siCoverSearchRadius*siCoverSearchRadius {
			continue
		}
		facing := s.originX*tx + s.originZ*tz
		if facing < -0.3 {
			continue
		}
		dist := float32(math.Sqrt(float64(distSq)))
		var dirX, dirZ float32
		if dist > 1e-3 {
			dirX = dx / dist
			dirZ = dz / dist
		}
		align := fx*dirX + fz*dirZ
		anglePenalty := 1 - align // [0, 2]

		var capacityPenalty float32
		claim := sys.occupancyClaim[s.ent]
		if owner, ok := claimed[s.ent]; ok && owner != unit {
			claim++
		}
		switch {
		case claim >= 2:
			capacityPenalty = 20
		case claim >= 1:
			capacityPenalty = 1
		}

		score := s.quality*100 + facing*20 - dist*5 - anglePenalty*30 - capacityPenalty*50
		if score > bestScore {
			bestScore = score
			bestEnt = s.ent
			bestPos = s.worldPos
		}
	}
	if bestEnt == (ecs.Entity{}) {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	return bestEnt, bestPos, true
}
