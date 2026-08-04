package systems

import (
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
	smoke     *ecs.Filter1[components.SmokeField]
	blast     *ecs.Filter1[components.BlastMark]
	unsafe    *ecs.Filter1[components.UnsafeArea]
	despawned []ecs.Entity
}

func NewThreatDecaySystem() *ThreatDecaySystem {
	return &ThreatDecaySystem{
		despawned: make([]ecs.Entity, 0, 32),
	}
}

func (sys *ThreatDecaySystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter1[components.ThreatSource](w)
	sys.smoke = ecs.NewFilter1[components.SmokeField](w)
	sys.blast = ecs.NewFilter1[components.BlastMark](w)
	sys.unsafe = ecs.NewFilter1[components.UnsafeArea](w)
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

	qs := sys.smoke.Query()
	for qs.Next() {
		if float64(now) >= qs.Get().ExpiresAt {
			sys.despawned = append(sys.despawned, qs.Entity())
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
