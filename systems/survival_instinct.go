package systems

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SurvivalInstinctSystem pulls suppressed units toward cover. Writes
// TacticalOverride markers and overrides the unit's ActionQueue with a
// MoveTo against the chosen cover slot.
//
// Two passes per tick:
//
//  1. ScatterProtocol — aggregates per-squad suppression in a rolling window
//     on SquadState. A spike flips the squad to Scrambling and aggregates
//     a squad-wide ThreatDir for members whose own ThreatDir is still zero.
//
//  2. Per-unit acquire / clear — the unit acquires cover when its Threat
//     crosses BehaviorRules.SuppressionThreshold, or when the squad is
//     Scrambling (threshold drops to 0). Clears on Until expiry, sustained
//     low suppression, or explicit player order.
//
// FormationSystem skips members carrying TacticalOverride. UnitMovement is
// override-blind; it just executes the queued action.
//
// Helpers split across survival_instinct_scramble.go and
// survival_instinct_cover.go.
type SurvivalInstinctSystem struct {
	unitFilter   *ecs.Filter3[components.Unit, components.WorldPos, components.Threat]
	slotFilter   *ecs.Filter2[components.WorldPos, components.CoverSlot]
	unsafeFilter *ecs.Filter2[components.UnsafeArea, components.WorldPos]
	buildFilter  *ecs.Filter1[components.Building]
	// Position scoring (P4): trench / defilade cells come from the chunk
	// bakes, hull shadow from parked friendly vehicles.
	vehicleFilter *ecs.Filter3[components.Vehicle, components.WorldPos, components.Motion]
	navGridMap    *ecs.Map[components.NavGrid]
	coverMapMap   *ecs.Map[components.CoverMap]
	chunkIndexRes ecs.Resource[TerrainChunkIndex]
	trenchRes     ecs.Resource[components.TrenchNetwork]
	squadFilter   *ecs.Filter2[components.Squad, components.CommandRoster]
	queueMap      *ecs.Map[components.ActionQueue]
	overrideMap   *ecs.Map[components.TacticalOverride]
	memberMap     *ecs.Map[components.SquadMember]
	behaviorMap   *ecs.Map[components.BehaviorRules]
	threatMap     *ecs.Map[components.Threat]
	squadStateMap *ecs.Map[components.SquadState]
	planMap       *ecs.Map[components.SquadPlan]
	rosterMap     *ecs.Map[components.CommandRoster]
	microPathMap  *ecs.Map[components.MicroPath]
	posMap        *ecs.Map[components.WorldPos]
	blackboardMap *ecs.Map[components.LocalBlackboard]
	orderQueueMap *ecs.Map[components.OrderQueueHead]
	issuedAtMap   *ecs.Map[components.OrderIssuedAt]
	eventLogRes   ecs.Resource[components.EventLog]

	world *ecs.World

	slots     []siCoverSlot
	cands     []siCandidate
	unsafe    []siUnsafeZone
	foots     []components.AABB2D
	acquires  []siAcquireOp
	clears    []ecs.Entity
	stateAdds []siStateAdd
	squadInfo map[ecs.Entity]siSquadInfo

	// Persistent capacity tracking: slot entity → number of units currently
	// holding it. pickCover uses it to penalise full slots without hard
	// rejecting them.
	occupancyClaim map[ecs.Entity]uint8
}

// PostLoad repopulates the occupancy ledger from live TacticalOverride
// assignments — the ledger mirrors AssignedSlot counts (inc on assign, dec
// on release), so counting live components recreates it.
func (sys *SurvivalInstinctSystem) PostLoad(simNow float64) {
	clear(sys.occupancyClaim)
	q := ecs.NewFilter1[components.TacticalOverride](sys.world).Query()
	for q.Next() {
		ov := q.Get()
		if ov.AssignedSlot != (ecs.Entity{}) {
			sys.occupancyClaim[ov.AssignedSlot]++
		}
	}
}

// siCoverSlot is the per-tick snapshot of one live CoverSlot entity.
type siCoverSlot struct {
	ent      ecs.Entity
	worldX   float32
	worldZ   float32
	worldPos components.WorldPos
	originX  float32
	originZ  float32
	quality  float32
}

// siUnsafeZone is the per-tick snapshot of one live UnsafeArea (P3).
type siUnsafeZone struct {
	x, z, radius float32
}

// siAcquireOp is a queued "give this unit cover" decision. Batched because
// Add[TacticalOverride] can't run inside the live unit filter.
type siAcquireOp struct {
	unit   ecs.Entity
	slot   ecs.Entity
	pos    components.WorldPos
	reason components.TacticalOverrideReason
	dirX   float32
	dirZ   float32
}

// siStateAdd queues attaching SquadState to a squad after the query closes.
type siStateAdd struct {
	squad ecs.Entity
	init  components.SquadState
}

// siSquadInfo is the per-squad snapshot read by the unit pass.
type siSquadInfo struct {
	code      components.SquadStateCode
	threatDir rl.Vector3
}

// Fallback threshold for soloists / units without BehaviorRules.
const siSuppressionDefault float32 = 0.45

// Hysteresis: sustained sub-clearThreshold time needed before clearing the
// marker (rides through firing-burst gaps).
const siClearLowDuration float32 = 3.0

// Safety timeout — force-clear stuck overrides after this many seconds.
const siSafetyUntil float32 = 30.0

// Max distance (m) from the unit to a candidate slot.
const siCoverSearchRadius float32 = 30.0

// How far past a zone's edge an evacuation aims — stopping exactly on the
// boundary re-triggers the moment the zone breathes.
const siEvacMargin float32 = 6.0

// Fallback relocation distance when no position scores (P4).
const siRelocateDist float32 = 12.0

// A unit within this of its chosen position counts as being IN it — past it
// the man is still moving and must not lie down (stance) nor face away from
// his line of travel (combat-move throttle).
const siReachedRadius float32 = 1.5

// Re-scoring a held position: throttle, and the move it must be worth.
const siReEvalInterval float32 = 0.5
const siReEvalMinMove float32 = 3.0

// A freshly issued player order owns the unit for this long — no NEW
// override may be placed inside the window (squad Scrambling excepted).
const siOrderGraceWindow float32 = 4.0

// Squad-average suppression rise that flips a squad into Scrambling.
const scrambleDeltaTrigger float32 = 0.4

// Drop below this delta sustained for `Duration` → Engaged.
const scrambleRecoveryDelta float32 = 0.1
const scrambleRecoveryDuration float32 = 15.0

// Force-clear Scrambling after this many seconds regardless of delta.
const scrambleSafetyDuration float32 = 60.0

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
	sys.unsafeFilter = ecs.NewFilter2[components.UnsafeArea, components.WorldPos](w)
	sys.buildFilter = ecs.NewFilter1[components.Building](w)
	sys.vehicleFilter = ecs.NewFilter3[components.Vehicle, components.WorldPos, components.Motion](w)
	sys.navGridMap = ecs.NewMap[components.NavGrid](w)
	sys.coverMapMap = ecs.NewMap[components.CoverMap](w)
	sys.chunkIndexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.trenchRes = ecs.NewResource[components.TrenchNetwork](w)
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.overrideMap = ecs.NewMap[components.TacticalOverride](w)
	sys.memberMap = ecs.NewMap[components.SquadMember](w)
	sys.behaviorMap = ecs.NewMap[components.BehaviorRules](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.squadStateMap = ecs.NewMap[components.SquadState](w)
	sys.planMap = ecs.NewMap[components.SquadPlan](w)
	sys.rosterMap = ecs.NewMap[components.CommandRoster](w)
	sys.microPathMap = ecs.NewMap[components.MicroPath](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.issuedAtMap = ecs.NewMap[components.OrderIssuedAt](w)
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
	now := float32(ctx.SimNow)

	// Pass 1 — snapshot live cover slots.
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

	// Pass 1b — snapshot live unsafe areas (P3). Standing in one is its own
	// reason to move, and cover inside one is not cover.
	sys.unsafe = sys.unsafe[:0]
	qZ := sys.unsafeFilter.Query()
	for qZ.Next() {
		area, pos := qZ.Get()
		x, z := worldXZ(*pos)
		sys.unsafe = append(sys.unsafe, siUnsafeZone{x: x, z: z, radius: area.Radius})
	}

	// Pass 1c — building footprints. Every candidate the scorer knows is
	// terrain-derived, so a man indoors could only "take cover" by leaving the
	// building — off an upper storey, or out of the room his order put him in.
	// Until interior positions exist (P4 tail) the instinct leaves him be.
	sys.foots = sys.foots[:0]
	qB := sys.buildFilter.Query()
	for qB.Next() {
		sys.foots = append(sys.foots, qB.Get().Footprint)
	}

	// Pass 1.5 — ScatterProtocol: per squad, decide Idle / Engaged /
	// Scrambling; squadInfo is read by Pass 2.
	sys.runScatterProtocol(now)

	// Build claimed-slot set from existing overrides for occupancyPenalty.
	claimed := map[ecs.Entity]ecs.Entity{} // slot → owning unit
	qOv := sys.unitFilter.Query()
	for qOv.Next() {
		ent := qOv.Entity()
		if ov := sys.overrideMap.Get(ent); ov != nil && ov.AssignedSlot != (ecs.Entity{}) {
			claimed[ov.AssignedSlot] = ent
		}
	}

	sys.acquires = sys.acquires[:0]
	sys.clears = sys.clears[:0]

	// Pass 2 — per-unit decision. Scalar field writes on the override
	// pointer happen in-place; archetype changes are queued.
	// BehaviorRules.SuppressionThreshold gates against Threat.Total.
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, threat := q.Get()
		threshold := sys.thresholdFor(ent)
		clearThreshold := threshold * 0.6
		scrambling, squadThreat := sys.squadScrambleContext(ent)
		// While scrambling, every member acquires cover even when its own
		// Threat is cold; threatDir falls back to the squad aggregate.
		effectiveThreshold := threshold
		if scrambling {
			effectiveThreshold = 0
		}
		threatDir := threat.ThreatDir
		if threatDir.X == 0 && threatDir.Z == 0 && scrambling {
			threatDir = squadThreat
		}

		// Bounding (P6): the wave on the move is not looking for cover — it
		// IS moving to the next position, and an override halfway there is the
		// old cover-freeze that made squads under fire stop advancing. The
		// covering wave takes cover whether or not it is personally suppressed.
		plan, slot := sys.squadPlanFor(ent)
		if plan != nil && plan.Mode == components.SquadPlanBounding {
			if plan.InMovingWave(slot) {
				if sys.overrideMap.Has(ent) {
					sys.clears = append(sys.clears, ent)
				}
				continue
			}
			// The covering wave holds and SHOOTS: it only leaves its ground
			// when personally pinned (its own threshold), so the usual
			// scramble-to-cover does not turn overwatch into a foot race.
			if threatDir.X == 0 && threatDir.Z == 0 {
				threatDir = squadThreat
			}
		}

		// Standing in a shelled area is its own trigger: the threshold drops
		// like Scrambling, and the bearing points out of the zone even when
		// the channels have not caught up yet.
		if zone := sys.zoneUnder(pos); zone >= 0 {
			effectiveThreshold = 0
			if dir, ok := sys.evacDir(pos, zone); ok {
				threatDir = dir
			}
		}

		if sys.insideFootprint(pos) {
			if sys.overrideMap.Has(ent) {
				sys.clears = append(sys.clears, ent)
			}
			continue
		}

		existing := sys.overrideMap.Get(ent)
		if existing == nil {
			// Self-heal external clears (SquadService removes overrides on a
			// player order): the blackboard link and the occupancy ledger
			// must not outlive the override.
			if bb := sys.blackboardMap.Get(ent); bb != nil && bb.AssignedCover != (ecs.Entity{}) {
				if sys.occupancyClaim[bb.AssignedCover] > 0 {
					sys.occupancyClaim[bb.AssignedCover]--
				}
				bb.AssignedCover = ecs.Entity{}
			}
			if rules := sys.behaviorFor(ent); rules != nil &&
				(rules.HoldUntilOrdered || !rules.AllowAutoReposition) {
				continue
			}
			inZone := sys.zoneUnder(pos) >= 0
			if threat.Total <= effectiveThreshold && !scrambling && !inZone {
				continue
			}
			if !scrambling && !inZone && sys.orderGraceActive(ent, now) {
				continue
			}
			op, ok := sys.planPosition(ent, pos, threat, threatDir, inZone, claimed)
			if !ok {
				continue
			}
			if op.slot != (ecs.Entity{}) {
				claimed[op.slot] = ent
			}
			sys.acquires = append(sys.acquires, op)
			continue
		}

		// Override held — keep or clear?
		if now > existing.Until {
			sys.clears = append(sys.clears, ent)
			continue
		}
		// A held place is only as good as the bearing it was chosen against
		// (P4): re-score when the fire swings past a right angle or the place
		// itself has been swallowed by a beaten zone. Throttled — re-picking
		// every tick would thrash the queue.
		if now-existing.LastEvalAt >= siReEvalInterval && sys.positionStale(existing, threat) {
			existing.LastEvalAt = now
			if op, ok := sys.planPosition(ent, pos, threat, threatDir,
				sys.zoneUnder(pos) >= 0, claimed); ok &&
				centerXZDistSq(op.pos, existing.CoverPos) > siReEvalMinMove*siReEvalMinMove {
				if op.slot != (ecs.Entity{}) {
					claimed[op.slot] = ent
				}
				sys.acquires = append(sys.acquires, op)
				continue
			}
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

	// Pass 3 — apply ECS mutations. Clears first so a cleared unit can be
	// re-acquired on the same tick if still suppressed.
	for _, e := range sys.clears {
		if ov := sys.overrideMap.Get(e); ov != nil {
			if ov.AssignedSlot != (ecs.Entity{}) {
				if sys.occupancyClaim[ov.AssignedSlot] > 0 {
					sys.occupancyClaim[ov.AssignedSlot]--
				}
			}
			sys.restoreSavedOrder(e, ov)
		}
		if bb := sys.blackboardMap.Get(e); bb != nil {
			bb.AssignedCover = ecs.Entity{}
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
		existing := sys.overrideMap.Get(op.unit)
		// P1: stash the displaced head ONCE — the original order rides
		// through slot re-acquires and is restored when the last override
		// clears. A re-acquire carries the earlier stash forward.
		var savedKind components.ActionKind
		var savedTarget components.WorldPos
		if existing != nil {
			savedKind, savedTarget = existing.SavedKind, existing.SavedTarget
		} else if aq.Count > 0 {
			savedKind = aq.Actions[aq.Head].Kind
			savedTarget = aq.Actions[aq.Head].Target
		}
		// Retarget in place + MicroPath.Dirty instead of
		// ClearActions+PushAction so cover behind a corner routes via A*
		// (straight-line steering would jam against the wall).
		if aq.Count > 0 && aq.Actions[aq.Head].Kind == components.ActionMoveTo {
			aq.Actions[aq.Head].Target = op.pos
		} else {
			ClearActions(aq)
			PushAction(aq, components.Action{Kind: components.ActionMoveTo, Target: op.pos})
		}
		if mp := sys.microPathMap.Get(op.unit); mp != nil {
			mp.Dirty = true
		}
		// Release old claim if the unit held a different slot.
		if existing != nil &&
			existing.AssignedSlot != (ecs.Entity{}) && existing.AssignedSlot != op.slot {
			if sys.occupancyClaim[existing.AssignedSlot] > 0 {
				sys.occupancyClaim[existing.AssignedSlot]--
			}
		}
		if sys.overrideMap.Has(op.unit) {
			sys.overrideMap.Remove(op.unit)
		}
		sys.overrideMap.Add(op.unit, &components.TacticalOverride{
			Reason:       op.reason,
			ChosenDirX:   op.dirX,
			ChosenDirZ:   op.dirZ,
			LastEvalAt:   now,
			Until:        now + siSafetyUntil,
			LowSuppSince: 0,
			AssignedSlot: op.slot,
			SavedKind:    savedKind,
			SavedTarget:  savedTarget,
			CoverPos:     op.pos,
		})
		if bb := sys.blackboardMap.Get(op.unit); bb != nil {
			bb.AssignedCover = op.slot
		}
		// An evacuation holds no slot — the ledger counts cover claims only.
		if op.slot != (ecs.Entity{}) {
			sys.occupancyClaim[op.slot]++
		}
	}
}

// restoreSavedOrder puts the action the instinct displaced back at the head
// of the queue. It only fires while the queue still belongs to the instinct:
// head == its own cover MoveTo (bit-exact — nothing mutates action targets),
// or the queue is empty (the cover MoveTo popped on arrival). A head written
// by anyone else since is left untouched.
func (sys *SurvivalInstinctSystem) restoreSavedOrder(e ecs.Entity, ov *components.TacticalOverride) {
	aq := sys.queueMap.Get(e)
	if aq == nil {
		return
	}
	if aq.Count > 0 {
		head := &aq.Actions[aq.Head]
		if head.Kind != components.ActionMoveTo || head.Target != ov.CoverPos {
			return
		}
		if ov.SavedKind == components.ActionNone {
			ClearActions(aq)
		} else {
			head.Kind = ov.SavedKind
			head.Target = ov.SavedTarget
		}
	} else {
		if ov.SavedKind == components.ActionNone {
			return
		}
		PushAction(aq, components.Action{Kind: ov.SavedKind, Target: ov.SavedTarget})
	}
	if mp := sys.microPathMap.Get(e); mp != nil {
		mp.Dirty = true
	}
}

// pushSuppressionEvent records a SuppressionStart when a squad first flips
// to Scrambling. Uses the first live member's pos (cheap vs. SquadCenter).
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

// rosterMember returns the first live member of `squad`. Resolves via
// Map.Get (not a nested query) because the caller already holds an open
// squadFilter query — a nested query that short-circuits its auto-close
// leaves a lock dangling and panics the next archetype mutation.
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

// thresholdFor returns the squad's BehaviorRules.SuppressionThreshold or
// the system default.
func (sys *SurvivalInstinctSystem) thresholdFor(unit ecs.Entity) float32 {
	if br := sys.behaviorFor(unit); br != nil && br.SuppressionThreshold > 0 {
		return br.SuppressionThreshold
	}
	return siSuppressionDefault
}

// behaviorFor resolves the unit's squad BehaviorRules; nil for soloists.
func (sys *SurvivalInstinctSystem) behaviorFor(unit ecs.Entity) *components.BehaviorRules {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return nil
	}
	return sys.behaviorMap.Get(mem.Squad)
}

// orderGraceActive reports whether the unit's squad head order was issued
// inside the grace window — the player just spoke, the instinct stays quiet.
func (sys *SurvivalInstinctSystem) orderGraceActive(unit ecs.Entity, now float32) bool {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return false
	}
	head := sys.orderQueueMap.Get(mem.Squad)
	if head == nil || head.First == (ecs.Entity{}) || !sys.world.Alive(head.First) {
		return false
	}
	iss := sys.issuedAtMap.Get(head.First)
	return iss != nil && now-iss.Time < siOrderGraceWindow
}

// insideFootprint reports whether the unit stands within any building.
func (sys *SurvivalInstinctSystem) insideFootprint(pos *components.WorldPos) bool {
	x, z := worldXZ(*pos)
	for i := range sys.foots {
		f := &sys.foots[i]
		if x >= f.MinX && x <= f.MaxX && z >= f.MinZ && z <= f.MaxZ {
			return true
		}
	}
	return false
}

// squadPlanFor returns the unit's squad plan and its roster slot; (nil, 0)
// for soloists.
func (sys *SurvivalInstinctSystem) squadPlanFor(unit ecs.Entity) (*components.SquadPlan, uint8) {
	mem := sys.memberMap.Get(unit)
	if mem == nil || mem.Squad == (ecs.Entity{}) {
		return nil, 0
	}
	return sys.planMap.Get(mem.Squad), mem.SlotIndex
}

// squadScrambleContext returns (scrambling, threatDir). Soloists get
// (false, zero); scrambling squads return the aggregate computed earlier.
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
