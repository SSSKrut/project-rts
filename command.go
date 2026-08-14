package main

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

// HitTestKind tags what kind of world entity the cursor's WorldPos landed on.
type HitTestKind uint8

const (
	HitTerrain HitTestKind = iota
	HitBuilding
	HitTrench
	HitUnit
)

type HitTestResult struct {
	Kind   HitTestKind
	Entity ecs.Entity
}

type HitTester struct {
	BuildingFilter *ecs.Filter1[components.Building]
	BuildingMap    *ecs.Map[components.Building]
	TrenchRootMap  *ecs.Map[components.TrenchRoot]
	TrenchRoots    *ecs.Filter1[components.TrenchRoot]
	Trenches       *components.TrenchNetwork
	// TrenchHitRadius is the snap distance (m) to a polyline segment.
	TrenchHitRadius float32
	UnitFilter      *ecs.Filter2[components.Unit, components.WorldPos]
	FactionMap      *ecs.Map[components.Faction]
	UnitHitRadius   float32
	// OwnFaction: units of this faction are never attack-pick targets.
	OwnFaction uint8
}

// HitTest classifies a WorldPos. Priority: Unit (closest in radius) ->
// Building (point-in-AABB) -> Trench (distance-to-polyline) -> Terrain. Unit
// wins so RMB on an enemy inside a building reads as AttackTarget.
func (h *HitTester) HitTest(target components.WorldPos) HitTestResult {
	wx := float32(target.Chunk.X)*components.ChunkSize + target.Local.X
	wz := float32(target.Chunk.Z)*components.ChunkSize + target.Local.Z

	if h.UnitFilter != nil && h.FactionMap != nil {
		radius := h.UnitHitRadius
		if radius <= 0 {
			radius = 1.5
		}
		bestDSq := radius * radius
		var bestEnt ecs.Entity
		q := h.UnitFilter.Query()
		for q.Next() {
			_, pos := q.Get()
			ent := q.Entity()
			f := h.FactionMap.Get(ent)
			if f == nil || f.ID == h.OwnFaction {
				continue
			}
			ux := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
			uz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
			dSq := (ux-wx)*(ux-wx) + (uz-wz)*(uz-wz)
			if dSq < bestDSq {
				bestDSq = dSq
				bestEnt = ent
			}
		}
		if bestEnt != (ecs.Entity{}) {
			return HitTestResult{Kind: HitUnit, Entity: bestEnt}
		}
	}

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
// target. `kindOverride != nil` wins; a kind that demands an entity but the
// hit doesn't match falls back to MoveTo (misclick).
func resolveTargetIntoOrder(hit HitTestResult, kindOverride *components.OrderKindCode) (components.OrderKindCode, ecs.Entity) {
	hitIsEntity := hit.Kind == HitBuilding || hit.Kind == HitTrench || hit.Kind == HitUnit
	hitMatchesKind := func(k components.OrderKindCode) bool {
		switch k {
		case components.OrderKindGarrison, components.OrderKindOccupyBuilding:
			return hit.Kind == HitBuilding
		case components.OrderKindOccupyTrench:
			return hit.Kind == HitTrench
		case components.OrderKindAttackTarget:
			return hit.Kind == HitUnit
		}
		return false
	}

	if kindOverride != nil {
		spec := components.SpecForOrderKind(*kindOverride)
		if spec.NeedsEntity {
			if hitIsEntity && hitMatchesKind(*kindOverride) {
				return *kindOverride, hit.Entity
			}
			return components.OrderKindMoveTo, ecs.Entity{}
		}
		return *kindOverride, ecs.Entity{}
	}
	switch hit.Kind {
	case HitBuilding:
		return components.OrderKindOccupyBuilding, hit.Entity
	case HitTrench:
		return components.OrderKindOccupyTrench, hit.Entity
	case HitUnit:
		return components.OrderKindAttackTarget, hit.Entity
	default:
		return components.OrderKindMoveTo, ecs.Entity{}
	}
}

// detectPreset returns the MovementPreset whose profile equals `p`, or
// PresetDefault if no preset matches.
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

// cyclePreset advances the preset index by `step` with wrap.
func cyclePreset(cur components.MovementPreset, step int) components.MovementPreset {
	const count = 6
	next := (int(cur) + step) % count
	if next < 0 {
		next += count
	}
	return components.MovementPreset(next)
}

// RMBModifiers captures the keyboard modifier state at the moment of RMB
// press (snapshot semantics: locked on press because held-key state may
// change before release).
type RMBModifiers struct {
	Sneak      bool // Ctrl+RMB
	Sprint     bool // Double-RMB within rmbDoubleWindow
	AttackMove bool // Alt+RMB
}

func rmbModifiersFromPress(ctrl, alt, double bool) RMBModifiers {
	return RMBModifiers{
		Sneak:      ctrl,
		Sprint:     double,
		AttackMove: alt,
	}
}

// buildBuildingPopupSections constructs the building right-click popup items.
// `building` must be a live Building entity. Level entities are pulled from
// buildingPlanIndex.Levels[building] in DisplayOrder ascending; single-level
// buildings skip the L-picker.
//
// Labels are ASCII-only — runtime UI is English-only until i18n lands.
func buildBuildingPopupSections(
	building ecs.Entity,
	planIndex *systems.BuildingPlanIndex,
	levelMap *ecs.Map[components.Level],
) []ui.ContextMenuSection {
	atk := ui.ContextMenuSection{
		Header: "Attack",
		Items: []ui.ContextMenuItem{
			{
				Label:   "Clear and occupy",
				Tooltip: "Enter, destroy enemies inside, hold the building",
				Glyph:   'C',
				Kind:    components.OrderKindClearBuilding,
				Enabled: true,
			},
			{
				Label:   "Suppress fire",
				Tooltip: "Fire on the building to suppress its garrison",
				Glyph:   'S',
				Kind:    components.OrderKindSuppressFire,
				Enabled: false,
			},
		},
	}
	inter := ui.ContextMenuSection{
		Header: "Interact",
		Items: []ui.ContextMenuItem{
			{
				Label:   "Attacking position at windows",
				Tooltip: "Spread across windows with shooting arcs",
				Glyph:   'G',
				Kind:    components.OrderKindGarrison,
				Enabled: true,
			},
			{
				Label:                "Hidden position",
				Tooltip:              "Enter quietly, stay crouched, hold fire",
				Glyph:                'H',
				Kind:                 components.OrderKindOccupyBuilding,
				HoldFireCrouchPreset: true,
				Enabled:              true,
			},
		},
	}

	if planIndex != nil && levelMap != nil {
		levels := planIndex.Levels[building]
		if len(levels) >= 2 {
			type lvlInfo struct {
				entity ecs.Entity
				name   string
				order  uint8
			}
			var arr []lvlInfo
			for _, lvl := range levels {
				if l := levelMap.Get(lvl); l != nil {
					name := l.Label()
					if name == "" {
						name = fmt.Sprintf("L%d", l.DisplayOrder)
					}
					arr = append(arr, lvlInfo{entity: lvl, name: name, order: l.DisplayOrder})
				}
			}
			for i := 1; i < len(arr); i++ {
				for j := i; j > 0 && arr[j-1].order > arr[j].order; j-- {
					arr[j-1], arr[j] = arr[j], arr[j-1]
				}
			}
			for _, l := range arr {
				inter.Items = append(inter.Items, ui.ContextMenuItem{
					Label:       fmt.Sprintf("Occupy %s", l.name),
					Tooltip:     "Enter this level via the nearest stairs",
					Glyph:       '-',
					Kind:        components.OrderKindMoveTo,
					LevelEntity: l.entity,
					Enabled:     true,
				})
			}
		}
	}
	return []ui.ContextMenuSection{atk, inter}
}

// pickRoomTarget maps the raw press point to a room of `lvl`: the NEAREST
// room rect's centre at the storey floor (containment = distance zero). The
// level AABB centre is only for room-less levels — on a ring building it is
// the middle of the open well, a goal nav can never reach. Shared by the
// popup commit, the hover ghost and the ai_* level probes so preview,
// execution and gate all order the same point.
func pickRoomTarget(lvl *components.Level, raw components.WorldPos) components.WorldPos {
	wx := float32(raw.Chunk.X)*components.ChunkSize + raw.Local.X
	wz := float32(raw.Chunk.Z)*components.ChunkSize + raw.Local.Z
	cx, cz := lvl.AABB.CenterX(), lvl.AABB.CenterZ()
	best := float32(math.MaxFloat32)
	for r := uint8(0); r < lvl.RoomCount; r++ {
		rm := &lvl.Rooms[r]
		dx := float32(0)
		if wx < rm.MinX {
			dx = rm.MinX - wx
		} else if wx > rm.MaxX {
			dx = wx - rm.MaxX
		}
		dz := float32(0)
		if wz < rm.MinZ {
			dz = rm.MinZ - wz
		} else if wz > rm.MaxZ {
			dz = wz - rm.MaxZ
		}
		if d := dx*dx + dz*dz; d < best {
			best = d
			cx, cz = rm.CenterX(), rm.CenterZ()
			if d == 0 {
				break
			}
		}
	}
	return components.WorldPos{}.Add(rl.Vector3{X: cx, Y: lvl.AABB.MinY, Z: cz})
}

// issueBuildingPopupOrder commits a building-popup item, bypassing hit-test.
// LevelEntity != zero ⇒ MoveTo on the picked room (rawTarget inside a room
// rect) or the Level centre; nav routes via stairs.
// HoldFireCrouchPreset ⇒ OccupyBuilding + PresetStealth + HoldFire RoE override.
func issueBuildingPopupOrder(
	selected []ecs.Entity,
	item ui.ContextMenuItem,
	building ecs.Entity,
	pressTarget components.WorldPos,
	rawTarget components.WorldPos,
	shiftHeld bool,
	squadService *systems.SquadService,
	navService *systems.NavService,
	squadMemberMap *ecs.Map[components.SquadMember],
	posMap *ecs.Map[components.WorldPos],
	actionQueueMap *ecs.Map[components.ActionQueue],
	levelMap *ecs.Map[components.Level],
) {
	params := systems.OrderParams{}
	target := pressTarget
	entity := building
	kind := item.Kind

	if item.LevelEntity != (ecs.Entity{}) && levelMap != nil {
		if lvl := levelMap.Get(item.LevelEntity); lvl != nil {
			target = pickRoomTarget(lvl, rawTarget)
			entity = item.LevelEntity
			kind = components.OrderKindMoveTo
		}
	}

	fmt.Printf("[rmb-popup] item=%q kind=%d holdFireCrouch=%v level=%v target=(%.1f,%.1f)\n",
		item.Label, kind, item.HoldFireCrouchPreset, item.LevelEntity != (ecs.Entity{}),
		target.Local.X+float32(target.Chunk.X)*components.ChunkSize,
		target.Local.Z+float32(target.Chunk.Z)*components.ChunkSize)

	if item.HoldFireCrouchPreset {
		// "Hidden position" is order + STANDING rules in one gesture — the
		// crouch/hold-fire half must survive the order's completion.
		groups := groupSelectionByOwner(selected, squadMemberMap)
		for _, s := range groups.SquadsToOrder {
			squadService.IssueOrderHidden(s, target, entity, shiftHeld)
		}
		for _, e := range groups.Soloists {
			pushSoloMove(actionQueueMap, posMap, e, target, shiftHeld)
		}
		return
	}

	issueDirectOrder(selected, kind, target, entity, shiftHeld, params,
		squadService, navService, squadMemberMap, posMap, actionQueueMap)
}

// issueDirectOrder dispatches an order to every selected squad / soloist
// without hit-test (kind / entity / target already known).
func issueDirectOrder(
	selected []ecs.Entity,
	kind components.OrderKindCode,
	target components.WorldPos,
	entity ecs.Entity,
	shiftHeld bool,
	params systems.OrderParams,
	squadService *systems.SquadService,
	navService *systems.NavService,
	squadMemberMap *ecs.Map[components.SquadMember],
	posMap *ecs.Map[components.WorldPos],
	actionQueueMap *ecs.Map[components.ActionQueue],
) {
	if len(selected) == 0 {
		return
	}
	groups := groupSelectionByOwner(selected, squadMemberMap)
	for _, s := range groups.SquadsToOrder {
		squadService.IssueOrder(s, kind, target, entity, shiftHeld, params)
	}
	for _, e := range groups.Soloists {
		pushSoloMove(actionQueueMap, posMap, e, target, shiftHeld)
	}
}

// pushSoloMove drives one non-squad unit toward `target` — the soloist arm
// of every RMB order path (and the ai_march_* scenes, which call it directly
// so the gate exercises the live code). One MoveTo with the FINAL target:
// MicroPathSystem plans the route (GoalSnap drift flips Dirty next tick).
// The old path-dump overflowed the 4-slot ActionQueue on any long order,
// leaving only the last 4 waypoints — a straight chord across terrain and
// obstacles (ISSUES #13).
func pushSoloMove(
	actionQueueMap *ecs.Map[components.ActionQueue],
	posMap *ecs.Map[components.WorldPos],
	e ecs.Entity,
	target components.WorldPos,
	shiftHeld bool,
) {
	aq := actionQueueMap.Get(e)
	if aq == nil || posMap.Get(e) == nil {
		return
	}
	if !shiftHeld {
		systems.ClearActions(aq)
	}
	systems.PushAction(aq, components.Action{
		Kind: components.ActionMoveTo, Target: target,
	})
}

// applyModifiersToParams converts press-time modifiers into OrderParams.
// Sprint wins over Sneak when both are set (double-Ctrl+RMB sprints quietly
// is not a meaningful combo).
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
// clicks. Hit-test classifies the target → OrderKindCode; each touched
// squad gets its own Order, soloists get per-unit ActionQueue MoveTo.
// shiftHeld switches IssueOrder to append mode. `kindOverride` forces a kind.
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
	params := applyModifiersToParams(systems.OrderParams{}, mods)
	resolveRMBOrderWithParams(selected, target, shiftHeld, kindOverride, params, hitTester,
		squadService, navService, squadMemberMap, posMap, actionQueueMap)
}

// resolveRMBOrderWithParams accepts a fully-formed OrderParams. Used by the
// facing-drag path which sets HasFacing / FacingYawRad directly.
func resolveRMBOrderWithParams(
	selected []ecs.Entity,
	target components.WorldPos,
	shiftHeld bool,
	kindOverride *components.OrderKindCode,
	params systems.OrderParams,
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

	hit := HitTestResult{Kind: HitTerrain}
	if hitTester != nil {
		hit = hitTester.HitTest(target)
	}
	kind, entityTarget := resolveTargetIntoOrder(hit, kindOverride)
	fmt.Printf("[order] resolved kind=%d hitKind=%d entityTarget=%v target=(%.1f,%.1f)\n",
		kind, hit.Kind, entityTarget,
		target.Local.X+float32(target.Chunk.X)*components.ChunkSize,
		target.Local.Z+float32(target.Chunk.Z)*components.ChunkSize)

	groups := groupSelectionByOwner(selected, squadMemberMap)
	for _, s := range groups.SquadsToOrder {
		squadService.IssueOrder(s, kind, target, entityTarget, shiftHeld, params)
	}

	if len(groups.Soloists) == 0 {
		return
	}
	for _, e := range groups.Soloists {
		pushSoloMove(actionQueueMap, posMap, e, target, shiftHeld)
	}

}
