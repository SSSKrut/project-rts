package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// updateHover resolves what sits under the cursor and refreshes the LOS
// preview and the building widget. Runs after input, before the sim advances.
func (g *Game) updateHover() {
	// Hover: closest unit (Panel3D) / closest squad marker (PanelMap).
	g.Sel.Hovered = ecs.Entity{}
	switch g.Frame.Focused {
	case ui.Panel3D:
		if hit, ok := hoverUnitFromMouse(g.Filt.UnitRender, g.Filt.VehicleRender, *g.Frame.AnchorPos, g.Frame.Panel3DLocal, g.Frame.Panel3DW, g.Frame.Panel3DH); ok {
			g.Sel.Hovered = hit
		}
	case ui.PanelMap:
		mapCtx := ui.MapRenderCtx{
			World: g.App.World, Cam: g.UI.MapCam, SquadFilter: g.Filt.Squad,
			SquadCenter:        g.squadCenter,
			MapMarkerCache:     &g.Res.MapMarkerCache,
			ContactFilter:      g.Filt.Contact,
			ContactMap:         g.Maps.Contact,
			ContactOverrideMap: g.Maps.ContactOverride,
		}
		if h := ui.PickContactAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 14); h != (ecs.Entity{}) {
			g.Sel.Hovered = h
		} else {
			g.Sel.Hovered = ui.PickSquadAt(g.Frame.Cursor, mapCtx, g.Frame.PanelMap, 12)
		}
	}

	if g.Frame.Focused == ui.Panel3D {
		g.Frame.GhostTarget, g.Frame.GhostTargetOK = mouseTargetWorldPos(systems.CurrentCamera,
			g.Frame.AnchorPos.ToRenderSpace(systems.CurrentOriginChunk),
			g.Frame.Panel3DLocal, g.Frame.Panel3DW, g.Frame.Panel3DH)
	}

	losHeld := rl.IsKeyDown(rl.KeyV) && g.Frame.Focused == ui.Panel3D && !g.UI.Floating.IsBusy(g.Frame.Cursor)
	g.Ctx.LOS.update(g.App.World, losHeld, g.Frame.GhostTargetOK, g.Frame.GhostTarget, g.Sel.Units)

	// Building hover: cursor's ground target inside any Building.Footprint.
	g.Sel.HoveredBuilding = ecs.Entity{}
	g.Sel.HoveredLevel = ecs.Entity{}
	if g.Frame.Focused == ui.Panel3D && g.Frame.GhostTargetOK {
		gtX := g.Frame.GhostTarget.Local.X + float32(g.Frame.GhostTarget.Chunk.X)*components.ChunkSize
		gtZ := g.Frame.GhostTarget.Local.Z + float32(g.Frame.GhostTarget.Chunk.Z)*components.ChunkSize
		qbf := g.Filt.Building.Query()
		for qbf.Next() {
			b := qbf.Get()
			if b.Footprint.Contains(gtX, gtZ) {
				g.Sel.HoveredBuilding = qbf.Entity()
				qbf.Close()
				break
			}
		}
		// Narrow outline to the storey the ray hits; falls back to
		// whole-building if no Level box catches the ray.
		if g.Sel.HoveredBuilding != (ecs.Entity{}) {
			ray := rl.GetScreenToWorldRayEx(g.Frame.Panel3DLocal, systems.CurrentCamera, g.Frame.Panel3DW, g.Frame.Panel3DH)
			if lvl, ok := pickLevelUnderRay(ray, g.Sel.HoveredBuilding, &g.Res.BuildingPlanIx, g.Maps.Level); ok {
				g.Sel.HoveredLevel = lvl
			}
		}
	}

	// Sticky pinned building wins over the hovered one so the cursor can move onto
	// chips without the panel vanishing.
	effectiveBuilding := g.Sel.Building
	if effectiveBuilding != (ecs.Entity{}) && !g.App.World.Alive(effectiveBuilding) {
		g.Sel.Building = ecs.Entity{}
		effectiveBuilding = ecs.Entity{}
	}
	if effectiveBuilding == (ecs.Entity{}) {
		effectiveBuilding = g.Sel.HoveredBuilding
	}
	g.UI.BuildingWidget = nil
	if effectiveBuilding != (ecs.Entity{}) {
		if bvm := g.Maps.BuildingViewMode.Get(effectiveBuilding); bvm != nil {
			if rootPos := g.Maps.Pos.Get(effectiveBuilding); rootPos != nil {
				elev := float32(3)
				if bldg := g.Maps.Building.Get(effectiveBuilding); bldg != nil {
					elev = float32(bldg.Stories)*components.FloorHeight + 1
				}
				above := *rootPos
				above.Local.Y += elev
				rp := above.ToRenderSpace(systems.CurrentOriginChunk)
				w := int32(g.Frame.Panel3DContent.Width)
				h := int32(g.Frame.Panel3DContent.Height)
				if w > 0 && h > 0 {
					sp := rl.GetWorldToScreenEx(rp, systems.CurrentCamera, w, h)
					if sp.X >= 0 && sp.X <= g.Frame.Panel3DContent.Width && sp.Y >= 0 && sp.Y <= g.Frame.Panel3DContent.Height {
						screen := rl.Vector2{
							X: g.Frame.Panel3DContent.X + sp.X,
							Y: g.Frame.Panel3DContent.Y + sp.Y,
						}
						levels := g.Res.BuildingPlanIx.Levels[effectiveBuilding]
						g.UI.BuildingWidget = ui.ComputeBuildingWidget(
							effectiveBuilding, bvm, levels, screen, g.Maps.Level,
						)
					}
				}
			}
		}
	}
}
