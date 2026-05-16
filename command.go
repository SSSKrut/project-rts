package main

import (
	"rts-go/components"
	"rts-go/systems"

	"github.com/mlange-42/ark/ecs"
)

// HitTestKind tags what kind of world entity the cursor's WorldPos landed on.
// PHASE-11.md P6: resolver maps cursor-hit → OrderKindCode.
type HitTestKind uint8

const (
	HitTerrain HitTestKind = iota
	HitBuilding
	HitTrench
)

// HitTestResult is the answer to "what's under this target Pos?". For
// terrain hits Entity is zero. For Building / Trench hits Entity is the
// matching entity.
type HitTestResult struct {
	Kind   HitTestKind
	Entity ecs.Entity
}

// HitTester bundles the per-frame handles needed to resolve a cursor target.
// Built once in main.go and reused. Building hit-test iterates a Filter1 of
// Building components; Trench hit-test reads the TrenchNetwork resource and
// the TrenchRoot map to attribute the hit to a specific TrenchRoot entity.
type HitTester struct {
	BuildingFilter *ecs.Filter1[components.Building]
	BuildingMap    *ecs.Map[components.Building]
	TrenchRootMap  *ecs.Map[components.TrenchRoot]
	TrenchRoots    *ecs.Filter1[components.TrenchRoot]
	Trenches       *components.TrenchNetwork
	// TrenchHitRadius is how close (metres) the target must be to a polyline
	// segment to count as a trench hit. PHASE-11.md P6 suggests 2-3 m.
	TrenchHitRadius float32
}

// HitTest classifies a WorldPos. Priority: Building (point-in-AABB) → Trench
// (distance-to-polyline) → Terrain. Building wins over trench when an
// authored trench accidentally clips a footprint; that's the rarer case in
// real maps and the easier mistake to read.
func (h *HitTester) HitTest(target components.WorldPos) HitTestResult {
	wx := float32(target.Chunk.X)*components.ChunkSize + target.Local.X
	wz := float32(target.Chunk.Z)*components.ChunkSize + target.Local.Z

	if h.BuildingFilter != nil {
		q := h.BuildingFilter.Query()
		for q.Next() {
			b := q.Get()
			if b.Footprint.Contains(wx, wz) {
				ent := q.Entity()
				q.Close()
				return HitTestResult{Kind: HitBuilding, Entity: ent}
			}
		}
	}

	if h.Trenches != nil && h.TrenchRoots != nil {
		// Walk each polyline; if any segment is within radius, find the
		// TrenchRoot pointing at this Index via TrenchRootMap iteration.
		radius := h.TrenchHitRadius
		if radius <= 0 {
			radius = 2.5
		}
		rSq := radius * radius
		bestIdx := -1
		bestDSq := rSq
		for i := range h.Trenches.Lines {
			pts := h.Trenches.Lines[i].Points
			for k := 1; k < len(pts); k++ {
				ax := float32(pts[k-1].Chunk.X)*components.ChunkSize + pts[k-1].Local.X
				az := float32(pts[k-1].Chunk.Z)*components.ChunkSize + pts[k-1].Local.Z
				bx := float32(pts[k].Chunk.X)*components.ChunkSize + pts[k].Local.X
				bz := float32(pts[k].Chunk.Z)*components.ChunkSize + pts[k].Local.Z
				dx, dz := bx-ax, bz-az
				lenSq := dx*dx + dz*dz
				if lenSq < 1e-6 {
					continue
				}
				t := ((wx-ax)*dx + (wz-az)*dz) / lenSq
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				cx := ax + dx*t
				cz := az + dz*t
				dSq := (wx-cx)*(wx-cx) + (wz-cz)*(wz-cz)
				if dSq < bestDSq {
					bestDSq = dSq
					bestIdx = i
				}
			}
		}
		if bestIdx >= 0 {
			// Look up the TrenchRoot entity with matching Index.
			qr := h.TrenchRoots.Query()
			for qr.Next() {
				root := qr.Get()
				if root.Index == bestIdx {
					ent := qr.Entity()
					qr.Close()
					return HitTestResult{Kind: HitTrench, Entity: ent}
				}
			}
		}
	}

	return HitTestResult{Kind: HitTerrain}
}

// resolveTargetIntoOrder converts a HitTestResult to an OrderKindCode + entity
// target. PHASE-11.md P6 mapping. `kindOverride != nil` (from pie menu) wins
// over hit-test.
func resolveTargetIntoOrder(hit HitTestResult, kindOverride *components.OrderKindCode) (components.OrderKindCode, ecs.Entity) {
	if kindOverride != nil {
		// Pie menu override: keep the entity from hit-test if the kind matches
		// the natural mapping; otherwise drop entity (terrain Pos suffices).
		switch *kindOverride {
		case components.OrderKindGarrison:
			if hit.Kind == HitBuilding {
				return components.OrderKindGarrison, hit.Entity
			}
			return components.OrderKindGarrison, ecs.Entity{}
		case components.OrderKindOccupyTrench:
			if hit.Kind == HitTrench {
				return components.OrderKindOccupyTrench, hit.Entity
			}
			return components.OrderKindOccupyTrench, ecs.Entity{}
		default:
			return *kindOverride, ecs.Entity{}
		}
	}
	switch hit.Kind {
	case HitBuilding:
		return components.OrderKindGarrison, hit.Entity
	case HitTrench:
		return components.OrderKindOccupyTrench, hit.Entity
	default:
		return components.OrderKindMoveTo, ecs.Entity{}
	}
}

// detectPreset returns the MovementPreset whose profile equals `p`, or
// PresetDefault if no preset matches (player has hand-edited an axis). Used
// by the `[` / `]` hotkeys to find the current position in the preset cycle.
func detectPreset(p components.MovementProfile) components.MovementPreset {
	presets := [6]components.MovementPreset{
		components.PresetDefault, components.PresetCautious, components.PresetRush,
		components.PresetSprint, components.PresetStealth, components.PresetProneCrawl,
	}
	for _, preset := range presets {
		if components.ApplyPreset(preset) == p {
			return preset
		}
	}
	return components.PresetDefault
}

// cyclePreset advances the preset index by `step` (typically ±1) with wrap.
// Hotkeys `[` (step=-1) and `]` (step=+1) drive this.
func cyclePreset(cur components.MovementPreset, step int) components.MovementPreset {
	const count = 6
	next := (int(cur) + step) % count
	if next < 0 {
		next += count
	}
	return components.MovementPreset(next)
}

// RMBModifiers captures the keyboard modifier state at the moment of RMB
// press (snapshot semantics: held-key state may change before the release
// that actually commits the order, so we lock it on press). Phase 13 M13.5
// uses these to attach optional OrderParamMovementProfile / OrderParamAttackMove
// to the spawned order entity.
type RMBModifiers struct {
	Sneak      bool // Ctrl+RMB: Stealth preset MovementProfile override
	Sprint     bool // Double-RMB (within rmbDoubleWindow): Sprint preset
	AttackMove bool // Alt+RMB: OrderParamAttackMove flag (Phase 14 scaffold)
}

// rmbModifiersFromPress maps captured press-time modifier flags into the
// canonical RMBModifiers struct. Wraps the boolean state so the resolver
// signature stays stable when more modifiers land (Phase 19+ joint, etc.).
func rmbModifiersFromPress(ctrl, alt, double bool) RMBModifiers {
	return RMBModifiers{
		Sneak:      ctrl,
		Sprint:     double,
		AttackMove: alt,
	}
}

// applyModifiersToParams converts press-time modifiers into the per-order
// OrderParams fields. Sneak / Sprint write OrderParams.MovementOverride;
// AttackMove sets the bool flag. Sprint wins over Sneak when both are set
// (intentional — double-Ctrl+RMB sprints quietly is not a meaningful combo).
func applyModifiersToParams(p systems.OrderParams, mods RMBModifiers) systems.OrderParams {
	if mods.Sprint {
		profile := components.ApplyPreset(components.PresetSprint)
		p.MovementOverride = &profile
	} else if mods.Sneak {
		profile := components.ApplyPreset(components.PresetStealth)
		p.MovementOverride = &profile
	}
	if mods.AttackMove {
		p.AttackMove = true
	}
	return p
}

// resolveRMBOrder is the shared RMB entry point for 3D-panel and Map-panel
// clicks. Phase 11 reshape (M11.3 + M11.5) + Phase 13 M13.5 extension:
//
//   - Hit-test classifies the target as Terrain / Building / Trench, mapping
//     to OrderKindCode {MoveTo / Garrison / OccupyTrench}.
//   - Multi-squad selection no longer dissolves squads (closes ISSUES #3).
//     Each touched squad gets its own Order; soloists in the same selection
//     still receive per-unit ActionQueue MoveTo (Phase 7 fallback).
//   - shiftHeld switches IssueOrder to append mode (chained orders via
//     OrderChain.Next).
//   - `kindOverride` from a pie menu commit forces the kind regardless of
//     hit-test.
//   - mods carries the press-time modifier state (Phase 13 M13.5): Ctrl =
//     Stealth preset, Double-RMB = Sprint preset, Alt = AttackMove flag.
func resolveRMBOrder(
	selected []ecs.Entity,
	target components.WorldPos,
	shiftHeld bool,
	kindOverride *components.OrderKindCode,
	mods RMBModifiers,
	hitTester *HitTester,
	squadService *systems.SquadService,
	navService *systems.NavService,
	squadMemberMap *ecs.Map[components.SquadMember],
	posMap *ecs.Map[components.WorldPos],
	actionQueueMap *ecs.Map[components.ActionQueue],
) {
	if len(selected) == 0 {
		return
	}

	// Hit-test the target (terrain by default if no tester is wired up).
	hit := HitTestResult{Kind: HitTerrain}
	if hitTester != nil {
		hit = hitTester.HitTest(target)
	}
	kind, entityTarget := resolveTargetIntoOrder(hit, kindOverride)

	// PHASE-11.md P9: distribute orders by squad ownership. SquadsToOrder is
	// the unique squads touched by the selection; Soloists are units not in
	// any squad. Never call SquadService.Leave — that was the ISSUES #3 bug.
	groups := groupSelectionByOwner(selected, squadMemberMap)
	params := applyModifiersToParams(systems.OrderParams{}, mods)
	for _, s := range groups.SquadsToOrder {
		squadService.IssueOrder(s, kind, target, entityTarget, shiftHeld, params)
	}

	if len(groups.Soloists) == 0 {
		return
	}
	for _, e := range groups.Soloists {
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
