package ui

import (
	"rts-go/components"
)

// ContactMenuTag values used as ContextMenuItem.Tag for the contact RMB
// popup. Caller dispatches on these in the Committed handler.
const (
	ContactMenuTagNone         uint16 = 0
	ContactMenuTagApplyPreset  uint16 = 1
	ContactMenuTagOpenBuilder  uint16 = 2
	ContactMenuTagResetToAuto  uint16 = 3
	ContactMenuTagDelete       uint16 = 4
)

// BuildContactContextSections returns the 3 quick-pick sections (12 presets)
// + utility section (advanced builder / reset / delete). Builtin presets are
// looked up by name from the SymbologyPresets resource.
func BuildContactContextSections(presets *components.SymbologyPresets) []ContextMenuSection {
	if presets == nil {
		return nil
	}
	get := func(name string) (components.SymbolSpec, bool) {
		for i := range presets.All {
			if presets.All[i].Name == name {
				return presets.All[i].Spec, true
			}
		}
		return components.SymbolSpec{}, false
	}
	pick := func(name, label string) ContextMenuItem {
		s, ok := get(name)
		return ContextMenuItem{
			Label:       label,
			Tag:         ContactMenuTagApplyPreset,
			ContactSpec: s,
			Enabled:     ok,
		}
	}
	return []ContextMenuSection{
		{
			Header: "Hostile",
			Items: []ContextMenuItem{
				pick("hostile_infantry", "Infantry"),
				pick("hostile_vehicle", "Vehicle"),
				pick("hostile_air", "Air"),
				pick("hostile_naval", "Naval"),
			},
		},
		{
			Header: "Neutral",
			Items: []ContextMenuItem{
				pick("neutral_infantry", "Infantry"),
				pick("neutral_vehicle", "Vehicle"),
				pick("neutral_air", "Air"),
				pick("neutral_naval", "Naval"),
			},
		},
		{
			Header: "Unknown",
			Items: []ContextMenuItem{
				pick("unknown_infantry", "Infantry"),
				pick("unknown_vehicle", "Vehicle"),
				pick("unknown_air", "Air"),
				pick("unknown_naval", "Naval"),
			},
		},
		{
			Items: []ContextMenuItem{
				{Label: "Change icon (advanced)...", Tag: ContactMenuTagOpenBuilder, Enabled: true},
				{Label: "Reset to auto", Tag: ContactMenuTagResetToAuto, Enabled: true},
				{Label: "Delete contact", Tag: ContactMenuTagDelete, Enabled: true},
			},
		},
	}
}
