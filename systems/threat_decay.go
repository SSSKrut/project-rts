package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ThreatDecaySystem despawns short-lived field entities past their lifetime:
// ThreatSource (age > TTL), SmokeField, BlastMark and UnsafeArea (now >=
// ExpiresAt). Kept separate so readers get the contract "live within its
// window".
type ThreatDecaySystem struct {
	filter    *ecs.Filter1[components.ThreatSource]
	smoke     *ecs.Filter2[components.SmokeField, components.WorldPos]
	blast     *ecs.Filter1[components.BlastMark]
	unsafe    *ecs.Filter1[components.UnsafeArea]
	atmRes    ecs.Resource[components.Atmosphere]
	despawned []ecs.Entity
}

func NewThreatDecaySystem() *ThreatDecaySystem {
	return &ThreatDecaySystem{
		despawned: make([]ecs.Entity, 0, 32),
	}
}

func (sys *ThreatDecaySystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter1[components.ThreatSource](w)
	sys.smoke = ecs.NewFilter2[components.SmokeField, components.WorldPos](w)
	sys.blast = ecs.NewFilter1[components.BlastMark](w)
	sys.unsafe = ecs.NewFilter1[components.UnsafeArea](w)
	sys.atmRes = ecs.NewResource[components.Atmosphere](w)
}

func (ThreatDecaySystem) Name() string { return "threat_decay" }

// Every tick — O(live-threats) peaks at ~120 entries during heavy firefight.
func (ThreatDecaySystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ThreatDecaySystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)
	sys.despawned = sys.despawned[:0]

	q := sys.filter.Query()
	for q.Next() {
		t := q.Get()
		if now-t.SpawnTime > t.TTL {
			sys.despawned = append(sys.despawned, q.Entity())
		}
	}
	q.Close()

	// Smoke drifts downwind at half the local wind — a screening volume that
	// slides off its target is the tactical cost of smoking in a gale.
	atm := sys.atmRes.Get()
	dt := float32(ctx.Delta.Seconds())
	qs := sys.smoke.Query()
	for qs.Next() {
		sf, pos := qs.Get()
		if float64(now) >= sf.ExpiresAt {
			sys.despawned = append(sys.despawned, qs.Entity())
			continue
		}
		if atm != nil && dt > 0 {
			wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
			wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
			wxv, wzv := WindAt(atm, wx, wz, ctx.SimNow)
			*pos = pos.Add(rl.Vector3{X: wxv * 0.5 * dt, Z: wzv * 0.5 * dt})
		}
	}
	qs.Close()

	qb := sys.blast.Query()
	for qb.Next() {
		if float64(now) >= qb.Get().ExpiresAt {
			sys.despawned = append(sys.despawned, qb.Entity())
		}
	}
	qb.Close()

	qu := sys.unsafe.Query()
	for qu.Next() {
		if float64(now) >= qu.Get().ExpiresAt {
			sys.despawned = append(sys.despawned, qu.Entity())
		}
	}

	// Remove after closing the query (Ark forbids mid-query archetype mutation).
	for _, e := range sys.despawned {
		if ctx.World.Alive(e) {
			ctx.World.RemoveEntity(e)
		}
	}
}
