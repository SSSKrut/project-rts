package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ThreatDecaySystem despawns ThreatSource entities whose age exceeds their
// TTL. Tiny serial pass; the spawn writer is WeaponSystem.serialApply
// (M14.5), the reader will be Phase 15 SurvivalInstinctSystem.
//
// Kept as its own System (rather than inlined into WeaponSystem) so the
// Phase 15 reader gets a clean contract: "ThreatSource entities are live
// for up to threatTTL seconds after their spawn - query the cluster, weight
// by Severity, ignore the rest." The writer / reader / cleaner split
// matches the same pattern Phase 14 follows for the Suppression field
// (writer = WeaponSystem.propagateSuppression; reader = Phase 15;
// cleaner = WeaponSystem.decaySuppression inline because Suppression has
// no archetype change).
type ThreatDecaySystem struct {
	filter    *ecs.Filter1[components.ThreatSource]
	despawned []ecs.Entity
	elapsed   float32
}

func NewThreatDecaySystem() *ThreatDecaySystem {
	return &ThreatDecaySystem{
		despawned: make([]ecs.Entity, 0, 32),
	}
}

func (sys *ThreatDecaySystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter1[components.ThreatSource](w)
}

func (ThreatDecaySystem) Name() string { return "threat_decay" }

// Every tick - the work is O(live-threats) which peaks at ~120 entries
// during a heavy firefight (Phase 14 estimate). Tiny budget.
func (ThreatDecaySystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ThreatDecaySystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed
	sys.despawned = sys.despawned[:0]

	q := sys.filter.Query()
	for q.Next() {
		t := q.Get()
		if now-t.SpawnTime > t.TTL {
			sys.despawned = append(sys.despawned, q.Entity())
		}
	}
	// Apply removals after closing the query - Ark forbids archetype
	// mutation inside a live filter walk.
	for _, e := range sys.despawned {
		if ctx.World.Alive(e) {
			ctx.World.RemoveEntity(e)
		}
	}
}
