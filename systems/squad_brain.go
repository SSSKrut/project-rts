package systems

import (
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// SquadBrainSystem is the operational tier (P5): it decides HOW the squad
// executes the order it already has, never WHAT the order is. The output is a
// SquadPlan on the Squad entity — FormationSystem drives the slots from it and
// SurvivalInstinct reads it before handing a man his cover.
//
// Three behaviours, no more (P6): Bounding (move under contact in waves),
// Relocate (leave a beaten zone, keep the order), ClearSeq (sequence a
// ClearBuilding into stack-up / entry / storey sweeps).
//
// Decisions run per squad on a hash bucket, so the cost spreads across frames
// and the cadence lands near squadBrainCadence.
type SquadBrainSystem struct {
	squadFilter *ecs.Filter3[components.Squad, components.CommandRoster, components.MacroPath]
	unitFilter  *ecs.Filter2[components.Unit, components.WorldPos]

	planMap       *ecs.Map[components.SquadPlan]
	posMap        *ecs.Map[components.WorldPos]
	threatMap     *ecs.Map[components.Threat]
	behaviorMap   *ecs.Map[components.BehaviorRules]
	factionMap    *ecs.Map[components.Faction]
	orderQueueMap *ecs.Map[components.OrderQueueHead]
	orderKindMap  *ecs.Map[components.OrderKind]
	orderTgtMap   *ecs.Map[components.OrderTarget]
	buildingMap   *ecs.Map[components.Building]
	vehicleMap    *ecs.Map[components.Vehicle]
	floorMap      *ecs.Map[components.Floor]
	formationMap  *ecs.Map[components.FormationData]
	unsafeFilter  *ecs.Filter2[components.UnsafeArea, components.WorldPos]
	childIndexRes ecs.Resource[BuildingChildIndex]

	slotPlanner *BuildingSlotPlanner
	world       *ecs.World

	adds   []squadPlanAdd
	unsafe []siUnsafeZone
	stacks []BuildingSlot
}

type squadPlanAdd struct {
	squad ecs.Entity
	plan  components.SquadPlan
}

const (
	// Decision cadence in ticks (hash-bucketed per squad).
	squadBrainCadence uint32 = 90

	// Bounding enter / exit pressure on the roster's worst Threat.Total, and
	// how long the calm has to hold before the squad walks upright again.
	boundEnterThreat float32 = 0.25
	boundExitThreat  float32 = 0.08
	boundCalmHold    float32 = 4.0

	// One bound: how far ahead the moving wave runs, how close counts as
	// arrived, and the cap on a phase that never arrives.
	boundStep       float32 = 18.0
	boundArriveDist float32 = 5.0
	boundPhaseMax   float32 = 12.0

	// Relocate: share of the roster standing in a beaten zone that pauses the
	// march, and the clean window before it resumes.
	relocateShare    float32 = 0.5
	relocateCleanFor float32 = 5.0

	// ClearSeq phase caps — a stalled step advances rather than deadlocking.
	clearStackTimeout float32 = 10.0
	clearEnterTimeout float32 = 25.0
	clearSweepTimeout float32 = 40.0
	clearStackArrive  float32 = 3.5
)

// brainOff kills the operational tier for A/B scene runs: the same scene with
// the brain silent is the only honest baseline for "did bounding help".
var brainOff = os.Getenv("RTS_BRAIN_OFF") != ""

func NewSquadBrainSystem() *SquadBrainSystem {
	return &SquadBrainSystem{
		adds:   make([]squadPlanAdd, 0, 4),
		unsafe: make([]siUnsafeZone, 0, 8),
	}
}

func (sys *SquadBrainSystem) InitUI(w *ecs.World) {
	sys.world = w
	sys.squadFilter = ecs.NewFilter3[components.Squad, components.CommandRoster, components.MacroPath](w)
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.planMap = ecs.NewMap[components.SquadPlan](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.behaviorMap = ecs.NewMap[components.BehaviorRules](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderTgtMap = ecs.NewMap[components.OrderTarget](w)
	sys.buildingMap = ecs.NewMap[components.Building](w)
	sys.vehicleMap = ecs.NewMap[components.Vehicle](w)
	sys.floorMap = ecs.NewMap[components.Floor](w)
	sys.formationMap = ecs.NewMap[components.FormationData](w)
	sys.unsafeFilter = ecs.NewFilter2[components.UnsafeArea, components.WorldPos](w)
	sys.childIndexRes = ecs.NewResource[BuildingChildIndex](w)
	sys.slotPlanner = NewBuildingSlotPlanner(w)
}

func (SquadBrainSystem) Name() string { return "squad_brain" }

func (SquadBrainSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *SquadBrainSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive || brainOff {
		return
	}
	now := float32(ctx.SimNow)

	sys.unsafe = sys.unsafe[:0]
	qz := sys.unsafeFilter.Query()
	for qz.Next() {
		area, pos := qz.Get()
		x, z := worldXZ(*pos)
		sys.unsafe = append(sys.unsafe, siUnsafeZone{x: x, z: z, radius: area.Radius})
	}

	sys.adds = sys.adds[:0]
	q := sys.squadFilter.Query()
	for q.Next() {
		squad := q.Entity()
		_, roster, mp := q.Get()
		if roster.Count == 0 {
			continue
		}
		plan := sys.planMap.Get(squad)
		if plan == nil {
			sys.adds = append(sys.adds, squadPlanAdd{squad: squad})
			continue
		}
		if (uint32(squad.ID())+ctx.FrameIndex)%squadBrainCadence != 0 {
			continue
		}
		sys.decide(squad, roster, mp, plan, now)
	}

	for _, add := range sys.adds {
		if !sys.world.Alive(add.squad) || sys.planMap.Has(add.squad) {
			continue
		}
		cpy := add.plan
		sys.planMap.Add(add.squad, &cpy)
	}
}

// decide is the whole arbitration: ClearSeq (explicit order) wins, then
// Relocate (standing in fire nobody can answer), then Bounding.
func (sys *SquadBrainSystem) decide(squad ecs.Entity, roster *components.CommandRoster,
	mp *components.MacroPath, plan *components.SquadPlan, now float32) {

	kind, target, haveOrder := sys.headOrder(squad)

	if haveOrder && kind == components.OrderKindClearBuilding &&
		target.Entity != (ecs.Entity{}) && sys.world.Alive(target.Entity) {
		sys.stepClearSeq(squad, roster, plan, target.Entity, now)
		return
	}
	if plan.Mode == components.SquadPlanClearSeq {
		sys.setMode(plan, components.SquadPlanNone, now)
	}

	// Standing rules that pin a squad in place also pin the brain (P2).
	if br := sys.behaviorMap.Get(squad); br != nil &&
		(br.HoldUntilOrdered || !br.AllowAutoReposition) {
		sys.setMode(plan, components.SquadPlanNone, now)
		return
	}

	if sys.stepRelocate(squad, roster, plan, now) {
		return
	}

	// Bounding is an infantry behaviour: hull locomotion under fire belongs to
	// the M4 reflexes, and a column that halts half its trucks fights the M7
	// pace cap instead of helping it.
	moving := haveOrder && mp.HasGoal && orderKindMoves(kind) && !sys.hasVehicle(roster)
	if !moving {
		sys.setMode(plan, components.SquadPlanNone, now)
		return
	}
	sys.stepBounding(roster, mp, plan, now)
}

// orderKindMoves reports whether the order's execution is a march the brain
// may break into bounds.
func orderKindMoves(kind components.OrderKindCode) bool {
	switch kind {
	case components.OrderKindMoveTo, components.OrderKindPatrol,
		components.OrderKindAttackTarget:
		return true
	}
	return false
}

func (sys *SquadBrainSystem) setMode(plan *components.SquadPlan, mode components.SquadPlanMode, now float32) {
	if plan.Mode == mode {
		return
	}
	plan.Mode = mode
	plan.Phase = 0
	plan.Floor = 0
	plan.Since = now
	plan.Timer = 0
	plan.CalmSince = 0
}

// stepBounding enters / advances / leaves the bounding overwatch. The wave
// split is by roster slot parity so the commander (slot 0) always bounds with
// wave 0 — the anchor the macro path measures progress against keeps moving.
func (sys *SquadBrainSystem) stepBounding(roster *components.CommandRoster,
	mp *components.MacroPath, plan *components.SquadPlan, now float32) {

	pressure := sys.worstThreat(roster)
	if plan.Mode != components.SquadPlanBounding {
		if pressure < boundEnterThreat {
			sys.setMode(plan, components.SquadPlanNone, now)
			return
		}
		sys.setMode(plan, components.SquadPlanBounding, now)
		plan.WaveMask = 0
		for i := uint8(0); i < roster.Count; i++ {
			if i%2 == 1 {
				plan.WaveMask |= 1 << i
			}
		}
		plan.Phase = 0
		plan.Timer = now + boundPhaseMax
		plan.Anchor = sys.boundAnchor(roster, mp)
		return
	}

	// Leaving: the fire has to stay off for a while, else every gap in a
	// burst stands the squad up.
	if pressure < boundExitThreat {
		if plan.CalmSince == 0 {
			plan.CalmSince = now
		} else if now-plan.CalmSince >= boundCalmHold {
			sys.setMode(plan, components.SquadPlanNone, now)
			return
		}
	} else {
		plan.CalmSince = 0
	}

	if now >= plan.Timer || sys.waveArrived(roster, plan) {
		plan.Phase ^= 1
		plan.Timer = now + boundPhaseMax
		plan.Anchor = sys.boundAnchor(roster, mp)
	}
}

// waveArrived: every live member of the moving wave is within the arrival
// ring of the bound.
func (sys *SquadBrainSystem) waveArrived(roster *components.CommandRoster, plan *components.SquadPlan) bool {
	ax, az := worldXZ(plan.Anchor)
	seen := false
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) || !plan.InMovingWave(i) {
			continue
		}
		p := sys.posMap.Get(mem)
		if p == nil {
			continue
		}
		seen = true
		x, z := worldXZ(*p)
		if dx, dz := x-ax, z-az; dx*dx+dz*dz > boundArriveDist*boundArriveDist {
			return false
		}
	}
	return seen
}

// boundAnchor walks the macro path forward from the roster centroid and
// returns the point one bound ahead (the goal when the path runs out).
func (sys *SquadBrainSystem) boundAnchor(roster *components.CommandRoster,
	mp *components.MacroPath) components.WorldPos {

	center, ok := SquadCenter(sys.world, roster, sys.posMap)
	if !ok {
		return mp.Goal
	}
	remain := boundStep
	cur := center
	for i := mp.Head; i < mp.Count; i++ {
		wp := mp.Waypoints[i]
		d := wp.Sub(cur)
		seg := float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
		if seg < 1e-3 {
			continue
		}
		if seg >= remain {
			t := remain / seg
			return cur.Add(rl.Vector3{X: d.X * t, Y: d.Y * t, Z: d.Z * t})
		}
		remain -= seg
		cur = wp
	}
	d := mp.Goal.Sub(cur)
	seg := float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
	if seg <= remain || seg < 1e-3 {
		return mp.Goal
	}
	t := remain / seg
	return cur.Add(rl.Vector3{X: d.X * t, Y: d.Y * t, Z: d.Z * t})
}

// stepRelocate holds the march while most of the roster is standing in a
// beaten zone. Returns true when the plan is (still) Relocate.
func (sys *SquadBrainSystem) stepRelocate(squad ecs.Entity, roster *components.CommandRoster,
	plan *components.SquadPlan, now float32) bool {

	var live, inZone float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		p := sys.posMap.Get(mem)
		if p == nil {
			continue
		}
		live++
		x, z := worldXZ(*p)
		for j := range sys.unsafe {
			z0 := &sys.unsafe[j]
			if dx, dz := x-z0.x, z-z0.z; dx*dx+dz*dz <= z0.radius*z0.radius {
				inZone++
				break
			}
		}
	}
	if live == 0 {
		return false
	}
	share := inZone / live
	if plan.Mode == components.SquadPlanRelocate {
		if share > 0 {
			plan.CalmSince = 0
			return true
		}
		if plan.CalmSince == 0 {
			plan.CalmSince = now
			return true
		}
		if now-plan.CalmSince < relocateCleanFor {
			return true
		}
		sys.setMode(plan, components.SquadPlanNone, now)
		// The men are wherever the evacuation left them: re-form in place, then
		// carry on with the order that never stopped being theirs.
		if fd := sys.formationMap.Get(squad); fd != nil {
			fd.ReformPending = true
		}
		return false
	}
	if share >= relocateShare {
		sys.setMode(plan, components.SquadPlanRelocate, now)
		return true
	}
	return false
}

func (sys *SquadBrainSystem) hasVehicle(roster *components.CommandRoster) bool {
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem != (ecs.Entity{}) && sys.world.Alive(mem) && sys.vehicleMap.Has(mem) {
			return true
		}
	}
	return false
}

func (sys *SquadBrainSystem) worstThreat(roster *components.CommandRoster) float32 {
	var worst float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		if t := sys.threatMap.Get(mem); t != nil && t.Total > worst {
			worst = t.Total
		}
	}
	return worst
}

// headOrder resolves the squad's current order kind + target.
func (sys *SquadBrainSystem) headOrder(squad ecs.Entity) (components.OrderKindCode, components.OrderTarget, bool) {
	head := sys.orderQueueMap.Get(squad)
	if head == nil || head.First == (ecs.Entity{}) || !sys.world.Alive(head.First) {
		return 0, components.OrderTarget{}, false
	}
	kind := sys.orderKindMap.Get(head.First)
	if kind == nil {
		return 0, components.OrderTarget{}, false
	}
	var tgt components.OrderTarget
	if t := sys.orderTgtMap.Get(head.First); t != nil {
		tgt = *t
	}
	return kind.Code, tgt, true
}
