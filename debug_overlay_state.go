package main

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/ui"
)

// DebugOverlayState bundles per-overlay toggles flipped via the Debug widget.
// Overlays are clipped to a 2-chunk Chebyshev radius around CurrentOriginChunk
// so they don't paint the whole world.
type DebugOverlayState struct {
	NavGrid      bool
	CoverMap     bool
	CoverSlots   bool
	LevelNavGrid bool
	Vision       bool
	Transitions  bool
	UnitPaths    bool
}

var debugOverlay DebugOverlayState

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

const debugOverlayChunkRadius int32 = 2

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

// unitPathsSelectedSquad returns the single squad owning every selected
// entity. Zero ⇒ no narrowing (drawUnitPaths shows all).
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
			return ecs.Entity{}
		}
	}
	return sq
}
