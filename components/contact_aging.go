package components

// Aging parameters for rendered contact alpha. Contacts never auto-delete on
// Phase 18.5 — they only fade. Player removes them manually via the RMB
// context menu (Track 18.5.F).
const (
	ContactGhostAfter   float32 = 5.0  // age in seconds when fade begins
	ContactFadeDuration float32 = 25.0 // additional seconds until floor alpha
	ContactFloorAlpha   float32 = 0.3  // do not fully disappear
)

// ContactAgeAlpha returns 1.0 for fresh contacts, lerps to ContactFloorAlpha
// over the GhostAfter..GhostAfter+FadeDuration window, floors after that.
// Lives in components so systems and ui share one definition.
func ContactAgeAlpha(lastSeen, now float32) float32 {
	age := now - lastSeen
	if age <= ContactGhostAfter {
		return 1.0
	}
	if age >= ContactGhostAfter+ContactFadeDuration {
		return ContactFloorAlpha
	}
	t := (age - ContactGhostAfter) / ContactFadeDuration
	return 1.0 - (1.0-ContactFloorAlpha)*t
}
