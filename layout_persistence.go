package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"

	"rts-go/ui"
)

// Phase 13.5 M13.5.5 - minimal layout persistence.
//
// Schema is a single JSON object with version + the two split ratios we
// can mutate from the UI today. Phase 22 will extend this into a proper
// layout-tree (named presets, dock state, undocked window positions); the
// version field gives us a clean compat boundary for that.

// layoutSavePath is where the panel ratios live. Sits alongside the
// per-world save directory (`save/world-default/`) but at the root of `save/`
// because the layout is user-level, not per-world.
const layoutSavePath = "./save/layout.json"

// layoutFileVersion identifies the on-disk schema revision. Bumped when
// fields are added/removed in a way Phase 22 readers can't infer. Phase 13.5
// keeps it at 1.
const layoutFileVersion uint16 = 1

// layoutFile is the JSON-serialised representation of mutable layout state.
type layoutFile struct {
	Version        uint16  `json:"version"`
	RightColRatio  float32 `json:"right_col_ratio"`
	InspectorRatio float32 `json:"inspector_ratio"`
}

// loadLayout reads the layout file (if present) and applies its ratios to
// the PanelManager BEFORE the first Recompute. Missing file = no-op (defaults
// stay). Parse error / corruption = log warning + no-op (defaults stay).
// Out-of-range ratios are clamped to a safe band so a hand-edited file can't
// produce a 100%-Inspector layout the user can't recover from.
func loadLayout(panelMgr *ui.PanelManager) {
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
	panelMgr.RightColRatio = clampRatio(lf.RightColRatio, 0.10, 0.60)
	panelMgr.InspectorRatio = clampRatio(lf.InspectorRatio, 0.10, 0.90)
}

// saveLayout marshals the current PanelManager ratios into layout.json. Atomic
// via .tmp + os.Rename - partial writes from a crash mid-Marshal can't leave
// a corrupt file in place. Errors are logged but not returned: layout
// persistence failure shouldn't crash the game.
func saveLayout(panelMgr *ui.PanelManager) {
	if panelMgr == nil {
		return
	}
	dir := filepath.Dir(layoutSavePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("layout: mkdir failed (%v)", err)
		return
	}
	lf := layoutFile{
		Version:        layoutFileVersion,
		RightColRatio:  panelMgr.RightColRatio,
		InspectorRatio: panelMgr.InspectorRatio,
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

// clampRatio bounds a float to [lo, hi]. Used when reading user-editable
// JSON so a hand-typed value out of [0,1] doesn't wreck the layout.
func clampRatio(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
