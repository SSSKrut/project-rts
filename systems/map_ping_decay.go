package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MapPingDecaySystem despawns MapPing entities once their TTL has expired.
// Tiny serial pass; same shape as ThreatDecaySystem.
type MapPingDecaySystem struct {
	filter    *ecs.Filter1[components.MapPing]
	despawned []ecs.Entity
	elapsed   float32
}

func NewMapPingDecaySystem() *MapPingDecaySystem {
	return &MapPingDecaySystem{despawned: make([]ecs.Entity, 0, 16)}
}

func (sys *MapPingDecaySystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter1[components.MapPing](w)
}

func (MapPingDecaySystem) Name() string { return "map_ping_decay" }

func (MapPingDecaySystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MapPingDecaySystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed
	sys.despawned = sys.despawned[:0]
	q := sys.filter.Query()
	for q.Next() {
		p := q.Get()
		if now-p.SpawnAt > p.TTL {
			sys.despawned = append(sys.despawned, q.Entity())
		}
	}
	for _, e := range sys.despawned {
		if ctx.World.Alive(e) {
			ctx.World.RemoveEntity(e)
		}
	}
}

// MapPingService spawns MapPing entities. Used by DamageService (KIA),
// VisionSystem hooks (EnemyContact - Phase 16), etc. Lives as a small handle
// object so callers don't take a direct ecs.World dependency.
type MapPingService struct {
	world   *ecs.World
	posMap  *ecs.Map[components.WorldPos]
	pingMap *ecs.Map[components.MapPing]
	clock   func() float32
}

func NewMapPingService(w *ecs.World, clock func() float32) *MapPingService {
	return &MapPingService{
		world:   w,
		posMap:  ecs.NewMap[components.WorldPos](w),
		pingMap: ecs.NewMap[components.MapPing](w),
		clock:   clock,
	}
}

// Spawn places one ping at `at`. Caller picks kind / colour / TTL; helpers
// SpawnKIA / SpawnContact wrap the common defaults.
func (s *MapPingService) Spawn(at components.WorldPos, ping components.MapPing) {
	if s == nil {
		return
	}
	if ping.SpawnAt == 0 && s.clock != nil {
		ping.SpawnAt = s.clock()
	}
	ent := s.world.NewEntity()
	pos := at
	s.posMap.Add(ent, &pos)
	s.pingMap.Add(ent, &ping)
}
