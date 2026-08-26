package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ForceTrafficSystem releases scheduled ground forces. It is the ground twin of
// AirTrafficSystem, deliberately: "something shows up at time T" is ONE
// mechanism, and a mission's starting order of battle is just the arrivals with
// At = 0 (block D P2). A separate "initial placement" path would be the same
// code with a different name and its own bugs.
type ForceTrafficSystem struct {
	arrivalFilter *ecs.Filter1[components.ForceArrival]
	arrivalMap    *ecs.Map[components.ForceArrival]
	world         *ecs.World

	squads   *SquadService
	roles    *RoleService
	vehicles VehicleSpawner
	units    func(components.WorldPos) ecs.Entity

	due []ecs.Entity
}

// VehicleSpawner is the slice of the vehicle factory this system needs; keeping
// it an interface stops systems/ from importing entities/.
type VehicleSpawner interface {
	Spawn(pos components.WorldPos, kind components.VehicleKind,
		faction uint8, controller uint8) ecs.Entity
}

func NewForceTrafficSystem() *ForceTrafficSystem { return &ForceTrafficSystem{} }

func (sys *ForceTrafficSystem) InitUI(w *ecs.World) {
	sys.arrivalFilter = ecs.NewFilter1[components.ForceArrival](w)
	sys.arrivalMap = ecs.NewMap[components.ForceArrival](w)
	sys.world = w
}

// SetSpawners wires the factories after boot builds them.
func (sys *ForceTrafficSystem) SetSpawners(sq *SquadService, ro *RoleService,
	veh VehicleSpawner, units func(components.WorldPos) ecs.Entity) {
	sys.squads, sys.roles, sys.vehicles, sys.units = sq, ro, veh, units
}

func (ForceTrafficSystem) Name() string { return "force_traffic" }

func (ForceTrafficSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ForceTrafficSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive || sys.squads == nil {
		return
	}
	now := float32(ctx.SimNow)

	// Collect first, release after: spawning inside a live query would change
	// archetypes while Ark holds it locked.
	sys.due = sys.due[:0]
	q := sys.arrivalFilter.Query()
	for q.Next() {
		if q.Get().At <= now {
			sys.due = append(sys.due, q.Entity())
		}
	}
	q.Close()

	for _, ent := range sys.due {
		arr := sys.arrivalMap.Get(ent)
		if arr == nil {
			continue
		}
		sys.release(*arr)
		sys.world.RemoveEntity(ent)
	}
}

func (sys *ForceTrafficSystem) release(a components.ForceArrival) {
	switch a.Kind {
	case components.ForceVehicle:
		if sys.vehicles != nil {
			sys.vehicles.Spawn(a.Pos, a.Vehicle, a.Side, a.Ctrl)
		}
	default:
		if sys.roles == nil || sys.units == nil {
			return
		}
		sys.squads.CreateFromTemplate(SquadTemplate(a.Template), a.Pos,
			components.FormationLoose, components.Faction{ID: a.Side},
			components.Controller{Owner: a.Ctrl}, sys.roles, sys.units)
	}
}

// SpawnForceArrival files one scheduled ground arrival.
func SpawnForceArrival(w *ecs.World, a components.ForceArrival) ecs.Entity {
	ent := w.NewEntity()
	v := a
	ecs.NewMap[components.ForceArrival](w).Add(ent, &v)
	ecs.NewMap[components.AlwaysActive](w).Add(ent, &components.AlwaysActive{})
	return ent
}
