package main

import (
	"rts-go/components"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

// resolveRMBOrder is the shared MoveTo entry point used by both the 3D-panel
// RMB handler and the map-panel RMB handler (Phase 10 P10). It routes the
// order through SquadService when every selected unit belongs to one squad,
// and otherwise falls back to per-unit pathed MoveTo (Phase 7 behaviour).
//
// `shiftHeld == false` clears the unit's queue before pushing the path so RMB
// is the usual "go here now" verb; `shiftHeld == true` appends, matching the
// existing chained-waypoints UX.
//
// Phase 11 will plug an entity-resolver in front of this (target a Building
// → Garrison, target a Trench → Occupy, etc.); the signature is kept small so
// that resolver can wrap it.
func resolveRMBOrder(
	selected []ecs.Entity,
	target components.WorldPos,
	shiftHeld bool,
	squadService *systems.SquadService,
	navService *systems.NavService,
	squadMemberMap *ecs.Map[components.SquadMember],
	posMap *ecs.Map[components.WorldPos],
	actionQueueMap *ecs.Map[components.ActionQueue],
) {
	if len(selected) == 0 {
		return
	}
	commonSquad, homogeneous := groupSelected(selected, squadMemberMap)
	if homogeneous && commonSquad != (ecs.Entity{}) {
		squadService.OrderMoveTo(commonSquad, target)
		return
	}
	for _, e := range selected {
		if squadMemberMap.Has(e) {
			squadService.Leave(e)
		}
		aq := actionQueueMap.Get(e)
		pos := posMap.Get(e)
		if aq == nil || pos == nil {
			continue
		}
		if !shiftHeld {
			systems.ClearActions(aq)
		}
		path := navService.FindPath(*pos, target, systems.NavOpts{
			Locomotion: components.LocomotionFoot,
		})
		if len(path) == 0 {
			systems.PushAction(aq, components.Action{
				Kind: components.ActionMoveTo, Target: target,
			})
		} else {
			for _, wp := range path {
				systems.PushAction(aq, components.Action{
					Kind: components.ActionMoveTo, Target: wp,
				})
			}
		}
	}
}
