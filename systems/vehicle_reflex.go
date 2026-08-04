package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// VehicleReflexSystem is the priority-arbitration layer above the driver:
// when a vehicle's Threat crosses the trigger and no reflex is running, it
// stamps VehicleOverride with the class reflex (FaceThreat / SmokeAndReverse /
// Flee). FaceThreat and SmokeAndReverse own the driver directly; Flee routes a
// retreat through the order queue. All state lives in VehicleOverride, so the
// system keeps no cross-tick accumulators and needs no PostLoad hook.
type VehicleReflexSystem struct {
	filter       *ecs.Filter5[components.Vehicle, components.WorldPos, components.Motion, components.Threat, components.VehicleOverride]
	queueMap     *ecs.Map[components.ActionQueue]
	posMap       *ecs.Map[components.WorldPos]
	smokeMap     *ecs.Map[components.SmokeField]
	awareMap     *ecs.Map[components.Awareness]
	factionMap   *ecs.Map[components.Faction]
	worldRef     *ecs.World
	smokePending []components.WorldPos
	// Releasing a reflex asks the member's squad to re-form in place: the
	// retreat can end tens of metres off the slot, and an idle squad has no
	// other mechanism that brings a straggler home.
	squadMemberMap *ecs.Map[components.SquadMember]
	formationMap   *ecs.Map[components.FormationData]
}

const (
	// Threat.Total that arms a reflex, and the re-entry cooldown after one
	// fires (anchored on VehicleOverride.LastAt).
	reflexTriggerThreshold float32 = 0.4
	reflexCooldown         float32 = 10.0

	faceThreatDuration   float32 = 6.0
	faceThreatDoneErr    float32 = 0.1 // hull within this of the threat bearing
	smokeReverseDuration float32 = 7.0
	smokeRetreatDist     float32 = 8.0 // release once backed this far from origin
	fleeDuration         float32 = 8.0
	fleeAwayDist         float32 = 40.0

	// A vehicle that has a live hostile in view within this window is trading
	// fire — its gunner handles the threat, so the reflex holds. Reflexes are
	// for fire it can't answer (unseen shooter / ambush).
	reflexEngageWindow float32 = 3.0

	smokeFieldRadius float32 = 8.0
	smokeFieldTTL    float32 = 8.0
)

func NewVehicleReflexSystem() *VehicleReflexSystem {
	return &VehicleReflexSystem{smokePending: make([]components.WorldPos, 0, 8)}
}

func (sys *VehicleReflexSystem) InitUI(w *ecs.World) {
	sys.worldRef = w
	sys.filter = ecs.NewFilter5[components.Vehicle, components.WorldPos, components.Motion, components.Threat, components.VehicleOverride](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.smokeMap = ecs.NewMap[components.SmokeField](w)
	sys.awareMap = ecs.NewMap[components.Awareness](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.formationMap = ecs.NewMap[components.FormationData](w)
}

func (VehicleReflexSystem) Name() string { return "vehicle_reflex" }

func (VehicleReflexSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *VehicleReflexSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	now := float32(ctx.SimNow)
	sys.smokePending = sys.smokePending[:0]

	q := sys.filter.Query()
	for q.Next() {
		veh, pos, mot, threat, ov := q.Get()
		if ov.Kind != components.VehicleReflexNone {
			sys.tickActive(q.Entity(), pos, mot, threat, ov, now)
			continue
		}
		sys.tryTrigger(q.Entity(), veh, pos, mot, threat, ov, now)
	}

	// Smoke entities are created outside the query — archetype changes cannot
	// happen while it is live.
	for i := range sys.smokePending {
		p := sys.smokePending[i]
		e := sys.worldRef.NewEntity()
		sys.posMap.Add(e, &p)
		sys.smokeMap.Add(e, &components.SmokeField{
			Radius:    smokeFieldRadius,
			ExpiresAt: float64(now) + float64(smokeFieldTTL),
		})
	}
}

// tickActive tracks the live threat direction and releases the override once
// its exit condition or timer is met. The driver keeps steering per the
// reflex until then.
func (sys *VehicleReflexSystem) tickActive(ent ecs.Entity, pos *components.WorldPos,
	mot *components.Motion, threat *components.Threat,
	ov *components.VehicleOverride, now float32) {
	// The trigger-time bearing goes stale the moment the shooter moves or a
	// second source joins the cluster; FaceThreat would then present the
	// hull's side to the live threat and call it done.
	tx, tz := -threat.ThreatDir.X, -threat.ThreatDir.Z
	if tx*tx+tz*tz > 1e-4 {
		ov.ThreatYaw = float32(math.Atan2(float64(tx), float64(tz)))
		if ov.Kind == components.VehicleReflexFlee {
			ov.Retreat = pos.Add(rl.Vector3{
				X: threat.ThreatDir.X * fleeAwayDist,
				Z: threat.ThreatDir.Z * fleeAwayDist,
			})
		}
	}
	released := now >= ov.Until
	switch ov.Kind {
	case components.VehicleReflexFaceThreat:
		if absAngle(wrapAngle(ov.ThreatYaw-mot.Yaw)) < faceThreatDoneErr {
			released = true
		}
	case components.VehicleReflexSmokeAndReverse:
		if dSq := horizDistSq(*pos, ov.Retreat); dSq >= smokeRetreatDist*smokeRetreatDist {
			released = true
		}
	}
	if released {
		ov.Kind = components.VehicleReflexNone
		sys.requestReform(ent)
	}
}

// requestReform asks the member's squad to re-form in place. A marching squad
// drops the flag on its next pass (its slot push already recovers the hull);
// an idle one drives everyone back to their slots — without it a hull that
// fled after the order completed stays parked where the panic ended.
func (sys *VehicleReflexSystem) requestReform(ent ecs.Entity) {
	sm := sys.squadMemberMap.Get(ent)
	if sm == nil || sm.Squad == (ecs.Entity{}) || !sys.worldRef.Alive(sm.Squad) {
		return
	}
	if fd := sys.formationMap.Get(sm.Squad); fd != nil {
		fd.ReformPending = true
	}
}

// tryTrigger arms a reflex when threat is high, the class has one, and the
// cooldown has elapsed.
func (sys *VehicleReflexSystem) tryTrigger(ent ecs.Entity, veh *components.Vehicle,
	pos *components.WorldPos, mot *components.Motion, threat *components.Threat,
	ov *components.VehicleOverride, now float32) {
	spec := components.SpecForVehicle(veh.Kind)
	if spec.ReflexKind == components.VehicleReflexNone {
		return
	}
	if threat.Total < reflexTriggerThreshold {
		return
	}
	if ov.LastAt != 0 && now-ov.LastAt < reflexCooldown {
		return
	}
	// "Return fire beats flinch" only holds for a vehicle that CAN return
	// fire — an unarmed truck flees even from a visible shooter.
	if spec.WeaponCount > 0 && sys.engaging(ent, now) {
		return
	}
	// Direction to the dominant threat: ThreatDir points from the source
	// toward us, so the threat sits along -ThreatDir.
	tx := -threat.ThreatDir.X
	tz := -threat.ThreatDir.Z
	if tx*tx+tz*tz < 1e-4 {
		return // no directional signal — cannot orient a reflex
	}
	ov.Kind = spec.ReflexKind
	ov.ThreatYaw = float32(math.Atan2(float64(tx), float64(tz)))
	ov.LastAt = now

	switch spec.ReflexKind {
	case components.VehicleReflexFaceThreat:
		ov.Until = now + faceThreatDuration
	case components.VehicleReflexSmokeAndReverse:
		ov.Until = now + smokeReverseDuration
		ov.Retreat = *pos // origin snapshot for the retreat-distance exit
		sys.smokePending = append(sys.smokePending, *pos)
	case components.VehicleReflexFlee:
		// Retreat point only — the driver owns locomotion for the duration
		// (P8-f). Pushing it into the ActionQueue destroyed the player's
		// order, and for a squad member FormationSystem overwrote it anyway.
		ov.Until = now + fleeDuration
		ov.Retreat = pos.Add(rl.Vector3{
			X: threat.ThreatDir.X * fleeAwayDist,
			Z: threat.ThreatDir.Z * fleeAwayDist,
		})
	}
}

// engaging reports whether the vehicle has a recent, live hostile in its
// Awareness FIFO — i.e. it is already trading fire and shouldn't flinch.
func (sys *VehicleReflexSystem) engaging(ent ecs.Entity, now float32) bool {
	aware := sys.awareMap.Get(ent)
	if aware == nil {
		return false
	}
	own := sys.factionMap.Get(ent)
	if own == nil {
		return false
	}
	for i := range aware.LastSeen {
		e := aware.LastSeen[i]
		if e.Time == 0 || e.Target == (ecs.Entity{}) || now-e.Time > reflexEngageWindow {
			continue
		}
		if !sys.worldRef.Alive(e.Target) {
			continue
		}
		if f := sys.factionMap.Get(e.Target); f != nil && f.ID != own.ID {
			return true
		}
	}
	return false
}

func absAngle(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func horizDistSq(a, b components.WorldPos) float32 {
	d := a.Sub(b)
	return d.X*d.X + d.Z*d.Z
}
