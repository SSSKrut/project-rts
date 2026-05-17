package components

// Faction — hostility group an entity belongs to. Phase 14 introduces the
// first real player-vs-enemy split: WeaponSystem fires only at targets whose
// Faction.ID differs from the seer's, the map renderer tints squads by
// faction (player blue/green, enemy red/orange), and SquadService stamps
// Faction onto each unit at CreateFromTemplate time.
//
// Phase 14 ships two factions (Player=0, EnemyRed=1); slots 2..7 are
// reserved for future scenarios (multi-side skirmishes, neutral civilians).
// Expanding past 8 requires widening the field — defer until needed.
type Faction struct {
	ID uint8
}

const (
	// FactionPlayer is the default and the implicit identity for any entity
	// that has no Faction component (zero-value uint8). Backwards-compat:
	// pre-Phase-14 spawn paths land here without touching anything.
	FactionPlayer uint8 = 0
	// FactionEnemyRed is the first hostile faction — the Phase 14 test scene
	// uses it for the lone TmplMotorRifle that exercises the combat loop.
	FactionEnemyRed uint8 = 1
)
