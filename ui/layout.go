package ui

import rl "github.com/gen2brain/raylib-go/raylib"

// Phase 18.C layout - one fixed TopBar strip + a recursive workspace tree
// below. The tree is mutated by user actions (corner-drag split, chevron
// menu swap/close); ratios stored on each Split node persist through
// resizes. Top bar stays fixed at the top of the screen at all times.
//
//   +----------------- TopBar (36 px) ----------------+
//   |                                                 |
//   |              Workspace tree                     |
//   |                                                 |
//   +-------------------------------------------------+
//
// Default Field preset:
//
//   Split(Horiz, 0.80) [
//     Split(Vert, 0.75) [
//       Leaf(3D),
//       Split(Horiz, 0.40) [
//         Leaf(Inspector),
//         Leaf(Map),
//       ]
//     ],
//     Leaf(Timeline),
//   ]

const topBarHeight int32 = 36

// DefaultRightColRatio is the side column's default share of width.
const DefaultRightColRatio float32 = 0.25

// DefaultInspectorRatio splits the side column vertically.
const DefaultInspectorRatio float32 = 0.4

// DefaultTimelineRatio - timeline panel share of the height below the top
// bar.
const DefaultTimelineRatio float32 = 0.20

// LayoutPreset selects the starting tree at first launch (no layout.json
// present yet). Tab swaps content between PanelMap and Panel3D leaves
// post-launch instead of swapping the whole tree, so user-edited layouts
// stay intact.
type LayoutPreset uint8

const (
	PresetField   LayoutPreset = iota // 3D in the big slot, Map in the side
	PresetCommand                     // Map in the big slot, 3D in the side
)

// presetFieldTree builds the default Field workspace tree. Inverse ratios:
// Timeline gets (1 - 0.80) = 0.20 height, side col gets (1 - 0.75) = 0.25
// width, side col bottom gets (1 - 0.40) = 0.60 of its column height.
func presetFieldTree() *LayoutNode {
	return NewSplit(SplitHorizontal, 1-DefaultTimelineRatio,
		NewSplit(SplitVertical, 1-DefaultRightColRatio,
			NewLeaf(Panel3D, "Field"),
			NewSplit(SplitHorizontal, DefaultInspectorRatio,
				NewLeaf(PanelInspect, "Inspector"),
				NewLeaf(PanelMap, "Map"),
			),
		),
		NewLeaf(PanelTimeline, "Timeline"),
	)
}

// presetCommandTree mirrors Field but with 3D and Map swapped.
func presetCommandTree() *LayoutNode {
	return NewSplit(SplitHorizontal, 1-DefaultTimelineRatio,
		NewSplit(SplitVertical, 1-DefaultRightColRatio,
			NewLeaf(PanelMap, "Map"),
			NewSplit(SplitHorizontal, DefaultInspectorRatio,
				NewLeaf(PanelInspect, "Inspector"),
				NewLeaf(Panel3D, "Field"),
			),
		),
		NewLeaf(PanelTimeline, "Timeline"),
	)
}

// presetTree returns the initial workspace tree for the given preset.
func presetTree(p LayoutPreset) *LayoutNode {
	if p == PresetCommand {
		return presetCommandTree()
	}
	return presetFieldTree()
}

// WorkspaceRect returns the rect available for the workspace tree given the
// current screen size (everything below the top bar).
func WorkspaceRect(screenW, screenH int32) rl.Rectangle {
	avail := screenH - topBarHeight
	if avail < 0 {
		avail = 0
	}
	return rl.Rectangle{
		X: 0, Y: float32(topBarHeight),
		Width:  float32(screenW),
		Height: float32(avail),
	}
}

// TopBarRect returns the rect for the fixed top bar.
func TopBarRect(screenW int32) rl.Rectangle {
	return rl.Rectangle{X: 0, Y: 0, Width: float32(screenW), Height: float32(topBarHeight)}
}

// WidgetTitle returns the canonical title for a PanelID, used both for
// chrome rendering and the chevron menu listing.
func WidgetTitle(id PanelID) string {
	switch id {
	case Panel3D:
		return "Field"
	case PanelMap:
		return "Map"
	case PanelInspect:
		return "Inspector"
	case PanelTimeline:
		return "Timeline"
	}
	return string(id)
}

// WorkspacePanelKinds is the list of widget kinds the chevron menu can
// switch a leaf to. TopBar is intentionally excluded — it's not a
// workspace widget.
var WorkspacePanelKinds = [...]PanelID{Panel3D, PanelMap, PanelInspect, PanelTimeline}
