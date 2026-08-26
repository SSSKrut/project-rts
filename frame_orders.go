package main

import (
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// handleOrders is the RMB command surface (tap / facing-drag / building popup)
// plus the keyboard command set.
func (g *Game) handleOrders() {
	// RMB orders. Flow: press snapshots target+modifiers+hovered-building;
	// while held, drag>8px enters facing-drag mode and hold≥200ms over a
	// building opens the ContextMenu popup; release commits the hovered popup
	// item / facing-drag yaw / tap; ESC closes the popup.
	// Phase 18.5.F: RMB on a Map contact opens the classification popup
	// instead of going through the order pathway.
	if rl.IsMouseButtonPressed(rl.MouseButtonRight) && !g.UI.Floating.IsBusy(g.Frame.Cursor) &&
		g.Frame.Focused == ui.PanelMap && !g.UI.CtxMenu.IsActive() && !g.UI.ContactCtxMenu.IsActive() {
		mapCtx := g.mapPickCtx()
		if hit := ui.PickContactAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 14); hit != (ecs.Entity{}) && g.App.World.Alive(hit) {
			sections := ui.BuildContactContextSections(&g.Res.Symbology)
			g.UI.ContactCtxMenu.Begin(g.Frame.Cursor, sections, ui.PanelMap, g.Frame.PanelMapContent)
			g.UI.ContactMenuTarget = hit
		}
	}

	if rl.IsMouseButtonPressed(rl.MouseButtonRight) && !g.UI.Floating.IsBusy(g.Frame.Cursor) && !g.UI.ContactCtxMenu.IsActive() {
		var (
			pressTarget components.WorldPos
			pressRaw    components.WorldPos
			targetOK    bool
		)
		switch g.Frame.Focused {
		case ui.Panel3D:
			pressTarget, targetOK = mouseTargetWorldPos(systems.CurrentCamera,
				g.Frame.AnchorPos.ToRenderSpace(systems.CurrentOriginChunk),
				g.Frame.Panel3DLocal, g.Frame.Panel3DW, g.Frame.Panel3DH)
			pressRaw = pressTarget
			// Snap to ground-floor centre when cursor visually over a
			// building but raycast lands just outside the footprint.
			if targetOK && g.Sel.HoveredBuilding != (ecs.Entity{}) {
				if levels := g.Res.BuildingPlanIx.Levels[g.Sel.HoveredBuilding]; len(levels) > 0 {
					if lvl := g.Maps.Level.Get(levels[0]); lvl != nil {
						wx := lvl.AABB.CenterX()
						wz := lvl.AABB.CenterZ()
						cx := int32(math.Floor(float64(wx) / float64(components.ChunkSize)))
						cz := int32(math.Floor(float64(wz) / float64(components.ChunkSize)))
						pressTarget = components.WorldPos{
							Chunk: components.ChunkCoord{X: cx, Z: cz},
							Local: rl.Vector3{
								X: wx - float32(cx)*components.ChunkSize,
								Y: lvl.AABB.MinY,
								Z: wz - float32(cz)*components.ChunkSize,
							},
						}
					}
				}
			}
		case ui.PanelMap:
			pressTarget = ui.MapPanelToWorld(g.Frame.Cursor, g.UI.MapCam, g.Frame.PanelMapContent)
			pressRaw = pressTarget
			targetOK = true
		}
		if targetOK && len(g.Sel.Units) > 0 && (g.Frame.Focused == ui.Panel3D || g.Frame.Focused == ui.PanelMap) {
			now := float32(g.App.Elapsed().Seconds())
			g.UI.RMB.Active = true
			g.UI.RMB.SourcePanel = g.Frame.Focused
			g.UI.RMB.PressOrigin = g.Frame.Cursor
			g.UI.RMB.PressTarget = pressTarget
			g.UI.RMB.PressRaw = pressRaw
			g.UI.RMB.PressTimeSec = now
			g.UI.RMB.HoveredBldg = g.Sel.HoveredBuilding
			g.UI.RMB.HasSelection = true
			g.UI.RMB.FacingActive = false
			g.UI.RMB.Ctrl = g.Frame.Ctrl
			g.UI.RMB.Alt = g.Frame.Alt
			g.UI.RMB.Double = (now - g.UI.LastRMBPressAt) <= rmbDoubleWindow
			g.UI.LastRMBPressAt = now
			fmt.Printf("[rmb] press g.Sel.Hovered=%v target=(%.1f,%.1f) g.Sel.Units=%d g.Frame.Focused=%s\n",
				g.Sel.HoveredBuilding != (ecs.Entity{}),
				pressTarget.Local.X+float32(pressTarget.Chunk.X)*components.ChunkSize,
				pressTarget.Local.Z+float32(pressTarget.Chunk.Z)*components.ChunkSize,
				len(g.Sel.Units), g.Frame.Focused)
		}
	}

	if g.UI.RMB.Active {
		rmbDown := rl.IsMouseButtonDown(rl.MouseButtonRight)
		rmbReleased := rl.IsMouseButtonReleased(rl.MouseButtonRight)

		if rmbDown && !g.UI.CtxMenu.IsActive() && !g.UI.RMB.FacingActive {
			dx := g.Frame.Cursor.X - g.UI.RMB.PressOrigin.X
			dy := g.Frame.Cursor.Y - g.UI.RMB.PressOrigin.Y
			if dx*dx+dy*dy > 8*8 {
				g.UI.RMB.FacingActive = true
			} else {
				now := float32(g.App.Elapsed().Seconds())
				if (now-g.UI.RMB.PressTimeSec) >= 0.200 &&
					g.UI.RMB.HoveredBldg != (ecs.Entity{}) &&
					g.UI.RMB.SourcePanel == ui.Panel3D {
					sections := buildBuildingPopupSections(g.UI.RMB.HoveredBldg, &g.Res.BuildingPlanIx, g.Maps.Level)
					g.UI.CtxMenu.Begin(g.Frame.Cursor, sections, g.UI.RMB.SourcePanel, g.Frame.Panel3DContent)
				}
			}
		}

		// Popup active: LMB / ESC / RMB-release (single-gesture commit).
		if g.UI.CtxMenu.IsActive() {
			escPressed := rl.IsKeyPressed(rl.KeyEscape)
			lmbPressedForMenu := rl.IsMouseButtonPressed(rl.MouseButtonLeft)
			res := g.UI.CtxMenu.Tick(g.Frame.Cursor, lmbPressedForMenu, escPressed)
			switch {
			case rmbReleased && g.UI.CtxMenu.IsActive():
				if item, ok := g.UI.CtxMenu.HoveredItemDetails(); ok && item.Enabled {
					g.UI.CtxMenu.Reset()
					issueBuildingPopupOrder(g.Sel.Units, item, g.UI.RMB.HoveredBldg,
						g.UI.RMB.PressTarget, g.UI.RMB.PressRaw, g.Frame.Shift,
						g.Svc.Squad, g.Svc.Nav, g.Maps.SquadMember, g.Maps.Pos, g.Maps.ActionQueue, g.Maps.Level)
				} else {
					g.UI.CtxMenu.Reset()
				}
			case res.Committed:
				issueBuildingPopupOrder(g.Sel.Units, res.Item, g.UI.RMB.HoveredBldg,
					g.UI.RMB.PressTarget, g.UI.RMB.PressRaw, g.Frame.Shift,
					g.Svc.Squad, g.Svc.Nav, g.Maps.SquadMember, g.Maps.Pos, g.Maps.ActionQueue, g.Maps.Level)
			}
		}

		if rmbReleased && !g.UI.CtxMenu.IsActive() {
			switch {
			case g.UI.RMB.FacingActive:
				dx := g.Frame.Cursor.X - g.UI.RMB.PressOrigin.X
				dy := g.Frame.Cursor.Y - g.UI.RMB.PressOrigin.Y
				yaw := float32(math.Atan2(float64(dx), float64(-dy)))
				mods := rmbModifiersFromPress(g.UI.RMB.Ctrl, g.UI.RMB.Alt, g.UI.RMB.Double)
				params := applyModifiersToParams(systems.OrderParams{
					HasFacing:    true,
					FacingYawRad: yaw,
				}, mods)
				resolveRMBOrderWithParams(g.Sel.Units, g.UI.RMB.PressTarget, g.Frame.Shift, nil, params, g.Ctx.HitTest,
					g.Svc.Squad, g.Svc.Nav, g.Maps.SquadMember, g.Maps.Pos, g.Maps.ActionQueue)
			default:
				// Shift+RMB on subset → IndividualPosition path.
				placed := false
				if g.Frame.Shift {
					if _, ok := detectSubsetOfSquad(g.Sel.Units, g.Maps.SquadMember, g.Maps.Roster); ok {
						placeIndividualPositions(g.App.World, g.Sel.Units, g.UI.RMB.PressTarget,
							g.Maps.IndividualPos, float32(g.App.Elapsed().Seconds()))
						placed = true
					}
				}
				if !placed {
					mods := rmbModifiersFromPress(g.UI.RMB.Ctrl, g.UI.RMB.Alt, g.UI.RMB.Double)
					fmt.Printf("[rmb] tap commit target=(%.1f,%.1f) g.Sel.Units=%d\n",
						g.UI.RMB.PressTarget.Local.X+float32(g.UI.RMB.PressTarget.Chunk.X)*components.ChunkSize,
						g.UI.RMB.PressTarget.Local.Z+float32(g.UI.RMB.PressTarget.Chunk.Z)*components.ChunkSize,
						len(g.Sel.Units))
					resolveRMBOrder(g.Sel.Units, g.UI.RMB.PressTarget, g.Frame.Shift, nil, mods, g.Ctx.HitTest,
						g.Svc.Squad, g.Svc.Nav, g.Maps.SquadMember, g.Maps.Pos, g.Maps.ActionQueue)
				}
			}
		}

		if rmbReleased {
			g.UI.RMB.Active = false
			g.UI.RMB.FacingActive = false
		}
	}

	// H -> Stop order. Mirrors resolveRMBOrder's squad-vs-soloist split.
	if rl.IsKeyPressed(rl.KeyH) && len(g.Sel.Units) > 0 {
		groups := groupSelectionByOwner(g.Sel.Units, g.Maps.SquadMember)
		for _, s := range groups.SquadsToOrder {
			g.Svc.Squad.CancelAllOrders(s)
		}
		for _, e := range groups.Soloists {
			if aq := g.Maps.ActionQueue.Get(e); aq != nil {
				systems.ClearActions(aq)
				systems.PushAction(aq, components.Action{Kind: components.ActionStop})
			}
		}
	}

	// T -> form Squad. Block C: the shape is chosen by COMPOSITION, not by the
	// player — loose scatter for infantry, column for anything with a hull.
	if rl.IsKeyPressed(rl.KeyT) && len(g.Sel.Units) >= 2 {
		// An airframe never joins a ground squad (P12: flights are their own
		// problem, deferred). A slot would put FormationSystem in charge of its
		// position, which means ground waypoints for something that flies.
		members := withoutAircraft(g.Sel.Units, g.App.World)
		mixed := containsVehicle(members, g.App.World)
		kind := components.FormationLoose
		if mixed {
			kind = components.FormationColumn
		}
		newSquad := ecs.Entity{}
		if len(members) >= 2 {
			newSquad = g.Svc.Squad.CreateFromUnits(members, kind)
		}
		if newSquad != (ecs.Entity{}) && g.App.World.Alive(newSquad) {
			if r := g.Maps.Roster.Get(newSquad); r != nil {
				g.Sel.Units = append(g.Sel.Units[:0], r.Members[:r.Count]...)
			}
		}
	}

	if rl.IsKeyPressed(rl.KeyU) && len(g.Sel.Units) > 0 {
		for _, e := range g.Sel.Units {
			g.Svc.Squad.Leave(e)
		}
	}

	// O -> toggle behavior panel floating (Q lifts the camera on this branch).
	// Uses the floatSpawn ID convention so the panel shares its scroll state
	// with the workspace flavour.
	if rl.IsKeyPressed(rl.KeyO) {
		id := "float:" + string(ui.PanelBehavior)
		if g.UI.Floating.IsOpen(id) {
			g.UI.Floating.Close(id)
		} else {
			g.floatSpawn(ui.PanelBehavior, ui.WidgetTitle(ui.PanelBehavior),
				rl.Rectangle{X: 220, Y: 80, Width: 380, Height: 440})
		}
	}

	// [ / ] cycle MovementProfile presets; ' toggles Posture.
	if len(g.Sel.Units) > 0 {
		if commonSquad, homo := groupSelected(g.Sel.Units, g.Maps.SquadMember); homo && commonSquad != (ecs.Entity{}) {
			if g.App.World.Alive(commonSquad) {
				profile := g.Maps.MovementProfile.Get(commonSquad)
				if profile != nil {
					switch {
					case rl.IsKeyPressed(rl.KeyLeftBracket):
						*profile = components.ApplyPreset(cyclePreset(detectPreset(*profile), -1))
					case rl.IsKeyPressed(rl.KeyRightBracket):
						*profile = components.ApplyPreset(cyclePreset(detectPreset(*profile), +1))
					case rl.IsKeyPressed(rl.KeyApostrophe):
						if profile.Posture == components.PostureStandard {
							profile.Posture = components.PostureQuiet
						} else {
							profile.Posture = components.PostureStandard
						}
					}
				}
			}
		}
	}

	digitKeys := [5]int32{rl.KeyOne, rl.KeyTwo, rl.KeyThree, rl.KeyFour, rl.KeyFive}
	for i, k := range digitKeys {
		if !rl.IsKeyPressed(k) {
			continue
		}
		if g.Frame.Ctrl {
			if commonSquad, homo := groupSelected(g.Sel.Units, g.Maps.SquadMember); homo && commonSquad != (ecs.Entity{}) {
				g.Sel.Binds[i] = bindEntry{Squad: commonSquad}
			} else {
				cp := make([]ecs.Entity, len(g.Sel.Units))
				copy(cp, g.Sel.Units)
				g.Sel.Binds[i] = bindEntry{Units: cp}
			}
		} else {
			b := g.Sel.Binds[i]
			switch {
			case b.Squad != (ecs.Entity{}) && g.App.World.Alive(b.Squad):
				if r := g.Maps.Roster.Get(b.Squad); r != nil {
					g.Sel.Units = append(g.Sel.Units[:0], r.Members[:r.Count]...)
				} else {
					g.Sel.Units = nil
				}
			case b.Squad != (ecs.Entity{}):
				g.Sel.Binds[i] = bindEntry{}
				g.Sel.Units = nil
			default:
				g.Sel.Units = append(g.Sel.Units[:0], b.Units...)
			}
			// Recall snapshots go stale — this runs BEFORE the frame's
			// post-Advance scrub, and RMB handlers deref this frame.
			g.Sel.Units = compactAlive(g.App.World, g.Sel.Units)
		}
	}

	if !g.Frame.WASDActive && len(g.Sel.NavPath) > 0 {
		g.Sel.NavPath = stepAlongPath(g.Frame.AnchorPos, g.Sel.NavPath, g.Frame.AnchorSpeed*float32(g.Frame.DtReal.Seconds()))
	}

	if g.Frame.Focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyX) {
		g.Svc.Stamper.StampHeightmap(*g.Frame.AnchorPos, systems.Crater(2.0, 4.0), 4.0)
	}

	// B toggles InteriorOpen on every building; PgUp/PgDn cycles CurrentLevel.
	if g.Frame.Focused == ui.Panel3D && rl.IsKeyPressed(rl.KeyB) {
		qbf := g.Filt.Building.Query()
		for qbf.Next() {
			root := qbf.Entity()
			bvm := g.Maps.BuildingViewMode.Get(root)
			if bvm == nil {
				continue
			}
			bvm.InteriorOpen = !bvm.InteriorOpen
		}
	}
	if g.Frame.Focused == ui.Panel3D && (rl.IsKeyPressed(rl.KeyPageDown) || rl.IsKeyPressed(rl.KeyPageUp)) {
		step := 1
		if rl.IsKeyPressed(rl.KeyPageUp) {
			step = -1
		}
		qbf := g.Filt.Building.Query()
		for qbf.Next() {
			root := qbf.Entity()
			bvm := g.Maps.BuildingViewMode.Get(root)
			if bvm == nil {
				continue
			}
			levels := g.Res.BuildingPlanIx.Levels[root]
			if len(levels) <= 1 {
				continue
			}
			idx := 0
			for i, l := range levels {
				if l == bvm.CurrentLevel {
					idx = i
					break
				}
			}
			idx += step
			if idx < 0 {
				idx = 0
			}
			if idx >= len(levels) {
				idx = len(levels) - 1
			}
			bvm.CurrentLevel = levels[idx]
		}
	}

	if rl.IsKeyPressed(rl.KeyP) {
		if g.Frame.Ctrl {
			g.App.Prof.PrintSnapshot()
			g.App.Trace.Mark("snapshot")
		} else {
			g.UI.ExpandedHUD = !g.UI.ExpandedHUD
		}
	}

	if rl.IsKeyPressed(rl.KeyF5) {
		path := quicksavePath()
		meta := systems.SaveMeta{
			MapName:    worldMap.Name,
			SimNow:     g.App.Elapsed().Seconds(),
			TickIndex:  g.App.TickIndex(),
			FrameIndex: uint64(g.App.FrameIndex()),
		}
		if err := systems.SaveWorld(g.App.World, path, meta); err != nil {
			fmt.Printf("quicksave: %v\n", err)
		} else {
			fmt.Printf("quicksave: %s tick=%d\n", path, g.App.TickIndex())
		}
	}
	if rl.IsKeyPressed(rl.KeyF9) {
		if _, err := os.Stat(quicksavePath()); err == nil {
			relaunchWithLoad(g.App, quicksavePath())
		} else {
			fmt.Printf("quickload: no quicksave at %s\n", quicksavePath())
		}
	}
}

func withoutAircraft(units []ecs.Entity, world *ecs.World) []ecs.Entity {
	airMap := ecs.NewMap[components.Aircraft](world)
	out := make([]ecs.Entity, 0, len(units))
	for _, u := range units {
		if u != (ecs.Entity{}) && world.Alive(u) && !airMap.Has(u) {
			out = append(out, u)
		}
	}
	return out
}

func containsVehicle(units []ecs.Entity, world *ecs.World) bool {
	vehMap := ecs.NewMap[components.Vehicle](world)
	for _, u := range units {
		if u != (ecs.Entity{}) && world.Alive(u) && vehMap.Has(u) {
			return true
		}
	}
	return false
}

