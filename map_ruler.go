package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/ui"
)

// Map ruler input (Phase 19.7 M4). Hold L over the map and drag: the reading
// freezes on release so it can be read without a steady hand, and clears when
// L goes up. L rather than R (roofs) or B (building interiors) — those are
// 3D-panel keys, and one key meaning two things is a bug waiting for the day
// the focus rule changes.
type rulerState struct {
	Active   bool
	Dragging bool
	From     components.WorldPos
	To       components.WorldPos
}

// handleMapRuler returns true when it claimed the LMB press, so the map's
// selection path never sees it.
func (g *Game) handleMapRuler() bool {
	st := &g.UI.Ruler
	if !rl.IsKeyDown(rl.KeyL) {
		*st = rulerState{}
		return false
	}
	// Release ends the drag wherever the cursor is: a drag that wandered off
	// the panel would otherwise resume the moment it wandered back.
	if st.Dragging && rl.IsMouseButtonReleased(rl.MouseButtonLeft) {
		st.Dragging = false
	}
	if g.Frame.Focused != ui.PanelMap || g.chromeBusy() {
		return false
	}
	consumed := false
	cursorWorld := ui.MapPanelToWorld(g.Frame.Cursor, g.UI.MapCam, g.Frame.PanelMapContent)

	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		st.Active = true
		st.Dragging = true
		st.From = cursorWorld
		st.To = cursorWorld
		consumed = true
	}
	if st.Dragging {
		st.To = cursorWorld
		consumed = true
	}
	return consumed
}

// drawMapRuler mirrors the input gate: nothing to draw unless a measurement
// is live.
func (g *Game) drawMapRuler() {
	if !g.UI.Ruler.Active {
		return
	}
	dist := components.Distance(g.UI.Ruler.From, g.UI.Ruler.To)
	rng := ui.RulerNoWeapon
	if reach := g.selectionWeaponReach(); reach > 0 {
		rng = ui.RulerOutOfRange
		if dist <= reach {
			rng = ui.RulerInRange
		}
	}
	ui.DrawMapRuler(g.Frame.PanelMap, g.UI.MapCam,
		g.UI.Ruler.From, g.UI.Ruler.To, g.hudFont, rng)
}

// selectionWeaponReach is the longest primary-weapon range in the selection,
// squads expanded to their members. 0 = nothing selected can shoot, which is
// why the readout stays neutral rather than claiming "out of range".
func (g *Game) selectionWeaponReach() float32 {
	best := float32(0)
	consider := func(unit ecs.Entity) {
		eq := g.Ctx.Inspector.EquipmentMap.Get(unit)
		if eq == nil || eq.Primary == (ecs.Entity{}) || !g.App.World.Alive(eq.Primary) {
			return
		}
		if wp := g.Ctx.Inspector.WeaponMap.Get(eq.Primary); wp != nil && wp.RangeM > best {
			best = wp.RangeM
		}
	}
	for _, e := range g.Sel.Units {
		if !g.App.World.Alive(e) {
			continue
		}
		if roster := g.Maps.Roster.Get(e); roster != nil {
			for i := uint8(0); i < roster.Count; i++ {
				if m := roster.Members[i]; m != (ecs.Entity{}) && g.App.World.Alive(m) {
					consider(m)
				}
			}
			continue
		}
		consider(e)
	}
	return best
}
