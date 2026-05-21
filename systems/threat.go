package systems

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ThreatSystem - Phase 17 M17.0 per-unit Threat aggregator. Each tick:
//
//  1. Decay every Threat channel by its ThreatSpec rate * dt.
//  2. Drain the unit's DangerBuffer - per event read the spec, add Strength
//     to the named channel, accumulate Pos into a strength-weighted ThreatDir.
//  3. Clamp each channel to [0, 1], recompute Total + State.
//  4. Reset Head/Count so producers start the next tick with an empty ring.
//
// Phase 14 producer (propagateSuppression direct write into Threat.Suppression)
// is replaced in M17.0.2 by DangerBulletImpact events; future kinds plug into
// the same drain via threatSpecs.
type ThreatSystem struct {
	filter   *ecs.Filter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos]
	elapsed  float32
	lastTick float32
}

func NewThreatSystem() *ThreatSystem {
	return &ThreatSystem{}
}

func (sys *ThreatSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos](w)
}

func (ThreatSystem) Name() string { return "threat" }

func (ThreatSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: 250 * time.Millisecond,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ThreatSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed
	dt := now - sys.lastTick
	if dt < 0 || dt > 1.0 {
		dt = float32(ctx.Delta.Seconds())
	}
	sys.lastTick = now

	q := sys.filter.Query()
	for q.Next() {
		_, threat, buf, pos := q.Get()
		tickThreat(threat, buf, pos, dt)
	}
}

// tickThreat applies the per-unit decay + drain + classify pipeline. Pulled
// out as a free function so M17.0.4 can table-test it without spinning an
// ECS world.
func tickThreat(threat *components.Threat, buf *components.DangerBuffer, pos *components.WorldPos, dt float32) {
	// 1. Decay every channel by its per-second rate. ChannelDecayRates is a
	//    4-entry table indexed by ThreatChannel.
	threat.Suppression = decayValue(threat.Suppression, dt, components.ChannelDecayRates[components.ThreatChannelSuppression])
	threat.ShotsFired = decayValue(threat.ShotsFired, dt, components.ChannelDecayRates[components.ThreatChannelShotsFired])
	threat.Endangered = decayValue(threat.Endangered, dt, components.ChannelDecayRates[components.ThreatChannelEndangered])
	threat.Injury = decayValue(threat.Injury, dt, components.ChannelDecayRates[components.ThreatChannelInjury])

	// 2. Drain the buffer. Each event contributes Strength to its channel
	//    and a weighted vote toward the new ThreatDir. Weight is the event
	//    Strength itself - per-tick drain means every event in the ring is
	//    "fresh" (same recency), so recency*strength reduces to strength.
	if buf.Count > 0 {
		var tx, tz, totalWeight float32
		// Walk Count entries ending at Head (oldest first). Head points at
		// the next write slot; the oldest entry sits Count slots behind it.
		start := int(buf.Head) - int(buf.Count)
		if start < 0 {
			start += components.DangerBufferSize
		}
		for i := uint8(0); i < buf.Count; i++ {
			idx := (start + int(i)) % components.DangerBufferSize
			ev := &buf.Events[idx]
			channel := components.ChannelForDanger(ev.Kind)
			switch channel {
			case components.ThreatChannelSuppression:
				threat.Suppression += ev.Strength
			case components.ThreatChannelShotsFired:
				threat.ShotsFired += ev.Strength
			case components.ThreatChannelEndangered:
				threat.Endangered += ev.Strength
			case components.ThreatChannelInjury:
				threat.Injury += ev.Strength
			default:
				continue
			}
			// ThreatDir vote: vector from event location toward the unit,
			// weighted by Strength. Per-tick drain means all events are
			// "fresh", so recency*strength collapses to strength.
			delta := pos.Sub(ev.Pos)
			dx, dz := delta.X, delta.Z
			d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
			if d > 1e-3 {
				tx += (dx / d) * ev.Strength
				tz += (dz / d) * ev.Strength
				totalWeight += ev.Strength
			}
		}
		if totalWeight > 0 {
			l := float32(math.Sqrt(float64(tx*tx + tz*tz)))
			if l > 0 {
				threat.ThreatDir = rl.Vector3{X: tx / l, Z: tz / l}
			}
		}
		buf.Head = 0
		buf.Count = 0
	}

	// 3. Clamp channels.
	threat.Suppression = clamp01(threat.Suppression)
	threat.ShotsFired = clamp01(threat.ShotsFired)
	threat.Endangered = clamp01(threat.Endangered)
	threat.Injury = clamp01(threat.Injury)

	// 4. Total + State.
	total := threat.Suppression + threat.ShotsFired + threat.Endangered + threat.Injury
	if total > 1 {
		total = 1
	}
	threat.Total = total
	threat.State = components.ClassifyThreat(total)
}

// decayValue applies a per-second linear drop, clamped at 0.
func decayValue(value, dt, rate float32) float32 {
	if value <= 0 || rate <= 0 {
		return value
	}
	value -= rate * dt
	if value < 0 {
		return 0
	}
	return value
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
