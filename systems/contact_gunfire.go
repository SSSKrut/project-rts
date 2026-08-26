package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// A shot is an emission. Until this pass existed, a shooter nobody had seen did
// not exist at all — not for the sim and not for the player, who watched men
// die with no marker anywhere on the map. applyCombatEvidence only ever
// PROMOTED tracks that already existed, and the audio bubble
// (audio_detect.go) never counted gunfire.
//
// The one discipline that makes this safe is the one the ESM pass already set
// (contact_emitter.go): a gunfire contact NEVER touches Awareness. It tells the
// player where the fire came from; it does not hand anybody a firing solution.
// Otherwise every ambush would be answered by men who cannot see their
// attacker.
const (
	// How long after a shot the report still files a contact. Matches
	// combatEvidenceWindow so the marker and the hostile promotion decay
	// together.
	gunshotEchoS float32 = 4.0
	// Heard radius is the weapon's own reach, floored and capped: a pistol
	// still carries, a 125 mm does not reach across the map.
	gunshotMinM float32 = 120
	gunshotMaxM float32 = 400
)

// gunshotRec is one heard shot. Unlike an ESM intercept this carries the
// shooter's real position: a muzzle report is a bang with a direction AND a
// distance, and a marker the player cannot place is not the ask.
type gunshotRec struct {
	observer  ecs.Entity
	target    ecs.Entity
	at        components.WorldPos
	dim       components.DimensionMask
	obsFactID uint8
}

// gunshotReach is how far this entity's last shot is audible, 0 if it has not
// fired inside the echo window.
func (sys *ContactSystem) gunshotReach(ent ecs.Entity) float32 {
	eq := sys.equipMap.Get(ent)
	if eq == nil || eq.Active == (ecs.Entity{}) || !sys.worldRef.Alive(eq.Active) {
		return 0
	}
	w := sys.weaponMap.Get(eq.Active)
	if w == nil || w.LastFiredAt <= 0 || sys.elapsed-w.LastFiredAt > gunshotEchoS {
		return 0
	}
	r := w.RangeM
	if r < gunshotMinM {
		r = gunshotMinM
	}
	if r > gunshotMaxM {
		r = gunshotMaxM
	}
	return r
}

// runGunfirePass collects who heard whom shoot. No facing, no LOS, no detection
// meter — a report is heard, not seen.
func (sys *ContactSystem) runGunfirePass() {
	sys.shotBuf = sys.shotBuf[:0]
	firing := false
	for i := range sys.unitsBuf {
		if sys.unitsBuf[i].shotHeardM > 0 {
			firing = true
			break
		}
	}
	if !firing {
		return
	}
	// Shooter-major, one record per shooter: the marker is about who fired, and
	// a seer-major walk would file the same bang once per pair of ears.
	for ui := range sys.unitsBuf {
		cand := &sys.unitsBuf[ui]
		if cand.shotHeardM <= 0 || cand.faction == components.FactionPlayer {
			continue
		}
		reachSq := cand.shotHeardM * cand.shotHeardM
		for si := range sys.seersBuf {
			s := &sys.seersBuf[si]
			if s.faction != components.FactionPlayer || s.ent == cand.ent {
				continue
			}
			dx := cand.x - s.x
			dz := cand.z - s.z
			if dx*dx+dz*dz > reachSq {
				continue
			}
			sys.shotBuf = append(sys.shotBuf, gunshotRec{
				observer: s.ent, target: cand.ent,
				at: cand.pos, dim: cand.dimMask, obsFactID: s.faction,
			})
			break
		}
	}
}

// applyGunfireUpsert files the markers. Runs after both the visual and the ESM
// upsert: a shooter that is actually SEEN keeps its sensor track, and one only
// heard on ESM gets upgraded from a bearing to a position, because a muzzle
// report carries range and a radio intercept does not.
func (sys *ContactSystem) applyGunfireUpsert() {
	if len(sys.shotBuf) == 0 {
		return
	}
	reg := sys.registryRes.Get()
	if reg.Tracked == nil {
		*reg = components.NewContactRegistry()
	}
	for i := range sys.shotBuf {
		rec := &sys.shotBuf[i]
		if !sys.worldRef.Alive(rec.target) {
			delete(reg.Tracked, rec.target)
			continue
		}
		if ent, ok := reg.Tracked[rec.target]; ok && sys.worldRef.Alive(ent) {
			c := sys.contactMap.Get(ent)
			if c == nil {
				continue
			}
			// A live sensor track this tick is better than a bang; only refresh
			// what the shot genuinely improves.
			if c.Source >= components.SourceCloseRangeID {
				continue
			}
			c.EstimatedPos = rec.at
			c.BearingOnly = false
			c.LastSeenTime = sys.elapsed
			c.PerceivedAffil = components.AffilHostile
			if c.Source < components.SourceCombatEvidence {
				c.Source = components.SourceCombatEvidence
			}
			continue
		}
		newEnt := sys.worldRef.NewEntity()
		c := components.Contact{
			Tracked:        rec.target,
			Observer:       rec.obsFactID,
			EstimatedPos:   rec.at,
			LastSeenTime:   sys.elapsed,
			PerceivedAffil: components.AffilHostile,
			PerceivedDim:   components.DimensionFromMask(rec.dim),
			Source:         components.SourceCombatEvidence,
		}
		sys.contactMap.Add(newEnt, &c)
		reg.Tracked[rec.target] = newEnt
	}
	sys.shotBuf = sys.shotBuf[:0]
}
