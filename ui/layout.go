package ui

import rl "github.com/gen2brain/raylib-go/raylib"

// L1 layout - fixed grid driven by screen size. Two presets:
//
//   Field:                          Command:
//   +-------------+--------+        +-------------+--------+
//   |             |  Insp  |        |             |  Insp  |
//   |     3D      +--------+        |     Map     +--------+
//   |    (big)    |  Map   |        |    (big)    |   3D   |
//   |             | (side) |        |             | (side) |
//   +-------------+--------+        +-------------+--------+
//   |     Time controls   |        |     Time controls   |
//   +---------------------+        +---------------------+
//
// Field puts the 3D scene in the big slot (Sun Tzu's "the general on the
// hill" - see your own troops up close). Command swaps to map-primary
// (operations-room view). Inspector and Time controls don't move.
//
// Column split (sideCol vs 3D/map): 75% / 25%. Side column splits vertically
// between Inspector (top 40%) and the secondary view (bottom 60%). Time bar
// = 64 px tall at the bottom. Everything in px from screenW/H, no absolute
// coords stored.

const timeBarHeight int32 = 64

// DefaultRightColRatio is the side column's default share of screen width.
// Wide enough that inspector text rows aren't clipped at 1600x900; narrow
// enough that the big slot stays usable for marquee selects.
//
// Phase 13.5 M13.5.1: ratios moved from package consts to PanelManager fields
// so splitter drag (M13.5.2) can mutate them at runtime. NewPanelManager
// seeds the fields with these defaults; loadLayout (M13.5.5) overrides them
// from save/layout.json on startup when present.
const DefaultRightColRatio float32 = 0.25

// DefaultInspectorRatio splits the side column vertically. Inspector gets the
// top share, the secondary view (map in Field, 3D in Command) gets the rest.
const DefaultInspectorRatio float32 = 0.4

func layoutField(screenW, screenH int32, rightCol, inspector float32) map[PanelID]rl.Rectangle {
	return splitGrid(screenW, screenH, Panel3D, PanelMap, rightCol, inspector)
}

func layoutCommand(screenW, screenH int32, rightCol, inspector float32) map[PanelID]rl.Rectangle {
	return splitGrid(screenW, screenH, PanelMap, Panel3D, rightCol, inspector)
}

// splitGrid computes the four panel rects when `big` occupies the left/main
// column and `side` sits in the lower right beneath the inspector. Returns
// rectangles for all four canonical PanelIDs.
//
// rightCol = side column width as a fraction of screen width (0..1).
// inspector = inspector's share of the side column height (0..1).
func splitGrid(screenW, screenH int32, big, side PanelID, rightCol, inspector float32) map[PanelID]rl.Rectangle {
	rightW := int32(float32(screenW) * rightCol)
	leftW := screenW - rightW
	contentH := screenH - timeBarHeight

	bigRect := rl.Rectangle{
		X: 0, Y: 0,
		Width:  float32(leftW),
		Height: float32(contentH),
	}
	inspectH := int32(float32(contentH) * inspector)
	inspectRect := rl.Rectangle{
		X:      float32(leftW),
		Y:      0,
		Width:  float32(rightW),
		Height: float32(inspectH),
	}
	sideRect := rl.Rectangle{
		X:      float32(leftW),
		Y:      float32(inspectH),
		Width:  float32(rightW),
		Height: float32(contentH - inspectH),
	}
	timeRect := rl.Rectangle{
		X:      0,
		Y:      float32(contentH),
		Width:  float32(screenW),
		Height: float32(timeBarHeight),
	}
	return map[PanelID]rl.Rectangle{
		big:          bigRect,
		side:         sideRect,
		PanelInspect: inspectRect,
		PanelTime:    timeRect,
	}
}
