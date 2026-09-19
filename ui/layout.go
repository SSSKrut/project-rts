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

// Three surfaces and no more (lite pivot): the field to act in, the map to
// command from, the inspector to read. Everything else was a widget the player
// had to learn before it told him anything.
func presetFieldTree() *LayoutNode {
	return NewSplit(SplitVertical, 1-DefaultRightColRatio,
		NewLeaf(Panel3D, "Field"),
		NewSplit(SplitHorizontal, DefaultInspectorRatio,
			NewLeaf(PanelInspect, "Inspector"),
			NewLeaf(PanelMap, "Map"),
		),
	)
}

func presetCommandTree() *LayoutNode {
	return NewSplit(SplitVertical, 1-DefaultRightColRatio,
		NewLeaf(PanelMap, "Map"),
		NewSplit(SplitHorizontal, DefaultInspectorRatio,
			NewLeaf(PanelInspect, "Inspector"),
			NewLeaf(Panel3D, "Field"),
		),
	)
}

func presetTree(p LayoutPreset) *LayoutNode {
	if p == PresetCommand {
		return presetCommandTree()
	}
	return presetFieldTree()
}

func WorkspaceRect(screenW, screenH int32) rl.Rectangle {
	if Chromeless {
		return rl.Rectangle{Width: float32(screenW), Height: float32(screenH)}
	}
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
	if Chromeless {
		return rl.Rectangle{}
	}
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
	case PanelBehavior:
		return "Behavior"
	case PanelDebug:
		return "Debug"
	}
	return string(id)
}

// WorkspacePanelKinds enumerates what the chevron menu can switch a leaf to.
// TopBar is excluded — it's not a workspace widget. Behavior stays out of the
// three-surface cut on purpose: standing rules are what keeps working when the
// radio net drops, so the lite game needs them reachable. Debug is a dev tool,
// not player UI.
var WorkspacePanelKinds = [...]PanelID{Panel3D, PanelMap, PanelInspect, PanelBehavior, PanelDebug}

// KnownPanelKind guards a layout loaded from disk: a saved leaf naming a widget
// that no longer exists must fall back, not render nothing.
func KnownPanelKind(id PanelID) bool {
	for _, k := range WorkspacePanelKinds {
		if k == id {
			return true
		}
	}
	return false
}
