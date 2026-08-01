package components

// IconKind picks the central glyph on a SymbolSpec (APP-6 sub-class within
// the chosen Dimension). Phase 18.5 ships a tight curated set; later phases
// extend without changing layout.
type IconKind uint8

const (
	IconNone IconKind = iota
	IconInfantry        // crossed X inside the frame
	IconArmor           // filled oval
	IconArtillery       // filled dot
	IconAirborne        // upward chevron
	IconRecon           // diagonal slash
	IconHQ              // small flag-rectangle
	IconSupply          // square with vertical bar (logistics)
	IconNavalShip       // wavy line
	IconAircraft        // V/arrow
)

// Echelon enumerates standard NATO unit sizes (team..battalion). Phase 18.5
// renders no echelon row on placeholder draw — placeholder for Track 18.5.D
// builder UI hookup.
type Echelon uint8

const (
	EchelonNone Echelon = iota
	EchelonTeam
	EchelonSquad
	EchelonSection
	EchelonPlatoon
	EchelonCompany
	EchelonBattalion
	EchelonBrigade
)

// ModifierKind is a slot for APP-6 amendments (reinforced, taskforce, HQ
// emphasis). Phase 18.5: only ModifierNone shipped; the field exists so
// Track D's builder UI binary-compatible with later wave of modifiers.
type ModifierKind uint8

const (
	ModifierNone ModifierKind = iota
)

// SymbolSpec - flat, 32-byte composite. Map key for any bake cache (Spec
// → Texture) Track 18.5.C+ may add later. Affiliation drives frame shape +
// fill color; Dimension+Icon picks the central glyph; Echelon stamps the
// top edge; Modifiers stamp the sides.
type SymbolSpec struct {
	Affiliation Affiliation
	Dimension   Dimension
	Icon        IconKind
	Echelon     Echelon
	Modifiers   [4]ModifierKind
}

// SymbologyPreset binds a user-readable Name to a SymbolSpec. Stored in the
// SymbologyPresets resource and persisted to save/symbols.json by Track D.
type SymbologyPreset struct {
	Name string
	Spec SymbolSpec
}

// SymbologyPresets - singleton library. Holds builtin 3×4 quick-pick presets
// + any player-saved presets from the builder panel.
type SymbologyPresets struct {
	All []SymbologyPreset
}

// UnitSymbolOverride freezes a per-unit Spec set via the Symbol Editor's
// "Apply to selection". Lookup order in spec resolution: UnitSymbolOverride
// > role default.
type UnitSymbolOverride struct {
	Spec SymbolSpec
}

// SquadSymbolOverride is the player-assigned Spec for a Squad marker. Set
// via the Symbol Editor's "Apply to selection" while a whole squad is
// selected. Lookup order for a squad marker: SquadSymbolOverride > roster
// composition.
type SquadSymbolOverride struct {
	Spec SymbolSpec
}

// ContactSymbolOverride is the player-classified Spec for a Contact. Set
// by RMB context menu (Track 18.5.F, copies a preset's Spec) or the Symbol
// Editor "Apply to selection" (Track 18.5.D). Co-exists with ContactPlayerSet.
type ContactSymbolOverride struct {
	Spec SymbolSpec
}
