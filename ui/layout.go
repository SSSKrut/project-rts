package ui

import rl "github.com/gen2brain/raylib-go/raylib"

// Phase 18 layout - five rects driven by screen size + three ratios:
//
//   Field:                            Command:
//   +-----------------------------+   +-----------------------------+
//   | TopBar (32 px - time controls + future menu)                  |
//   +-----------------+-----------+   +-----------------+-----------+
//   |                 |   Insp    |   |                 |   Insp    |
//   |       3D        +-----------+   |       Map       +-----------+
//   |     (big)       |   Map     |   |     (big)       |    3D     |
//   |                 |  (side)   |   |                 |  (side)   |
//   +-----------------+-----------+   +-----------------+-----------+
//   | Timeline (squad rows + order blocks)                          |
//   +---------------------------------------------------------------+
//
// Field puts the 3D scene in the big slot, Command swaps to map-primary.
// Inspector lives at the same screen position in both presets. Top bar /
// Timeline span full width and are preset-agnostic.
//
// Heights: top bar = 32 px fixed; timeline = (screenH - 32) * TimelineRatio.
// Central row gets the remainder. Column split inside the central row
// (sideCol vs big) = RightColRatio; side column splits vertically by
// InspectorRatio.

const topBarHeight int32 = 36

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

// DefaultTimelineRatio - timeline panel share of the height below the top
// bar. 0.20 fits ~5 squad rows at 1080p.
const DefaultTimelineRatio float32 = 0.20

func layoutField(screenW, screenH int32, rightCol, inspector, timeline float32) map[PanelID]rl.Rectangle {
	return splitGrid(screenW, screenH, Panel3D, PanelMap, rightCol, inspector, timeline)
}

func layoutCommand(screenW, screenH int32, rightCol, inspector, timeline float32) map[PanelID]rl.Rectangle {
	return splitGrid(screenW, screenH, PanelMap, Panel3D, rightCol, inspector, timeline)
}

// splitGrid lays out five rects: PanelTopBar (full width, top), the central
// row split into `big` (left/main) + side column (Inspector top, `side`
// below), and PanelTimeline (full width, bottom).
//
// rightCol = side column width as a fraction of screen width (0..1).
// inspector = inspector's share of the central row height (0..1).
// timeline = timeline panel's share of (screenH - topBarHeight) (0..1).
func splitGrid(screenW, screenH int32, big, side PanelID, rightCol, inspector, timeline float32) map[PanelID]rl.Rectangle {
	topBar := rl.Rectangle{X: 0, Y: 0, Width: float32(screenW), Height: float32(topBarHeight)}

	avail := screenH - topBarHeight
	if avail < 0 {
		avail = 0
	}
	timelineH := int32(float32(avail) * timeline)
	centralH := avail - timelineH

	rightW := int32(float32(screenW) * rightCol)
	leftW := screenW - rightW

	bigRect := rl.Rectangle{
		X: 0, Y: float32(topBarHeight),
		Width:  float32(leftW),
		Height: float32(centralH),
	}
	inspectH := int32(float32(centralH) * inspector)
	inspectRect := rl.Rectangle{
		X:      float32(leftW),
		Y:      float32(topBarHeight),
		Width:  float32(rightW),
		Height: float32(inspectH),
	}
	sideRect := rl.Rectangle{
		X:      float32(leftW),
		Y:      float32(topBarHeight + inspectH),
		Width:  float32(rightW),
		Height: float32(centralH - inspectH),
	}
	timelineRect := rl.Rectangle{
		X:      0,
		Y:      float32(topBarHeight + centralH),
		Width:  float32(screenW),
		Height: float32(timelineH),
	}
	return map[PanelID]rl.Rectangle{
		PanelTopBar:   topBar,
		big:           bigRect,
		side:          sideRect,
		PanelInspect:  inspectRect,
		PanelTimeline: timelineRect,
	}
}
