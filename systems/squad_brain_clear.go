package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// brainFloor is one storey of the building being cleared.
type brainFloor struct {
	level uint8
	y     float32
}

// stepClearSeq sequences a ClearBuilding order: stack up outside the nearest
// door, put the point pair through it, then sweep storey by storey. The floor
// only advances once the current one is clean — that gate IS the behaviour.
func (sys *SquadBrainSystem) stepClearSeq(squad ecs.Entity, roster *components.CommandRoster,
	plan *components.SquadPlan, building ecs.Entity, now float32) {

	if plan.Mode != components.SquadPlanClearSeq {
		sys.setMode(plan, components.SquadPlanClearSeq, now)
		plan.Phase = components.ClearPhaseStackUp
		plan.Timer = now + clearStackTimeout
		// One phase per decision: advancing in the same call would skip the
		// stack-up entirely whenever the approach leg already parked the squad
		// near the door.
		return
	}
	bld := sys.buildingMap.Get(building)
	if bld == nil {
		return
	}

	switch plan.Phase {
	case components.ClearPhaseStackUp:
		if sys.stackReady(roster, building) || now >= plan.Timer {
			plan.Phase = components.ClearPhaseEnter
			plan.Timer = now + clearEnterTimeout
		}
	case components.ClearPhaseEnter:
		if sys.anyInside(roster, bld.Footprint) || now >= plan.Timer {
			plan.Phase = components.ClearPhaseSweep
			plan.Floor = 0
			plan.Timer = now + clearSweepTimeout
		}
	case components.ClearPhaseSweep:
		floors := sys.floorsOf(building)
		if len(floors) == 0 {
			return
		}
		if sys.hostilesOnFloor(building, bld.Footprint, squad, floors, plan.Floor) > 0 {
			return
		}
		if int(plan.Floor)+1 < len(floors) {
			plan.Floor = floors[int(plan.Floor)+1].level
			plan.Timer = now + clearSweepTimeout
			return
		}
		plan.Phase = components.ClearPhaseDone
	}
}

// stackReady: at least the point pair is standing on its stack marks.
func (sys *SquadBrainSystem) stackReady(roster *components.CommandRoster, building ecs.Entity) bool {
	center, ok := SquadCenter(sys.world, roster, sys.posMap)
	if !ok {
		return false
	}
	sys.stacks = sys.slotPlanner.PlanStackUp(building, center, int(roster.Count))
	if len(sys.stacks) == 0 {
		return true // no door children streamed in — don't stall the sequence
	}
	ready := 0
	for i := uint8(0); i < roster.Count && int(i) < len(sys.stacks); i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		p := sys.posMap.Get(mem)
		if p == nil {
			continue
		}
		if centerXZDistSq(*p, sys.stacks[i].Pos) <= clearStackArrive*clearStackArrive {
			ready++
		}
	}
	return ready >= 2
}

func (sys *SquadBrainSystem) anyInside(roster *components.CommandRoster, fp components.AABB2D) bool {
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		p := sys.posMap.Get(mem)
		if p == nil {
			continue
		}
		x, z := worldXZ(*p)
		if x >= fp.MinX && x <= fp.MaxX && z >= fp.MinZ && z <= fp.MaxZ {
			return true
		}
	}
	return false
}

// floorsOf lists the building's live storeys, ascending.
func (sys *SquadBrainSystem) floorsOf(building ecs.Entity) []brainFloor {
	idx := sys.childIndexRes.Get()
	if idx == nil {
		return nil
	}
	var out []brainFloor
	for _, ch := range idx.Loaded[building] {
		if !sys.world.Alive(ch) {
			continue
		}
		f := sys.floorMap.Get(ch)
		if f == nil {
			continue
		}
		p := sys.posMap.Get(ch)
		if p == nil {
			continue
		}
		out = append(out, brainFloor{level: f.Level, y: p.Local.Y})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].level > out[j].level; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// hostilesOnFloor counts live hostiles inside the footprint whose height puts
// them on storey `level`. Anyone inside but off every plate counts as ground
// floor — a body on the doorstep must not read as "upstairs is clean".
func (sys *SquadBrainSystem) hostilesOnFloor(building ecs.Entity, fp components.AABB2D,
	squad ecs.Entity, floors []brainFloor, level uint8) uint8 {

	var ownFaction uint8
	if f := sys.factionMap.Get(squad); f != nil {
		ownFaction = f.ID
	}
	var count uint8
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos := q.Get()
		f := sys.factionMap.Get(ent)
		if f == nil || f.ID == ownFaction {
			continue
		}
		x, z := worldXZ(*pos)
		if x < fp.MinX || x > fp.MaxX || z < fp.MinZ || z > fp.MaxZ {
			continue
		}
		if floorAt(floors, pos.Local.Y) == level {
			count++
			if count == 255 {
				q.Close()
				break
			}
		}
	}
	return count
}

// floorAt maps a height to the storey it belongs to: the highest plate at or
// below it, ground floor otherwise.
func floorAt(floors []brainFloor, y float32) uint8 {
	if len(floors) == 0 {
		return 0
	}
	level := floors[0].level
	for i := range floors {
		if y >= floors[i].y-1.5 {
			level = floors[i].level
		}
	}
	return level
}
