package systems

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Snapshot resource codecs (18.9 M4): JSON blobs keyed by name. These
// resources are UI-facing (journal, user presets), not sim state — float
// round-trip precision is not load-bearing here.

func appendResourceSection(w *ecs.World, out *saveBuf) error {
	type blob struct {
		name string
		data []byte
	}
	var blobs []blob
	add := func(name string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("save %s: %w", name, err)
		}
		blobs = append(blobs, blob{name, data})
		return nil
	}
	evRes := ecs.NewResource[components.EventLog](w)
	if ev := evRes.Get(); ev != nil {
		if err := add("EventLog", ev); err != nil {
			return err
		}
	}
	ohRes := ecs.NewResource[components.OrderHistory](w)
	if oh := ohRes.Get(); oh != nil {
		if err := add("OrderHistory", oh); err != nil {
			return err
		}
	}
	out.u16(uint16(len(blobs)))
	for _, b := range blobs {
		out.str(b.name)
		out.u32(uint32(len(b.data)))
		out.Write(b.data)
	}
	return nil
}

func readResourceSection(w *ecs.World, r *saveReader) error {
	n := int(r.u16("resCount"))
	for i := 0; i < n; i++ {
		name := r.str("resName")
		size := int(r.u32("resSize"))
		data := r.bytes(size, "resBlob")
		if r.err != nil {
			return r.err
		}
		var err error
		switch name {
		case "EventLog":
			res := ecs.NewResource[components.EventLog](w)
			err = json.Unmarshal(data, res.Get())
		case "OrderHistory":
			res := ecs.NewResource[components.OrderHistory](w)
			err = json.Unmarshal(data, res.Get())
		case "FormationPresets":
			// Block C removed the editor and its presets. A save written before
			// that still carries the blob; skipping it keeps old saves loadable
			// instead of failing on a resource nobody reads any more.
		default:
			err = fmt.Errorf("unknown resource codec %q", name)
		}
		if err != nil {
			return fmt.Errorf("load resource %s: %w", name, err)
		}
	}
	return nil
}

// Symbology sidecar (closes deferred 18.5): the preset library persists in
// save/symbols.json across sessions, independent of world snapshots.
const symbolsPath = "save/symbols.json"

func SaveSymbologySidecar(w *ecs.World) error {
	res := ecs.NewResource[components.SymbologyPresets](w)
	sp := res.Get()
	if sp == nil {
		return nil
	}
	data, err := json.MarshalIndent(sp, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(symbolsPath), 0o755); err != nil {
		return err
	}
	tmp := symbolsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, symbolsPath)
}

// LoadSymbologySidecar returns the persisted preset library, or ok=false
// when none exists (boot falls back to builtins).
func LoadSymbologySidecar() (components.SymbologyPresets, bool) {
	data, err := os.ReadFile(symbolsPath)
	if err != nil {
		return components.SymbologyPresets{}, false
	}
	var sp components.SymbologyPresets
	if err := json.Unmarshal(data, &sp); err != nil {
		fmt.Printf("symbols.json: %v\n", err)
		return components.SymbologyPresets{}, false
	}
	return sp, len(sp.All) > 0
}
