package components

// Faction is the hostility group an entity belongs to. WeaponSystem fires
// only at targets with a different Faction.ID. Phase 18.5 adds Neutral
// (armed non-combatants / coalition AI) and Wildlife (animals — never
// triggers CombatEvidence promotion in ContactSystem).
type Faction struct {
	ID uint8
}

const (
	// FactionPlayer is the default and implicit identity for any entity
	// without a Faction component (zero-value uint8).
	FactionPlayer   uint8 = 0
	FactionEnemyRed uint8 = 1
	FactionNeutral  uint8 = 2
	FactionWildlife uint8 = 3
)
