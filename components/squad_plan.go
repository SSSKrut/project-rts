package components

// SquadPlanMode is what the squad brain has decided to do WITH the current
// order — never instead of it (P1). None means "execute the order plainly".
type SquadPlanMode uint8

const (
	SquadPlanNone SquadPlanMode = iota
	// SquadPlanBounding — move under contact in alternating waves: one wave
	// runs to the next bound, the other holds in positions covering it.
	SquadPlanBounding
	// SquadPlanRelocate — the roster is standing in a beaten zone; the march
	// pauses while the men clear it, then resumes the same order.
	SquadPlanRelocate
	// SquadPlanClearSeq — ClearBuilding sequenced: stack up, enter, sweep
	// storey by storey.
	SquadPlanClearSeq
)

// ClearSeq phases (SquadPlan.Phase while Mode == SquadPlanClearSeq).
const (
	ClearPhaseStackUp uint8 = iota
	ClearPhaseEnter
	ClearPhaseSweep
	ClearPhaseDone
)

// SquadPlan lives on the Squad entity. POD; the brain is the only writer,
// FormationSystem and SurvivalInstinct are the readers.
//
// Phase doubles as the bounding wave index (0 or 1): the wave whose WaveMask
// bit equals Phase==1 is the one moving.
type SquadPlan struct {
	Mode     SquadPlanMode
	Phase    uint8
	Floor    uint8
	WaveMask uint8
	Timer    float32
	Since    float32
	// CalmSince: when the pressure first dropped below the exit threshold
	// (0 while still under fire) — hysteresis out of Bounding.
	CalmSince float32
	Anchor    WorldPos
}

// InMovingWave reports whether roster slot `slot` is in the wave that bounds
// this phase.
func (p *SquadPlan) InMovingWave(slot uint8) bool {
	if slot >= 8 {
		return true
	}
	return (p.WaveMask&(1<<slot) != 0) == (p.Phase == 1)
}

// SquadPlanLabel is the player-facing name of a mode (ASCII — runtime UI).
func SquadPlanLabel(m SquadPlanMode) string {
	switch m {
	case SquadPlanBounding:
		return "Bounding"
	case SquadPlanRelocate:
		return "Relocating"
	case SquadPlanClearSeq:
		return "Clearing"
	}
	return ""
}

// ClearPhaseLabel names one ClearSeq step (ASCII — runtime UI).
func ClearPhaseLabel(p uint8) string {
	switch p {
	case ClearPhaseStackUp:
		return "Stack up"
	case ClearPhaseEnter:
		return "Entry"
	case ClearPhaseSweep:
		return "Sweep"
	case ClearPhaseDone:
		return "Held"
	}
	return ""
}
