package main

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/ui"
)

// DebugOverlayState bundles every per-overlay toggle the player can flip via
// the Debug widget (ui/debug_panel.go). The hold-hotkey approach (N/C/V/F/Y/J)
// was dropped in favour of stateful toggles so the player can examine
// overlays at length without holding a key. main.go's render loop reads
// this struct each frame and skips overlay draws whose flag is false.
//
// All overlays are also clipped to a 2-chunk Chebyshev radius around
// `systems.CurrentOriginChunk` so they don't paint the whole world; useful
// when many chunks are loaded and overlay rendering would otherwise dominate
// the frame.
type DebugOverlayState struct {
	NavGrid      bool // surface NavGrid Cost / Flags plates
	CoverMap     bool // CoverMap DirMask shaded plates
	CoverSlots   bool // CoverSlot entity cubes + OriginDir lines
	LevelNavGrid bool // per-Level interior NavGrid plates
	Vision       bool // Awareness.LastSeen pair lines (seer → target)
	Transitions  bool // TransitionRegistry edges (surface↔level, level↔level)
	UnitPaths    bool // each unit's MicroPath waypoints as a line strip
}

// debugOverlay is the singleton state read by main.go's render loop. Widget
// click handlers mutate it through pointers supplied by debugOverlayToggles.
var debugOverlay DebugOverlayState

// debugOverlayToggles returns the row list the Debug widget renders. Built
// fresh each frame so the slice is a stable view into the singleton's fields
// without leaking pointers across struct moves (none possible here — it's a
// global).
func debugOverlayToggles(s *DebugOverlayState) []ui.DebugToggle {
	return []ui.DebugToggle{
		{Label: "Nav grid", On: &s.NavGrid},
		{Label: "Cover map", On: &s.CoverMap},
		{Label: "Cover slots", On: &s.CoverSlots},
		{Label: "Level nav grid", On: &s.LevelNavGrid},
		{Label: "Vision pairs", On: &s.Vision},
		{Label: "Transitions", On: &s.Transitions},
		{Label: "Unit paths", On: &s.UnitPaths},
	}
}

// debugOverlayChunkRadius is the Chebyshev distance from CurrentOriginChunk
// inside which overlay draws are kept. 2 → 5x5 grid of chunks (diameter 5
// in chunk units, or 4 chunks from edge to edge after the centre chunk).
const debugOverlayChunkRadius int32 = 2

// debugChunkInRadius returns true when cc sits within the overlay radius
// around `origin` (Chebyshev metric — square ring, not Euclidean). Used to
// gate every per-chunk overlay iteration so distant chunks don't repaint.
func debugChunkInRadius(cc, origin components.ChunkCoord) bool {
	dx := cc.X - origin.X
	if dx < 0 {
		dx = -dx
	}
	dz := cc.Z - origin.Z
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx <= debugOverlayChunkRadius
	}
	return dz <= debugOverlayChunkRadius
}

// unitPathsSelectedSquad returns the single squad that owns every entity in
// `selected`. Zero entity means the selection contains no squad members or
// spans multiple squads — drawUnitPaths interprets that as "show all".
func unitPathsSelectedSquad(
	selected []ecs.Entity,
	memberMap *ecs.Map[components.SquadMember],
) ecs.Entity {
	if len(selected) == 0 {
		return ecs.Entity{}
	}
	var sq ecs.Entity
	for _, ent := range selected {
		m := memberMap.Get(ent)
		if m == nil {
			continue
		}
		if sq == (ecs.Entity{}) {
			sq = m.Squad
			continue
		}
		if m.Squad != sq {
			return ecs.Entity{} // mixed squads → no narrowing
		}
	}
	return sq
}
