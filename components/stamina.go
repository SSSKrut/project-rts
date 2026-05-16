package components

// Stamina is the per-Unit fatigue tank. UnitMovementSystem (M13.3) drains it
// when Pace > Walk and recovers it when Pace=Walk + Stance ∈ {Stand, Crouch}.
// At Current==0 the StaminaExhausted marker forces Walk regardless of squad's
// MovementProfile.Pace until Current rises past the regen threshold.
//
// MaxLevel is per-role (RoleService.AssignRole writes it via the per-role
// modifier — MG / AT / Engineer / Demo / Radio = 0.7..0.85 ×, others = 1.0).
// See PHASE-13.md P2 for the table.
//
// Phase 13 ranges normalised to 0..MaxLevel ≤ 1.0; no per-stance speed cost
// table per fatigue level — Phase 15 may add doctrine-based scaling.
type Stamina struct {
	Current     float32
	MaxLevel    float32
	RecoverRate float32
}

// StaminaExhausted is a transient marker added by UnitMovementSystem when
// Stamina.Current reaches 0 and removed once it climbs back over the regen
// threshold (PHASE-13.md P3: > 0.3 × MaxLevel). While present, Pace is forced
// to Walk regardless of squad's MovementProfile.Pace — without touching the
// MovementProfile so the player's setting persists across exhaustion cycles.
//
// Add / remove batched through a worker-safe deferred mutation slice in
// UnitMovementSystem (same pattern as the Phase 11.6 separation-set updates).
type StaminaExhausted struct{}
