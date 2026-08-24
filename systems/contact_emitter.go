package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// emitterRec is one ESM intercept. `at` is where the RECEIVER stood, not where
// the emitter is — that is the entire point of a bearing.
type emitterRec struct {
	observer  ecs.Entity
	target    ecs.Entity
	at        components.WorldPos
	bearing   float32
	obsFactID uint8
}

// runEmitterPass is the passive half of the emissions dilemma (Phase 20 P4). It
// is a separate pass rather than another channel inside visualMagnitude because
// it answers a different question and returns a different answer: no facing, no
// LOS, no detection meter, and a direction instead of a point.
//
// Two rules carry the design:
//
//   - Range is min(what the emitter radiates, what the receiver can hear).
//     Both numbers are visible in the panel, so "why did they hear me" always
//     has an answer.
//   - An intercept NEVER touches Awareness. Awareness feeds pickTarget and the
//     utility evaluator with a position, and a bearing has none; writing the
//     emitter's true position there would make ESM strictly better than eyes
//     and quietly turn the dilemma into a free upgrade.
func (sys *ContactSystem) runEmitterPass() {
	sys.emitBuf = sys.emitBuf[:0]
	radiating := false
	for i := range sys.unitsBuf {
		if sys.unitsBuf[i].emitRange > 0 {
			radiating = true
			break
		}
	}
	if !radiating {
		return
	}
	for si := range sys.seersBuf {
		s := &sys.seersBuf[si]
		ci := s.sensors.FindChannel(components.SensorESM)
		if ci < 0 || !s.sensors.ChannelOn(uint8(ci)) {
			continue
		}
		esmRange := s.sensors.Channels[ci].BaseRangeM
		for ui := range sys.unitsBuf {
			cand := &sys.unitsBuf[ui]
			if cand.emitRange <= 0 || cand.ent == s.ent || cand.faction == s.faction {
				continue
			}
			reach := cand.emitRange
			if esmRange < reach {
				reach = esmRange
			}
			dx := cand.x - s.x
			dz := cand.z - s.z
			if dx*dx+dz*dz > reach*reach {
				continue
			}
			sys.emitBuf = append(sys.emitBuf, emitterRec{
				observer:  s.ent,
				target:    cand.ent,
				at:        s.pos,
				bearing:   float32(math.Atan2(float64(dx), float64(dz))),
				obsFactID: s.faction,
			})
		}
	}
}

// applyEmitterUpsert files bearing tracks. It runs AFTER the visual upsert so
// that anything actually seen this tick already owns a real track: an intercept
// must never overwrite a position with a direction.
//
// When several receivers hear the same emitter the LAST one in seer order wins
// the track. Deterministic but arbitrary, and deliberately so for now — every
// bearing is equally true, and picking a "best" one is meaningless. The moment
// two bearings should CROSS into a fix, this is the place that has to keep both.
func (sys *ContactSystem) applyEmitterUpsert() {
	if len(sys.emitBuf) == 0 {
		return
	}
	reg := sys.registryRes.Get()
	if reg.Tracked == nil {
		*reg = components.NewContactRegistry()
	}
	for i := range sys.emitBuf {
		rec := &sys.emitBuf[i]
		if rec.obsFactID != components.FactionPlayer {
			continue
		}
		if !sys.worldRef.Alive(rec.target) {
			delete(reg.Tracked, rec.target)
			continue
		}
		ent, ok := reg.Tracked[rec.target]
		if ok && sys.worldRef.Alive(ent) {
			c := sys.contactMap.Get(ent)
			if c == nil || !c.BearingOnly {
				continue // a second source already ranged it
			}
			c.EstimatedPos = rec.at
			c.Bearing = rec.bearing
			c.LastSeenTime = sys.elapsed
			continue
		}
		newEnt := sys.worldRef.NewEntity()
		c := components.Contact{
			Tracked:        rec.target,
			Observer:       rec.obsFactID,
			EstimatedPos:   rec.at,
			LastSeenTime:   sys.elapsed,
			PerceivedAffil: components.AffilUnknown,
			PerceivedDim:   components.DimUnknownClass,
			Source:         components.SourceSensor,
			Bearing:        rec.bearing,
			BearingOnly:    true,
		}
		sys.contactMap.Add(newEnt, &c)
		reg.Tracked[rec.target] = newEnt
	}
	sys.emitBuf = sys.emitBuf[:0]
}
