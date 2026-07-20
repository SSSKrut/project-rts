package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// combatEvidenceWindow - LastFiredAt within this many seconds of `now`
// signals an active shooter, promoting the contact to Hostile via the
// CombatEvidence source. Matches threatTTL roughly so the indicator decays
// in step with ThreatSource fade.
const combatEvidenceWindow float32 = 4.0

// applyCombatEvidence walks live contacts and inspects the tracked entity's
// Weapon for a recent shot. Recent fire ⇒ promote PerceivedAffil=Hostile +
// Source=CombatEvidence (unless PlayerSet or CloseRangeID already higher).
//
// Phase 18.5: ThreatSource has no owner field, so we use the tracked unit's
// own Weapon.LastFiredAt as a proxy. When ThreatSource gets an Owner field
// later, swap this to a positional / owner match for stricter semantics.
func (sys *ContactSystem) applyCombatEvidence() {
	reg := sys.registryRes.Get()
	if reg.Tracked == nil {
		return
	}
	for tracked, contactEnt := range reg.Tracked {
		if !sys.worldRef.Alive(tracked) {
			delete(reg.Tracked, tracked)
			continue
		}
		if !sys.worldRef.Alive(contactEnt) {
			delete(reg.Tracked, tracked)
			continue
		}
		c := sys.contactMap.Get(contactEnt)
		if c == nil {
			continue
		}
		if sys.contactPlayerSet.Has(contactEnt) {
			continue
		}
		if c.Source >= components.SourceCloseRangeID {
			continue
		}
		// Locate the active weapon on the tracked entity. Alive-check before
		// deref: unarmed vehicles carry a zero Active, and Map.Get panics on
		// dead/zero entities.
		var lastFired float32
		if eq := sys.equipMap.Get(tracked); eq != nil &&
			eq.Active != (ecs.Entity{}) && sys.worldRef.Alive(eq.Active) {
			if w := sys.weaponMap.Get(eq.Active); w != nil {
				lastFired = w.LastFiredAt
			}
		}
		if lastFired <= 0 {
			continue
		}
		if sys.elapsed-lastFired > combatEvidenceWindow {
			continue
		}
		c.PerceivedAffil = components.AffilHostile
		if c.Source < components.SourceCombatEvidence {
			c.Source = components.SourceCombatEvidence
		}
	}
}
