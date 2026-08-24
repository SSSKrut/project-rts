package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AirReflexSystem is the airborne mirror of VehicleReflexSystem (P7): it arms
// AircraftOverride, the driver executes it next tick. Reflexes are for fire
// the airframe cannot answer; the trigger hierarchy is missile-first because
// an inbound round outranks any amount of suppression.
type AirReflexSystem struct {
	airFilter     *ecs.Filter4[components.Aircraft, components.WorldPos, components.Motion, components.Threat]
	missileFilter *ecs.Filter2[components.Missile, components.WorldPos]
	overrideMap   *ecs.Map[components.AircraftOverride]
	hpMap         *ecs.Map[components.HP]
	queueMap      *ecs.Map[components.ActionQueue]

	inbound []inboundMissile
}

type inboundMissile struct {
	target ecs.Entity
	x, z   float32
}

const (
	airReflexThreshold float32 = 0.4
	airReflexCooldown  float32 = 10.0
	airBreakDuration   float32 = 3.0
	airFlareDuration   float32 = 3.5
	airMissileWarnM    float32 = 420.0
	airAbortHPFrac     float32 = 0.4
)

func NewAirReflexSystem() *AirReflexSystem { return &AirReflexSystem{} }

func (sys *AirReflexSystem) InitUI(w *ecs.World) {
	sys.airFilter = ecs.NewFilter4[components.Aircraft, components.WorldPos, components.Motion, components.Threat](w)
	sys.missileFilter = ecs.NewFilter2[components.Missile, components.WorldPos](w)
	sys.overrideMap = ecs.NewMap[components.AircraftOverride](w)
	sys.hpMap = ecs.NewMap[components.HP](w)
	sys.queueMap = ecs.NewMap[components.ActionQueue](w)
}

func (AirReflexSystem) Name() string { return "air_reflex" }

func (AirReflexSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *AirReflexSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	now := float32(ctx.SimNow)

	sys.inbound = sys.inbound[:0]
	qm := sys.missileFilter.Query()
	for qm.Next() {
		m, pos := qm.Get()
		if m.Decoyed || m.Target == (ecs.Entity{}) {
			continue
		}
		sys.inbound = append(sys.inbound, inboundMissile{
			target: m.Target,
			x:      float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			z:      float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
		})
	}

	q := sys.airFilter.Query()
	for q.Next() {
		ac, pos, _, threat := q.Get()
		ent := q.Entity()
		ov := sys.overrideMap.Get(ent)
		if ov == nil {
			continue
		}
		if ov.Kind != components.AirReflexNone && now >= ov.Until {
			ov.Kind = components.AirReflexNone
		}

		// Abort is a decision about the task, not a maneuver: it fires once,
		// regardless of cooldown, and SendHome owns everything after.
		if !ac.Egressing {
			if hp := sys.hpMap.Get(ent); hp != nil && hp.Max > 0 &&
				hp.Current < hp.Max*airAbortHPFrac {
				SendHome(ac, sys.queueMap.Get(ent))
				ov.Kind = components.AirReflexAbort
				ov.Until = now + airBreakDuration
				ov.LastAt = now
				continue
			}
		}
		if ov.Kind != components.AirReflexNone {
			continue
		}

		px := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		pz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z

		// Missile inbound → flares + break. Outranks suppression and ignores
		// the cooldown: a second launch is a second emergency.
		armed := false
		for i := range sys.inbound {
			in := &sys.inbound[i]
			if in.target != ent {
				continue
			}
			dx, dz := in.x-px, in.z-pz
			if dx*dx+dz*dz > airMissileWarnM*airMissileWarnM {
				continue
			}
			ov.Kind = components.AirReflexFlare
			ov.Until = now + airFlareDuration
			ov.ThreatYaw = float32(math.Atan2(float64(dx), float64(dz)))
			ov.LastAt = now
			armed = true
			break
		}
		if armed {
			continue
		}

		if threat.Suppression >= airReflexThreshold &&
			(ov.LastAt == 0 || now-ov.LastAt >= airReflexCooldown) {
			tx := -threat.ThreatDir.X
			tz := -threat.ThreatDir.Z
			if tx*tx+tz*tz < 1e-4 {
				continue
			}
			ov.Kind = components.AirReflexBreak
			ov.Until = now + airBreakDuration
			ov.ThreatYaw = float32(math.Atan2(float64(tx), float64(tz)))
			ov.LastAt = now
		}
	}
}
