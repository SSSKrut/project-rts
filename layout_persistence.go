package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"

	"rts-go/ui"
)

// Layout persistence: serialises the workspace tree + active preset.
// Old file versions are silently rejected; defaults apply on miss/error.

const layoutSavePath = "./save/layout.json"

// Unchanged by the timeline block: an older file simply has no "timeline" key,
// which reads as absent and leaves the constructor defaults in place. Bumping
// would throw away the player's saved workspace tree for no reason.
const layoutFileVersion uint16 = 3

type nodeJSON struct {
	Kind     string     `json:"kind"`             // "leaf" | "split"
	Panel    string     `json:"panel,omitempty"`  // leaf only
	Title    string     `json:"title,omitempty"`  // leaf only
	Orient   string     `json:"orient,omitempty"` // split only: "v" | "h"
	Ratio    float32    `json:"ratio,omitempty"`
	Children []nodeJSON `json:"children,omitempty"`
}

// timelineJSON is the part of the timeline view worth surviving a restart:
// how the player sized and scaled the panel. OffsetT / ScrollY are where they
// happened to be looking and are rebuilt by Follow on the next frame.
type timelineJSON struct {
	LabelW       float32 `json:"label_w"`
	PixelsPerSec float32 `json:"px_per_sec"`
	Follow       bool    `json:"follow"`
}

type layoutFile struct {
	Version  uint16       `json:"version"`
	Preset   uint8        `json:"layout_preset"`
	Tree     nodeJSON     `json:"tree"`
	Timeline timelineJSON `json:"timeline"`
}

func encodeNode(n *ui.LayoutNode) nodeJSON {
	if n == nil {
		return nodeJSON{}
	}
	if n.IsLeaf() {
		return nodeJSON{
			Kind:  "leaf",
			Panel: string(n.Panel),
			Title: n.Title,
		}
	}
	orient := "v"
	if n.Orient == ui.SplitHorizontal {
		orient = "h"
	}
	return nodeJSON{
		Kind:   "split",
		Orient: orient,
		Ratio:  n.Ratio,
		Children: []nodeJSON{
			encodeNode(n.Children[0]),
			encodeNode(n.Children[1]),
		},
	}
}

func decodeNode(j nodeJSON) *ui.LayoutNode {
	switch j.Kind {
	case "leaf":
		return ui.NewLeaf(ui.PanelID(j.Panel), j.Title)
	case "split":
		if len(j.Children) != 2 {
			return nil
		}
		a := decodeNode(j.Children[0])
		b := decodeNode(j.Children[1])
		if a == nil || b == nil {
			return nil
		}
		o := ui.SplitVertical
		if j.Orient == "h" {
			o = ui.SplitHorizontal
		}
		return ui.NewSplit(o, j.Ratio, a, b)
	}
	return nil
}

// loadLayout installs the persisted tree on the PanelManager BEFORE the
// first Recompute, and restores the timeline leaf's view. Missing /
// parse-fail / wrong version ⇒ defaults stay.
func loadLayout(panelMgr *ui.PanelManager, tl *ui.TimelineViewState) {
	if panelMgr == nil {
		return
	}
	data, err := os.ReadFile(layoutSavePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("layout: load skipped (%v)", err)
		}
		return
	}
	var lf layoutFile
	if err := json.Unmarshal(data, &lf); err != nil {
		log.Printf("layout: parse failed, using defaults (%v)", err)
		return
	}
	if lf.Version != layoutFileVersion {
		log.Printf("layout: file version %d != expected %d, using defaults",
			lf.Version, layoutFileVersion)
		return
	}
	root := decodeNode(lf.Tree)
	if root == nil {
		log.Printf("layout: tree decode produced nil, using defaults")
		return
	}
	panelMgr.SetWorkspace(root)
	panelMgr.Layout = ui.LayoutPreset(lf.Preset)
	// A pre-timeline file leaves the block zeroed; PixelsPerSec is never zero
	// once written, so it doubles as "this file has a timeline section".
	if tl != nil && lf.Timeline.PixelsPerSec > 0 {
		tl.LabelW = lf.Timeline.LabelW
		tl.PixelsPerSec = lf.Timeline.PixelsPerSec
		tl.Follow = lf.Timeline.Follow
		tl.Normalize()
	}
}

// saveLayout marshals the current workspace tree + active preset into
// layout.json. Atomic via .tmp + os.Rename. Errors are logged.
func saveLayout(panelMgr *ui.PanelManager, tl *ui.TimelineViewState) {
	if panelMgr == nil {
		return
	}
	dir := filepath.Dir(layoutSavePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("layout: mkdir failed (%v)", err)
		return
	}
	lf := layoutFile{
		Version: layoutFileVersion,
		Preset:  uint8(panelMgr.Layout),
		Tree:    encodeNode(panelMgr.Workspace),
	}
	if tl != nil {
		lf.Timeline = timelineJSON{
			LabelW:       ui.TimelineLabelWidth(*tl),
			PixelsPerSec: tl.PixelsPerSec,
			Follow:       tl.Follow,
		}
	}
	data, err := json.MarshalIndent(&lf, "", "  ")
	if err != nil {
		log.Printf("layout: marshal failed (%v)", err)
		return
	}
	tmp := layoutSavePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("layout: write tmp failed (%v)", err)
		return
	}
	if err := os.Rename(tmp, layoutSavePath); err != nil {
		log.Printf("layout: rename failed (%v)", err)
		_ = os.Remove(tmp)
		return
	}
}
