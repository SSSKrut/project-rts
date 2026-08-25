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

// Bumped by the lite cut: a version-3 file can name widgets that no longer
// exist, and a tree of dead leaves is worse than the default layout.
const layoutFileVersion uint16 = 4

type nodeJSON struct {
	Kind     string     `json:"kind"`             // "leaf" | "split"
	Panel    string     `json:"panel,omitempty"`  // leaf only
	Title    string     `json:"title,omitempty"`  // leaf only
	Orient   string     `json:"orient,omitempty"` // split only: "v" | "h"
	Ratio    float32    `json:"ratio,omitempty"`
	Children []nodeJSON `json:"children,omitempty"`
}

// attentionJSON is a pointer field in layoutFile so an older file (no key)
// is distinguishable from "everything set to off" — the zero value of the
// matrix is a valid policy that must not be inferred from absence.
type attentionJSON struct {
	Kinds []uint8 `json:"kinds"`
	Muted bool    `json:"muted"`
}

type layoutFile struct {
	Version   uint16         `json:"version"`
	Preset    uint8          `json:"layout_preset"`
	Tree      nodeJSON       `json:"tree"`
	Attention *attentionJSON `json:"attention,omitempty"`
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
		// A leaf naming a widget this build no longer has would render
		// nothing at all; the inspector is the safe universal fallback.
		id := ui.PanelID(j.Panel)
		if !ui.KnownPanelKind(id) {
			return ui.NewLeaf(ui.PanelInspect, ui.WidgetTitle(ui.PanelInspect))
		}
		return ui.NewLeaf(id, j.Title)
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
func loadLayout(panelMgr *ui.PanelManager,
	att *ui.AttentionMatrix, muted *bool) {
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
	// A file written by an older build has no attention block; the caller's
	// defaults stand. A shorter list (new event kinds since) fills as far as
	// it reaches and leaves the rest at their default.
	if lf.Attention == nil {
		return
	}
	if att != nil {
		for i, v := range lf.Attention.Kinds {
			if i >= len(att) || ui.AutoReaction(v) >= ui.AutoReactionCount {
				continue
			}
			att[i] = ui.AutoReaction(v)
		}
	}
	if muted != nil {
		*muted = lf.Attention.Muted
	}
}

// persistLayout is the single call site shape: every surface that can change
// the saved layout goes through here rather than assembling the argument list.
func (g *Game) persistLayout() {
	saveLayout(g.UI.PanelMgr, &g.UI.Attention.Matrix, &g.UI.Cues.Muted)
}

// saveLayout marshals the current workspace tree + active preset into
// layout.json. Atomic via .tmp + os.Rename. Errors are logged.
func saveLayout(panelMgr *ui.PanelManager,
	att *ui.AttentionMatrix, muted *bool) {
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
	if att != nil {
		kinds := make([]uint8, len(att))
		for i, v := range att {
			kinds[i] = uint8(v)
		}
		lf.Attention = &attentionJSON{Kinds: kinds}
		if muted != nil {
			lf.Attention.Muted = *muted
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
