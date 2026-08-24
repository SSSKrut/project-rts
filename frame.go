package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/core"
	"rts-go/systems"
	"rts-go/ui"
)

// RunFrame advances and draws one frame; false ends the loop (a headless run
// finished). The phase order below is a contract: input feeds the sim, the sim
// runs exactly once, and the render half reads the post-tick world.
func (g *Game) RunFrame() bool {
	// A recovered sim panic freezes the world (it may be mid-mutation /
	// query-locked): error screen instead of a crash.
	if g.Dev.Fatal != nil {
		drawFatalScreen(g.Dev.Fatal, g.hudFont)
		return true
	}
	if rl.IsWindowResized() {
		g.UI.ScreenW = int32(rl.GetScreenWidth())
		g.UI.ScreenH = int32(rl.GetScreenHeight())
		g.UI.PanelMgr.Recompute(g.UI.ScreenW, g.UI.ScreenH)
		g.UI.Scene3DRT.EnsureSize(g.UI.PanelMgr.Get(ui.Panel3D))
	}

	// Real-time dt for input-side stepping; the sim advances in fixed
	// 60 Hz ticks inside g.App.Advance.
	if g.headless {
		g.Frame.DtReal = core.SimDt
	} else {
		g.Frame.DtReal = time.Duration(float64(rl.GetFrameTime()) * float64(time.Second))
	}
	g.Svc.Squad.SetClock(float32(g.App.Elapsed().Seconds()))

	g.Frame.Cursor = rl.GetMousePosition()
	g.Frame.Focused = g.UI.PanelMgr.FocusedAt(g.Frame.Cursor)
	g.Frame.Shift = rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
	g.Frame.Ctrl = rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)
	g.Frame.Alt = rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt)

	g.Frame.Panel3D = g.UI.PanelMgr.Get(ui.Panel3D)
	g.Frame.PanelMap = g.UI.PanelMgr.Get(ui.PanelMap)

	if rl.IsKeyPressed(rl.KeyTab) {
		// Tab during a splitter drag aborts the drag before flipping the
		// preset (avoids half-applied resize state).
		if g.UI.PanelMgr.IsDragging() {
			g.UI.PanelMgr.AbortDrag()
		}
		g.UI.PanelMgr.TogglePreset()
		g.UI.PanelMgr.Recompute(g.UI.ScreenW, g.UI.ScreenH)
		g.UI.Scene3DRT.EnsureSize(g.UI.PanelMgr.Get(ui.Panel3D))
		g.Frame.Panel3D = g.UI.PanelMgr.Get(ui.Panel3D)
		g.Frame.PanelMap = g.UI.PanelMgr.Get(ui.PanelMap)
	}

	g.handleChrome()
	g.updateTestScenes()
	if *shotPathFlag != "" {
		g.applyShotSelection()
		g.applyShotOrders()
	}
	// Before input: the bar must own its rect while clicks are being routed.
	g.layoutSquadBar()
	g.handleInput()
	g.handleOrders()
	g.updateHover()

	// Gate orbit/wheel by focus; suppress while a floater owns the cursor.
	systems.OrbitInputEnabled = (g.Frame.Focused == ui.Panel3D || g.Frame.Focused == ui.PanelNone) &&
		!g.UI.Floating.IsBusy(g.Frame.Cursor)

	if g.Dev.PendingSteps > 0 && g.App.TimeScale == 0 {
		g.Dev.Fatal = guardedStepOnce(g.App)
		g.Dev.PendingSteps--
	} else {
		g.Dev.PendingSteps = 0
		g.Dev.Fatal = guardedAdvance(g.App)
	}
	if g.Dev.Fatal != nil {
		if g.headless {
			fmt.Printf("SIM PANIC: %s\n%s\n", g.Dev.Fatal.Msg,
				strings.Join(g.Dev.Fatal.Stack, "\n"))
			os.Exit(2)
		}
		return true
	}
	maybeSaveAt(g.App)

	if g.headless {
		writeReplayHash(g.App)
		if *runTicksFlag > 0 {
			if g.App.TickIndex() >= *runTicksFlag {
				if os.Getenv("RTS_PROF") != "" {
					g.App.Prof.PrintSnapshot()
				}
				return false
			}
		} else if g.Scene.AI != nil && g.Scene.AI.verdictDone {
			if os.Getenv("RTS_PROF") != "" {
				g.App.Prof.PrintSnapshot()
			}
			return false
		}
		return true
	}

	hitchCheck(g.App, rl.GetFrameTime()*1000, rl.GetTime())

	// Units die inside Advance; scrub the selection before the render
	// half derefs components (Ark Map.Get panics on dead entities).
	g.Sel.Units = compactAlive(g.App.World, g.Sel.Units)
	g.Ctx.Coverage.update(g.App.World, g.Sel.Units)
	g.Frame.MapClusters.Rebuild(g.App.World, g.Filt.Contact,
		g.Maps.ContactOverride, g.Svc.Squad.Clock())
	// Reads the events this tick produced; may drop TimeScale before the
	// next frame's input runs.
	g.updateAttention()

	g.drawScene3D()
	g.drawUI()
	return !g.shotDone
}

// updateTestScenes ticks the scripted -scene harnesses; no-op in normal play.
func (g *Game) updateTestScenes() {
	if g.Scene.Door != nil {
		g.Scene.Door.EnsureInit()
		g.Scene.Door.Update(float32(g.App.Elapsed().Seconds()))
		g.Scene.Door.HandleHotkeys(g.Frame.Focused == ui.Panel3D)
	}

	if g.Scene.AI != nil {
		g.Scene.AI.Update(float32(g.App.Elapsed().Seconds()))
	}
}
