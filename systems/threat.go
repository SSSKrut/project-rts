package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ThreatSystem is the per-unit Threat aggregator. Each tick: decay every
// channel, drain DangerBuffer (Strength → channel + weighted ThreatDir),
// clamp to [0, 1], recompute Total + State, reset ring for next tick.
type ThreatSystem struct {
	filter    *ecs.Filter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos]
	vehFilter *ecs.Filter4[components.Vehicle, components.Threat, components.DangerBuffer, components.WorldPos]
	lastTick  float32
}

func NewThreatSystem() *ThreatSystem {
	return &ThreatSystem{}
}

func (sys *ThreatSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos](w)
	sys.vehFilter = ecs.NewFilter4[components.Vehicle, components.Threat, components.DangerBuffer, components.WorldPos](w)
}

func (ThreatSystem) Name() string { return "threat" }

func (ThreatSystem) LODPolicy() core.LODPolicy {
	// Active-only: filters are not tier-scoped (see ContactSystem note).
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// PostLoad re-seeds lastTick so the first post-load dt is the same float32
// subtraction a continuous run would compute.
func (sys *ThreatSystem) PostLoad(simNow float64) { sys.lastTick = float32(simNow) }

func (sys *ThreatSystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)
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

	// Vehicles run the same aggregator so their reflexes read a live Threat.
	qv := sys.vehFilter.Query()
	for qv.Next() {
		_, threat, buf, pos := qv.Get()
		tickThreat(threat, buf, pos, dt)
	}
}

// tickThreat applies the per-unit decay + drain + classify pipeline. Pulled
// out as a free function so it can be table-tested without an ECS world.
func tickThreat(threat *components.Threat, buf *components.DangerBuffer, pos *components.WorldPos, dt float32) {
	threat.Suppression = decayValue(threat.Suppression, dt, components.ChannelDecayRates[components.ThreatChannelSuppression])
	threat.ShotsFired = decayValue(threat.ShotsFired, dt, components.ChannelDecayRates[components.ThreatChannelShotsFired])
	threat.Endangered = decayValue(threat.Endangered, dt, components.ChannelDecayRates[components.ThreatChannelEndangered])
	threat.Injury = decayValue(threat.Injury, dt, components.ChannelDecayRates[components.ThreatChannelInjury])

	// Drain the buffer: each event contributes Strength to its channel and
	// a weighted vote toward ThreatDir. Per-tick drain means recency*strength
	// collapses to strength.
	if buf.Count > 0 {
		var tx, tz, totalWeight float32
		// Walk Count entries ending at Head (oldest first).
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
			// weighted by Strength.
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

	threat.Suppression = clamp01(threat.Suppression)
	threat.ShotsFired = clamp01(threat.ShotsFired)
	threat.Endangered = clamp01(threat.Endangered)
	threat.Injury = clamp01(threat.Injury)

	total := threat.Suppression + threat.ShotsFired + threat.Endangered + threat.Injury
	if total > 1 {
		total = 1
	}
	threat.Total = total
	threat.State = components.ClassifyThreat(total)
	// Every channel decayed to zero → the direction is stale intel; readers
	// (reflex orientation, cover pick) must not act on it.
	if total == 0 {
		threat.ThreatDir = rl.Vector3{}
	}
}

// decayValue applies a per-second linear drop clamped at 0.
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
