package components

// Stamina is the per-Unit fatigue tank. UnitMovementSystem drains it when
// Pace > Walk and recovers it when Pace=Walk + Stance in {Stand, Crouch}.
// At Current==0 the StaminaExhausted marker forces Walk regardless of the
// squad's MovementProfile.Pace until Current rises past the regen threshold.
type Stamina struct {
	Current     float32
	MaxLevel    float32
	RecoverRate float32
}

// StaminaExhausted is a transient marker added by UnitMovementSystem when
// Stamina.Current reaches 0 and removed once it climbs back over the regen
// threshold. While present, Pace is forced to Walk without touching the
// MovementProfile so the player's setting persists across exhaustion cycles.
//
// Add / remove batched through a worker-safe deferred mutation slice.
type StaminaExhausted struct{}
