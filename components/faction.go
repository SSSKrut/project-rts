package components

// Faction is the hostility group an entity belongs to. WeaponSystem fires
// only at targets with a different Faction.ID; the map renderer tints
// squads by faction. SquadService stamps Faction onto each unit at
// CreateFromTemplate time.
//
// Two factions ship today (Player=0, EnemyRed=1); slots 2..7 are reserved
// for multi-side scenarios / neutral civilians.
type Faction struct {
	ID uint8
}

const (
	// FactionPlayer is the default and implicit identity for any entity
	// without a Faction component (zero-value uint8).
	FactionPlayer uint8 = 0
	// FactionEnemyRed is the first hostile faction.
	FactionEnemyRed uint8 = 1
)
