package ui

import rl "github.com/gen2brain/raylib-go/raylib"

const topBarHeight int32 = 36

const DefaultRightColRatio float32 = 0.25
const DefaultInspectorRatio float32 = 0.4
const DefaultTimelineRatio float32 = 0.20

// LayoutPreset selects the starting tree at first launch. Tab swaps the
// content of PanelMap / Panel3D leaves post-launch (not the whole tree),
// so user-edited layouts stay intact.
type LayoutPreset uint8

const (
	PresetField   LayoutPreset = iota // 3D in the big slot, Map in the side
	PresetCommand                     // Map in the big slot, 3D in the side
)

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

func presetTree(p LayoutPreset) *LayoutNode {
	if p == PresetCommand {
		return presetCommandTree()
	}
	return presetFieldTree()
}

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

func TopBarRect(screenW int32) rl.Rectangle {
	return rl.Rectangle{X: 0, Y: 0, Width: float32(screenW), Height: float32(topBarHeight)}
}

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
	case PanelFormation:
		return "Formation"
	case PanelDebug:
		return "Debug"
	}
	return string(id)
}

// WorkspacePanelKinds enumerates what the chevron menu can switch a leaf
// to. TopBar is excluded — it's not a workspace widget.
var WorkspacePanelKinds = [...]PanelID{Panel3D, PanelMap, PanelInspect, PanelTimeline, PanelFormation, PanelDebug}
